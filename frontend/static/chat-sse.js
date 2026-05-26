// ============================================================
// chat-sse.js — SSE 流处理 + 事件分发
// ============================================================
//
// 重构后：sendMessage() 通过 sessionManager 获取 ChatSession，
// SSE 读取流程由 ChatSession 管理，事件分发委托给 SSEResponser。
//
// 切换对话时，旧对话的 SSE 连接继续在后台接收数据，
// 数据累积到 ChatSession.streamingMsg 中，不丢失。
// ============================================================

import { state } from './chat-state.js';
import { sessionManager } from './chat-session-manager.js';
import { addMessage, applyStreamingState, showError, showSources, showTokenUsage, autoScrollToBottom, updateHeaderTitle, showToast, isScrolledToBottom, throttleRender, restoreInputArea } from './chat-ui.js';
import { handleReasoningEvent, finalizeReasoningArea } from './chat-reasoning.js';
import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { updateTickNav } from './chat-ticknav.js';
import { TITLE_STATE, fetchChatTitle, truncateTitle, newChat } from './chat-api.js';
import { addDirtyChat } from './chat-list.js';

'use strict';

// ============================================================
// 辅助函数 — 发送前准备
// ============================================================

/**
 * 删除欢迎消息，将输入区域移回 main-content 底部
 * @param {HTMLElement} chatContainer
 */
function removeWelcomeMessage(chatContainer) {
    const welcomeEl = chatContainer.querySelector('.welcome-message');
    if (!welcomeEl) return;

    const inputArea = welcomeEl.querySelector('.input-area');
    if (inputArea) {
        const mainContent = document.querySelector('.main-content');
        if (mainContent) {
            mainContent.appendChild(inputArea);
        }
    }
    welcomeEl.remove();

    const scrollContainer = document.getElementById('scrollContainer');
    if (scrollContainer) {
        scrollContainer.classList.remove('welcome-state');
    }
}

/**
 * 添加用户消息到界面和状态，首次消息自动设置标题
 * @param {string} content
 * @param {string} createdAt  ISO 格式时间戳
 */
function addUserMessage(content, createdAt) {
    addMessage('user', content, createdAt);
    state.messages.push({ role: 'user', content, id: 0, created_at: createdAt });

    if (!state.dialogTitle) {
        const title = truncateTitle(content);
        if (title) {
            updateHeaderTitle(title);
            state.titleState = TITLE_STATE.ORIGINAL;

            // 首次消息：立即在侧边栏插入一条新对话条目
            // newChat() 已预先获取了 SN（state.currentChatSN），直接传给 addDirtyChat
            // 这样侧边栏条目从创建起就拥有真实 SN，点击即可正常切换
            // 匿名用户和登录用户都会添加侧边栏条目
            addDirtyChat(title, state.currentChatSN || null);
        }
    }
}

/**
 * 发送前准备：验证输入、清理 UI、初始化状态
 * @returns {{ content: string, createdAt: string, session: ChatSession } | null}
 */
function prepareChat() {
    const messageInput = document.getElementById('messageInput');
    const chatContainer = document.getElementById('chatContainer');
    const content = messageInput.value.trim();
    if (!content || sessionManager.isStreaming) return null;

    // 清空输入框
    messageInput.value = '';
    messageInput.style.height = 'auto';

    // 删除欢迎消息
    removeWelcomeMessage(chatContainer);

    // 生成 UTC 时间
    const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');

    // 新消息发出，即将滚动到底部，重置用户滚动状态
    state.userScrolledUp = false;

    // 添加用户消息
    addUserMessage(content, createdAt);

    // 更新刻度导航
    updateTickNav();

    // 创建空的 assistant 消息占位
    const assistantBubble = addMessage('assistant', '', null, true);
    const contentDiv = assistantBubble.querySelector('.bubble');

    // 获取或创建当前对话的 ChatSession
    const sn = state.currentChatSN;
    const session = sessionManager.getOrCreate(sn);
    session.isStreaming = true;
    session._isActive = true;
    session.assistantBubble = assistantBubble;
    session.contentDiv = contentDiv;
    session.resetStreaming();

    // 同步更新 Alpine store 的 isStreaming，确保 :disabled 绑定即时生效
    try {
        window.Alpine.store('chats').startStreaming(sn);
    } catch(e) {}

    // 确保流式开始前页面已在底部。
    // addMessage 内部的 autoScrollToBottom 使用同步 scrollTop 赋值，
    // 如果 SSE 事件在 scroll 事件之前到达（如 reasoning 事件首条创建 DOM），
    // 再次滚动可确保位置正确。
    autoScrollToBottom();

    // 统一切换所有 UI 组件到流式状态
    applyStreamingState(true);

    // 创建 AbortController
    session.abortController = new AbortController();

    return { content, createdAt, session };
}

// ============================================================
// SSE 流读取（ChatSession 的方法）
// ============================================================

/**
 * 读取 SSE 流数据，按行解析并分发事件到 SSEResponser
 * @param {Response} response
 * @param {ChatSession} session
 */
async function readSSEBuffer(response, session) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // 按行分割
        const lines = buffer.split('\n');
        buffer = lines.pop() || ''; // 保留未完成的行

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed || !trimmed.startsWith('data: ')) continue;

            const jsonStr = trimmed.slice(6);
            try {
                const event = JSON.parse(jsonStr);
                // 通过 SSEResponser 分发事件
                dispatchEventToResponser(event, session);
            } catch (e) {
                console.warn('解析 SSE 事件失败:', jsonStr);
            }
        }
    }

    // 处理 buffer 中剩余的数据
    if (buffer.trim().startsWith('data: ')) {
        const jsonStr = buffer.trim().slice(6);
        try {
            const event = JSON.parse(jsonStr);
            dispatchEventToResponser(event, session);
        } catch (e) {
            console.warn('解析剩余 SSE 事件失败:', jsonStr);
        }
    }
}

/**
 * 将 SSE 事件分发到 session 的 SSEResponser
 * @param {object} event
 * @param {ChatSession} session
 */
function dispatchEventToResponser(event, session) {
    const responser = session.responser;
    if (!responser) return;

    switch (event.type) {
        case 'reasoning':
            responser.onReasoning(event);
            break;

        case 'reasoning_end':
            responser.onReasoningEnd();
            break;

        case 'text':
            responser.onText(event);
            break;

        case 'sources':
            responser.onSources(event);
            break;

        case 'done':
            responser.onDone(event);
            break;

        case 'error':
            responser.onError(event);
            break;

        default:
            console.warn('未知事件类型:', event.type);
    }
}

/**
 * 发起 fetch 请求并读取 SSE 流
 * @param {ChatSession} session
 * @param {string} content
 * @param {string} createdAt
 */
async function fetchStream(session, content, createdAt) {
    // 锁定本轮会话的深度思考状态（防止流式过程中用户乱点按钮导致状态漂移）
    state.sessionDeepThinkingEnabled = state.deepThinkActive;

    // 发送请求 — 只传用户最新的一句话，历史由后端维护
    const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            message: { id: 0, role: 'user', content, created_at: createdAt },
            stream: true,
            deep_think: state.deepThinkActive,
            web_search_enabled: state.webSearchActive
        }),
        signal: session.abortController.signal
    });

    if (!response.ok) {
        const errText = await response.text();
        throw new Error(`服务器错误 [${response.status}]: ${errText}`);
    }

    await readSSEBuffer(response, session);
}

// ============================================================
// 辅助函数 — 错误处理
// ============================================================

/**
 * 处理 AbortError：更新 reasoning 区域状态，空气泡显示中断提示
 * @param {ChatSession} session
 */
function handleAbortError(session) {
    const assistantBubble = session.assistantBubble;
    if (!assistantBubble) return;

    // 请求已取消：将 reasoning 标题改为"AI 思路已被掐断"
    const area = assistantBubble.querySelector('.reasoning-area.active');
    if (area) {
        const titleEl = area.querySelector('.reasoning-title');
        if (titleEl) {
            titleEl.textContent = 'AI 思路已被掐断';
        }
        area.classList.remove('active');
        area.classList.add('done');
        // 清理 reasoning 节流渲染定时器
        const reasoningContentEl = area.querySelector('.reasoning-content');
        if (reasoningContentEl && reasoningContentEl.renderTimer) {
            clearTimeout(reasoningContentEl.renderTimer);
            reasoningContentEl.renderTimer = null;
        }
    }
    // 如果 assistant 气泡为空（尚未收到任何 text 事件），显示中断提示
    const contentDiv = session.contentDiv;
    if (contentDiv && !contentDiv.textContent.trim()) {
        contentDiv.innerHTML = '⏹ 已中断';
        contentDiv.classList.remove('streaming');
        assistantBubble.classList.add('interrupted');
    }
}

/**
 * 处理流式请求错误
 * @param {Error} err
 * @param {ChatSession} session
 */
function handleStreamError(err, session) {
    if (err.name === 'AbortError') {
        // 标记为中断，cleanupAfterStream 据此跳过标题自动修改等操作
        state._wasAborted = true;
        handleAbortError(session);
    } else {
        console.error('请求失败:', err);
        if (session.assistantBubble) {
            showError(session.assistantBubble, err.message);
        }
    }
}

// ============================================================
// 辅助函数 — 流结束清理
// ============================================================

/**
 * 标题自动修改：仅在第一组消息（第一条用户消息 + 第一条 AI 回复）后尝试优化标题
 * @param {boolean} wasAborted  是否被用户中断
 */
function autoUpdateTitle(wasAborted) {
    // 条件：
    //   - 未被中断（AI 回复被掐断时不取标题，因为回复不完整）
    //   - 标题未被修改过（titleState >= 1 表示已被 AI 或用户修改，不再请求）
    //   - 仅在第一组消息时允许（messages.length <= 2，即第一条用户消息 + 第一条 AI 回复）
    // 更严格的触发条件：仅在第一轮对话后自动请求 AI 生成标题，后续轮次不再自动触发
    if (wasAborted || state.titleState >= TITLE_STATE.AI || state.messages.length > 2) return;

    // 原标题：使用当前已有的标题（可能是 AI 已修改过的），而不是重新从第一条消息截取
    // 这样后端可以基于当前标题判断是否需要更新
    // 如果 dialogTitle 为空（新对话首次发送消息），传空字符串让后端基于对话历史生成
    const originalTitle = state.dialogTitle || '';
    // 异步调用，不阻塞后续操作
    // 传递当前 chat SN，确保后端返回时前端能精确定位到正确的对话
    fetchChatTitle(originalTitle, false, state.currentChatSN);
}

/**
 * 流结束清理：重置状态、恢复 UI、清理定时器、自动修改标题
 * @param {ChatSession} session
 * @param {boolean} wasAborted  是否被用户中断
 */
function cleanupAfterStream(session, wasAborted) {
    // 清理中断标记（默认 false，确保每次 sendMessage 调用都有干净的初始值）
    state._wasAborted = false;
    session.isStreaming = false;
    session.abortController = null;

    // 仅当此 session 仍是活跃 session 时，才操作全局 UI
    // 避免后台流完成时错误地影响当前 active chat 的 UI 状态
    const isActiveSession = sessionManager.getActive() === session;
    if (isActiveSession) {
        applyStreamingState(false);
        document.getElementById('messageInput').focus();
    } else {
        // 后台流完成：仅更新 Alpine store 中对应 chat 的 isStreaming，
        // 不触碰全局 UI（当前 active chat 的 streaming 状态不受影响）
        try {
            var chats = window.Alpine.store('chats');
            if (chats) {
                var chatData = chats.getOrCreate(session.sn);
                if (chatData) {
                    chatData.isStreaming = false;
                    // streamingMsg 由 SSEResponser.onDone() 中的 finalizeStreaming() 负责归档清理
                }
            }
        } catch(e) {}
    }

    // 移除 streaming 类
    const contentDiv = session.contentDiv;
    if (contentDiv) {
        contentDiv.classList.remove('streaming');
    }

    // 清理渲染状态（防止取消请求后定时器残留）
    session.clearRenderTimer();

    // 清理 reasoning 区域的节流渲染定时器（防止取消请求后定时器残留）
    const assistantBubble = session.assistantBubble;
    if (assistantBubble) {
        const reasoningContentEl = assistantBubble.querySelector('.reasoning-content');
        if (reasoningContentEl && reasoningContentEl.renderTimer) {
            clearTimeout(reasoningContentEl.renderTimer);
            reasoningContentEl.renderTimer = null;
        }
    }

    // 标题自动修改
    autoUpdateTitle(wasAborted);

    // 第一轮对话完成后，获取当前对话的信息并更新侧边栏单个条目（登录用户可见）
    getCurrentChatIfNeeded(wasAborted);
}

/**
 * getCurrentChatIfNeeded 在第一轮对话完成后，调用专用 API 获取当前对话的信息，
 * 然后只更新侧边栏中对应的单个条目（而非刷新整个列表）。
 * 这避免了 refreshChatListIfNeeded 的"全量替换"方式可能导致的重复条目和标题覆盖问题。
 * 条件：
 *   - 未被中断（AI 回复不完整时列表无意义）
 *   - 仅在第一组消息后触发（state.messages.length <= 2）
 */
async function getCurrentChatIfNeeded(wasAborted) {
    // 被中断或非第一轮对话，跳过
    if (wasAborted || state.messages.length > 2) return;

    try {
        const response = await fetch('/api/chat/current');
        if (!response.ok) return;
        const data = await response.json();
        if (data.sn) {
            // 后端在新对话时 currentChat.title 为空字符串（尚未设置），
            // 此时前端已有正确的原始标题（来自用户首条消息截取），
            // 因此仅当后端返回的 title 非空时才更新，避免空标题覆盖前端已有标题。
            const title = data.title || undefined;
            const { updateChatEntry } = await import('./chat-list.js');
            updateChatEntry(data.sn, title, data.title_state);
        }
    } catch (e) {
        console.warn('获取当前对话信息失败:', e);
    }
}

// ============================================================
// 主入口
// ============================================================

/**
 * isNewChat 判断当前对话是否为新对话（尚未初始化 SN）。
 * 新对话特征：SN 为空且没有消息历史。
 * @returns {boolean}
 */
function isNewChat() {
    return !state.currentChatSN && state.messages.length === 0;
}

/**
 * sendMessage 发送用户消息并启动 SSE 流式接收
 * 如果是新对话（SN 为空），先调用 POST /api/chat/new 初始化并获取 SN，
 * 然后再发送用户消息。
 */
export async function sendMessage() {
    // 新对话：先向后台初始化对话并获取 SN
    if (isNewChat()) {
        const data = await newChat();
        if (data && data.sn) {
            state.currentChatSN = data.sn;
        }
        // 即使匿名用户返回空 SN，也不阻塞后续流程
    }

    const chatData = prepareChat();
    if (!chatData) return;

    const { content, createdAt, session } = chatData;

    try {
        await fetchStream(session, content, createdAt);
    } catch (err) {
        handleStreamError(err, session);
    } finally {
        cleanupAfterStream(session, !!state._wasAborted);
    }
}
