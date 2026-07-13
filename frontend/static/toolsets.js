// ============================================================
// toolsets.js — 通用工具函数模块
// ============================================================

/**
 * escapeHtml 将字符串中的 HTML 特殊字符转义为实体
 * @param {string} str
 * @returns {string}
 */
export function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

/**
 * isCJKChar 判断字符是否属于 CJK（中日韩）字符集
 * 涵盖：CJK统一表意文字、扩展A/B/C/D/E/F、兼容表意文字、全角符号、部首等
 * @param {string} ch - 单个字符
 * @returns {boolean}
 */
function isCJKChar(ch) {
    const code = ch.charCodeAt(0);
    return (
        (code >= 0x4E00 && code <= 0x9FFF)   || // CJK Unified Ideographs
        (code >= 0x3400 && code <= 0x4DBF)   || // CJK Extension A
        (code >= 0x20000 && code <= 0x2FFFF) || // CJK Extension B~F
        (code >= 0xF900 && code <= 0xFAFF)   || // CJK Compatibility Ideographs
        (code >= 0xFF01 && code <= 0xFF60)   || // Fullwidth Forms
        (code >= 0x2E80 && code <= 0x2EFF)   || // CJK Radicals Supplement
        (code >= 0x3000 && code <= 0x303F)   || // CJK Symbols and Punctuation
        (code >= 0xFE30 && code <= 0xFE4F)   || // CJK Compatibility Forms
        (code >= 0x2FF0 && code <= 0x2FFF)      // Ideographic Description Characters
    );
}

/**
 * visualLength 计算字符串的"视觉长度"。
 * 中文汉字等 CJK 字符每个算 1.5 个字符，ASCII 等窄字符每个算 1 个字符。
 *
 * @param {string} str
 * @returns {number}
 */
export function visualLength(str) {
    let len = 0;
    for (const ch of str) {
        len += isCJKChar(ch) ? 1.5 : 1;
    }
    return len;
}

/**
 * truncateByVisualLength 根据视觉长度截断字符串，超出部分以 "…" 结尾。
 * 使用 visualLength 计算长度，确保中英文混排时的截断更合理。
 *
 * @param {string} str
 * @param {number} maxLen - 最大视觉长度
 * @returns {string}
 */
export function truncateByVisualLength(str, maxLen) {
    if (!str) return '';
    if (visualLength(str) <= maxLen) return str;

    let result = '';
    let len = 0;
    for (const ch of str) {
        const charLen = isCJKChar(ch) ? 1.5 : 1;
        // 预留 "…"（视觉长度为 1）的空间
        if (len + charLen > maxLen - 1) break;
        result += ch;
        len += charLen;
    }
    return result + '…';
}

/**
 * truncate 截断字符串到指定长度，超出部分以 "……" 结尾
 * @param {string} str
 * @param {number} maxLen
 * @returns {string}
 */
export function truncate(str, maxLen) {
    if (str.length <= maxLen) return str;
    return str.slice(0, maxLen) + '……';
}
