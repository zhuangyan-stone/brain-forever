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
 * @param {Array<{label:string, formatKey:string}>} items - 菜单项
 * @param {object} [opts]
 * @param {string} [opts.position='bottom'] - 菜单弹出位置
 * @param {{text:string, markdown:string, html:string}} [opts.content] - 内容数据，用于菜单项点击时重新复制
 */
/**
 * 当前打开的菜单引用，用于 hover 切换
 */
let _openMenuAnchor = null;
let _openMenuEl = null;
let _menuHideTimer = null;

/**
 * 关闭当前打开的下拉菜单
 */
function closeDropdownMenu() {
    if (_openMenuEl) {
        _openMenuEl.remove();
        _openMenuEl = null;
    }
    // 恢复按钮的 tooltip
    if (_openMenuAnchor) {
        restoreTooltipAttr(_openMenuAnchor);
    }
    _openMenuAnchor = null;
    clearTimeout(_menuHideTimer);
    _menuHideTimer = null;
}

/**
 * 临时移除按钮的 data-tooltip 属性（避免 tooltip 与下拉菜单冲突）
 * 将原值保存在 dataset 中以便恢复
 * @param {HTMLElement} btn
 */
function suppressTooltipAttr(btn) {
    const val = btn.getAttribute('data-tooltip');
    if (val !== null) {
        btn.dataset._savedTooltip = val;
        btn.removeAttribute('data-tooltip');
    }
}

/**
 * 恢复按钮的 data-tooltip 属性
 * @param {HTMLElement} btn
 */
function restoreTooltipAttr(btn) {
    if (btn && btn.dataset._savedTooltip !== undefined) {
        btn.setAttribute('data-tooltip', btn.dataset._savedTooltip);
        delete btn.dataset._savedTooltip;
    }
}

/**
 * showDropdownMenu 在目标按钮下方显示一个下拉菜单
 * @param {HTMLElement} anchor - 触发菜单的按钮元素
 * @param {Array<{label:string, formatKey:string}>} items - 菜单项
 * @param {object} [opts]
 * @param {string} [opts.position='bottom'] - 菜单弹出位置
 * @param {{text:string, markdown:string, html:string}} [opts.content] - 内容数据，用于菜单项点击时重新复制
 */
function showDropdownMenu(anchor, items, opts) {
    // 如果已经是同一个 anchor，不重复创建
    if (_openMenuAnchor === anchor && _openMenuEl && document.body.contains(_openMenuEl)) {
        return;
    }

    // 移除已有的下拉菜单
    closeDropdownMenu();

    const menu = document.createElement('div');
    menu.className = 'copy-dropdown-menu';

    items.forEach((item) => {
        const option = document.createElement('div');
        option.className = 'copy-dropdown-item';
        option.textContent = item.label;
        option.dataset.formatKey = item.formatKey;
        option.addEventListener('click', (e) => {
            e.stopPropagation();
            closeDropdownMenu();
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

    _openMenuAnchor = anchor;
    _openMenuEl = menu;

    // 临时隐藏按钮的 tooltip，避免与下拉菜单冲突
    suppressTooltipAttr(anchor);

    // 点击外部关闭
    const closeHandler = (ev) => {
        if (!menu.contains(ev.target) && ev.target !== anchor) {
            closeDropdownMenu();
            document.removeEventListener('click', closeHandler, true);
            document.removeEventListener('scroll', scrollHandler, true);
        }
    };
    // 延迟绑定，避免立即触发
    setTimeout(() => {
        document.addEventListener('click', closeHandler, true);
    }, 0);

    // 页面滚动时自动关闭菜单
    const scrollHandler = () => {
        if (document.body.contains(menu)) {
            closeDropdownMenu();
            document.removeEventListener('click', closeHandler, true);
            document.removeEventListener('scroll', scrollHandler, true);
        }
    };
    // 延迟绑定，避免因初始渲染触发的 scroll 事件误关闭
    setTimeout(() => {
        document.addEventListener('scroll', scrollHandler, true);
    }, 0);
}

/**
 * 处理菜单项点击
 * @param {string} formatKey - 'plain' | 'markdown' | 'html'
 * @param {{text:string, markdown:string, html:string}|null} content
 */
function handleMenuItemClick(formatKey, content) {
    const formatName = FORMAT_NAMES[formatKey] || 'Markdown';

    // 菜单中已不显示当前默认格式，所以点击任何菜单项都是切换格式
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
 * 获取非当前默认格式的菜单项列表（当前格式不在菜单中显示）
 * @returns {Array<{label:string, formatKey:string}>}
 */
function getMenuItems() {
    const allFormats = ['plain', 'markdown', 'html'];
    return allFormats
        .filter(key => key !== defaultFormat)
        .map(formatKey => ({
            label: `复制为 ${FORMAT_NAMES[formatKey]}`,
            formatKey: formatKey,
        }));
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
 * 获取按钮对应的复制内容（代码块或消息）
 * @param {HTMLElement} btn
 * @returns {{text:string, markdown:string, html:string}|null}
 */
function getCopyContentForBtn(btn) {
    if (btn.classList.contains('code-copy-btn')) {
        const pre = btn.closest('pre');
        if (!pre) return null;
        const content = getCodeBlockContent(pre);
        return content.text ? content : null;
    }
    if (btn.classList.contains('copy-msg-btn')) {
        const messageEl = btn.closest('.message');
        if (!messageEl) return null;
        const content = getMessageContent(messageEl);
        return content.text ? content : null;
    }
    return null;
}

/**
 * 获取按钮对应的菜单位置
 * @param {HTMLElement} btn
 * @returns {'top'|'bottom'}
 */
function getDropdownPosition(btn) {
    return btn.classList.contains('code-copy-btn') ? 'top' : 'bottom';
}

/**
 * 初始化复制按钮和消息操作按钮的事件委托
 *
 * 交互逻辑：
 * - 点击复制按钮 → 执行默认格式复制（不弹出菜单）
 * - 鼠标悬停复制按钮 → 弹出格式选择下拉菜单
 * - 鼠标离开复制按钮/菜单 → 关闭下拉菜单（带短暂延迟，便于移动到菜单上点击）
 */
export function initCopyHandlers() {
    const chatContainer = document.getElementById('chatContainer');
    if (!chatContainer) return;

    // ============================================================
    // 点击处理：仅执行默认复制，不再弹出菜单
    // ============================================================

    // 事件委托：代码块复制按钮点击处理
    chatContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('.copy-btn.code-copy-btn');
        if (!btn) return;

        const content = getCopyContentForBtn(btn);
        if (!content) return;

        doDefaultCopy(content).then(ok => {
            const name = getDefaultFormatLabel();
            showToast(ok ? `✓ 已复制（${name}）` : `复制失败（${name}）`, ok ? 'success' : 'error', 2000);
        });
    });

    // 事件委托：消息操作按钮（复制消息 / 删除消息）
    chatContainer.addEventListener('click', (e) => {
        // 复制消息按钮
        const copyMsgBtn = e.target.closest('.copy-msg-btn');
        if (copyMsgBtn) {
            e.stopPropagation();
            const content = getCopyContentForBtn(copyMsgBtn);
            if (!content) return;

            doDefaultCopy(content).then(ok => {
                const name = getDefaultFormatLabel();
                showToast(ok ? `✓ 已复制（${name}）` : `复制失败（${name}）`, ok ? 'success' : 'error', 2000);
            });
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

    // ============================================================
    // Hover 处理：鼠标悬停时弹出格式选择下拉菜单
    // ============================================================

    let hoverTimer = null;

    chatContainer.addEventListener('mouseover', (e) => {
        const btn = e.target.closest('.copy-btn.code-copy-btn, .copy-msg-btn');
        if (!btn) {
            // 鼠标不在按钮上，但可能在菜单上，不处理
            return;
        }

        // 清除之前的定时器
        clearTimeout(hoverTimer);

        // 如果已经有菜单且指向同一个按钮，不重复创建
        if (_openMenuAnchor === btn && _openMenuEl && document.body.contains(_openMenuEl)) {
            return;
        }

        // 延迟 200ms 显示菜单，避免快速划过时闪烁
        hoverTimer = setTimeout(() => {
            const content = getCopyContentForBtn(btn);
            if (!content) return;

            // 关闭其他可能已打开的菜单
            closeDropdownMenu();

            const position = getDropdownPosition(btn);
            showDropdownMenu(btn, getMenuItems(), { position, content });
        }, 200);
    });

    // 鼠标离开时，延迟关闭菜单（给用户移动到菜单上的时间）
    chatContainer.addEventListener('mouseout', (e) => {
        const btn = e.target.closest('.copy-btn.code-copy-btn, .copy-msg-btn');
        if (!btn) return;

        // 检查鼠标是否移到了菜单上
        const relatedTarget = e.relatedTarget;
        if (relatedTarget && _openMenuEl && (_openMenuEl.contains(relatedTarget) || btn.contains(relatedTarget))) {
            return;
        }

        clearTimeout(hoverTimer);

        // 延迟关闭，给用户时间移动到菜单
        _menuHideTimer = setTimeout(() => {
            // 再次检查鼠标是否在按钮或菜单上
            if (_openMenuEl && !_openMenuEl.matches(':hover') && _openMenuAnchor && !_openMenuAnchor.matches(':hover')) {
                closeDropdownMenu();
            }
        }, 300);
    });

    // 菜单自身 hover 时取消关闭定时器
    document.addEventListener('mouseover', (e) => {
        if (_openMenuEl && _openMenuEl.contains(e.target)) {
            clearTimeout(_menuHideTimer);
            _menuHideTimer = null;
        }
    });

    // 菜单自身 mouseleave 时延迟关闭
    document.addEventListener('mouseout', (e) => {
        if (_openMenuEl && _openMenuEl.contains(e.target) && !_openMenuEl.contains(e.relatedTarget)) {
            _menuHideTimer = setTimeout(() => {
                if (_openMenuEl && !_openMenuEl.matches(':hover') && _openMenuAnchor && !_openMenuAnchor.matches(':hover')) {
                    closeDropdownMenu();
                }
            }, 300);
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
