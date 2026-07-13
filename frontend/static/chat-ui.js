// ============================================================
// chat-ui.js — DOM 操作（addMessage、showSources、toast、错误显示等）
// ============================================================

import { escapeHtml, truncateByVisualLength } from './toolsets.js';
import { renderMarkdown } from './chat-markdown.js';
import { SwipePager } from './components/swipe-pager.js';
import { ICON_GLOBE } from './svg_icons_re.js';

'use strict';

// ============================================================
// 工具函数 — WebSource 条目 DOM 创建
// ============================================================

/**
 * createSourceItemElement — 创建单个 source 条目的 DOM 元素
 * 提取为独立函数，供 Alpine x-init 中的 SwipePager renderPage 使用。
 * @param {object} src - source 数据
 * @returns {HTMLElement}
 */
export function createSourceItemElement(src) {
    const item = document.createElement('div');
    item.className = 'source-item';

    // 清理标题：去除重复的网站名称以及 "（发布时间：XXXX）" 后缀
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
        ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)" class="source-content-preview">${escapeHtml(truncateByVisualLength(src.content, 100))}</div>` : ''}
    `;
    return item;
}

/**
 * initSourcesPager — 在容器上初始化 SwipePager 触摸翻页组件
 * 供 Alpine x-init 调用，通过 window._initSourcesPager 暴露。
 *
 * @param {HTMLElement} container - .sources-items-container 元素
 * @param {object} assistant - group.assistant 对象（含 sources 数组）
 * @returns {SwipePager|null}
 */
export function initSourcesPager(container, assistant) {
    if (!container || !assistant || !assistant.sources || assistant.sources.length === 0) return null;

    const PAGE_SIZE = 5;
    const totalPages = Math.ceil(assistant.sources.length / PAGE_SIZE);
    if (totalPages <= 1) {
        // 只有一页时不用翻页组件，直接渲染全部条目
        container.innerHTML = '';
        assistant.sources.forEach(function(src) {
            container.appendChild(createSourceItemElement(src));
        });
        return null;
    }

    const pager = new SwipePager(container, {
        totalPages: totalPages,
        renderPage: function(pane, pageIndex) {
            pane.innerHTML = '';
            if (pageIndex === null || pageIndex < 0 || pageIndex >= totalPages) return;
            const start = pageIndex * PAGE_SIZE;
            const end = Math.min(start + PAGE_SIZE, assistant.sources.length);
            for (let i = start; i < end; i++) {
                pane.appendChild(createSourceItemElement(assistant.sources[i]));
            }
        },
        showDots: true,
        dotsClass: 'sources-pagination-dots',
        dotClass: 'sources-pagination-dot',
        onPageChange: function() {
            scrollPanelIntoView(container);
        },
    });
    pager.mount(0);
    // 在容器上存储 pager 引用，供 updateSourcesPagerInDOM 外部更新
    container._pager = pager;
    return pager;
}

/**
 * updateSourcesPagerInDOM — 更新当前活跃组中 sources 的 SwipePager
 * 在流式期间新 sources 到达时调用，使翻页组件反映最新数据。
 *
 * ★ 仅在 body 可见（panel 已展开）时初始化/更新 SwipePager。
 *    body 隐藏（display:none）时容器宽度为 0，SwipePager 三栏布局会坍缩，
 *    因此标记为数据待更新，由 toggle() 展开时触发。
 *
 * @param {object} assistant - group.assistant 对象
 */
export function updateSourcesPagerInDOM(assistant) {
    if (!assistant || !assistant.sources || assistant.sources.length === 0) return;
    const lastGroup = document.querySelector('.message-group:last-child');
    if (!lastGroup) return;
    const container = lastGroup.querySelector('.sources-items-container');
    if (!container) return;

    // 如果 body 隐藏（display:none），容器无宽度 → 跳过，标记数据待同步
    const body = container.closest('.sources-body');
    if (body && body.style.display === 'none') {
        container.dataset.sourcesDirty = '1';
        return;
    }

    const PAGE_SIZE = 5;
    const newTotalPages = Math.ceil(assistant.sources.length / PAGE_SIZE);

    if (container._pager) {
        // 已有翻页组件 → 更新总页数
        container._pager.updateTotal(newTotalPages);
    } else {
        // 首次初始化 SwipePager（或直接渲染单页内容）
        initSourcesPager(container, assistant);
    }
}

// 暴露给 Alpine / buttons.js 调用
window._initSourcesPager = initSourcesPager;
window._updateSourcesPagerInDOM = updateSourcesPagerInDOM;

/** UI 渲染节流间隔（毫秒） */
const UI_RENDER_INTERVAL = 180;

/** 判断"滚动到底部"的误差容限（px），底部剩余内容小于此值即视为已到底 */
export const SCROLL_BOTTOM_THRESHOLD = 4;

/**
 * _autoScrolling — 是否正在执行自动滚动
 * autoScrollToBottom() 在滚动前设为 true，scroll handler 检测到此标志
 * 时跳过处理（忽略自己触发的 scroll 事件），通过 setTimeout(0) 清除，
 * 确保同一事件循环内的 scroll 事件都被忽略。
 */
export let _autoScrolling = false;

/**
 * lastScrollHeight — 上次 autoScrollToBottom 执行时的 scrollHeight
 * 用于在 scroll handler 中检测 scroll anchoring（内容增长后浏览器自动调整 scrollTop）。
 * 当 scrollHeight 比记录值大时，说明是内容增长引起的 scroll anchoring，而非用户手动滚动。
 * 在 _autoScrolling 被 setTimeout(0) 清除后，scroll anchoring 才触发时起兜底作用。
 */
let lastScrollHeight = 0;

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
    if (!sc) { return; }
    const scrollHeight = sc.scrollHeight;
    const scrollTop = sc.scrollTop;
    const clientHeight = sc.clientHeight;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < SCROLL_BOTTOM_THRESHOLD;

    // 从 Alpine store 获取当前活跃 chat 的滚动状态
    try {
        var chats = window.Alpine.store('chats');
        if (chats && chats.active && chats.active.userScrolledUp) {
            return;
        }
    } catch(e) {}

    // 记录本次执行时的 scrollHeight，供 scroll handler 检测 scroll anchoring
    lastScrollHeight = scrollHeight;
    // 设置 _autoScrolling 标志，scroll handler 检测到后跳过处理
    _autoScrolling = true;
    sc.scrollTop = scrollHeight;
    // 下一帧清除标志（确保同一个 scroll 事件链内都被忽略）
    setTimeout(function() {
        _autoScrolling = false;
    }, 0);
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
    }, UI_RENDER_INTERVAL);
}

/**
 * setInputEnabled 切换输入区域的 streaming 样式
 *
 * 注意：
 *   - messageInput.disabled 已由 Alpine 的 :disabled 绑定
 *     ($store.chats.active?.isStreaming ?? false) 响应式管理，此处不再手动设置。
 *   - input-area 的 streaming class 未被 Alpine 管理，仍需手动切换。
 *
 * @param {boolean} enabled
 * @private
 */
function setInputEnabled(enabled) {
    if (dom.inputArea) {
        dom.inputArea.classList.toggle('streaming', !enabled);
    }
}

/**
 * applyStreamingState 统一管理流式输出中 UI 组件的状态。
 *
 * 职责范围（Alpine 未覆盖的 DOM 元素）：
 *   1. input-area 的 streaming class（控制输入区域样式）
 *
 * messageInput.disabled、stopStreamingBtn 和删除按钮的 disabled
 * 已由 Alpine 的 :disabled / x-show 绑定响应式管理，此处不再处理。
 *
 * @param {boolean} isStreaming
 */
export function applyStreamingState(isStreaming) {
    // 1. 输入区域 streaming 样式
    setInputEnabled(!isStreaming);
}

/**
 * showToast 显示一个自动消失的 Toast 消息
 *
 * 通过 Alpine.store('ui').showToast() 直接操作响应式数据，
 * Alpine 自动触发 x-for/x-show/x-transition 更新 DOM。
 * 不再保留原生 JS 降级路径——Alpine 为本地文件，始终可用。
 *
 * @param {string} message
 * @param {'error'|'success'|'info'} [type='error']
 * @param {number} [duration=4000] - 显示时长（毫秒）
 */
export function showToast(message, type, duration) {
    type = type || 'error';
    duration = duration || 4000;

    Alpine.store('ui').showToast(message, type, duration);
}
// 注册到 window，供普通 <script>（如 alpine-api.js）在运行时调用
window.showToast = showToast;

/**
 * showToastHTML 显示支持 HTML 内容和点击回调的 Toast
 *
 * @param {string} html - 支持 HTML 标签的消息内容
 * @param {'error'|'success'|'info'} [type='error']
 * @param {number} [duration=4000]
 * @param {function} [onClick] - 点击 toast 时的回调函数
 */
export function showToastHTML(html, type, duration, onClick) {
    type = type || 'error';
    duration = duration || 4000;

    Alpine.store('ui').showToastHTML(html, type, duration, onClick);
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
 * addMessage 添加消息到 Alpine store 的 groups 数组。
 * 不再创建 DOM — 由 Alpine x-for 模板自动渲染。
 *
 * 用户消息（role='user'）会创建新的 group；
 * 助手消息（role='assistant'）更新最后一个 group 的 assistant 数据。
 *
 * @param {'user'|'assistant'} role
 * @param {string} content
 * @param {string|null} [createdAt=null] - ISO 格式时间戳
 * @param {boolean} [isStreaming=false] - 是否为流式占位（assistant 空气泡）
 * @returns {number} group.id（用户消息时返回新 group 的 id，assistant 时返回 -1）
 */
export function addMessage(role, content, createdAt = null, isStreaming = false) {
    try {
        var chats = window.Alpine.store('chats');
        if (!chats || !chats.active) return -1;

        if (role === 'user') {
            // 用户消息：创建新 group
            var groupId = chats.addGroup(content, createdAt);
            autoScrollToBottom();
            return groupId;
        } else {
            // assistant 消息：更新最后一个 group 的 assistant 数据
            var lastGroup = chats.getLastGroup();
            if (!lastGroup) return -1;

            if (!isStreaming) {
                // 非流式（历史恢复）：assistant 已预先初始化，只需填充数据
                lastGroup.assistant.content = content || '';
                lastGroup.assistant.createdAt = createdAt || null;
                lastGroup.assistant.contentHTML = renderMarkdown(content || '');
            }
            // 流式占位：assistant 已由 addGroup() 预先初始化（内容为空），
            // 不需要额外操作，SSEResponser.onText 会逐步更新 content/contentHTML
            autoScrollToBottom();
            return -1;
        }
    } catch(e) {
        // Alpine store 尚未初始化（极早期场景），忽略
        console.warn('addMessage: Alpine store 不可用', e);
        return -1;
    }
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
    // 跳过 Alpine 管理的 panel（含 x-data 属性），RAG 使用独立 panel
    let panel = lastGroup.querySelector('.sources-panel:not([x-data])');
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
                ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)">${escapeHtml(truncateByVisualLength(src.content, 100))}</div>` : ''}
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
        // ---- 联网搜索结果（Alpine 响应式管理） ----
        // 不再创建 DOM，只需确保 Alpine store 中有完整的数据。
        // Alpine 模板通过 group.assistant.sources 响应式渲染，
        // SwipePager 由 Alpine x-init 初始化。
        // 数据已在调用方（onSources/flushToDOM）中同步到 store。
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

// 暴露给 Alpine / buttons.js 中 sourcesPanel 组件使用
window.scrollPanelIntoView = scrollPanelIntoView;

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
 * updateHeaderTitle 更新 header 左侧的对话标题
 * 注意：此函数只负责 header 标题和 Alpine store，不负责侧边栏 DOM 更新。
 * 侧边栏标题更新请调用 updateChatTitleBySN()。
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
    // 同步更新 Alpine store 中当前对话的标题
    try {
        var activeChat = window.Alpine.store('chats').active;
        if (activeChat) {
            activeChat.title = title;
        }
    } catch(e) {}
}

/**
 * showWelcomeMessage 通过 Alpine store 显示欢迎信息，
 * 同时将输入面板移动到欢迎消息内部，实现欢迎词与输入面板一起垂直居中。
 * 欢迎文本由调用方预先设置到 store（chats.welcomeMessage），
 * 若 store 中为空则使用默认文本兜底。
 */
export function showWelcomeMessage() {
    // ★ 恢复输入面板（移除 .collapsed 类），否则删除已折叠的对话后，
    //   输入面板虽处于 welcome 状态居中位置，但仍保留收缩样式。
    restoreInputArea();

    // 清空 header 标题（欢迎页不需要显示对话标题）
    updateHeaderTitle('');

    // ★ 确保 .welcome-message 元素存在于 DOM 中
    //    selectChat() 会从 DOM 中移除 .welcome-message 元素（existingWelcome.remove()），
    //    因此后续调用 showWelcomeMessage() 时 querySelector 可能找不到元素。
    //    需要手动重新创建，并添加 x-show 属性让 Alpine 接管响应式控制。
    var chatContainer = document.getElementById('chatContainer');
    var welcomeMsgEl = chatContainer ? chatContainer.querySelector('.welcome-message') : null;
    if (!welcomeMsgEl && chatContainer) {
        // 手动创建 welcome-message 元素
        // ★ 必须添加 x-show 属性，否则 Alpine 无法响应式隐藏它，
        //   用户发送消息后欢迎语仍会可见。
        welcomeMsgEl = document.createElement('div');
        welcomeMsgEl.className = 'welcome-message';
        welcomeMsgEl.setAttribute('x-show', "$store.chats.activeIndex === -1");
        // 不设 style.display，由 Alpine 的 x-show 控制

        var welcomeText = document.createElement('p');
        welcomeText.className = 'welcome-text';
        // 使用 x-text 让 Alpine 响应式管理文本内容（welcomeMessage 清空时 fallback 到默认文本）
        welcomeText.setAttribute('x-text', "$store.chats.welcomeMessage || '你好！我是「第2大脑」AI助手'");
        var chats = window.Alpine.store('chats');
        // 初始文本：先直接设置一次，让 Alpine 编译 x-text 之前有内容
        welcomeText.textContent = (chats && chats.welcomeMessage) || '你好！我是「第2大脑」AI助手';
        welcomeMsgEl.appendChild(welcomeText);
        
        // 插入到 chatContainer 的最前面（在 x-for 模板之前）
        var xforTemplate = chatContainer.querySelector('template[x-for]');
        if (xforTemplate) {
            chatContainer.insertBefore(welcomeMsgEl, xforTemplate);
        } else {
            chatContainer.appendChild(welcomeMsgEl);
        }

        // ★ 调用 Alpine.initTree() 让 Alpine 处理新元素上的 x-show / x-text 指令，
        //   否则动态添加的 Alpine 指令不会生效。
        if (window.Alpine && typeof window.Alpine.initTree === 'function') {
            window.Alpine.initTree(welcomeMsgEl);
        }
    }

    // ★ 将 input-area 移入 welcome-message
    var inputArea = document.querySelector('.input-area');
    if (welcomeMsgEl && inputArea && inputArea.parentNode !== welcomeMsgEl) {
        welcomeMsgEl.appendChild(inputArea);
        inputArea.style.marginTop = '12px';
    }

    // 显式添加 welcome-state，使 scroll-container 切换为 flex 垂直居中布局
    var scrollContainer = document.getElementById('scrollContainer');
    if (scrollContainer) {
        scrollContainer.classList.add('welcome-state');
    }
}

// ============================================================
// 输入面板折叠/恢复 — 由 chat.js 的滚动处理器和 chat-sse.js 的流结束清理调用
// ============================================================

/** 输入面板是否处于折叠状态 */
let _isInputCollapsed = false;

/**
 * 折叠输入面板（隐藏 send-mode-corner 和 input-footer）
 * 同步 isCollapsed 到 Alpine chats store，使 Alpine 模板可响应式感知折叠状态。
 */
export function collapseInputArea() {
    if (_isInputCollapsed) return;
    _isInputCollapsed = true;
    const inputArea = document.querySelector('.input-area');
    if (inputArea) inputArea.classList.add('collapsed');
    // 同步到 Alpine store，使 x-show 等绑定可响应折叠状态变化
    try { Alpine.store('chats').inputCollapsed = true; } catch(e) {}
}

/**
 * 恢复输入面板（显示所有内容）
 */
export function restoreInputArea() {
    if (!_isInputCollapsed) return;
    _isInputCollapsed = false;
    const inputArea = document.querySelector('.input-area');
    if (inputArea) inputArea.classList.remove('collapsed');
    // 同步到 Alpine store
    try { Alpine.store('chats').inputCollapsed = false; } catch(e) {}
}

/**
 * 查询输入面板是否处于折叠状态
 * @returns {boolean}
 */
export function isInputCollapsed() {
    return _isInputCollapsed;
}
