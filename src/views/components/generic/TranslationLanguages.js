// Single source of truth for the language picker shown across the
// translation UI surfaces (ChatMessages, SendMessage, future glossary editor).
//
// The list is intentionally small — twelve common WhatsApp UI languages —
// so the dropdown stays scannable. Anything else can be typed via the
// MCP/REST API directly. New entries here are the only change needed for
// every component to pick them up.
export const TRANSLATION_LANGUAGES = Object.freeze([
    { code: 'en', name: 'English' },
    { code: 'id', name: 'Indonesian' },
    { code: 'es', name: 'Spanish' },
    { code: 'pt', name: 'Portuguese' },
    { code: 'fr', name: 'French' },
    { code: 'de', name: 'German' },
    { code: 'it', name: 'Italian' },
    { code: 'nl', name: 'Dutch' },
    { code: 'ja', name: 'Japanese' },
    { code: 'ko', name: 'Korean' },
    { code: 'zh', name: 'Chinese' },
    { code: 'ar', name: 'Arabic' },
]);

// Pretty-printer for the effective-language chip and dropdown labels.
// Falls back to the raw code when the language isn't in the list so
// edge-case inputs (like "ja-JP") don't render as empty strings.
export function languageLabel(code) {
    const found = TRANSLATION_LANGUAGES.find(l => l.code === code);
    return found ? `${found.name} (${found.code})` : (code || 'unknown');
}

// localStorage key shared by every component that remembers a target lang
// so the user's last choice is consistent across surfaces.
export const TRANSLATION_LANG_STORAGE_KEY = 'translationTargetLang';

export function loadStoredTargetLang(fallback = 'en') {
    try {
        return localStorage.getItem(TRANSLATION_LANG_STORAGE_KEY) || fallback;
    } catch (_) {
        return fallback;
    }
}

export function storeTargetLang(code) {
    try {
        localStorage.setItem(TRANSLATION_LANG_STORAGE_KEY, code || 'en');
    } catch (_) { /* storage may be disabled — silently ignore */ }
}

export default TRANSLATION_LANGUAGES;
