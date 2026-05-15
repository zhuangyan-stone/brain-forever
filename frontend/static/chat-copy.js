// ============================================================
// chat-copy.js — 复制菜单 + 事件委托
// ============================================================

import { copyPlainText, copyMarkdown, copyHtml, htmlToMarkdown } from './clipboard.js';
import { state } from './chat-state.js';
import { showToast } from './chat-ui.js';
import { setActiveTick } from './chat-ticknav.js';
import { showDeleteModal } from './chat-delete.js';

'use strict';

// ============================================================
// localStorage 键名
// ============================================================
const LS_KEY_DEFAULT_FORMAT = 'brainforever_default_copy_format';

// ============================================================
// 默认复制格式状态
// ============================================================
// 可选值: 'plain', 'markdown', 'html'
const FORMAT_NAMES = {
    plain: '纯文本',
    markdown: 'Markdown',
    html: 'HTML',
};

/** 当前默认复制格式，初始为 markdown */
let defaultFormat = loadDefaultFormat();

/**
 * 从 localStorage 加载默认复制格式
 * @returns {'plain'|'markdown'|'html'}
 */
function loadDefaultFormat() {
    try {
        const saved = localStorage.getItem(LS_KEY_DEFAULT_FORMAT);
        if (saved && FORMAT_NAMES[saved]) {
            return saved;
        }
    } catch (_) {
        // localStorage 不可用时使用默认值
    }
    return 'markdown';
}

/**
 * 将默认复制格式保存到 localStorage
 */
function saveDefaultFormat() {
    try {
        localStorage.setItem(LS_KEY_DEFAULT_FORMAT, defaultFormat);
    } catch (_) {
        // localStorage 不可用时忽略
    }
}

/**
 * 更新所有复制按钮上的格式标签文字
 */
function updateAllCopyBtnLabels() {
    const label = getDefaultFormatLabel();
    document.querySelectorAll('.copy-msg-btn .copy-btn-label').forEach(el => {
        el.textContent = `复制为 ${label}`;
    });
    document.querySelectorAll('.copy-btn.code-copy-btn .copy-btn-label').forEach(el => {
        el.textContent = `复制为 ${label}`;
    });
}

/**
 * showDropdownMenu 在目标按钮下方显示一个下拉菜单
 * @param {HTMLElement} anchor - 触发菜单的按钮元素
 * @param {Array<{label:string, formatKey:string, isDefault:boolean}>} items - 菜单项
 * @param {object} [opts]
 * @param {string} [opts.position='bottom'] - 菜单弹出位置
 * @param {{text:string, markdown:string, html:string}} [opts.content] - 内容数据，用于菜单项点击时重新复制
 */
function showDropdownMenu(anchor, items, opts) {
    // 移除已有的下拉菜单
    const existing = document.querySelector('.copy-dropdown-menu');
    if (existing) existing.remove();

    const menu = document.createElement('div');
    menu.className = 'copy-dropdown-menu';

    items.forEach((item) => {
        const option = document.createElement('div');
        option.className = 'copy-dropdown-item';
        if (item.isDefault) {
            option.classList.add('default');
        }
        option.textContent = item.label;
        option.dataset.formatKey = item.formatKey;
        option.addEventListener('click', (e) => {
            e.stopPropagation();
            menu.remove();
            // 菜单项点击处理：由 handleMenuItemClick 统一处理
            handleMenuItemClick(item.formatKey, opts && opts.content);
        });
        menu.appendChild(option);
    });

    // 定位
    const rect = anchor.getBoundingClientRect();
    const position = (opts && opts.position) || 'bottom';
    if (position === 'bottom') {
        menu.style.top = (rect.bottom + 4) + 'px';
        menu.style.left = rect.left + 'px';
    } else {
        menu.style.bottom = (window.innerHeight - rect.top + 4) + 'px';
        menu.style.left = rect.left + 'px';
    }
    menu.style.position = 'fixed';
    menu.style.zIndex = '9999';

    document.body.appendChild(menu);

    // 点击外部关闭
    const closeHandler = (ev) => {
        if (!menu.contains(ev.target) && ev.target !== anchor) {
            menu.remove();
            document.removeEventListener('click', closeHandler, true);
        }
    };
    // 延迟绑定，避免立即触发
    setTimeout(() => {
        document.addEventListener('click', closeHandler, true);
    }, 0);
}

/**
 * 处理菜单项点击
 * @param {string} formatKey - 'plain' | 'markdown' | 'html'
 * @param {{text:string, markdown:string, html:string}|null} content
 */
function handleMenuItemClick(formatKey, content) {
    const formatName = FORMAT_NAMES[formatKey] || 'Markdown';

    // 如果用户选的是当前默认格式（且是 markdown），不实际复制，只弹成功提示
    if (formatKey === defaultFormat) {
        showToast(`✓ 已复制（${formatName}）`, 'success', 2000);
        return;
    }

    // 用户选了其他格式：更新默认格式并持久化
    defaultFormat = formatKey;
    saveDefaultFormat();
    updateAllCopyBtnLabels();

    // 执行新格式的复制
    if (content) {
        let copyPromise;
        switch (formatKey) {
            case 'plain':
                copyPromise = copyPlainText(content.text);
                break;
            case 'html':
                copyPromise = copyHtml(content.html);
                break;
            case 'markdown':
            default:
                copyPromise = copyMarkdown(content.markdown);
                break;
        }
        copyPromise.then(ok => {
            showToast(ok ? `✓ 已复制（${formatName}）` : `复制失败（${formatName}）`, ok ? 'success' : 'error', 2000);
        });
    }
}

/**
 * 生成菜单项配置
 * @param {string} formatKey - 'plain' | 'markdown' | 'html'
 * @returns {{label:string, formatKey:string, isDefault:boolean}}
 */
function makeMenuItem(formatKey) {
    return {
        label: `复制为 ${FORMAT_NAMES[formatKey]}`,
        formatKey: formatKey,
        isDefault: formatKey === defaultFormat,
    };
}

/**
 * 获取消息的纯文本、Markdown 源和 HTML
 * @param {HTMLElement} messageEl - .message 元素
 * @returns {{text:string, markdown:string, html:string}}
 */
function getMessageContent(messageEl) {
    const bubble = messageEl.querySelector('.bubble');
    const text = bubble ? bubble.textContent : '';
    const html = bubble ? bubble.innerHTML : '';

    // 获取原始 Markdown 源
    const group = messageEl.closest('.message-group');
    const msgId = group ? group.dataset.msgId : null;
    const isUser = messageEl.classList.contains('user');
    const role = isUser ? 'user' : 'assistant';
    let markdown = null;
    if (msgId) {
        const msg = state.messages.find(m => String(m.id) === msgId && m.role === role);
        if (msg && msg.content) {
            markdown = msg.content;
        }
    }
    if (!markdown) {
        if (isUser) {
            markdown = text;
        } else {
            markdown = html ? htmlToMarkdown(html) : text;
        }
    }

    return { text, markdown, html };
}

/**
 * 获取代码块内容的纯文本、Markdown 和 HTML
 * @param {HTMLElement} pre - <pre> 元素
 * @returns {{text:string, markdown:string, html:string}}
 */
function getCodeBlockContent(pre) {
    const code = pre.querySelector('code');
    const text = code ? code.textContent : '';
    const lang = pre.getAttribute('data-lang') || '';
    const markdown = lang
        ? '```' + lang + '\n' + text + '\n```'
        : '```\n' + text + '\n```';
    const codeEl = pre.querySelector('code');
    const highlightedHtml = codeEl ? codeEl.innerHTML : '';
    const html = highlightedHtml
        ? `<pre><code${lang ? ` class="language-${lang}"` : ''}>${highlightedHtml}</code></pre>`
        : '';
    return { text, markdown, html };
}

/**
 * 执行默认复制操作（根据 defaultFormat）
 * @param {{text:string, markdown:string, html:string}} content
 * @returns {Promise<boolean>}
 */
async function doDefaultCopy(content) {
    switch (defaultFormat) {
        case 'plain':
            return await copyPlainText(content.text);
        case 'html':
            return await copyHtml(content.html);
        case 'markdown':
        default:
            return await copyMarkdown(content.markdown);
    }
}

/**
 * 初始化复制按钮和消息操作按钮的事件委托
 */
export function initCopyHandlers() {
    const chatContainer = document.getElementById('chatContainer');
    if (!chatContainer) return;

    // 事件委托：代码块复制按钮点击处理
    chatContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('.copy-btn.code-copy-btn');
        if (!btn) return;

        const pre = btn.closest('pre');
        if (!pre) return;

        const content = getCodeBlockContent(pre);
        if (!content.text) return;

        // 先执行默认复制
        doDefaultCopy(content).then(ok => {
            const name = getDefaultFormatLabel();
            showToast(ok ? `✓ 已复制（${name}）` : `复制失败（${name}）`, ok ? 'success' : 'error', 2000);
        });

        // 再弹出菜单
        showDropdownMenu(btn, [
            makeMenuItem('plain'),
            makeMenuItem('markdown'),
            makeMenuItem('html'),
        ], { position: 'top', content: content });
    });

    // 事件委托：消息操作按钮（复制消息 / 删除消息）
    chatContainer.addEventListener('click', (e) => {
        // 复制消息按钮（带格式选择的下拉菜单）
        const copyMsgBtn = e.target.closest('.copy-msg-btn');
        if (copyMsgBtn) {
            e.stopPropagation();
            const messageEl = copyMsgBtn.closest('.message');
            if (!messageEl) return;

            const content = getMessageContent(messageEl);
            if (!content.text) return;

            // 先执行默认复制
            doDefaultCopy(content).then(ok => {
                const name = getDefaultFormatLabel();
                showToast(ok ? `✓ 已复制（${name}）` : `复制失败（${name}）`, ok ? 'success' : 'error', 2000);
            });

            // 再弹出菜单
            showDropdownMenu(copyMsgBtn, [
                makeMenuItem('plain'),
                makeMenuItem('markdown'),
                makeMenuItem('html'),
            ], { content: content });
            return;
        }

        // 删除消息按钮（组级删除按钮，直接挂在 .message-group 下）
        const deleteMsgBtn = e.target.closest('.delete-msg-btn');
        if (deleteMsgBtn) {
            e.stopPropagation();

            const group = deleteMsgBtn.closest('.message-group');
            if (!group) return;

            // 找到该组中的用户消息，获取 data-msg-index
            const userMsg = group.querySelector('.message.user');
            if (!userMsg) return;

            const msgIndex = parseInt(userMsg.dataset.msgIndex, 10);
            if (isNaN(msgIndex) || msgIndex < 0) return;

            // 设置活动刻度索引并显示删除确认框
            state.activeTickIndex = msgIndex;
            setActiveTick(msgIndex);
            showDeleteModal();
            return;
        }
    });

    // 初始化时更新所有复制按钮标签，以匹配 localStorage 中保存的默认格式
    updateAllCopyBtnLabels();
}

/**
 * 获取当前默认格式的显示名称（导出供其他模块使用）
 * @returns {string}
 */
export function getDefaultFormatLabel() {
    return FORMAT_NAMES[defaultFormat] || 'Markdown';
}

/**
 * 设置默认复制格式
 * @param {'plain'|'markdown'|'html'} format
 */
export function setDefaultFormat(format) {
    if (FORMAT_NAMES[format]) {
        defaultFormat = format;
        updateAllCopyBtnLabels();
    }
}

/**
 * 获取当前默认复制格式
 * @returns {'plain'|'markdown'|'html'}
 */
export function getDefaultFormat() {
    return defaultFormat;
}
