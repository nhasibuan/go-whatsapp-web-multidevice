// Vue mixin: shared translation helpers used by multiple components.
//
// Mixins keep behavior DRY without forcing a particular composition style.
// Vue 3 supports composables too, but the rest of this codebase uses the
// Options API consistently — staying with mixins keeps the surface
// uniform and avoids forcing a build step.
//
// What lives here:
//   - variantLabel / variantColor: tiny pure helpers used wherever a
//     suggestion card is rendered.
//   - copyTranslationText: one-liner clipboard fallback that mirrors the
//     behavior in the original ChatMessages.js implementation.

const VARIANT_LABELS = Object.freeze({
    literal: 'Literal',
    natural: 'Natural',
    tone_matched: 'Tone-matched',
});

const VARIANT_COLORS = Object.freeze({
    literal: 'grey',
    natural: 'blue',
    tone_matched: 'teal',
});

export const translationMixin = {
    methods: {
        variantLabel(variant) {
            return VARIANT_LABELS[variant] || variant || 'Variant';
        },
        variantColor(variant) {
            return VARIANT_COLORS[variant] || 'grey';
        },
        async copyTranslationText(text) {
            // Prefer the async clipboard API; fall back to the textarea
            // trick for older browsers / non-secure contexts (HTTP).
            try {
                if (navigator.clipboard && navigator.clipboard.writeText) {
                    await navigator.clipboard.writeText(text);
                } else {
                    const textarea = document.createElement('textarea');
                    textarea.value = text;
                    textarea.setAttribute('readonly', '');
                    textarea.style.position = 'absolute';
                    textarea.style.left = '-9999px';
                    document.body.appendChild(textarea);
                    textarea.select();
                    document.execCommand('copy');
                    document.body.removeChild(textarea);
                }
                window.showSuccessInfo?.('Translation copied to clipboard');
            } catch (_e) {
                window.showErrorInfo?.('Could not copy to clipboard');
            }
        },
    },
};

export default translationMixin;
