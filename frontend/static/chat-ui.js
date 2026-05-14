// ============================================================
// chat-ui.js — DOM 操作（addMessage、showSources、toast、错误显示等）
// ============================================================

import { escapeHtml, truncate } from './toolsets.js';
import { state } from './chat-state.js';
import { renderMarkdown } from './chat-markdown.js';

'use strict';

// DOM 元素引用（由 chat.js 初始化时设置）
export const dom = {
    chatContainer: null,
    scrollContainer: null,
    messageInput: null,
    sendBtn: null,
    toastContainer: null,
};

/**
 * 初始化 DOM 引用（由 chat.js 在页面加载后调用）
 */
export function initDom() {
    dom.chatContainer = document.getElementById('chatContainer');
    dom.scrollContainer = document.getElementById('scrollContainer');
    dom.messageInput = document.getElementById('messageInput');
    dom.sendBtn = document.getElementById('sendBtn');
    dom.toastContainer = document.getElementById('toastContainer');
}

/**
 * scrollToBottom 滚动到底部
 */
export function scrollToBottom() {
    requestAnimationFrame(() => {
        const sc = dom.scrollContainer || dom.chatContainer;
        sc.scrollTop = sc.scrollHeight;
    });
}

/**
 * setInputEnabled 启用/禁用输入
 * @param {boolean} enabled
 */
export function setInputEnabled(enabled) {
    dom.messageInput.disabled = !enabled;
    dom.sendBtn.disabled = !enabled;
    if (enabled) {
        dom.sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" fill="currentColor"/></svg>`;
    } else {
        dom.sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" style="animation:spin 1s linear infinite"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8z" fill="currentColor"/><path d="M12 6c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6z" fill="var(--bg-input)"/></svg>`;
    }
}

/**
 * updateDeleteButtons 更新所有删除按钮的禁用状态
 */
export function updateDeleteButtons() {
    const deleteBtns = dom.chatContainer.querySelectorAll('.delete-msg-btn');
    deleteBtns.forEach(btn => {
        btn.disabled = state.isStreaming;
    });
}

/**
 * showToast 显示一个自动消失的 Toast 消息
 * @param {string} message
 * @param {'error'|'success'|'info'} [type='error']
 * @param {number} [duration=4000] - 显示时长（毫秒）
 */
export function showToast(message, type, duration) {
    if (!dom.toastContainer) return;
    type = type || 'error';
    duration = duration || 4000;

    const toast = document.createElement('div');
    toast.className = 'toast toast-' + type;
    toast.textContent = message;
    dom.toastContainer.appendChild(toast);

    // 触发动画
    requestAnimationFrame(() => {
        toast.classList.add('show');
    });

    // 自动移除
    setTimeout(() => {
        toast.classList.remove('show');
        // 等过渡动画结束后移除 DOM
        setTimeout(() => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        }, 300);
    }, duration);
}

/**
 * showError 显示错误信息
 * @param {HTMLElement} assistantBubble
 * @param {string} message
 */
export function showError(assistantBubble, message) {
    // 如果 assistant 气泡是空的，直接显示错误
    const contentDiv = assistantBubble.querySelector('.bubble');
    if (contentDiv && !contentDiv.textContent.trim()) {
        contentDiv.innerHTML = `❌ ${escapeHtml(message)}`;
        contentDiv.classList.remove('streaming');
        assistantBubble.classList.add('error');
    } else {
        // 如果 assistant 气泡已有内容（如流式已开始），改用 toast 显示错误，
        // 避免创建无法删除的独立错误消息气泡
        showToast(message, 'error', 6000);
    }
    scrollToBottom();
}

/**
 * showTokenUsage 在 assistant 消息气泡下方显示 token 用量信息。
 * 如果 is_estimated 为 true，则附加提示说明为估算值。
 * @param {HTMLElement} assistantBubble - assistant 消息的 .message 元素
 * @param {object} usage - { prompt_tokens, completion_tokens, total_tokens, is_estimated }
 */
export function showTokenUsage(assistantBubble, usage) {
    // 移除已有的 token-info（防止重复添加）
    const existing = assistantBubble.querySelector('.token-info');
    if (existing) existing.remove();

    const info = document.createElement('div');
    info.className = 'token-info';

    const text = `提示 ${usage.prompt_tokens} + 生成 ${usage.completion_tokens} = ${usage.total_tokens}`;

    if (usage.is_estimated) {
        info.title = '当前大模型未返回 token 消耗数据，此处为估算值，供参考';
        info.innerHTML = `词元消耗：<span class="token-estimated-icon">⚠</span> ${text} <span class="token-estimated-label">(估算)</span>`;
    } else {
        info.textContent = `词元消耗：${text}`;
    }

    // 插入到 message-actions 内部（与操作按钮同行显示）
    const actions = assistantBubble.querySelector('.message-actions');
    if (actions) {
        actions.prepend(info);
    }
}

/**
 * addMessage 添加消息气泡到聊天区域
 * 用户消息（role='user'）会创建新的 .message-group 包裹层；
 * 助手消息（role='assistant'）追加到当前 .message-group 内。
 * 返回创建的 .message 元素。
 * @param {'user'|'assistant'} role
 * @param {string} content
 * @param {boolean} [isStreaming=false]
 * @returns {HTMLElement}
 */
export function addMessage(role, content, isStreaming = false) {
    const div = document.createElement('div');
    div.className = `message ${role}`;

    // 为用户消息添加 data-msg-index 属性，用于刻度导航定位
    if (role === 'user') {
        div.dataset.msgIndex = state.userMsgCount;
        state.userMsgCount++;

        // 创建新的消息组包裹层，添加到聊天区域
        const group = document.createElement('div');
        group.className = 'message-group';
        group.appendChild(div);

        // 为消息组添加左上角删除按钮
        const groupDeleteBtn = document.createElement('button');
        groupDeleteBtn.className = 'msg-action-btn delete-msg-btn group-delete-btn';
        groupDeleteBtn.title = '删除本组对话';
        groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
        groupDeleteBtn.disabled = state.isStreaming;
        group.appendChild(groupDeleteBtn);

        dom.chatContainer.appendChild(group);

        // 记录为当前组，后续 assistant 消息会追加到此组内
        state.currentGroup = group;
    } else {
        // assistant 消息：追加到当前消息组
        if (state.currentGroup) {
            state.currentGroup.appendChild(div);
        } else {
            // 兜底：没有当前组时（如欢迎消息），创建一个独立的消息组
            const group = document.createElement('div');
            group.className = 'message-group';
            group.appendChild(div);

            // 为消息组添加左上角删除按钮
            const groupDeleteBtn = document.createElement('button');
            groupDeleteBtn.className = 'msg-action-btn delete-msg-btn group-delete-btn';
            groupDeleteBtn.title = '删除本组消息';
            groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
            groupDeleteBtn.disabled = state.isStreaming;
            group.appendChild(groupDeleteBtn);

            dom.chatContainer.appendChild(group);
            state.currentGroup = group;
        }
    }

    const inner = document.createElement('div');
    inner.className = 'message-inner';

    // 角色标签
    const label = document.createElement('div');
    label.className = 'role-label';
    label.textContent = role === 'user' ? '我' : '🤖 AI';
    if (role === 'assistant') {
        label.classList.add('role-label-ai');
    }
    inner.appendChild(label);

    // 气泡
    const bubble = document.createElement('div');
    bubble.className = 'bubble';
    if (isStreaming) {
        // 流式输出时用 textContent 保留原始 Markdown
        bubble.textContent = content;
        bubble.classList.add('streaming');
    } else {
        // 非流式（如欢迎消息）直接渲染 Markdown
        bubble.innerHTML = renderMarkdown(content);
    }
    inner.appendChild(bubble);

    // 添加操作按钮（仅复制按钮），放在气泡下方
    const actions = document.createElement('div');
    actions.className = 'message-actions';

    // 复制按钮
    const copyBtn = document.createElement('button');
    copyBtn.className = 'msg-action-btn copy-msg-btn';
    copyBtn.title = '复制当前消息内容';
    copyBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    actions.appendChild(copyBtn);

    inner.appendChild(actions);

    div.appendChild(inner);

    scrollToBottom();
    return div;
}

/**
 * showSources 显示引用来源面板
 * @param {Array} sources - 来源数据
 * @param {'rag'|'web'} type - 'rag' 表示知识库引用，'web' 表示联网搜索结果
 */
export function showSources(sources, type) {
    if (!sources || sources.length === 0) return;

    // 知识库引用只显示相似度超过 60% 的
    if (type === 'rag') {
        sources = sources.filter(src => src.score > 0.6);
        if (sources.length === 0) return;
    }

    // 获取当前消息组（最后一个 .message-group）
    const lastGroup = dom.chatContainer.querySelector('.message-group:last-child');
    if (!lastGroup) return; // 没有消息组时不做任何事

    // 在当前消息组内查找或创建 sources 面板（每个组独立）
    let panel = lastGroup.querySelector('.sources-panel');
    if (!panel) {
        panel = document.createElement('div');
        panel.className = 'sources-panel';
        // 将面板插入到组内 assistant 消息之后（组内最后一个 .message 之后）
        const lastMsg = lastGroup.querySelector('.message:last-child');
        if (lastMsg) {
            lastMsg.insertAdjacentElement('afterend', panel);
        } else {
            lastGroup.appendChild(panel);
        }
    }

    // 强制搜索按钮的 globe 图标（缩小版）
    const globeIconSvg = '<svg class="sources-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>';

    const section = document.createElement('div');
    section.className = 'sources-section';

    if (type === 'rag') {
        // ---- 知识库引用 ----
        const title = document.createElement('div');
        title.className = 'sources-title sources-collapsible';
        title.innerHTML = `${globeIconSvg} 参考了以下知识库内容`;
        title.setAttribute('role', 'button');
        title.tabIndex = 0;
        section.appendChild(title);

        const body = document.createElement('div');
        body.className = 'sources-body';
        body.style.display = 'none'; // 默认折叠

        sources.forEach((src) => {
            const item = document.createElement('div');
            item.className = 'source-item';
            item.innerHTML = `
                <span class="source-title">${escapeHtml(src.title)}</span>
                <span class="source-score">相似度: ${(src.score * 100).toFixed(1)}%</span>
                ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)">${escapeHtml(truncate(src.content, 100))}</div>` : ''}
            `;
            body.appendChild(item);
        });

        section.appendChild(body);

        // 点击标题切换折叠
        title.addEventListener('click', () => toggleSourcesSection(title, body));
        title.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                toggleSourcesSection(title, body);
            }
        });
    } else if (type === 'web') {
        // ---- 联网搜索结果（分页显示） ----
        // 分组：URL 非空的排在前面，URL 为空的排在后面
        const withUrl = sources.filter(s => s.url);
        const withoutUrl = sources.filter(s => !s.url);
        sources = withUrl.concat(withoutUrl);
        const PAGE_SIZE = 5;
        const totalPages = Math.ceil(sources.length / PAGE_SIZE);
        let currentPage = 0;

        const title = document.createElement('div');
        title.className = 'sources-title sources-collapsible';
        title.innerHTML = `${globeIconSvg} 参考了 ${sources.length} 个联网搜索结果`;
        title.setAttribute('role', 'button');
        title.tabIndex = 0;
        section.appendChild(title);

        const body = document.createElement('div');
        body.className = 'sources-body';
        body.style.display = 'none'; // 默认折叠

        // 容器：用于存放当前页的条目，切换页时清空重建
        const itemsContainer = document.createElement('div');
        itemsContainer.className = 'sources-items-container';
        body.appendChild(itemsContainer);

        // 分页圆点导航（底部居中）
        const dotsNav = document.createElement('div');
        dotsNav.className = 'sources-pagination-dots';
        body.appendChild(dotsNav);

        /**
         * 渲染指定页码的条目和圆点状态
         */
        function renderPage(page) {
            currentPage = page;

            // 清空条目容器
            itemsContainer.innerHTML = '';

            const start = page * PAGE_SIZE;
            const end = Math.min(start + PAGE_SIZE, sources.length);
            const pageSources = sources.slice(start, end);

            pageSources.forEach((src) => {
                console.log('[sources-panel] source item:', { title: src.title, content: src.content, publish_date: src.publish_date, url: src.url, site_name: src.site_name });
                const item = document.createElement('div');
                item.className = 'source-item';

                // 清理标题：去除标题中重复的网站名称以及搜索引擎附加的 "（发布时间：XXXX）" 后缀
                let cleanTitle = src.title || '';
                // 先去除 "（发布时间：XXXX）" 或 "(发布时间：XXXX)" 后缀
                cleanTitle = cleanTitle.replace(/[（(]发布时间：.*?[）)]/g, '').trim();
                const siteName = src.site_name || '';
                if (siteName) {
                    if (cleanTitle.startsWith(siteName)) {
                        cleanTitle = cleanTitle.slice(siteName.length);
                    }
                    if (cleanTitle.endsWith(siteName)) {
                        cleanTitle = cleanTitle.slice(0, -siteName.length);
                    }
                    cleanTitle = cleanTitle.replace(/^[\s\-_:—：]+|[\s\-_:—：]+$/g, '');
                }
                if (!cleanTitle) {
                    cleanTitle = src.title ? src.title.replace(/[（(]发布时间：.*?[）)]/g, '').trim() : '';
                }

                let siteBadgeHtml = '';
                if (src.site_icon || src.site_name) {
                    const iconHtml = src.site_icon
                        ? `<img class="source-site-icon" src="${escapeHtml(src.site_icon)}" alt="" loading="lazy" onerror="this.style.display='none'">`
                        : '';
                    const nameHtml = src.site_name
                        ? `<span class="source-site-name">${escapeHtml(src.site_name)}</span>`
                        : '';
                    if (iconHtml || nameHtml) {
                        siteBadgeHtml = `<span class="source-site-badge">${iconHtml}${nameHtml}</span>`;
                    }
                }

                // URL 为空时，标题不加链接效果（纯文本显示）
                const titleHtml = src.url
                    ? `<a class="source-title source-link" href="${escapeHtml(src.url)}" target="_blank" rel="noopener">${escapeHtml(cleanTitle)}</a>`
                    : `<span class="source-title">${escapeHtml(cleanTitle)}</span>`;

                // 发布时间格式化为 [发布于：XXXX]
                const publishHtml = src.publish_date
                    ? `<span style="color:var(--text-muted);font-size:0.75rem;display:block;margin-top:4px">[发布于：${escapeHtml(src.publish_date)}]</span>`
                    : '';

                item.innerHTML = `
                    <div class="source-title-row">
                        ${titleHtml}
                        ${siteBadgeHtml}
                    </div>
                    ${publishHtml}
                    ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)">${escapeHtml(truncate(src.content, 100))}</div>` : ''}
                `;
                itemsContainer.appendChild(item);
            });

            // 重建圆点导航（仅当有多页时显示）
            dotsNav.innerHTML = '';
            if (totalPages > 1) {
                for (let i = 0; i < totalPages; i++) {
                    const dot = document.createElement('span');
                    dot.className = 'sources-pagination-dot' + (i === currentPage ? ' active' : '');
                    dot.dataset.page = i;
                    dot.addEventListener('click', () => {
                        renderPage(i);
                    });
                    dotsNav.appendChild(dot);
                }
            }
        }

        // 初始渲染第 0 页
        renderPage(0);

        section.appendChild(body);

        // 点击标题切换折叠
        title.addEventListener('click', () => toggleSourcesSection(title, body));
        title.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                toggleSourcesSection(title, body);
            }
        });
    }

    panel.appendChild(section);
    scrollToBottom();
}

/**
 * toggleSourcesSection 切换引用来源区域的折叠/展开
 * @param {HTMLElement} titleEl
 * @param {HTMLElement} bodyEl
 */
function toggleSourcesSection(titleEl, bodyEl) {
    const isCollapsed = bodyEl.style.display === 'none';
    bodyEl.style.display = isCollapsed ? '' : 'none';
    titleEl.classList.toggle('collapsed', !isCollapsed);
    titleEl.classList.toggle('expanded', isCollapsed);
}

/**
 * updateHeaderTitle 更新 header 左侧的对话标题
 * @param {string} title
 */
export function updateHeaderTitle(title) {
    const el = document.getElementById('headerTitle');
    if (el) {
        el.textContent = title;
    }
    state.dialogTitle = title;
}

/**
 * showWelcomeMessage 显示独立于消息系统的欢迎信息
 */
export function showWelcomeMessage() {
    // 避免重复添加
    if (dom.chatContainer.querySelector('.welcome-message')) return;

    const el = document.createElement('div');
    el.className = 'welcome-message';
    el.textContent = '你好！我是脑力永恒 AI 助手，基于知识库的智能对话系统。请问有什么可以帮助你的？';
    dom.chatContainer.appendChild(el);

    // 将输入区域移动到欢迎消息内部，使二者作为一个整体居中
    const inputArea = document.querySelector('.input-area');
    if (inputArea) {
        el.appendChild(inputArea);
    }

    // 标记欢迎状态（新布局下标记在 .scroll-container 上）
    const scrollContainer = document.getElementById('scrollContainer');
    if (scrollContainer) {
        scrollContainer.classList.add('welcome-state');
    }

    // 设置 header 标题为欢迎语
    updateHeaderTitle('欢迎开始新对话');
}

/**
 * 停止所有 web-search-hint 的闪烁动画
 * @param {HTMLElement} assistantBubble
 */
export function stopSearchHintsAnimation(assistantBubble) {
    const hints = assistantBubble.querySelectorAll('.web-search-hint');
    hints.forEach(h => h.style.animation = 'none');
}
