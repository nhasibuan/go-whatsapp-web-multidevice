// Tiny façade around the translation REST endpoints. Components shouldn't
// hand-roll URLs — keeping the path strings here means a route rename only
// touches one file and consumers stay readable.
//
// Every method returns the unwrapped `results` payload so callers don't
// have to keep typing `response.data.results` and don't accidentally leak
// HTTP-shaped objects into Vue state.

function unwrap(response) {
    return response?.data?.results ?? null;
}

export const TranslationApi = {
    /**
     * POST /message/:id/translate — translate a stored message.
     * @param {string} messageId
     * @param {{ chat_jid?: string, target_lang?: string, source_lang?: string, force_refresh?: boolean }} payload
     */
    async translateMessage(messageId, payload) {
        const response = await window.http.post(
            `/message/${encodeURIComponent(messageId)}/translate`,
            payload || {},
        );
        return unwrap(response);
    },

    /**
     * POST /translate/draft — translate arbitrary draft text.
     * @param {{ text: string, target_lang: string, source_lang?: string, chat_jid?: string, force_refresh?: boolean }} payload
     */
    async translateDraft(payload) {
        const response = await window.http.post('/translate/draft', payload);
        return unwrap(response);
    },

    /**
     * GET /chat/:chat_jid/translation-prefs — read per-chat preferences.
     * Always returns sensible defaults even when no row exists yet.
     */
    async getChatPrefs(chatJid) {
        const response = await window.http.get(
            `/chat/${encodeURIComponent(chatJid)}/translation-prefs`,
        );
        return unwrap(response);
    },

    /**
     * PUT /chat/:chat_jid/translation-prefs — partial update.
     * Send only the fields you want to change.
     */
    async setChatPrefs(chatJid, patch) {
        const response = await window.http.put(
            `/chat/${encodeURIComponent(chatJid)}/translation-prefs`,
            patch,
        );
        return unwrap(response);
    },
};

export default TranslationApi;
