// ============================================================
// clipboard.js — 剪贴板功能模块
// ============================================================

/**
 * 回退方案：使用 document.execCommand('copy') 复制文本
 *
 * 当 navigator.clipboard 不可用时（如 HTTP 非安全上下文），
 * 通过创建临时 textarea 元素实现复制。
 *
 * @param {string} text
 * @returns {boolean}
 */
function fallbackCopyText(text) {
    try {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        // 确保 textarea 不可见
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        textarea.style.left = '-9999px';
        textarea.style.top = '-9999px';
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        const success = document.execCommand('copy');
        document.body.removeChild(textarea);
        return success;
    } catch (e) {
        console.warn('回退复制失败:', e);
        return false;
    }
}

/**
 * 终极兜底：选中文本并提示用户手动复制
 *
 * 当所有自动复制方案都失败时，创建一个可见的文本区域，
 * 选中文本并提示用户按 Ctrl+C（或 Cmd+C）。
 *
 * @param {string} text
 * @returns {boolean}
 */
function ultimateFallbackCopy(text) {
    try {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.position = 'fixed';
        textarea.style.left = '0';
        textarea.style.top = '0';
        textarea.style.width = '100%';
        textarea.style.height = '80px';
        textarea.style.zIndex = '99999';
        textarea.style.fontSize = '14px';
        textarea.style.padding = '8px';
        textarea.style.border = '2px solid #57C3C3';
        textarea.style.backgroundColor = 'var(--bg-color, #fff)';
        textarea.style.color = 'var(--text-color, #333)';
        // 添加提示占位文本
        textarea.placeholder = '请按 Ctrl+C（或 Cmd+C）复制内容，然后关闭此区域';
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        // 3 秒后自动移除
        setTimeout(() => {
            if (textarea.parentNode) {
                document.body.removeChild(textarea);
            }
        }, 8000);
        return true;
    } catch (e) {
        console.warn('终极兜底复制失败:', e);
        return false;
    }
}

/**
 * 安全地获取 navigator.clipboard（仅在安全上下文中可用）
 * @returns {boolean}
 */
function isClipboardApiAvailable() {
    return typeof navigator !== 'undefined' &&
        navigator.clipboard &&
        typeof navigator.clipboard.writeText === 'function';
}

/**
 * copyPlainText 复制纯文本到剪贴板
 * @param {string} text
 * @returns {Promise<boolean>}
 */
export async function copyPlainText(text) {
    try {
        if (isClipboardApiAvailable()) {
            await navigator.clipboard.writeText(text);
            return true;
        }
        // 回退方案
        return fallbackCopyText(text);
    } catch (e) {
        console.warn('复制纯文本失败:', e);
        // 再尝试一次回退，仍失败则用终极兜底
        return fallbackCopyText(text) || ultimateFallbackCopy(text);
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
        if (isClipboardApiAvailable()) {
            await navigator.clipboard.writeText(markdown);
            return true;
        }
        return fallbackCopyText(markdown);
    } catch (e) {
        console.warn('复制 Markdown 失败:', e);
        // 再尝试一次回退，仍失败则用终极兜底
        return fallbackCopyText(markdown) || ultimateFallbackCopy(markdown);
    }
}

/**
 * copyHtml 复制 HTML 富文本到剪贴板
 *
 * 优先使用 ClipboardItem API 写入 text/html MIME 类型。
 * 如果 Clipboard API 不可用，回退到纯文本复制。
 *
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

    // 如果 Clipboard API 不可用，回退到纯文本复制
    if (!isClipboardApiAvailable()) {
        const ok = fallbackCopyText(plainText);
        return Promise.resolve(ok || ultimateFallbackCopy(plainText));
    }

    try {
        return navigator.clipboard.write([
            new ClipboardItem({
                'text/plain': new Blob([plainText], { type: 'text/plain' }),
                'text/html': new Blob([wrapped], { type: 'text/html' }),
            })
        ]).then(() => true)
        .catch((err) => {
            console.warn('复制 HTML 失败:', err);
            const ok = fallbackCopyText(plainText);
            return ok || ultimateFallbackCopy(plainText);
        });
    } catch (e) {
        console.warn('ClipboardItem 构造异常:', e);
        const ok = fallbackCopyText(plainText);
        return Promise.resolve(ok || ultimateFallbackCopy(plainText));
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
