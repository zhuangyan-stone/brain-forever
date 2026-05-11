// ============================================================
// chat-copy.js — 复制菜单 + 事件委托
// ============================================================

import { copyPlainText, copyMarkdown, copyHtml, htmlToMarkdown } from './clipboard.js';
import { state } from './chat-state.js';
import { showToast } from './chat-ui.js';
import { setActiveTick } from './chat-ticknav.js';
import { showDeleteModal } from './chat-delete.js';

'use strict';

/**
 * showDropdownMenu 在目标按钮下方显示一个下拉菜单
 * @param {HTMLElement} anchor - 触发菜单的按钮元素
 * @param {Array<{label:string, action:()=>void}>} items - 菜单项列表
 * @param {object} [opts]
 * @param {string} [opts.position='bottom'] - 菜单弹出位置
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
        option.textContent = item.label;
        option.addEventListener('click', (e) => {
            e.stopPropagation();
            menu.remove();
            item.action();
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
 * 生成一个下拉菜单项，用于复制指定格式的内容
 * @param {() => Promise<boolean>} copyFn - 执行复制操作的异步函数，返回是否成功
 * @param {string} formatName - 格式名称（如 "纯文本"、"Markdown"、"HTML"）
 * @returns {{label:string, action:()=>void}}
 */
function makeCopyMenuItem(copyFn, formatName) {
    return {
        label: `复制为 ${formatName}`,
        action: () => {
            copyFn().then(ok => {
                showToast(ok ? `✓ 已复制（${formatName}）` : `复制失败（${formatName}）`, ok ? 'success' : 'error', 2000);
            });
        },
    };
}

/**
 * 初始化复制按钮和消息操作按钮的事件委托
 */
export function initCopyHandlers() {
    const chatContainer = document.getElementById('chatContainer');
    if (!chatContainer) return;

    // 事件委托：复制按钮点击处理（代码块复制）
    // 在 chatContainer 上监听 click 事件，通过事件委托处理所有 .copy-btn 的点击
    // 这样即使 innerHTML 被替换，事件也不会丢失
    chatContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('.copy-btn');
        if (!btn) return;

        // 找到所属的 <pre> 代码块
        const pre = btn.closest('pre');
        if (!pre) return;

        // 获取代码块的纯文本内容（忽略高亮标签）
        const code = pre.querySelector('code');
        const text = code ? code.textContent : '';
        if (!text) return;

        // 获取语言
        const lang = pre.getAttribute('data-lang') || '';

        // 构建 Markdown 格式（代码围栏）
        const markdown = lang
            ? '```' + lang + '\n' + text + '\n```'
            : '```\n' + text + '\n```';

        // 构建干净的 HTML 格式（只包含代码块本身，不含复制按钮等 UI 元素）
        const codeEl = pre.querySelector('code');
        const highlightedHtml = codeEl ? codeEl.innerHTML : '';
        const html = highlightedHtml
            ? `<pre><code${lang ? ` class="language-${lang}"` : ''}>${highlightedHtml}</code></pre>`
            : '';

        // 显示下拉菜单
        showDropdownMenu(btn, [
            makeCopyMenuItem(() => copyPlainText(text), '纯文本'),
            makeCopyMenuItem(() => copyMarkdown(markdown), 'Markdown'),
            makeCopyMenuItem(() => copyHtml(html), 'HTML'),
        ], { position: 'top' });
    });

    // 事件委托：消息操作按钮（复制消息 / 删除消息）
    chatContainer.addEventListener('click', (e) => {
        // 复制消息按钮（带格式选择的下拉菜单）
        const copyMsgBtn = e.target.closest('.copy-msg-btn');
        if (copyMsgBtn) {
            e.stopPropagation();
            const messageEl = copyMsgBtn.closest('.message');
            if (!messageEl) return;
            const bubble = messageEl.querySelector('.bubble');
            const text = bubble ? bubble.textContent : '';
            if (!text) return;

            // 获取渲染后的 HTML
            const html = bubble ? bubble.innerHTML : '';

            // 获取原始 Markdown 源
            // 从 message-group 获取 data-msg-id（同一组的 user 和 assistant 共享同一 ID）
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
            // 如果 messages 中找不到，回退策略
            if (!markdown) {
                if (isUser) {
                    // 用户消息：原始输入即纯文本/简单 Markdown，直接用 textContent
                    markdown = text;
                } else {
                    // 助手消息：用 Turndown 从 HTML 反向转换
                    markdown = html ? htmlToMarkdown(html) : text;
                }
            }

            showDropdownMenu(copyMsgBtn, [
                makeCopyMenuItem(() => copyPlainText(text), '纯文本'),
                makeCopyMenuItem(() => copyMarkdown(markdown), 'Markdown'),
                makeCopyMenuItem(() => copyHtml(html), 'HTML'),
            ]);
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
}
