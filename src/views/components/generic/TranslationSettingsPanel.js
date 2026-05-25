import { TRANSLATION_LANGUAGES, languageLabel } from "./TranslationLanguages.js";

// Reusable per-chat prefs editor. Stateless on its own — the parent owns
// the persisted prefs object and reacts to events emitted from here.
//
// The component intentionally doesn't call the API directly: that keeps
// it usable in dry-run modes (e.g. unit tests, or future bulk editors)
// and makes the parent's data flow explicit.
//
// Props:
//   - prefs: { target_lang, auto_translate_inbound, auto_translate_outbound,
//              effective_target_lang, ... } | null
//   - loading: bool
//   - saving: bool
//
// Events:
//   - update: emitted with a partial { target_lang | auto_translate_inbound |
//             auto_translate_outbound } patch the parent should persist.
export default {
    name: 'TranslationSettingsPanel',
    props: {
        prefs: {
            type: Object,
            default: null,
        },
        loading: { type: Boolean, default: false },
        saving: { type: Boolean, default: false },
    },
    emits: ['update'],
    data() {
        return {
            languages: TRANSLATION_LANGUAGES,
        };
    },
    computed: {
        targetSelection() {
            // Empty string is the canonical "use global default" value.
            return this.prefs?.target_lang || '';
        },
        effectiveLabel() {
            return languageLabel(this.prefs?.effective_target_lang || '');
        },
        autoInbound: {
            get() { return !!this.prefs?.auto_translate_inbound; },
            set(value) { this.$emit('update', { auto_translate_inbound: !!value }); },
        },
        autoOutbound: {
            get() { return !!this.prefs?.auto_translate_outbound; },
            set(value) { this.$emit('update', { auto_translate_outbound: !!value }); },
        },
    },
    methods: {
        onTargetLangChange(event) {
            const value = (event?.target?.value ?? '').toString();
            this.$emit('update', { target_lang: value });
        },
    },
    template: `
    <div class="ui segment">
        <div v-if="loading" class="ui active centered inline tiny loader"></div>
        <template v-else>
            <div class="fields">
                <div class="six wide field">
                    <label>Per-chat target language</label>
                    <select class="ui dropdown"
                            :value="targetSelection"
                            @change="onTargetLangChange"
                            :disabled="saving"
                            aria-label="per-chat translation target language">
                        <option value="">Use global default</option>
                        <option v-for="lang in languages"
                                :key="lang.code"
                                :value="lang.code">
                            {{ lang.name }} ({{ lang.code }})
                        </option>
                    </select>
                </div>
                <div class="ten wide field">
                    <label>Effective: <b>{{ effectiveLabel }}</b></label>
                    <p class="meta" style="font-size: 0.85em; color: #888; margin-top: 0.25em;">
                        Falls back to the global default when no per-chat override is set.
                    </p>
                </div>
            </div>
            <div class="fields">
                <div class="eight wide field">
                    <div class="ui toggle checkbox">
                        <input type="checkbox"
                               v-model="autoInbound"
                               :disabled="saving"
                               aria-label="auto translate inbound">
                        <label>Auto-translate incoming messages</label>
                    </div>
                </div>
                <div class="eight wide field">
                    <div class="ui toggle checkbox">
                        <input type="checkbox"
                               v-model="autoOutbound"
                               :disabled="saving"
                               aria-label="auto translate outbound">
                        <label>Auto-translate outgoing drafts (reserved)</label>
                    </div>
                </div>
            </div>
        </template>
    </div>
    `,
};
