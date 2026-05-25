export default {
  name: "ChatMessages",
  data() {
    return {
      jid: "",
      messages: [],
      loading: false,
      searchQuery: "",
      startTime: "",
      endTime: "",
      isFromMe: "",
      onlyMedia: false,
      currentPage: 1,
      pageSize: 20,
      totalMessages: 0,
      // Media download tracking
      downloadedMedia: {}, // messageId -> { file_path, media_type, file_size, status }
      downloadingMedia: new Set(), // Set of messageIds currently downloading
      mediaDownloadErrors: {}, // messageId -> error message
      maxConcurrentDownloads: 3,
      currentDownloads: 0,
      // Translation state: messageId -> { expanded, loading, error, suggestions, targetLang, sourceLang, provider, cacheHit }
      translations: {},
      // Default target language for newly opened translations. Persisted in
      // localStorage so the user's pick sticks across reloads.
      defaultTargetLang: localStorage.getItem('translationTargetLang') || 'en',
      // Per-chat translation preferences fetched from the server. Shape mirrors
      // the ChatPrefResponse DTO. Null until first fetch for the current JID.
      chatPrefs: null,
      // Working copy used by the prefs panel form. Decoupled from chatPrefs so
      // unsaved edits don't trigger downstream effects (e.g. auto-translate).
      chatPrefsDraft: { target_lang: '', auto_translate_inbound: false, auto_translate_outbound: false },
      chatPrefsOpen: false,
      chatPrefsSaving: false,
      // Inline auto-translation cache used when auto_translate_inbound is on.
      // Keyed by message id so we can re-render without refetching. Only the
      // 'natural' suggestion is shown inline; all 3 stay reachable via the
      // globe icon.
      autoTranslations: {},
      autoTranslateBatchInFlight: false,
    };
  },
  computed: {
    totalPages() {
      return Math.ceil(this.totalMessages / this.pageSize);
    },
    formattedJid() {
      return (
        this.jid.trim() + (this.jid.includes("@") ? "" : "@s.whatsapp.net")
      );
    },
  },
  methods: {
    isValidForm() {
      return this.jid.trim().length > 0;
    },
    openModal() {
      // Check if there's a pre-selected JID from chat list
      const selectedJid = localStorage.getItem("selectedChatJid");
      if (selectedJid) {
        this.jid = selectedJid;
        localStorage.removeItem("selectedChatJid"); // Clean up

        this.loadMessages();
      }

      $("#modalChatMessages")
        .modal({
          onShow: function () {
            // Initialize accordion after modal is shown
            setTimeout(() => {
              $("#modalChatMessages .ui.accordion").accordion();
            }, 100);
          },
        })
        .modal("show");
    },
    async loadMessages() {
      if (!this.isValidForm()) {
        showErrorInfo("Please enter a valid JID");
        return;
      }

      this.loading = true;
      try {
        const params = new URLSearchParams({
          offset: (this.currentPage - 1) * this.pageSize,
          limit: this.pageSize,
        });

        if (this.searchQuery.trim()) {
          params.append("search", this.searchQuery);
        }

        if (this.startTime) {
          params.append("start_time", this.startTime);
        }

        if (this.endTime) {
          params.append("end_time", this.endTime);
        }

        if (this.isFromMe !== "") {
          params.append("is_from_me", this.isFromMe);
        }

        if (this.onlyMedia) {
          params.append("media_only", "true");
        }

        const response = await window.http.get(
          `/chat/${this.formattedJid}/messages?${params}`
        );
        this.messages = response.data.results?.data || [];
        this.totalMessages = response.data.results?.pagination?.total || 0;

        if (this.messages.length === 0) {
          showErrorInfo("No messages found for the specified criteria");
        } else {
          // Auto-download media for loaded messages
          this.downloadAllMediaInMessages();
          // Fetch per-chat translation prefs once per chat. The result drives
          // both the settings panel and (when on) the auto-translate-inbound
          // pre-fetch below. Errors are non-fatal — they just mean the panel
          // shows blank and auto-translate stays off.
          this.fetchChatPrefs().then(() => {
            if (this.chatPrefs && this.chatPrefs.auto_translate_inbound) {
              this.runAutoTranslateForVisibleMessages();
            }
          });
        }
      } catch (error) {
        showErrorInfo(
          error.response?.data?.message || "Failed to load messages"
        );
      } finally {
        this.loading = false;
      }
    },
    searchMessages() {
      this.currentPage = 1;
      this.loadMessages();
    },
    nextPage() {
      if (this.currentPage < this.totalPages) {
        this.currentPage++;
        this.loadMessages();
      }
    },
    prevPage() {
      if (this.currentPage > 1) {
        this.currentPage--;
        this.loadMessages();
      }
    },
    handleReset() {
      this.jid = "";
      this.messages = [];
      this.searchQuery = "";
      this.startTime = "";
      this.endTime = "";
      this.isFromMe = "";
      this.onlyMedia = false;
      this.currentPage = 1;
      this.totalMessages = 0;
      // Clear media download state
      this.downloadedMedia = {};
      this.downloadingMedia.clear();
      this.mediaDownloadErrors = {};
      this.currentDownloads = 0;
      // Translation state
      this.translations = {};
      this.chatPrefs = null;
      this.chatPrefsOpen = false;
      this.autoTranslations = {};
      this.autoTranslateBatchInFlight = false;
    },
    formatTimestamp(timestamp) {
      if (!timestamp) return "N/A";
      return moment(timestamp).format("MMM DD, YYYY HH:mm:ss");
    },
    formatMessageType(message) {
      if (message.media_type) return message.media_type.toUpperCase();
      if (message.message_type) return message.message_type.toUpperCase();
      return "TEXT";
    },
    formatSender(message) {
      if (message.is_from_me) return "Me";
      return message.push_name || message.sender_jid || "Unknown";
    },
    getMessageContent(message) {
      if (message.content) return message.content;
      if (message.text) return message.text;
      if (message.caption) return message.caption;
      if (message.media_type) return `[${message.media_type.toUpperCase()}]`;
      return "[No content]";
    },
    getMediaDisplay(message) {
      if (!message.media_type || !message.url || !message.id) {
        return null;
      }

      const messageId = message.id;
      const downloadedInfo = this.downloadedMedia[messageId];
      const isDownloaded = this.isMediaDownloaded(messageId);
      const isDownloading = this.isMediaDownloading(messageId);
      const hasError = this.hasMediaDownloadError(messageId);

      // Show loading state
      if (isDownloading) {
        return {
          type: 'loading',
          content: `<div class="ui active mini inline loader"></div> Downloading ${message.media_type}...`
        };
      }

      // Show error state with retry option
      if (hasError) {
        return {
          type: 'error',
          content: `<div class="ui red message">
            <i class="exclamation triangle icon"></i>
            Failed to download ${message.media_type}
            <span class="ui mini button" style="cursor: pointer; margin-left: 10px;" 
                  onclick="document.dispatchEvent(new CustomEvent('retryMediaDownload', {detail: '${messageId}'}))">
              <i class="redo icon"></i> Retry
            </span>
          </div>`
        };
      }

      // Show downloaded media
      if (isDownloaded && downloadedInfo) {
        const filePath = downloadedInfo.file_path;
        const mediaType = downloadedInfo.media_type;
        const filename = downloadedInfo.filename;
        const fileSize = downloadedInfo.file_size;

        switch (mediaType.toLowerCase()) {
          case 'image':
            return {
              type: 'image',
              content: `<div class="ui fluid image">
                <img src="${filePath}" alt="${filename}" style="max-width: 300px; max-height: 300px; border-radius: 4px;" 
                     onerror="this.style.display='none'; this.nextElementSibling.style.display='block';">
                <div style="display: none;" class="ui placeholder segment">
                  <div class="ui icon header">
                    <i class="image outline icon"></i>
                    Image not available
                  </div>
                </div>
              </div>`
            };

          case 'video':
            return {
              type: 'video',
              content: `<div class="ui fluid">
                <video controls style="max-width: 300px; max-height: 300px; border-radius: 4px;" preload="metadata">
                  <source src="${filePath}" type="video/mp4">
                  <source src="${filePath}" type="video/webm">
                  <source src="${filePath}" type="video/ogg">
                  Your browser does not support the video tag.
                </video>
              </div>`
            };

          case 'audio':
            return {
              type: 'audio',
              content: `<div class="ui fluid">
                <audio controls style="width: 100%; max-width: 300px;">
                  <source src="${filePath}" type="audio/mpeg">
                  <source src="${filePath}" type="audio/ogg">
                  <source src="${filePath}" type="audio/wav">
                  Your browser does not support the audio tag.
                </audio>
              </div>`
            };

          case 'document':
            const sizeText = fileSize ? `(${Math.round(fileSize / 1024)} KB)` : '';
            return {
              type: 'document',
              content: `<div class="ui labeled button">
                <a href="${filePath}" download="${filename}" class="ui button">
                  <i class="download icon"></i>
                  ${filename} ${sizeText}
                </a>
                <div class="ui basic left pointing label">
                  Document
                </div>
              </div>`
            };

          case 'sticker':
            return {
              type: 'sticker',
              content: `<div class="ui">
                <img src="${filePath}" alt="Sticker" style="max-width: 150px; max-height: 150px; border-radius: 4px;" 
                     onerror="this.style.display='none'; this.nextElementSibling.style.display='block';">
                <div style="display: none;" class="ui placeholder segment">
                  <div class="ui icon header">
                    <i class="smile outline icon"></i>
                    Sticker not available
                  </div>
                </div>
              </div>`
            };

          default:
            return {
              type: 'unknown',
              content: `<div class="ui message">
                <i class="file icon"></i>
                Unknown media type: ${mediaType}
              </div>`
            };
        }
      }

      // Default: show media available label
      return {
        type: 'available',
        content: `<div class="ui tiny blue label">
          <i class="linkify icon"></i>
          ${message.media_type.toUpperCase()} Available
        </div>`
      };
    },
    getMessageStyle(message) {
      const baseStyle = {
        padding: "1em",
        margin: "0.5em 0",
      };

      if (message.is_from_me) {
        return {
          ...baseStyle,
          borderLeft: "4px solid #2185d0",
          backgroundColor: "#f8f9fa",
        };
      } else {
        return {
          ...baseStyle,
          borderLeft: "4px solid #767676",
        };
      }
    },
    // Media download methods
    isMediaDownloaded(messageId) {
      return this.downloadedMedia[messageId] && this.downloadedMedia[messageId].status === 'completed';
    },
    isMediaDownloading(messageId) {
      return this.downloadingMedia.has(messageId);
    },
    hasMediaDownloadError(messageId) {
      return !!this.mediaDownloadErrors[messageId];
    },
    async downloadMediaForMessage(message) {
      if (!message.media_type || !message.url || !message.id) {
        return;
      }

      const messageId = message.id;
      
      // Skip if already downloaded or downloading
      if (this.isMediaDownloaded(messageId) || this.isMediaDownloading(messageId)) {
        return;
      }

      // Check concurrent download limit
      if (this.currentDownloads >= this.maxConcurrentDownloads) {
        return;
      }

      try {
        this.downloadingMedia.add(messageId);
        this.currentDownloads++;
        
        // Clear any previous error
        if (this.mediaDownloadErrors[messageId]) {
          delete this.mediaDownloadErrors[messageId];
        }

        const response = await window.http.get(
          `/message/${messageId}/download?phone=${this.formattedJid}`
        );

        if (response.data && response.data.results) {
          this.downloadedMedia[messageId] = {
            file_path: response.data.results.file_path,
            media_type: response.data.results.media_type,
            file_size: response.data.results.file_size,
            filename: response.data.results.filename,
            status: 'completed'
          };
        }
      } catch (error) {
        console.error(`Failed to download media for message ${messageId}:`, error);
        this.mediaDownloadErrors[messageId] = error.response?.data?.message || 'Download failed';
      } finally {
        this.downloadingMedia.delete(messageId);
        this.currentDownloads--;
      }
    },
    async retryMediaDownload(messageId) {
      const message = this.messages.find(m => m.id === messageId);
      if (message) {
        // Clear the error first
        delete this.mediaDownloadErrors[messageId];
        await this.downloadMediaForMessage(message);
      }
    },
    async downloadAllMediaInMessages() {
      const mediaMessages = this.messages.filter(message =>
        message.media_type && message.url && message.id &&
        !this.isMediaDownloaded(message.id) && !this.isMediaDownloading(message.id)
      );

      if (mediaMessages.length === 0) {
        return;
      }

      // Download in batches to respect concurrency limit
      const downloadQueue = [...mediaMessages];

      const processQueue = async () => {
        while (downloadQueue.length > 0 && this.currentDownloads < this.maxConcurrentDownloads) {
          const message = downloadQueue.shift();
          if (message) {
            await this.downloadMediaForMessage(message);
            // Small delay to prevent overwhelming the server
            await new Promise(resolve => setTimeout(resolve, 100));
          }
        }

        // If there are still items in queue and we can download more, continue
        if (downloadQueue.length > 0 && this.currentDownloads < this.maxConcurrentDownloads) {
          setTimeout(processQueue, 500); // Wait a bit before checking again
        }
      };

      // Start processing
      processQueue();
    },
    backToChatList() {
      // Close current modal
      $('#modalChatMessages').modal('hide');

      // Open Chat List modal after a short delay
      setTimeout(() => {
        if (window.ChatListComponent && window.ChatListComponent.openModal) {
          window.ChatListComponent.openModal();
        } else {
          // Fallback: try to find and click the Chat List card
          const chatListCards = document.querySelectorAll('.card .header');
          for (let card of chatListCards) {
            if (card.textContent.includes('Chat List')) {
              card.click();
              break;
            }
          }
        }
      }, 200);
    },
    // ----- Translation -----
    isTranslatable(message) {
      // Only text-bearing messages are worth translating. Skip pure-media
      // messages and empty content.
      if (!message || !message.id) return false;
      const text = (message.content || message.text || message.caption || '').trim();
      return text.length > 0;
    },
    getTranslationState(messageId) {
      return this.translations[messageId] || null;
    },
    async toggleTranslate(message) {
      const id = message.id;
      const existing = this.translations[id];
      if (existing && existing.expanded) {
        // Collapse the panel without discarding the result so re-opening is instant.
        this.translations[id] = { ...existing, expanded: false };
        return;
      }
      if (existing && existing.suggestions && existing.suggestions.length > 0) {
        this.translations[id] = { ...existing, expanded: true };
        return;
      }
      await this.fetchTranslation(message, this.defaultTargetLang);
    },
    async fetchTranslation(message, targetLang) {
      const id = message.id;
      const lang = (targetLang || this.defaultTargetLang || 'en').trim();
      this.translations[id] = {
        ...(this.translations[id] || {}),
        expanded: true,
        loading: true,
        error: '',
        targetLang: lang,
        suggestions: [],
      };
      try {
        const payload = {
          chat_jid: this.formattedJid,
          target_lang: lang,
        };
        const response = await window.http.post(
          `/message/${encodeURIComponent(id)}/translate`,
          payload
        );
        const r = response.data?.results || {};
        this.translations[id] = {
          expanded: true,
          loading: false,
          error: '',
          suggestions: Array.isArray(r.suggestions) ? r.suggestions : [],
          targetLang: r.target_lang || lang,
          sourceLang: r.source_lang || '',
          provider: r.provider || '',
          cacheHit: !!r.cache_hit,
        };
      } catch (err) {
        const msg = err.response?.data?.message || err.message || 'Translation failed';
        this.translations[id] = {
          ...(this.translations[id] || {}),
          expanded: true,
          loading: false,
          error: msg,
          suggestions: [],
        };
      }
    },
    async retranslate(message, targetLang) {
      // Force a refresh in a different language; clears any cached UI state for this message.
      this.translations[message.id] = { expanded: true, loading: true, error: '', suggestions: [] };
      this.defaultTargetLang = targetLang;
      try { localStorage.setItem('translationTargetLang', targetLang); } catch (e) { /* ignore */ }
      await this.fetchTranslation(message, targetLang);
    },
    variantLabel(variant) {
      switch ((variant || '').toLowerCase()) {
        case 'literal': return 'Literal';
        case 'natural': return 'Natural';
        case 'tone_matched': return 'Tone-matched';
        default: return variant || 'Suggestion';
      }
    },
    variantColor(variant) {
      switch ((variant || '').toLowerCase()) {
        case 'literal': return 'grey';
        case 'natural': return 'blue';
        case 'tone_matched': return 'teal';
        default: return 'grey';
      }
    },
    copySuggestion(text) {
      if (!text) return;
      const fallback = () => {
        const ta = document.createElement('textarea');
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand('copy'); } catch (e) { /* ignore */ }
        document.body.removeChild(ta);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).catch(fallback);
      } else {
        fallback();
      }
      showSuccessInfo('Translation copied to clipboard');
    },
    // ----- Per-chat preferences -----
    async fetchChatPrefs() {
      // Reset before fetch so a slow request can't bleed into a different chat.
      this.chatPrefs = null;
      try {
        const response = await window.http.get(
          `/chat/${encodeURIComponent(this.formattedJid)}/translation-prefs`
        );
        const r = response.data?.results || {};
        this.chatPrefs = {
          chat_jid: r.chat_jid || this.formattedJid,
          target_lang: r.target_lang || '',
          effective_target_lang: r.effective_target_lang || this.defaultTargetLang,
          auto_translate_inbound: !!r.auto_translate_inbound,
          auto_translate_outbound: !!r.auto_translate_outbound,
        };
        // Sync the working draft used by the panel form.
        this.chatPrefsDraft = {
          target_lang: this.chatPrefs.target_lang,
          auto_translate_inbound: this.chatPrefs.auto_translate_inbound,
          auto_translate_outbound: this.chatPrefs.auto_translate_outbound,
        };
      } catch (err) {
        // Silently degrade — the panel will show blanks and auto-translate
        // stays off. Most likely cause is the translation feature being
        // disabled server-side, which is a valid configuration.
        console.warn('chat translation prefs fetch failed:', err?.response?.data?.message || err);
      }
    },
    toggleChatPrefsPanel() {
      this.chatPrefsOpen = !this.chatPrefsOpen;
    },
    async saveChatPrefs() {
      if (this.chatPrefsSaving) return;
      this.chatPrefsSaving = true;
      try {
        const payload = {
          target_lang: (this.chatPrefsDraft.target_lang || '').trim(),
          auto_translate_inbound: !!this.chatPrefsDraft.auto_translate_inbound,
          auto_translate_outbound: !!this.chatPrefsDraft.auto_translate_outbound,
        };
        const response = await window.http.put(
          `/chat/${encodeURIComponent(this.formattedJid)}/translation-prefs`,
          payload
        );
        const r = response.data?.results || {};
        const previous = this.chatPrefs;
        this.chatPrefs = {
          chat_jid: r.chat_jid || this.formattedJid,
          target_lang: r.target_lang || '',
          effective_target_lang: r.effective_target_lang || this.defaultTargetLang,
          auto_translate_inbound: !!r.auto_translate_inbound,
          auto_translate_outbound: !!r.auto_translate_outbound,
        };
        showSuccessInfo('Chat translation preferences saved');
        // If the user just turned on auto-translate-inbound (or changed the
        // target lang while it's already on), re-prefetch translations for
        // the visible page. The cache makes repeats free.
        const langChanged = previous && previous.effective_target_lang !== this.chatPrefs.effective_target_lang;
        if (this.chatPrefs.auto_translate_inbound && (langChanged || !previous?.auto_translate_inbound)) {
          if (langChanged) {
            // Different lang means existing cached UI translations are stale.
            this.autoTranslations = {};
          }
          this.runAutoTranslateForVisibleMessages();
        }
        if (!this.chatPrefs.auto_translate_inbound) {
          // Clear inline translations when turning the feature off.
          this.autoTranslations = {};
        }
      } catch (err) {
        showErrorInfo(err.response?.data?.message || 'Failed to save preferences');
      } finally {
        this.chatPrefsSaving = false;
      }
    },
    // ----- Auto-translate (inbound) -----
    async runAutoTranslateForVisibleMessages() {
      // Idempotent: bail out if a batch is already running for this page so
      // a quick re-render doesn't double-fire calls. Cache makes repeats
      // free anyway, but this keeps the network panel quiet in dev.
      if (this.autoTranslateBatchInFlight) return;
      if (!this.chatPrefs || !this.chatPrefs.auto_translate_inbound) return;
      const targets = this.messages.filter(m =>
        !m.is_from_me &&
        this.isTranslatable(m) &&
        !this.autoTranslations[m.id]
      );
      if (targets.length === 0) return;

      this.autoTranslateBatchInFlight = true;
      const concurrency = 3;
      const queue = [...targets];
      const workers = Array.from({ length: Math.min(concurrency, queue.length) }, async () => {
        while (queue.length > 0) {
          const m = queue.shift();
          if (!m) continue;
          await this.fetchAutoTranslation(m);
        }
      });
      try {
        await Promise.all(workers);
      } finally {
        this.autoTranslateBatchInFlight = false;
      }
    },
    async fetchAutoTranslation(message) {
      try {
        const lang = (this.chatPrefs?.effective_target_lang || this.defaultTargetLang || 'en').trim();
        const response = await window.http.post(
          `/message/${encodeURIComponent(message.id)}/translate`,
          { chat_jid: this.formattedJid, target_lang: lang }
        );
        const r = response.data?.results || {};
        // Prefer the 'natural' suggestion for the inline display since it's
        // the most readable; fall back to whatever came back first.
        const list = Array.isArray(r.suggestions) ? r.suggestions : [];
        const natural = list.find(s => (s.variant || '').toLowerCase() === 'natural') || list[0];
        if (natural && natural.text) {
          this.autoTranslations[message.id] = {
            text: natural.text,
            target_lang: r.target_lang || lang,
            cache_hit: !!r.cache_hit,
          };
        }
      } catch (err) {
        // Per-message failures are non-fatal — the user can still hit the
        // globe icon to see the full 3-card panel and any error there.
        console.warn('auto-translate failed for', message.id, err?.response?.data?.message || err);
      }
    },
    getAutoTranslation(messageId) {
      return this.autoTranslations[messageId] || null;
    },
  },
  mounted() {
    // Expose the openModal method globally for ChatList component to call
    window.ChatMessagesComponent = this;

    // Handle retry media download events
    this.handleRetryMediaDownload = (event) => {
      const messageId = event.detail;
      this.retryMediaDownload(messageId);
    };

    // Listen for retry media download events
    document.addEventListener('retryMediaDownload', this.handleRetryMediaDownload);
  },
  beforeUnmount() {
    // Clean up global reference
    if (window.ChatMessagesComponent === this) {
      delete window.ChatMessagesComponent;
    }

    // Clean up event listeners
    if (this.handleRetryMediaDownload) {
      document.removeEventListener('retryMediaDownload', this.handleRetryMediaDownload);
    }
  },
  template: `
    <div class="purple card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Chat</a>
            <div class="header">Chat Messages</div>
            <div class="description">
                View messages from specific chats with advanced filtering
            </div>
        </div>
    </div>
    
    <!--  Modal ChatMessages  -->
    <div class="ui large modal" id="modalChatMessages">
        <i class="close icon"></i>
        <div class="header">
            <i class="comment icon"></i>
            Chat Messages
        </div>
        <div class="content">
            <div class="ui form">
                <div class="field">
                    <label>Chat JID</label>
                    <input type="text" 
                           placeholder="Enter phone number or full JID (e.g. 1234567890 or group-id@g.us)" 
                           v-model="jid">
                </div>
                
                <div class="ui accordion">
                    <div class="title">
                        <i class="dropdown icon"></i>
                        Advanced Filters (Optional)
                    </div>
                    <div class="content">
                        <div class="fields">
                            <div class="eight wide field">
                                <label>Search Message Content</label>
                                <input type="text" 
                                       placeholder="Search in message text..." 
                                       v-model="searchQuery">
                            </div>
                            <div class="four wide field">
                                <label>Sender Filter</label>
                                <select class="ui dropdown" v-model="isFromMe">
                                    <option value="">All messages</option>
                                    <option value="true">My messages</option>
                                    <option value="false">Their messages</option>
                                </select>
                            </div>
                            <div class="four wide field">
                                <label>&nbsp;</label>
                                <div class="ui checkbox">
                                    <input type="checkbox" v-model="onlyMedia">
                                    <label>Media only</label>
                                </div>
                            </div>
                        </div>
                        
                        <div class="fields">
                            <div class="eight wide field">
                                <label>Start Date/Time</label>
                                <input type="datetime-local" v-model="startTime">
                            </div>
                            <div class="eight wide field">
                                <label>End Date/Time</label>
                                <input type="datetime-local" v-model="endTime">
                            </div>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="ui divider"></div>
            
            <div class="actions">
                <button class="ui primary button" 
                        :class="{'disabled': !isValidForm() || loading}"
                        @click="loadMessages">
                    <i class="search icon"></i>
                    {{ loading ? 'Loading...' : 'Load Messages' }}
                </button>
                <button class="ui button" @click="handleReset">
                    <i class="refresh icon"></i>
                    Reset
                </button>
                <button v-if="messages.length > 0"
                        class="ui basic button"
                        :class="{'teal': chatPrefsOpen}"
                        @click="toggleChatPrefsPanel">
                    <i class="cog icon"></i>
                    Translation settings
                </button>
            </div>

            <!-- Per-chat translation preferences panel -->
            <div v-if="chatPrefsOpen && messages.length > 0"
                 class="ui segment" style="margin-top: 0.5em; background: #fafafa;">
                <div class="ui small header">
                    <i class="globe icon"></i>
                    Translation preferences for {{ formattedJid }}
                    <div class="sub header" style="font-weight: normal;">
                        Effective target: <code>{{ chatPrefs?.effective_target_lang || defaultTargetLang }}</code>
                        <span v-if="!chatPrefs?.target_lang" style="color: #888;">
                            (using global default — set a per-chat override below)
                        </span>
                    </div>
                </div>
                <div class="ui form" style="margin-top: 0.5em;">
                    <div class="fields">
                        <div class="six wide field">
                            <label>Per-chat target language</label>
                            <select v-model="chatPrefsDraft.target_lang" class="ui dropdown">
                                <option value="">Use global default</option>
                                <option value="en">English</option>
                                <option value="id">Indonesian</option>
                                <option value="ja">Japanese</option>
                                <option value="zh">Chinese</option>
                                <option value="es">Spanish</option>
                                <option value="fr">French</option>
                                <option value="de">German</option>
                                <option value="pt">Portuguese</option>
                                <option value="ar">Arabic</option>
                                <option value="ko">Korean</option>
                                <option value="ru">Russian</option>
                                <option value="vi">Vietnamese</option>
                            </select>
                        </div>
                        <div class="five wide field">
                            <label>&nbsp;</label>
                            <div class="ui toggle checkbox">
                                <input type="checkbox" v-model="chatPrefsDraft.auto_translate_inbound">
                                <label>Auto-translate inbound messages</label>
                            </div>
                            <div style="color: #888; font-size: 0.85em; margin-top: 0.25em;">
                                Shows the natural translation under each non-self message on this page.
                            </div>
                        </div>
                        <div class="five wide field">
                            <label>&nbsp;</label>
                            <div class="ui toggle checkbox">
                                <input type="checkbox" v-model="chatPrefsDraft.auto_translate_outbound">
                                <label>Auto-translate outbound (reserved)</label>
                            </div>
                            <div style="color: #888; font-size: 0.85em; margin-top: 0.25em;">
                                Stored for future compose-assist integration.
                            </div>
                        </div>
                    </div>
                    <button class="ui small teal button"
                            :class="{'loading disabled': chatPrefsSaving}"
                            @click="saveChatPrefs">
                        <i class="save icon"></i> Save preferences
                    </button>
                </div>
            </div>
            
            <div v-if="loading" class="ui active centered inline loader"></div>
            
            <div v-else-if="messages.length === 0 && totalMessages === 0" class="ui placeholder segment">
                <div class="ui icon header">
                    <i class="comment outline icon"></i>
                    No messages loaded
                </div>
                <p>Enter a JID and click "Load Messages" to view chat history</p>
            </div>
            
            <div v-else-if="messages.length > 0">
                <div style="padding-top: 1em; padding-bottom: 1em;">
                    <div class="ui info message">
                        <div class="header">
                            Chat Messages for {{ formattedJid }}
                        </div>
                        <p>Showing {{ messages.length }} of {{ totalMessages }} messages</p>
                    </div>
                </div>
                
                <div class="ui divided items" style="max-height: 400px; overflow-y: auto; overflow-x: hidden; -webkit-overflow-scrolling: touch; scrollbar-width: thin;">
                    <div v-for="message in messages" :key="message.id" 
                         class="item" 
                         :style="getMessageStyle(message)">
                        <div class="content">
                            <div class="header">
                                <div class="ui horizontal label" 
                                     :class="message.is_from_me ? 'blue' : 'grey'">
                                    {{ formatSender(message) }}
                                </div>
                                <div class="ui right floated horizontal label">
                                    {{ formatMessageType(message) }}
                                </div>
                                <a v-if="isTranslatable(message)"
                                   class="ui right floated mini icon link"
                                   :class="getTranslationState(message.id) && getTranslationState(message.id).expanded ? 'teal' : 'grey'"
                                   :title="'Translate to ' + (getTranslationState(message.id)?.targetLang || defaultTargetLang)"
                                   style="margin-right: 0.5em; cursor: pointer;"
                                   @click.stop="toggleTranslate(message)">
                                    <i class="globe icon"></i>
                                </a>
                            </div>
                            <div class="meta">
                                <span>{{ formatTimestamp(message.timestamp) }}</span>
                                <span v-if="message.id" class="right floated">
                                    ID: {{ message.id }}
                                </span>
                            </div>
                            <div class="description">
                                <p>{{ getMessageContent(message) }}</p>
                                <!-- Inline auto-translation (when auto_translate_inbound is on). -->
                                <div v-if="getAutoTranslation(message.id)"
                                     style="margin-top: 0.25em; padding: 0.4em 0.6em; background: #f3f9ff; border-left: 3px solid #2185d0; border-radius: 3px; font-size: 0.92em;">
                                    <span style="color: #2185d0; font-weight: 600; font-size: 0.78em; text-transform: uppercase; letter-spacing: 0.05em;">
                                        <i class="globe icon" style="margin: 0 0.3em 0 0;"></i>{{ getAutoTranslation(message.id).target_lang }}
                                    </span>
                                    {{ getAutoTranslation(message.id).text }}
                                </div>
                                <div v-if="message.media_type && message.url" class="media-container" style="margin-top: 0.5em;">
                                    <div v-if="getMediaDisplay(message)" v-html="getMediaDisplay(message).content"></div>
                                </div>

                                <!-- Translation panel: 3 context-aware suggestions -->
                                <div v-if="getTranslationState(message.id) && getTranslationState(message.id).expanded"
                                     class="ui segment" style="margin-top: 0.75em; background: #fafafa;">
                                    <div class="ui tiny form" style="margin-bottom: 0.5em;">
                                        <div class="inline fields" style="margin: 0;">
                                            <div class="field">
                                                <label style="font-weight: 600; font-size: 0.85em;">Translate to</label>
                                            </div>
                                            <div class="field">
                                                <select :value="getTranslationState(message.id).targetLang || defaultTargetLang"
                                                        @change="retranslate(message, $event.target.value)"
                                                        class="ui mini dropdown">
                                                    <option value="en">English</option>
                                                    <option value="id">Indonesian</option>
                                                    <option value="ja">Japanese</option>
                                                    <option value="zh">Chinese</option>
                                                    <option value="es">Spanish</option>
                                                    <option value="fr">French</option>
                                                    <option value="de">German</option>
                                                    <option value="pt">Portuguese</option>
                                                    <option value="ar">Arabic</option>
                                                    <option value="ko">Korean</option>
                                                    <option value="ru">Russian</option>
                                                    <option value="vi">Vietnamese</option>
                                                </select>
                                            </div>
                                            <div class="field" v-if="getTranslationState(message.id).cacheHit">
                                                <span class="ui tiny grey label" title="Served from local cache">cached</span>
                                            </div>
                                            <div class="field" v-if="getTranslationState(message.id).provider">
                                                <span class="ui tiny basic label">{{ getTranslationState(message.id).provider }}</span>
                                            </div>
                                        </div>
                                    </div>

                                    <div v-if="getTranslationState(message.id).loading" class="ui active centered inline tiny loader"
                                         style="margin: 1em auto;"></div>

                                    <div v-else-if="getTranslationState(message.id).error" class="ui tiny red message">
                                        <i class="exclamation triangle icon"></i>
                                        {{ getTranslationState(message.id).error }}
                                    </div>

                                    <div v-else-if="getTranslationState(message.id).suggestions && getTranslationState(message.id).suggestions.length > 0">
                                        <div v-for="(s, idx) in getTranslationState(message.id).suggestions"
                                             :key="message.id + '-' + idx"
                                             class="ui segment" style="padding: 0.6em 0.8em; margin: 0.3em 0;">
                                            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.25em;">
                                                <span class="ui tiny horizontal label"
                                                      :class="variantColor(s.variant)">{{ variantLabel(s.variant) }}</span>
                                                <a class="ui mini button" style="cursor: pointer;"
                                                   @click.stop="copySuggestion(s.text)">
                                                    <i class="copy icon"></i> Copy
                                                </a>
                                            </div>
                                            <div style="font-size: 1em; line-height: 1.35;">{{ s.text }}</div>
                                            <div v-if="s.rationale" style="color: #888; font-size: 0.8em; margin-top: 0.2em;">
                                                {{ s.rationale }}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
                
                <!-- Pagination -->
                <div class="ui pagination menu" v-if="totalPages > 1">
                    <a class="icon item" @click="prevPage" :class="{ disabled: currentPage === 1 }">
                        <i class="left chevron icon"></i>
                    </a>
                    <div class="item">
                        Page {{ currentPage }} of {{ totalPages }}
                    </div>
                    <a class="icon item" @click="nextPage" :class="{ disabled: currentPage === totalPages }">
                        <i class="right chevron icon"></i>
                    </a>
                </div>
            </div>
        </div>
        <div class="actions">
            <button class="ui button" @click="backToChatList">
                <i class="arrow left icon"></i>
                Back to Chat List
            </button>
            <div class="ui approve button">Close</div>
        </div>
    </div>
    `,
};
