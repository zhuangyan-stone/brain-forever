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
 * truncate 截断字符串到指定长度，超出部分以 "..." 结尾
 * @param {string} str
 * @param {number} maxLen
 * @returns {string}
 */
export function truncate(str, maxLen) {
    if (str.length <= maxLen) return str;
    return str.slice(0, maxLen) + '...';
}
