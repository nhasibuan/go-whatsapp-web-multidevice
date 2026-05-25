package usecase

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	infraTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/translation"
	"github.com/sirupsen/logrus"
)

// Retrieval pool sizes — tuned to the brainstorm: top 8 from the chat over
// a 200-message window, top 4 from the user-style pool over 500 candidates.
const (
	ragChatPoolSize     = 200
	ragChatTopK         = 8
	ragStylePoolSize    = 500
	ragStyleTopK        = 4
	ragBackfillBurst    = 100 // messages to embed per background burst
	ragBackfillDebounce = 60  // seconds between backfill kicks for the same chat
)

// retrievalResult is what buildProviderInput surfaces back to the caller.
// Keeping context and style separate lets the provider render them in
// distinct prompt sections.
type retrievalResult struct {
	Context       []domainTranslation.ContextMessage
	StyleExamples []domainTranslation.ContextMessage
	Used          bool // true when at least one section was populated from RAG
}

// retrieveSimilarMessages embeds the query, scores cosine similarity over
// (a) recent embeddings in the same chat and (b) recent outbound embeddings
// across all chats for the device, and returns the top-K of each as
// ContextMessage slices. Any failure (no embedder, embed call fails,
// retrieval errors) returns Used=false so the caller falls back to the
// Phase 1 system context.
func (s *serviceTranslation) retrieveSimilarMessages(ctx context.Context, deviceID, chatJID, sourceText, excludeMessageID string) retrievalResult {
	if s.embedder == nil || strings.TrimSpace(sourceText) == "" {
		return retrievalResult{}
	}

	embedCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TranslationRequestTimeoutSec)*time.Second)
	defer cancel()

	vectors, err := s.embedder.Embed(embedCtx, []string{sourceText})
	if err != nil || len(vectors) != 1 || len(vectors[0]) == 0 {
		if err != nil {
			logrus.WithError(err).Warn("translation/rag: embed failed; falling back to system context")
		}
		return retrievalResult{}
	}
	queryVec := vectors[0]
	model := s.embedder.Model()

	var (
		chatCandidates  []*domainTranslation.EmbeddingCandidate
		styleCandidates []*domainTranslation.EmbeddingCandidate
	)

	if chatJID != "" {
		chatCandidates, err = s.translationRepo.ListChatEmbeddingCandidates(deviceID, chatJID, model, ragChatPoolSize)
		if err != nil {
			logrus.WithError(err).Warn("translation/rag: chat candidates lookup failed")
			chatCandidates = nil
		}
	}

	styleCandidates, err = s.translationRepo.ListUserStyleEmbeddingCandidates(deviceID, model, ragStylePoolSize)
	if err != nil {
		logrus.WithError(err).Warn("translation/rag: user-style candidates lookup failed")
		styleCandidates = nil
	}

	chatTop := topKByCosine(queryVec, chatCandidates, ragChatTopK, excludeMessageID)
	styleTop := topKByCosine(queryVec, styleCandidates, ragStyleTopK, excludeMessageID)

	out := retrievalResult{
		Context:       candidatesToContext(chatTop),
		StyleExamples: candidatesToContext(styleTop),
	}
	out.Used = len(out.Context) > 0 || len(out.StyleExamples) > 0
	return out
}

// topKByCosine scores each candidate's vector against the query, drops
// any with empty content, deduplicates by message ID, and returns the K
// highest-scoring candidates in descending similarity order.
func topKByCosine(query []float32, candidates []*domainTranslation.EmbeddingCandidate, k int, excludeMessageID string) []*domainTranslation.EmbeddingCandidate {
	if k <= 0 || len(candidates) == 0 {
		return nil
	}
	type scored struct {
		c *domainTranslation.EmbeddingCandidate
		s float32
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]scored, 0, len(candidates))
	for _, cand := range candidates {
		if cand == nil || strings.TrimSpace(cand.Content) == "" || len(cand.Vector) == 0 {
			continue
		}
		if cand.MessageID == excludeMessageID {
			continue
		}
		if _, dup := seen[cand.MessageID]; dup {
			continue
		}
		seen[cand.MessageID] = struct{}{}
		out = append(out, scored{c: cand, s: cosineSimilarity(query, cand.Vector)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].s > out[j].s })
	if len(out) > k {
		out = out[:k]
	}
	result := make([]*domainTranslation.EmbeddingCandidate, len(out))
	for i, item := range out {
		result[i] = item.c
	}
	return result
}

func candidatesToContext(items []*domainTranslation.EmbeddingCandidate) []domainTranslation.ContextMessage {
	if len(items) == 0 {
		return nil
	}
	out := make([]domainTranslation.ContextMessage, 0, len(items))
	for _, c := range items {
		out = append(out, domainTranslation.ContextMessage{
			Sender:    c.Sender,
			Content:   c.Content,
			IsFromMe:  c.IsFromMe,
			Timestamp: c.Timestamp,
		})
	}
	return out
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// ------------- Lazy backfill -------------

// backfillTracker debounces backfill kicks per (device, chat) pair so a hot
// chat doesn't trigger overlapping bursts. It also serves as a cheap
// in-flight gate — only one worker per chat at a time.
type backfillTracker struct {
	mu       sync.Mutex
	last     map[string]int64
	inflight map[string]struct{}
}

func newBackfillTracker() *backfillTracker {
	return &backfillTracker{
		last:     make(map[string]int64),
		inflight: make(map[string]struct{}),
	}
}

func (t *backfillTracker) shouldKick(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, busy := t.inflight[key]; busy {
		return false
	}
	now := time.Now().Unix()
	if last, ok := t.last[key]; ok && now-last < ragBackfillDebounce {
		return false
	}
	t.inflight[key] = struct{}{}
	t.last[key] = now
	return true
}

func (t *backfillTracker) finish(key string) {
	t.mu.Lock()
	delete(t.inflight, key)
	t.mu.Unlock()
}

// kickBackfill enqueues a single backfill burst for the chat. The first call
// for a fresh chat falls back to system context (RAG can't help without
// vectors) and this call ensures subsequent calls hit the retrieval path.
//
// The work runs on a detached goroutine with its own context so it survives
// after the originating request returns. Errors are logged at warn level —
// backfill is best-effort, the user-visible path never depends on it.
func (s *serviceTranslation) kickBackfill(deviceID, chatJID string) {
	if s.embedder == nil || s.translationRepo == nil || strings.TrimSpace(chatJID) == "" {
		return
	}
	if !config.TranslationRAGEnabled {
		return
	}
	key := deviceID + "|" + chatJID
	if !s.backfill.shouldKick(key) {
		return
	}

	go func() {
		defer s.backfill.finish(key)

		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		items, err := s.translationRepo.ListMessagesNeedingEmbedding(deviceID, chatJID, s.embedder.Model(), ragBackfillBurst)
		if err != nil {
			logrus.WithError(err).WithField("chat_jid", chatJID).Warn("translation/rag: backfill list failed")
			return
		}
		if len(items) == 0 {
			return
		}

		texts := make([]string, len(items))
		for i, it := range items {
			texts[i] = it.Content
		}

		vectors, err := s.embedder.Embed(bgCtx, texts)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"chat_jid": chatJID,
				"batch":    len(items),
			}).Warn("translation/rag: backfill embed failed")
			return
		}
		if len(vectors) != len(items) {
			logrus.WithFields(logrus.Fields{
				"chat_jid": chatJID,
				"want":     len(items),
				"got":      len(vectors),
			}).Warn("translation/rag: backfill embed size mismatch")
			return
		}

		stored := 0
		for i, it := range items {
			if len(vectors[i]) == 0 {
				continue
			}
			err := s.translationRepo.StoreEmbedding(&domainTranslation.MessageEmbedding{
				DeviceID:  deviceID,
				ChatJID:   it.ChatJID,
				MessageID: it.MessageID,
				Model:     s.embedder.Model(),
				Vector:    infraTranslation.FloatsToBytes(vectors[i]),
				CreatedAt: time.Now().Unix(),
			})
			if err != nil {
				logrus.WithError(err).WithField("message_id", it.MessageID).Warn("translation/rag: store embedding failed")
				continue
			}
			stored++
		}
		logrus.WithFields(logrus.Fields{
			"chat_jid": chatJID,
			"stored":   stored,
			"batch":    len(items),
		}).Info("translation/rag: backfill burst complete")
	}()
}
