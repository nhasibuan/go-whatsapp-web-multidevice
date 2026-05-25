import translationMixin from "./translationMixin.js";

// Presentational card for a single translation suggestion. Stateless on
// purpose — the parent owns selection, copy intent, and "use this"
// semantics, and signals them via events. This keeps the card reusable
// across both the inline panel in ChatMessages and the radio-list in
// SendMessage without cross-component coupling.
//
// Props:
//   - suggestion: { variant, text, rationale, confidence }
//   - actionLabel (optional): primary-action button label. When empty, the
//                             button is hidden — useful for read-only views.
//
// Events:
//   - copy: emitted after a successful clipboard copy
//   - select: emitted when the user clicks the primary action
export default {
    name: 'TranslationSuggestionCard',
    mixins: [translationMixin],
    props: {
        suggestion: {
            type: Object,
            required: true,
            validator: (s) => s && typeof s.variant === 'string' && typeof s.text === 'string',
        },
        actionLabel: {
            type: String,
            default: '',
        },
        showCopy: {
            type: Boolean,
            default: true,
        },
    },
    emits: ['copy', 'select'],
    methods: {
        async onCopy() {
            await this.copyTranslationText(this.suggestion.text);
            this.$emit('copy', this.suggestion);
        },
        onSelect() {
            this.$emit('select', this.suggestion);
        },
    },
    template: `
    <div class="content" style="padding: 0.5em 0;">
        <div class="header">
            <span class="ui mini horizontal label" :class="variantColor(suggestion.variant)">
                {{ variantLabel(suggestion.variant) }}
            </span>
        </div>
        <div class="description" style="margin-top: 0.4em;">{{ suggestion.text }}</div>
        <div v-if="suggestion.rationale"
             class="meta"
             style="font-style: italic; color: #888; margin-top: 0.25em;">
            {{ suggestion.rationale }}
        </div>
        <div style="margin-top: 0.4em; display: flex; gap: 0.5em; flex-wrap: wrap;">
            <button v-if="actionLabel"
                    class="ui mini compact primary button"
                    @click.prevent="onSelect">
                <i class="check icon"></i> {{ actionLabel }}
            </button>
            <button v-if="showCopy"
                    class="ui mini compact basic button"
                    @click.prevent="onCopy">
                <i class="copy icon"></i> Copy
            </button>
        </div>
    </div>
    `,
};
