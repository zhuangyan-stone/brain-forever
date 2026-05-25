// ============================================================
// chat-ui.js — DOM 操作（addMessage、showSources、toast、错误显示等）
// ============================================================

import { escapeHtml, truncate } from './toolsets.js';
import { updateCurrentChatTitle } from './chat-list.js';
import { state } from './chat-state.js';
import { renderMarkdown } from './chat-markdown.js';
import { SwipePager } from './components/swipe-pager.js';
import { getDefaultFormatLabel } from './chat-copy.js';
import { ICON_COPY, ICON_SEND, ICON_SPINNER, ICON_DELETE, ICON_GLOBE } from './svg_icons.js';

'use strict';

/** 判断"滚动到底部"的误差容限（px），底部剩余内容小于此值即视为已到底 */
export const SCROLL_BOTTOM_THRESHOLD = 4;

// DOM 元素引用（由 chat.js 初始化时设置）
export const dom = {
    chatContainer: null,
    scrollContainer: null,
    messageInput: null,
    sendBtn: null,
    toastContainer: null,
    inputArea: null,
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
    dom.inputArea = document.querySelector('.input-area');
}

/**
 * scrollToBottom 滚动到底部
 * 如果用户已手动向上滚动（userScrolledUp === true），则不执行自动滚动
 */
export function autoScrollToBottom() {
    const sc = dom.scrollContainer;
    const scrollHeight = sc.scrollHeight;
    const scrollTop = sc.scrollTop;
    const clientHeight = sc.clientHeight;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < SCROLL_BOTTOM_THRESHOLD;

    if (state.userScrolledUp) {
        return;
    }

    sc.scrollTop = scrollHeight;
}

/**
 * isScrolledToBottom 检测页面是否已滚动到底部（允许 SCROLL_BOTTOM_THRESHOLD px 误差）
 * @returns {boolean}
 */
export function isScrolledToBottom() {
    const sc = dom.scrollContainer;
    return sc.scrollHeight - sc.scrollTop - sc.clientHeight < SCROLL_BOTTOM_THRESHOLD;
}

/**
 * throttleRender 节流渲染 + 自动滚动
 * 对累积的文本做节流的 Markdown 渲染，渲染后自动滚动到底部。
 * timerHolder 可以是 DOM 元素或 state 对象，只要拥有 renderTimer 属性即可，
 * 从而实现 timer 的天然隔离（reasoning 存在 contentEl，content 存在 state）。
 * @param {{ renderTimer: number|null }} timerHolder - 持有 renderTimer 的对象
 * @param {HTMLElement} targetEl - 渲染目标元素（设置 innerHTML）
 * @param {() => string} getText - 返回当前需要渲染的文本
 */
export function throttleRender(timerHolder, targetEl, getText) {
    if (timerHolder.renderTimer) return;
    timerHolder.renderTimer = setTimeout(() => {
        timerHolder.renderTimer = null;
        targetEl.innerHTML = renderMarkdown(getText());
        autoScrollToBottom();
    }, state.RENDER_INTERVAL);
}

/**
 * setInputEnabled 启用/禁用输入
 * 当 enabled=false（流式输出中）时，发送按钮变为停止按钮（红色方块），
 * 点击可中断 LLM 生成。停止按钮不可 disabled，始终保持可点击。
 * @param {boolean} enabled
 * @private
 */
function setInputEnabled(enabled) {
    dom.messageInput.disabled = !enabled;
    if (dom.inputArea) {
        dom.inputArea.classList.toggle('streaming', !enabled);
    }
    if (enabled) {
        dom.sendBtn.disabled = false;
        dom.sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20">${ICON_SEND}</svg>`;
        dom.sendBtn.classList.remove('stop-btn');
        dom.sendBtn.dataset.tooltip = '发送';
    } else {
        // 流式输出中：显示停止按钮（红色方块），按钮保持可点击
        dom.sendBtn.disabled = false;
        dom.sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`;
        dom.sendBtn.classList.add('stop-btn');
        dom.sendBtn.dataset.tooltip = '停止生成';
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
 * applyStreamingState 统一管理流式输出中所有 UI 组件的禁用状态。
 *
 * 当 isStreaming=true（流式输出中）：
 *   - 输入框 disabled，发送按钮变为停止按钮（红色可点击）
 *   - 停止按钮（折叠模式）可点击
 *   - AI 标题按钮、登录按钮、新对话按钮 disabled
 *   - 所有删除按钮 disabled
 * 当 isStreaming=false（流式结束）：
 *   - 输入框启用，停止按钮恢复为发送按钮
 *   - 停止按钮（折叠模式）disabled 灰色
 *   - AI 标题按钮、登录按钮、新对话按钮启用
 *   - 所有删除按钮启用
 *
 * 替代 chat-sse.js 中散落的 8 个 enable/disable 函数，集中一处管理。
 * @param {boolean} isStreaming
 */
export function applyStreamingState(isStreaming) {
    // 1. 输入框 + 发送/停止按钮（复用已有逻辑）
    setInputEnabled(!isStreaming);

    // 2. 停止按钮（折叠模式下的独立中断按钮）
    const stopStreamingBtn = document.getElementById('stopStreamingBtn');
    if (stopStreamingBtn) {
        stopStreamingBtn.disabled = !isStreaming;
    }

    // 3. AI 标题按钮
    const aiTitleBtn = document.getElementById('aiTitleBtn');
    if (aiTitleBtn) aiTitleBtn.disabled = isStreaming;

    // 4. 登录按钮
    const loginBtn = document.getElementById('loginBtn');
    if (loginBtn) loginBtn.disabled = isStreaming;

    // 5. 新对话按钮
    const newChatBtn = document.getElementById('newChatBtn');
    if (newChatBtn) newChatBtn.disabled = isStreaming;

    // 6. 所有删除按钮
    updateDeleteButtons();
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
 * showError 通过 Toast 显示错误信息，同时在控制台输出出错信息
 * @param {HTMLElement} assistantBubble
 * @param {string} message
 */
export function showError(assistantBubble, message) {
    console.error('[SSE Error]', message);
    showToast(message, 'error', 6000);
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
        info.dataset.tooltip = '当前大模型未返回 token 消耗数据，此处为估算值，供参考';
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
 * @param {string|null} [createdAt=null] - ISO 格式时间戳，用于在角色标签后显示时间
 * @param {boolean} [isStreaming=false]
 * @returns {HTMLElement}
 */
export function addMessage(role, content, createdAt = null, isStreaming = false) {
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
        groupDeleteBtn.dataset.tooltip = '删除本组对话';
        groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg>';
        groupDeleteBtn.disabled = state.isStreaming;
        group.appendChild(groupDeleteBtn);

        dom.chatContainer.appendChild(group);

        // user 消息创建的新组即当前组，后续 assistant 消息会追加到此组内
        // 通过 dom.chatContainer.querySelector('.message-group:last-child') 查找
    } else {
        // assistant 消息：追加到当前消息组（即 DOM 中最后一个 .message-group）
        const lastGroup = dom.chatContainer.querySelector('.message-group:last-child');
        if (lastGroup) {
            lastGroup.appendChild(div);
        } else {
            // 兜底：没有当前组时（如欢迎消息），创建一个独立的消息组
            const group = document.createElement('div');
            group.className = 'message-group';
            group.appendChild(div);

            // 为消息组添加左上角删除按钮
            const groupDeleteBtn = document.createElement('button');
            groupDeleteBtn.className = 'msg-action-btn delete-msg-btn group-delete-btn';
            groupDeleteBtn.dataset.tooltip = '删除本组对话';
            groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg>';
            groupDeleteBtn.disabled = state.isStreaming;
            group.appendChild(groupDeleteBtn);

            dom.chatContainer.appendChild(group);
        }
    }

    const inner = document.createElement('div');
    inner.className = 'message-inner';

    // 角色标签（含时间）
    const label = document.createElement('div');
    label.className = 'role-label';
    const roleText = role === 'user' ? '我' : '🤖 AI';
    if (createdAt) {
        const d = new Date(createdAt);
        const hh = String(d.getHours()).padStart(2, '0');
        const mm = String(d.getMinutes()).padStart(2, '0');
        const ss = String(d.getSeconds()).padStart(2, '0');
        label.textContent = `${roleText} (${hh}:${mm}:${ss})`;
    } else {
        label.textContent = roleText;
    }
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
    copyBtn.dataset.tooltip = '复制当前消息内容';
    copyBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_COPY + '</svg><span class="copy-btn-label">复制为 ' + getDefaultFormatLabel() + '</span>';
    actions.appendChild(copyBtn);

    inner.appendChild(actions);

    div.appendChild(inner);

    autoScrollToBottom();
    return div;
}

/**
 * showSources 显示引用来源面板
 *
 * 幂等保证：同一个 message-group 中，同一类型（rag/web）的 sources section
 * 只会存在一份。如果已存在则更新其内容，不存在则创建。
 * 避免因多次调用导致同一批 sources 重复显示。
 *
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

    // 查找是否已有同类型的 section，有则复用（更新内容），无则创建
    let section = panel.querySelector(`.sources-section[data-source-type="${type}"]`);
    const isNew = !section;
    if (isNew) {
        section = document.createElement('div');
        section.className = 'sources-section';
        section.dataset.sourceType = type;
    }

    // 强制搜索按钮的 globe 图标（缩小版）
    const globeIconSvg = '<svg class="sources-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + ICON_GLOBE + '</svg>';

    if (type === 'rag') {
        // ---- 知识库引用 ----
        // 复用 section 时清空内容重新填充
        section.innerHTML = '';

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

        // 复用 section 时清空内容重新填充
        section.innerHTML = '';

        const title = document.createElement('div');
        title.className = 'sources-title sources-collapsible';
        title.innerHTML = `${globeIconSvg} 参考了 ${sources.length} 个联网搜索结果`;
        title.setAttribute('role', 'button');
        title.tabIndex = 0;
        section.appendChild(title);

        const body = document.createElement('div');
        body.className = 'sources-body';
        body.style.display = 'none'; // 默认折叠

        // 容器：用于存放当前页的条目（内含 slider 实现滑动动效）
        const itemsContainer = document.createElement('div');
        itemsContainer.className = 'sources-items-container';
        body.appendChild(itemsContainer);

        /**
         * 构建单个 source 条目的 DOM 元素
         */
        function createSourceItem(src) {
            const item = document.createElement('div');
            item.className = 'source-item';

            // 清理标题：去除标题中重复的网站名称以及搜索引擎附加的 "（发布时间：XXXX）" 后缀
            let cleanTitle = src.title || '';
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
                    ? `<img class="source-site-icon" src="${escapeHtml(src.site_icon)}" alt="" onerror="this.style.display='none'" decoding="async">`
                    : '';
                const nameHtml = src.site_name
                    ? `<span class="source-site-name">${escapeHtml(src.site_name)}</span>`
                    : '';
                if (iconHtml || nameHtml) {
                    siteBadgeHtml = `<span class="source-site-badge">${iconHtml}${nameHtml}</span>`;
                }
            }

            const titleHtml = src.url
                ? `<a class="source-title source-link" href="${escapeHtml(src.url)}" target="_blank" rel="noopener">${escapeHtml(cleanTitle)}</a>`
                : `<span class="source-title">${escapeHtml(cleanTitle)}</span>`;

            const publishHtml = src.publish_date
                ? `<span style="color:var(--text-muted);font-size:0.75rem;display:block;margin-top:4px">[发布于：${escapeHtml(src.publish_date)}]</span>`
                : '';

            item.innerHTML = `
                <div class="source-title-row">
                    ${titleHtml}
                    ${siteBadgeHtml}
                </div>
                ${publishHtml}
                ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)" class="source-content-preview">${escapeHtml(truncate(src.content, 100))}</div>` : ''}
            `;
            return item;
        }

        // ---- 使用 SwipePager 组件替代内联触摸翻页逻辑 ----
        const pager = new SwipePager(itemsContainer, {
            totalPages: totalPages,
            renderPage: (pane, pageIndex) => {
                pane.innerHTML = '';
                if (pageIndex === null || pageIndex < 0 || pageIndex >= totalPages) return;
                const start = pageIndex * PAGE_SIZE;
                const end = Math.min(start + PAGE_SIZE, sources.length);
                for (let i = start; i < end; i++) {
                    pane.appendChild(createSourceItem(sources[i]));
                }
            },
            showDots: true,
            dotsClass: 'sources-pagination-dots',
            dotClass: 'sources-pagination-dot',
            onPageChange: () => {
                // 翻页后，如果面板高度变化导致内容不可见，自动滚动补偿
                scrollPanelIntoView(itemsContainer);
            },
        });
        pager.mount(0);

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

    // 仅新创建的 section 需要追加到 panel
    if (isNew) {
        panel.appendChild(section);
    }
    autoScrollToBottom();
}

/**
 * scrollPanelIntoView 智能滚动，确保面板在可视区域内尽可能完整可见。
 *
 * 策略（优先级）：
 *   1. 面板底部不可见时，向上滚动直到底部可见；
 *      但如果面板顶部已到达可视区顶部，停止滚动（面板比可视区还高时）。
 *   2. 面板底部可见但顶部不可见时，向下滚动使顶部可见。
 *
 * @param {HTMLElement} panel - 要滚动到的面板元素
 */
function scrollPanelIntoView(panel) {
    const sc = dom.scrollContainer;
    if (!sc || !panel) return;

    requestAnimationFrame(() => {
        const panelRect = panel.getBoundingClientRect();
        const containerRect = sc.getBoundingClientRect();
        const panelTop = panelRect.top;
        const panelBottom = panelRect.bottom;
        const containerTop = containerRect.top;
        const containerBottom = containerRect.bottom;

        if (panelBottom > containerBottom) {
            // ---- 底部不可见：向上滚动，直到底部可见或顶部到达可视区顶部 ----
            const overflow = panelBottom - containerBottom + 8; // 多留 8px 间距
            const maxScroll = panelTop - containerTop;          // 顶部到可视区顶部的距离
            const scrollBy = Math.min(overflow, maxScroll);     // 取小值，防止顶部滚出
            if (scrollBy > 0) {
                sc.scrollBy({ top: scrollBy, behavior: 'smooth' });
            }
        } else if (panelTop < containerTop) {
            // ---- 底部可见但顶部不可见：向下滚动使顶部可见 ----
            const scrollBy = panelTop - containerTop - 8; // 负值，向上滚动（实际是向下）
            sc.scrollBy({ top: scrollBy, behavior: 'smooth' });
        }
    });
}

/**
 * toggleSourcesSection 切换引用来源区域的折叠/展开
 * 展开时自动滚动确保面板可见。
 * @param {HTMLElement} titleEl
 * @param {HTMLElement} bodyEl
 */
function toggleSourcesSection(titleEl, bodyEl) {
    const isCollapsed = bodyEl.style.display === 'none';
    bodyEl.style.display = isCollapsed ? '' : 'none';
    titleEl.classList.toggle('collapsed', !isCollapsed);
    titleEl.classList.toggle('expanded', isCollapsed);

    // 展开时，自动滚动确保面板可见
    if (isCollapsed) {
        const panel = titleEl.closest('.sources-panel');
        scrollPanelIntoView(panel);
    }
}

/**
 * updateHeaderTitle 更新 header 左侧的对话标题，并同步到侧边栏
 * @param {string} title
 */
export function updateHeaderTitle(title) {
    const el = document.getElementById('headerTitle');
    if (el) {
        // 标题有变化时触发滑动动画
        if (title !== el.textContent) {
            // 移除动画类以重置动画
            el.classList.remove('animate');
            // 强制回流以重新触发动画
            void el.offsetWidth;
            el.textContent = title;
            el.classList.add('animate');
        } else {
            el.textContent = title;
        }
    }
    state.dialogTitle = title;

    // 同步更新侧边栏中当前对话的标题
    if (title) {
        updateCurrentChatTitle(title);
    }
}

/**
 * showWelcomeMessage 显示独立于消息系统的欢迎信息
 */
export function showWelcomeMessage() {
    // 避免重复添加
    if (dom.chatContainer.querySelector('.welcome-message')) return;

    const el = document.createElement('div');
    el.className = 'welcome-message';
    el.textContent = '你好！我是‘第2大脑’ AI助手，多和我聊，我来构建你的第2大脑';
    dom.chatContainer.appendChild(el);

    // 将输入区域移动到欢迎消息内部，使二者作为一个整体居中
    const inputArea = document.querySelector('.input-area');
    if (inputArea) {
        el.appendChild(inputArea);
        // 欢迎状态必须显示完整输入面板，移除折叠状态
        inputArea.classList.remove('collapsed');
    }

    // 标记欢迎状态（新布局下标记在 .scroll-container 上）
    const scrollContainer = document.getElementById('scrollContainer');
    if (scrollContainer) {
        scrollContainer.classList.add('welcome-state');
    }

    // 设置 header 标题为欢迎语
    updateHeaderTitle('');
}

// ============================================================
// 输入面板折叠/恢复 — 由 chat.js 的滚动处理器和 chat-sse.js 的流结束清理调用
// ============================================================

/** 输入面板是否处于折叠状态 */
let _isInputCollapsed = false;

/**
 * 折叠输入面板（隐藏 send-mode-corner 和 input-footer）
 * 折叠时始终显示中断按钮（非流式时 disabled 灰色，流式时红色可点击）
 */
export function collapseInputArea() {
    if (_isInputCollapsed) return;
    _isInputCollapsed = true;
    const inputArea = document.querySelector('.input-area');
    if (inputArea) inputArea.classList.add('collapsed');
    const stopStreamingBtn = document.getElementById('stopStreamingBtn');
    if (stopStreamingBtn) {
        stopStreamingBtn.disabled = !state.isStreaming;
    }
}

/**
 * 恢复输入面板（显示所有内容）
 */
export function restoreInputArea() {
    if (!_isInputCollapsed) return;
    _isInputCollapsed = false;
    const inputArea = document.querySelector('.input-area');
    if (inputArea) inputArea.classList.remove('collapsed');
    const stopStreamingBtn = document.getElementById('stopStreamingBtn');
    if (stopStreamingBtn) {
        stopStreamingBtn.disabled = !state.isStreaming;
    }
}

/**
 * 查询输入面板是否处于折叠状态
 * @returns {boolean}
 */
export function isInputCollapsed() {
    return _isInputCollapsed;
}
