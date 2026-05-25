import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendMessage',
    components: {
        FormRecipient
    },
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
            // Translation (compose-assist) — replaces this.text with a chosen
            // suggestion before send.
            translateTargetLang: localStorage.getItem('translationTargetLang') || 'en',
            translateLoading: false,
            translateSuggestions: [],
            translateError: '',
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
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
            // Validate phone number is not empty except for status type
            const isPhoneValid = this.type === window.TYPESTATUS || this.phone.trim().length > 0;
            
            // Validate message is not empty and has reasonable length
            const isMessageValid = this.text.trim().length > 0 && this.text.length <= 4096;

            return isPhoneValid && isMessageValid
        },
        async handleSubmit() {
            // Add validation check here to prevent submission when form is invalid
            if (!this.isValidForm() || this.loading) {
                return;
            }
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
                    is_forwarded: this.is_forwarded
                };
                if (this.reply_message_id !== '') {
                    payload.reply_message_id = this.reply_message_id;
                }

                if (this.duration && this.duration > 0) {
                    payload.duration = this.duration;
                }

                // Add mentions if mention_everyone is checked (only for groups)
                if (this.mention_everyone && this.type === window.TYPEGROUP) {
                    payload.mentions = ["@everyone"];
                }

                const response = await window.http.post('/send/message', payload);
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response?.data?.message) {
                    throw new Error(error.response.data.message);
                }
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
            this.translateLoading = false;
            this.translateSuggestions = [];
            this.translateError = '';
        },
        // ----- Translation (compose-assist) -----
        async fetchTranslateSuggestions() {
            if (!this.text.trim() || this.translateLoading) return;
            this.translateLoading = true;
            this.translateError = '';
            this.translateSuggestions = [];
            try {
                const payload = {
                    text: this.text.trim(),
                    target_lang: this.translateTargetLang,
                };
                // Pass chat_jid when we have a recipient — gives the model
                // conversation context so the tone-matched variant is useful.
                if (this.type !== window.TYPESTATUS && this.phone.trim()) {
                    payload.chat_jid = this.phone_id;
                }
                const response = await window.http.post('/translate/draft', payload);
                const r = response.data?.results || {};
                this.translateSuggestions = Array.isArray(r.suggestions) ? r.suggestions : [];
                if (this.translateSuggestions.length === 0) {
                    this.translateError = 'No suggestions returned';
                }
            } catch (err) {
                this.translateError = err.response?.data?.message || err.message || 'Translation failed';
            } finally {
                this.translateLoading = false;
            }
        },
        useTranslation(suggestion) {
            // Replace mode: the chosen translation overwrites the typed text.
            // The user can still edit before pressing Send.
            this.text = suggestion.text || '';
            this.translateSuggestions = [];
            this.translateError = '';
            try { localStorage.setItem('translationTargetLang', this.translateTargetLang); } catch (e) { /* ignore */ }
            showSuccessInfo('Message replaced with translation. Review then press Send.');
        },
        clearTranslateSuggestions() {
            this.translateSuggestions = [];
            this.translateError = '';
        },
        translateVariantLabel(variant) {
            switch ((variant || '').toLowerCase()) {
                case 'literal': return 'Literal';
                case 'natural': return 'Natural';
                case 'tone_matched': return 'Tone-matched';
                default: return variant || 'Suggestion';
            }
        },
        translateVariantColor(variant) {
            switch ((variant || '').toLowerCase()) {
                case 'literal': return 'grey';
                case 'natural': return 'blue';
                case 'tone_matched': return 'teal';
                default: return 'grey';
            }
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

                <!-- Translate before send: replaces the message above with a chosen suggestion -->
                <div class="field">
                    <label>Translate before send (optional)</label>
                    <div class="ui action input" style="max-width: 100%;">
                        <select v-model="translateTargetLang" class="ui compact selection dropdown" style="border-radius: 4px 0 0 4px;">
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
                        <button class="ui teal button"
                                :class="{'disabled': translateLoading || !text.trim()}"
                                @click.prevent="fetchTranslateSuggestions">
                            <i class="globe icon"></i>
                            {{ translateLoading ? 'Translating…' : 'Translate' }}
                        </button>
                    </div>
                    <div v-if="translateError" class="ui tiny red message" style="margin-top: 0.5em;">
                        <i class="exclamation triangle icon"></i> {{ translateError }}
                    </div>
                    <div v-if="translateSuggestions.length > 0" class="ui segments" style="margin-top: 0.5em;">
                        <div v-for="(s, idx) in translateSuggestions" :key="'tr-' + idx"
                             class="ui segment" style="padding: 0.6em 0.8em;">
                            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.25em;">
                                <span class="ui tiny horizontal label"
                                      :class="translateVariantColor(s.variant)">{{ translateVariantLabel(s.variant) }}</span>
                                <button class="ui mini teal button" @click.prevent="useTranslation(s)">
                                    <i class="check icon"></i> Use this
                                </button>
                            </div>
                            <div style="font-size: 0.95em; line-height: 1.35;">{{ s.text }}</div>
                            <div v-if="s.rationale" style="color: #888; font-size: 0.78em; margin-top: 0.2em;">
                                {{ s.rationale }}
                            </div>
                        </div>
                        <div class="ui secondary segment" style="text-align: right; padding: 0.4em 0.8em;">
                            <a style="cursor: pointer; color: #888;" @click.prevent="clearTranslateSuggestions">
                                <i class="close icon"></i> Dismiss suggestions
                            </a>
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
    `
}