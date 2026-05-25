import FormRecipient from "./generic/FormRecipient.js";
import TranslationSuggestionCard from "./generic/TranslationSuggestionCard.js";
import translationMixin from "./generic/translationMixin.js";
import TranslationApi from "./generic/TranslationApi.js";
import {
    TRANSLATION_LANGUAGES,
    loadStoredTargetLang,
    storeTargetLang,
} from "./generic/TranslationLanguages.js";

// SendMessage owns the compose-assist flow described in the brainstorm:
// the user types in their language, optionally fetches three context-aware
// suggestions, picks one, and the existing /send/message pipeline ships it.
//
// Why a mixin + reusable card instead of a fresh component:
//   - The Vue Options API style is consistent across the rest of this
//     codebase (no SFCs, no build step). Mixins keep the surface uniform.
//   - The suggestion card is the same one used in ChatMessages — keeping
//     the visual treatment in lockstep is the whole point of pulling it out.
export default {
    name: 'SendMessage',
    components: {
        FormRecipient,
        TranslationSuggestionCard,
    },
    mixins: [translationMixin],
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            text: '',
            reply_message_id: '',
            is_forwarded: false,
            mention_everyone: false,
            duration: 0,
            loading: false,
            // Translate-before-send state.
            translateBeforeSend: false,
            translationLanguages: TRANSLATION_LANGUAGES,
            translationTargetLang: loadStoredTargetLang(),
            translationLoading: false,
            translationSuggestions: [],
            translationProvider: '',
            translationCached: false,
            translationError: '',
            originalDraft: '',
            selectedVariant: '',
        };
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
        canTranslate() {
            return !this.translationLoading
                && this.text.trim().length > 0
                && (this.translationTargetLang || '').trim().length > 0;
        },
    },
    methods: {
        openModal() {
            $('#modalSendMessage').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isShowReplyId() {
            return this.type !== window.TYPESTATUS;
        },
        isGroup() {
            return this.type === window.TYPEGROUP;
        },
        isValidForm() {
            const isPhoneValid = this.type === window.TYPESTATUS || this.phone.trim().length > 0;
            const isMessageValid = this.text.trim().length > 0 && this.text.length <= 4096;
            return isPhoneValid && isMessageValid;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) return;
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalSendMessage').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    message: this.text.trim(),
                    is_forwarded: this.is_forwarded,
                };
                if (this.reply_message_id !== '') payload.reply_message_id = this.reply_message_id;
                if (this.duration && this.duration > 0) payload.duration = this.duration;
                if (this.mention_everyone && this.type === window.TYPEGROUP) {
                    payload.mentions = ["@everyone"];
                }
                const response = await window.http.post('/send/message', payload);
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response?.data?.message) throw new Error(error.response.data.message);
                throw error;
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.text = '';
            this.reply_message_id = '';
            this.is_forwarded = false;
            this.mention_everyone = false;
            this.duration = 0;
            this.resetTranslationState();
        },
        // ---- Translate-before-send helpers ----
        resetTranslationState() {
            this.translationSuggestions = [];
            this.translationProvider = '';
            this.translationCached = false;
            this.translationError = '';
            this.originalDraft = '';
            this.selectedVariant = '';
        },
        onTranslateToggle() {
            if (!this.translateBeforeSend) {
                this.resetTranslationState();
            }
        },
        onTranslationLanguageChange() {
            storeTargetLang(this.translationTargetLang);
            // Drop existing suggestions so the user clearly re-fetches under
            // the new language; cheaper UX than auto-refetching as they type.
            this.translationSuggestions = [];
            this.translationError = '';
            this.selectedVariant = '';
        },
        async fetchSuggestions() {
            if (!this.canTranslate) return;
            if (!this.originalDraft) this.originalDraft = this.text;
            this.translationLoading = true;
            this.translationError = '';
            try {
                const payload = {
                    text: this.text.trim(),
                    target_lang: (this.translationTargetLang || 'en').trim(),
                };
                if (this.phone && this.phone.trim()) payload.chat_jid = this.phone_id;
                const result = await TranslationApi.translateDraft(payload) || {};
                this.translationSuggestions = result.suggestions || [];
                this.translationProvider = result.provider || '';
                this.translationCached = !!result.cached;
                const natural = this.translationSuggestions.find(s => s.variant === 'natural');
                this.selectedVariant = natural?.variant || this.translationSuggestions[0]?.variant || '';
            } catch (err) {
                this.translationError = err?.response?.data?.message || err?.message || 'Translation failed';
                showErrorInfo(this.translationError);
            } finally {
                this.translationLoading = false;
            }
        },
        applySuggestion(suggestion) {
            if (!suggestion) return;
            this.text = suggestion.text;
            this.selectedVariant = suggestion.variant;
        },
        undoSuggestion() {
            if (this.originalDraft) this.text = this.originalDraft;
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Message</div>
            <div class="description">
                Send any message to user or group
            </div>
        </div>
    </div>

    <!--  Modal SendMessage  -->
    <div class="ui small modal" id="modalSendMessage">
        <i class="close icon"></i>
        <div class="header">
            Send Message
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone" :show-status="true"/>
                <div class="field" v-if="isShowReplyId()">
                    <label>Reply Message ID</label>
                    <input v-model="reply_message_id" type="text"
                           placeholder="Optional: 57D29F74B7FC62F57D8AC2C840279B5B/3EB0288F008D32FCD0A424"
                           aria-label="reply_message_id">
                </div>
                <div class="field">
                    <label>Message</label>
                    <textarea v-model="text" placeholder="Hello this is message text"
                              aria-label="message"></textarea>
                </div>

                <!-- Translate-before-send -->
                <div class="field">
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="translate before send" v-model="translateBeforeSend" @change="onTranslateToggle">
                        <label>Translate before send</label>
                    </div>
                </div>
                <div v-if="translateBeforeSend" class="ui segment">
                    <div class="fields">
                        <div class="six wide field">
                            <label>Translate into</label>
                            <select class="ui dropdown"
                                    v-model="translationTargetLang"
                                    @change="onTranslationLanguageChange"
                                    aria-label="translate-before-send target language">
                                <option v-for="lang in translationLanguages"
                                        :key="lang.code"
                                        :value="lang.code">
                                    {{ lang.name }} ({{ lang.code }})
                                </option>
                            </select>
                        </div>
                        <div class="ten wide field" style="display: flex; align-items: flex-end;">
                            <button class="ui small primary button"
                                    :class="{ 'disabled': !canTranslate, 'loading': translationLoading }"
                                    @click.prevent="fetchSuggestions">
                                <i class="globe icon"></i>
                                Get 3 suggestions
                            </button>
                            <button v-if="originalDraft && originalDraft !== text"
                                    class="ui small basic button"
                                    style="margin-left: 0.5em;"
                                    @click.prevent="undoSuggestion"
                                    title="Restore your original draft">
                                <i class="undo icon"></i> Undo
                            </button>
                        </div>
                    </div>

                    <div v-if="translationError" class="ui red mini message">{{ translationError }}</div>

                    <div v-if="translationSuggestions.length > 0">
                        <div class="ui small horizontal label">
                            <span v-if="translationProvider">{{ translationProvider }}</span>
                            <span v-if="translationCached"> · cached</span>
                        </div>
                        <div class="ui relaxed divided list" style="margin-top: 0.5em;">
                            <div v-for="s in translationSuggestions"
                                 :key="s.variant"
                                 class="item"
                                 :class="{ 'active': selectedVariant === s.variant }"
                                 style="padding: 0.5em 0;">
                                <TranslationSuggestionCard
                                    :suggestion="s"
                                    action-label="Use this"
                                    @select="applySuggestion" />
                            </div>
                        </div>
                    </div>
                </div>

                <div class="field" v-if="isShowReplyId()">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark message as forwarded</label>
                    </div>
                </div>
                <div class="field" v-if="isGroup()">
                    <label>Mention Everyone</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="mention everyone" v-model="mention_everyone">
                        <label>Mention all group participants (@everyone)</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button"
                 :class="{'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `,
};
