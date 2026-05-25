import TranslationSuggestionCard from "./generic/TranslationSuggestionCard.js";
import TranslationSettingsPanel from "./generic/TranslationSettingsPanel.js";
import translationMixin from "./generic/translationMixin.js";
import TranslationApi from "./generic/TranslationApi.js";
import {
    TRANSLATION_LANGUAGES,
    languageLabel,
    loadStoredTargetLang,
    storeTargetLang,
} from "./generic/TranslationLanguages.js";

export default {
  name: "ChatMessages",
  components: {
    TranslationSuggestionCard,
    TranslationSettingsPanel,
  },
  mixins: [translationMixin],
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
      // Translation state
      // Language list is shared with SendMessage and any future translation
      // surface so a new language only needs to be added once.
      translationLanguages: TRANSLATION_LANGUAGES,
      translationTargetLang: loadStoredTargetLang(),
      translationsByMessage: {}, // messageId -> { suggestions, target_lang, source_text, provider, cached, loading, error }
      activeTranslationMessageId: null,
      // ---- Phase 4: per-chat preferences and auto-translate inbound ----
      // chatPrefs is the persisted row + effective_target_lang (the value
      // actually used after fallback to the global default). Always loaded
      // alongside the messages so the panel can render without 404 handling.
      chatPrefs: null,
      chatPrefsLoading: false,
      chatPrefsSaving: false,
      showTranslationPanel: false,
      // autoTranslations stores the auto-fetched "natural" suggestion per
      // message id. Rendered as a small inline accent panel under each
      // inbound bubble so the user gets a translation without clicking.
      autoTranslations: {},      // messageId -> { text, target_lang, provider }
      // Re-render guard: prevents Vue's reactive updates from re-firing the
      // same per-message calls during a single load. The translation cache
      // makes the system correct even without this latch — the latch keeps
      // the network panel clean and avoids burst noise on slower providers.
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
          // Phase 4: pull prefs and (if auto-translate-inbound is on)
          // prefetch translations for inbound text bubbles. Both calls are
          // best-effort — failures degrade silently to manual-translate UX.
          await this.loadChatPrefs();
          this.maybeAutoTranslateInbound();
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
      // Reset translation state
      this.translationsByMessage = {};
      this.activeTranslationMessageId = null;
      this.chatPrefs = null;
      this.showTranslationPanel = false;
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
    // Translation helpers
    onTranslationLanguageChange() {
      // Persist user pref so it survives reloads/modal reopens.
      storeTargetLang(this.translationTargetLang);
      // If a panel is open, retranslate so the user sees the new language immediately.
      if (this.activeTranslationMessageId) {
        const message = this.messages.find(m => m.id === this.activeTranslationMessageId);
        if (message) this.fetchTranslation(message);
      }
    },
    isTranslationOpen(messageId) {
      return this.activeTranslationMessageId === messageId;
    },
    isTranslationLoading(messageId) {
      const t = this.translationsByMessage[messageId];
      return !!(t && t.loading);
    },
    getTranslation(messageId) {
      return this.translationsByMessage[messageId] || null;
    },
    async toggleTranslation(message) {
      if (!message || !message.id) return;
      const messageId = message.id;
      if (this.activeTranslationMessageId === messageId) {
        this.activeTranslationMessageId = null;
        return;
      }
      this.activeTranslationMessageId = messageId;

      const existing = this.translationsByMessage[messageId];
      // Re-fetch when target language changed since last call.
      if (existing && existing.target_lang === this.translationTargetLang && Array.isArray(existing.suggestions) && existing.suggestions.length > 0) {
        return;
      }
      await this.fetchTranslation(message);
    },
    async fetchTranslation(message) {
      const messageId = message.id;
      // Initialize loading state without losing previous suggestions.
      this.translationsByMessage = {
        ...this.translationsByMessage,
        [messageId]: {
          ...(this.translationsByMessage[messageId] || {}),
          loading: true,
          error: '',
        },
      };
      try {
        const result = await TranslationApi.translateMessage(messageId, {
          chat_jid: this.formattedJid,
          target_lang: (this.translationTargetLang || 'en').trim(),
        }) || {};
        this.translationsByMessage = {
          ...this.translationsByMessage,
          [messageId]: {
            suggestions: result.suggestions || [],
            target_lang: result.target_lang || (this.translationTargetLang || 'en').trim(),
            source_text: result.source_text || '',
            provider: result.provider || '',
            cached: !!result.cached,
            loading: false,
            error: '',
          },
        };
      } catch (err) {
        const msg = err?.response?.data?.message || err?.message || 'Translation failed';
        this.translationsByMessage = {
          ...this.translationsByMessage,
          [messageId]: {
            ...(this.translationsByMessage[messageId] || {}),
            loading: false,
            error: msg,
          },
        };
        showErrorInfo(msg);
      }
    },
    async copyTranslationText(text) {
    // variantLabel / variantColor / copyTranslationText come from translationMixin.
    // ---- Phase 4: per-chat translation preferences ----
    toggleTranslationPanel() {
      this.showTranslationPanel = !this.showTranslationPanel;
    },
    effectiveTargetLangLabel() {
      const lang = this.chatPrefs?.effective_target_lang || this.translationTargetLang || 'en';
      return languageLabel(lang);
    },
    chatPrefsTargetSelection() {
      // Empty string is the canonical "use global default" value. The
      // dropdown shows it as a sentinel option so the user can clear an override.
      return this.chatPrefs?.target_lang || '';
    },
    onPrefsUpdate(patch) {
      // Bridge from the reusable settings panel to our internal updater.
      // Lang changes also trigger an auto-translate refresh because the
      // existing display batch is keyed off the previous language.
      if (patch && Object.prototype.hasOwnProperty.call(patch, 'target_lang')) {
        this.autoTranslations = {};
        this.updateChatPrefs(patch).then(() => this.maybeAutoTranslateInbound());
        return;
      }
      if (patch && Object.prototype.hasOwnProperty.call(patch, 'auto_translate_inbound')) {
        const checked = !!patch.auto_translate_inbound;
        this.updateChatPrefs(patch).then(() => {
          if (checked) this.maybeAutoTranslateInbound();
          else this.autoTranslations = {};
        });
        return;
      }
      this.updateChatPrefs(patch);
    },
    async loadChatPrefs() {
      if (!this.formattedJid) return;
      this.chatPrefsLoading = true;
      try {
        this.chatPrefs = await TranslationApi.getChatPrefs(this.formattedJid);
      } catch (err) {
        // Failing to load prefs shouldn't block the message UI — log and
        // fall back to global defaults inferred from translationTargetLang.
        const status = err?.response?.status;
        if (status && status !== 200) {
          console.warn('[chat-prefs] load failed', err?.response?.data?.message || err.message);
        }
        this.chatPrefs = null;
      } finally {
        this.chatPrefsLoading = false;
      }
    },
    async updateChatPrefs(patch) {
      // Partial-update wrapper: only the keys present in `patch` go on the
      // wire. The server validates that at least one field is set.
      if (!this.formattedJid) return;
      this.chatPrefsSaving = true;
      try {
        const updated = await TranslationApi.setChatPrefs(this.formattedJid, patch);
        this.chatPrefs = updated || this.chatPrefs;
        showSuccessInfo('Translation settings saved');
      } catch (err) {
        showErrorInfo(err?.response?.data?.message || err?.message || 'Failed to save settings');
      } finally {
        this.chatPrefsSaving = false;
      }
    },
    isInboundTextMessage(message) {
      if (!message || !message.id) return false;
      if (message.is_from_me) return false;
      // Skip non-text bubbles — translating "[IMAGE]" is just noise.
      const content = (message.content || message.text || message.caption || '').trim();
      if (!content) return false;
      const mediaType = (message.media_type || '').toLowerCase();
      if (mediaType && mediaType !== 'text' && mediaType !== '') return false;
      return true;
    },
    async maybeAutoTranslateInbound() {
      // Gate on the persisted toggle so the prefetch only runs when the
      // user actually opted in. The in-flight latch prevents Vue
      // re-renders from re-firing the same per-message calls.
      if (!this.chatPrefs?.auto_translate_inbound) return;
      if (this.autoTranslateBatchInFlight) return;
      const candidates = this.messages.filter(this.isInboundTextMessage);
      if (candidates.length === 0) return;

      this.autoTranslateBatchInFlight = true;
      try {
        // Concurrency cap of 3 keeps the dev network panel manageable and
        // mirrors the same back-pressure shape used for media downloads.
        const queue = candidates.slice();
        const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
          while (queue.length > 0) {
            const message = queue.shift();
            if (!message) break;
            if (this.autoTranslations[message.id]) continue;
            await this.fetchAutoTranslation(message);
          }
        });
        await Promise.all(workers);
      } finally {
        this.autoTranslateBatchInFlight = false;
      }
    },
    async fetchAutoTranslation(message) {
      // The cache-key on the server side dedupes by message+target_lang so
      // a re-run after a second loadMessages call is essentially free.
      try {
        const result = await TranslationApi.translateMessage(message.id, {
          chat_jid: this.formattedJid,
          target_lang: (this.chatPrefs?.effective_target_lang || this.translationTargetLang || 'en').trim(),
        }) || {};
        const natural = (result.suggestions || []).find(s => s.variant === 'natural')
                     || (result.suggestions || [])[0];
        if (!natural) return;
        // Mutate via spread so Vue picks up the change cheaply without a
        // deep-watch on every individual message.
        this.autoTranslations = {
          ...this.autoTranslations,
          [message.id]: {
            text: natural.text,
            target_lang: result.target_lang,
            provider: result.provider,
          },
        };
      } catch (err) {
        // Quietly skip failures — the user can still click the globe to
        // retry per-message. Don't toast every failed inbound translate.
        console.warn('[auto-translate] failed for', message.id, err?.response?.data?.message || err.message);
      }
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
            <span v-if="chatPrefs && chatPrefs.effective_target_lang"
                  class="ui small horizontal label"
                  style="margin-left: 0.5em;"
                  :title="'Translations are rendered into ' + effectiveTargetLangLabel()">
                <i class="globe icon"></i> {{ effectiveTargetLangLabel() }}
            </span>
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
                        Translation Settings (Optional)
                    </div>
                    <div class="content">
                        <div class="fields">
                            <div class="six wide field">
                                <label>Translate into</label>
                                <select class="ui dropdown"
                                        v-model="translationTargetLang"
                                        @change="onTranslationLanguageChange"
                                        aria-label="translation target language">
                                    <option v-for="lang in translationLanguages"
                                            :key="lang.code"
                                            :value="lang.code">
                                        {{ lang.name }} ({{ lang.code }})
                                    </option>
                                </select>
                            </div>
                        </div>
                    </div>
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
                <button class="ui button"
                        :class="{ 'active': showTranslationPanel }"
                        :disabled="!isValidForm()"
                        @click="toggleTranslationPanel">
                    <i class="globe icon"></i>
                    Translation settings
                </button>
            </div>

            <div v-if="showTranslationPanel" style="margin-top: 0.5em;">
                <TranslationSettingsPanel
                    :prefs="chatPrefs"
                    :loading="chatPrefsLoading"
                    :saving="chatPrefsSaving"
                    @update="onPrefsUpdate" />
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
                            </div>
                            <div class="meta">
                                <span>{{ formatTimestamp(message.timestamp) }}</span>
                                <span v-if="message.id" class="right floated">
                                    ID: {{ message.id }}
                                </span>
                            </div>
                            <div class="description">
                                <p>{{ getMessageContent(message) }}</p>
                                <div v-if="message.media_type && message.url" class="media-container" style="margin-top: 0.5em;">
                                    <div v-if="getMediaDisplay(message)" v-html="getMediaDisplay(message).content"></div>
                                </div>
                                <div v-if="autoTranslations[message.id]"
                                     class="ui blue mini message"
                                     style="margin-top: 0.5em; padding: 0.5em 0.75em;">
                                    <div style="display: flex; align-items: center; gap: 0.5em;">
                                        <i class="globe icon" style="margin: 0;"></i>
                                        <span class="ui mini horizontal label blue">{{ autoTranslations[message.id].target_lang }}</span>
                                        <span style="flex: 1;">{{ autoTranslations[message.id].text }}</span>
                                    </div>
                                </div>
                                <div v-if="message.id && getMessageContent(message) && getMessageContent(message) !== '[No content]'" style="margin-top: 0.5em;">
                                    <button class="ui mini basic button"
                                            :class="{ 'active': isTranslationOpen(message.id) }"
                                            @click="toggleTranslation(message)"
                                            :disabled="isTranslationLoading(message.id)"
                                            :aria-label="'Translate message ' + message.id">
                                        <i class="globe icon"></i>
                                        <span v-if="isTranslationLoading(message.id)">Translating...</span>
                                        <span v-else-if="isTranslationOpen(message.id)">Hide translations</span>
                                        <span v-else>Translate</span>
                                    </button>
                                    <div v-if="isTranslationOpen(message.id)" style="margin-top: 0.5em;">
                                        <div v-if="isTranslationLoading(message.id)" class="ui active centered inline tiny loader"></div>
                                        <div v-else-if="getTranslation(message.id) && getTranslation(message.id).error" class="ui red mini message">
                                            {{ getTranslation(message.id).error }}
                                        </div>
                                        <div v-else-if="getTranslation(message.id) && getTranslation(message.id).suggestions && getTranslation(message.id).suggestions.length > 0">
                                            <div class="ui small horizontal label">
                                                {{ getTranslation(message.id).target_lang }}
                                                <span v-if="getTranslation(message.id).provider"> · {{ getTranslation(message.id).provider }}</span>
                                                <span v-if="getTranslation(message.id).cached"> · cached</span>
                                            </div>
                                            <div class="ui mini stackable cards" style="margin-top: 0.5em;">
                                                <div v-for="s in getTranslation(message.id).suggestions" :key="s.variant" class="card" style="width: 100%;">
                                                    <TranslationSuggestionCard :suggestion="s" />
                                                </div>
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
