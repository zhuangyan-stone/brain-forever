// ============================================================
// clipboard.js — 剪贴板功能模块
// ============================================================

/**
 * copyPlainText 复制纯文本到剪贴板
 * @param {string} text
 * @returns {Promise<boolean>}
 */
export async function copyPlainText(text) {
    try {
        await navigator.clipboard.writeText(text);
        return true;
    } catch (e) {
        console.warn('复制纯文本失败:', e);
        return false;
    }
}

/**
 * copyMarkdown 复制 Markdown 到剪贴板
 *
 * Markdown 本质是纯文本，使用 navigator.clipboard.writeText() 即可。
 * 粘贴到 Typora、Obsidian 等 Markdown 编辑器时会被正确识别。
 *
 * @param {string} markdown
 * @returns {Promise<boolean>}
 */
export async function copyMarkdown(markdown) {
    try {
        await navigator.clipboard.writeText(markdown);
        return true;
    } catch (e) {
        console.warn('复制 Markdown 失败:', e);
        return false;
    }
}

/**
 * copyHtml 复制 HTML 富文本到剪贴板
 *
 * 使用 ClipboardItem API 写入 text/html MIME 类型。
 * 粘贴到 Word、Notion、Google Docs 等富文本编辑器时保留格式。
 *
 * ⚠️ 必须在用户点击事件的同步上下文中调用（或用 .then() 链），
 * 否则浏览器会因 "lack of user activation" 拒绝。
 *
 * @param {string} html HTML 富文本片段
 * @returns {Promise<boolean>}
 */
export function copyHtml(html) {
    if (!html) return Promise.resolve(false);

    // 从 HTML 中提取纯文本（用于 text/plain 回退）
    const plainText = extractPlainText(html);

    const wrapped = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
</head>
<body>
${html}
</body>
</html>`;

    try {
        return navigator.clipboard.write([
            new ClipboardItem({
                'text/plain': new Blob([plainText], { type: 'text/plain' }),
                'text/html': new Blob([wrapped], { type: 'text/html' }),
            })
        ]).then(() => true)
        .catch((err) => {
            console.warn('复制 HTML 失败:', err);
            return false;
        });
    } catch (e) {
        console.warn('ClipboardItem 构造异常:', e);
        return Promise.resolve(false);
    }
}

/**
 * extractPlainText 从 HTML 中提取纯文本（去除所有标签、转义字符）
 * @param {string} html
 * @returns {string}
 */
function extractPlainText(html) {
    const temp = document.createElement('div');
    temp.innerHTML = html;
    // 用 textContent 获取纯文本，替换连续空白为单个空格
    return (temp.textContent || '').replace(/\s+/g, ' ').trim();
}

/**
 * htmlToMarkdown 将 HTML 片段转换为 Markdown
 *
 * 使用全局 TurndownService（需在 index.html 中引入 turndown.min.js）。
 * 如果 TurndownService 不可用（如未加载），返回 null。
 *
 * @param {string} html HTML 片段
 * @returns {string|null} Markdown 文本，失败时返回 null
 */
export function htmlToMarkdown(html) {
    if (!html) return null;
    try {
        if (typeof TurndownService === 'undefined') {
            console.warn('TurndownService 未加载');
            return null;
        }
        const turndown = new TurndownService({
            headingStyle: 'atx',
            codeBlockStyle: 'fenced',
            emDelimiter: '*',
            bulletListMarker: '-',
        });
        return turndown.turndown(html);
    } catch (e) {
        console.warn('HTML 转 Markdown 失败:', e);
        return null;
    }
}

/**
 * showCopyFeedback 显示复制成功/失败的视觉反馈
 * @param {HTMLElement} btn - 被点击的按钮元素
 * @param {string} successText - 成功后显示的文本
 * @param {number} [resetDelay=2000]
 */
function showCopyFeedback(btn, successText, resetDelay) {
    const originalText = btn.textContent;
    const originalColor = btn.style.color;
    const originalBorderColor = btn.style.borderColor;

    if (successText) {
        btn.textContent = successText;
        btn.style.color = '#57C3C3';
        btn.style.borderColor = '#57C3C3';
    }

    setTimeout(() => {
        btn.textContent = originalText;
        btn.style.color = originalColor;
        btn.style.borderColor = originalBorderColor;
    }, resetDelay || 2000);
}
