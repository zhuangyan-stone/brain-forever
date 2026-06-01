// ============================================================
// chat-sse.js — SSE 流处理 + 事件分发
// ============================================================
//
// sendMessage() 通过 chatStreamMgr 获取 ChatStream，
// SSE 读取流程由 ChatStream 管理，事件分发委托给 SSEResponser。
//
// 切换对话时，旧对话的 SSE 连接继续在后台接收数据，
// 数据累积到 Alpine.store('chats') 的 streamMsg 中，不丢失。
// ============================================================

import { chatStreamMgr } from './chat-stream-mgr.js';
import { addMessage, applyStreamingState, showError, showSources, showTokenUsage, autoScrollToBottom, updateHeaderTitle, showToast, isScrolledToBottom, restoreInputArea } from './chat-ui.js';
import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { updateTickNav } from './chat-ticknav.js';
import { TITLE_STATE, fetchChatTitle, truncateTitle } from './chat-api.js';
import { addDirtyChat } from './chat-list.js';

'use strict';

// ============================================================
// 辅助函数 — 发送前准备
// ============================================================

/**
 * 删除欢迎消息（通过 Alpine store 清空 welcomeMessage）
 * 不再直接操作 DOM，由 Alpine 响应式模板驱动隐藏。
 * @param {HTMLElement} chatContainer - 保留参数以兼容调用方
 */
function removeWelcomeMessage(chatContainer) {
    try {
        var chats = window.Alpine.store('chats');
        if (chats) {
            chats.welcomeMessage = '';
        }
    } catch(e) {}

    // 将输入面板移回原位（.main-body 之后），恢复 margin-top: auto
    var inputArea = document.querySelector('.input-area');
    if (inputArea && inputArea.parentNode?.classList?.contains('welcome-message')) {
        var mainBody = document.getElementById('mainBody');
        if (mainBody) {
            mainBody.parentNode.insertBefore(inputArea, mainBody.nextSibling);
            inputArea.style.marginTop = '';
        }
    }

    // 显式移除 welcome-state，确保 scroll-container 回到块级流式布局（非 flex 居中）。
    // 注：Alpine 的 :class 绑定是"追加式"的，不会移除静态 class 中的同名类，
    // 因此必须通过 JS 显式移除，否则消息内容会被 flex justify-content:center 垂直居中，
    // 导致顶部消息无法通过滚动看到。
    var scrollContainer = document.getElementById('scrollContainer');
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

    // messages 已由 addMessage() → addGroup() 管理，不再需要单独的 messages push

    var chats = window.Alpine.store('chats');
    var activeChat = chats ? chats.active : null;

    if (!activeChat || !activeChat.title) {
        const title = truncateTitle(content);
        if (title) {
            updateHeaderTitle(title);
            if (activeChat) {
                activeChat.titleState = TITLE_STATE.ORIGINAL;
                activeChat.title = title;  // 同步设置 title，使 Alpine store 中的 active.title 立即可用
            }

            // 首次消息：尝试在侧边栏插入一条新对话条目
            // blankChat 尚无 SN，addDirtyChat 会跳过插入，
            // 侧边栏条目将在收到 SSE chat_created 事件后由
            // SSEResponser.onChatCreated() → addDirtyChat() 添加。
            // 匿名用户和登录用户都会添加侧边栏条目
            addDirtyChat(title, activeChat ? activeChat.sn : null);
        }
    }
}

/**
 * 发送前准备：验证输入、清理 UI、初始化状态
 * @returns {{ content: string, createdAt: string, stream: ChatStream } | null}
 */
function prepareChat() {
    const messageInput = document.getElementById('messageInput');
    const chatContainer = document.getElementById('chatContainer');
    const content = messageInput.value.trim();
    if (!content) return null;
    // 仅检查当前活跃对话是否正在流式，不阻塞其他对话的流式状态
    var chats = window.Alpine.store('chats');
    var activeChat = chats ? chats.active : null;
    if (activeChat && activeChat.isStreaming) return null;

    // 清空输入框
    messageInput.value = '';
    messageInput.style.height = 'auto';

    // 删除欢迎消息
    removeWelcomeMessage(chatContainer);

    // 生成 UTC 时间
    const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');

    // 新消息发出，即将滚动到底部，重置用户滚动状态
    var chats = window.Alpine.store('chats');
    if (chats && chats.active) {
        chats.active.userScrolledUp = false;
    }

    // ★ 如果是新对话（blankItem 存在且 activeIndex === -1），立即提升到 items[]
    //    这样在收到后端真实 SN 之前，items[] 中已有该 chat 的条目，
    //    用户切换到其他 chat 后仍可通过临时 SN 切回来。
    if (chats.blankItem && chats.activeIndex === -1) {
        chats.promoteBlankItem();
        // promoteBlankItem 后：
        //   1. blankItem.sn 被设为临时 SN（如 'new_2026-05-31T10-25-30'）
        //   2. blankItem 被移入 items[]，activeIndex 指向它
        //   3. blankItem 置为 null
        //   4. chats.active 现在指向 items[] 中的这个新条目
    }

    // 获取当前对话的 SN（promoteBlankItem 后已有临时 SN）
    var activeChat = chats ? chats.active : null;
    const sn = activeChat ? activeChat.sn : '';
    const stream = chatStreamMgr.getOrCreate(sn);

    // 标记为流式状态（startStreaming 会设置 isStreaming=true 并创建 streamingMsg）
    chats.startStreaming(sn);

    // 添加用户消息
    addUserMessage(content, createdAt);

    // 更新刻度导航
    updateTickNav();

    // 创建空的 assistant 消息占位（通过 Alpine store 数据驱动）
    addMessage('assistant', '', null, true);

    // 等待 Alpine 渲染完成后获取 DOM 引用
    requestAnimationFrame(function() {
        var chatContainer = document.getElementById('chatContainer');
        if (!chatContainer) return;
        var lastGroupEl = chatContainer.querySelector('.message-group:last-child');
        if (!lastGroupEl) return;
        var streamingBubble = lastGroupEl.querySelector('.bubble.streaming');
        var assistantMsgEl = lastGroupEl.querySelector('.message.assistant');
        if (assistantMsgEl) {
            stream.assistantBubble = assistantMsgEl;
            stream.contentDiv = streamingBubble || assistantMsgEl.querySelector('.bubble');
        }
    });

    // 确保流式开始前页面已在底部。
    autoScrollToBottom();

    // 统一切换所有 UI 组件到流式状态
    applyStreamingState(true);

    // 创建 AbortController
    stream.abortController = new AbortController();

    return { content, createdAt, stream };
}

// ============================================================
// SSE 流读取（ChatSession 的方法）
// ============================================================

/**
 * 读取 SSE 流数据，按行解析并分发事件到 SSEResponser
 * @param {Response} response
 * @param {ChatStream} stream
 */
async function readSSEBuffer(response, stream) {
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
                dispatchEventToResponser(event, stream);
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
                dispatchEventToResponser(event, stream);
        } catch (e) {
            console.warn('解析剩余 SSE 事件失败:', jsonStr);
        }
    }
}

/**
 * 将 SSE 事件分发到 session 的 SSEResponser
 * @param {object} event
 * @param {ChatStream} stream
 */
function dispatchEventToResponser(event, stream) {
    const responser = stream.responser;
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

        case 'web_source':
            responser.onWebSource(event);
            break;

        case 'chat_created':
            responser.onChatCreated(event);
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
 * @param {ChatStream} stream
 * @param {string} content
 * @param {string} createdAt
 */
async function fetchStream(stream, content, createdAt) {
    var settings = Alpine.store('settings');

    // 发送请求 — 只传用户最新的一句话，历史由后端维护
    // front_sn 传递前端生成的临时 SN，后端在 chat_created 事件中返回它，
    // 前端据此将临时 SN 替换为真实 SN。
    const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            message: { id: 0, role: 'user', content, created_at: createdAt },
            stream: true,
            deep_think: settings ? settings.deepThink : false,
            web_search_enabled: settings ? settings.webSearch : false,
            front_sn: stream.sn,  // 传递前端临时 SN
        }),
        signal: stream.abortController.signal
    });

    if (!response.ok) {
        const errText = await response.text();
        throw new Error(`服务器错误 [${response.status}]: ${errText}`);
    }

    await readSSEBuffer(response, stream);
}

// ============================================================
// 辅助函数 — 错误处理
// ============================================================

/**
 * 处理 AbortError：更新 reasoning 区域状态，空气泡显示中断提示
 * @param {ChatStream} stream
 */
function handleAbortError(stream) {
    const assistantBubble = stream.assistantBubble;
    if (!assistantBubble) return;

    // 设置 Alpine store 中的 reasoningState = 'done'
    // ★ 中断与完成在角色标签上统一显示"思考完成"，
    //    后端会为中断消息追加 broken message 标记。
    try {
        var chats = window.Alpine.store('chats');
        if (chats) {
            var chatData = chats.getOrCreate(stream.sn);
            if (chatData && chatData.streamingMsg) {
                chatData.streamingMsg.reasoningState = 'done';
            }
            // 同步到 group.assistant，使 Alpine 模板中的 role-label-ai 立即更新
            var groups = chatData.groups;
            if (groups && groups.length > 0) {
                var assistant = groups[groups.length - 1].assistant;
                if (assistant) {
                    assistant.reasoningState = 'done';
                }
            }
        }
    } catch(e) {}

    // 将 reasoning 区域从 active 切换为 done 状态
    const area = assistantBubble.querySelector('.reasoning-area.active');
    if (area) {
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
    const contentDiv = stream.contentDiv;
    if (contentDiv && !contentDiv.textContent.trim()) {
        contentDiv.innerHTML = '⏹ 已中断';
        contentDiv.classList.remove('streaming');
        assistantBubble.classList.add('interrupted');
    }
}

/**
 * 处理流式请求错误
 * @param {Error} err
 * @param {ChatStream} stream
 */
function handleStreamError(err, stream) {
    if (err.name === 'AbortError') {
        // 标记为中断，cleanupAfterStream 据此跳过标题自动修改等操作
        stream.wasAborted = true;
        handleAbortError(stream);
    } else {
        console.error('请求失败:', err);
        if (stream.assistantBubble) {
            showError(stream.assistantBubble, err.message);
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
    //   - 仅在第一组消息时允许（groups.length <= 1，即第一轮对话）
    // 更严格的触发条件：仅在第一轮对话后自动请求 AI 生成标题，后续轮次不再自动触发
    var chats = window.Alpine.store('chats');
    var activeChat = chats ? chats.active : null;
    if (!activeChat) return;
    if (wasAborted || activeChat.titleState >= TITLE_STATE.AI || activeChat.groups.length > 1) return;

    // 原标题：使用当前已有的标题（可能是 AI 已修改过的），而不是重新从第一条消息截取
    // 这样后端可以基于当前标题判断是否需要更新
    // 如果 title 为空（新对话首次发送消息），传空字符串让后端基于对话历史生成
    const originalTitle = activeChat.title || '';
    // 异步调用，不阻塞后续操作
    // 传递当前 chat SN，确保后端返回时前端能精确定位到正确的对话
    fetchChatTitle(originalTitle, false, activeChat.sn);
}

/**
 * 流结束清理：重置状态、恢复 UI、清理定时器、自动修改标题
 * @param {ChatStream} stream
 * @param {boolean} wasAborted  是否被用户中断
 */
function cleanupAfterStream(stream, wasAborted) {
    stream.abortController = null;

    // 更新 Alpine store 中对应 chat 的 isStreaming（无论活跃/后台都必须重置）
    // ★ 异常/中断/断连等非 done 事件路径下，finalizeStreaming 不会被调用，
    //    isStreaming 会永远卡在 true，导致登录按钮等依赖 :disabled 绑定的 UI 永久不可用。
    //    此处统一重置，与 onDone() 中的 finalizeStreaming() 形成互补。
    const chats = window.Alpine.store('chats');
    if (chats) {
        try {
            var chatData = chats.getOrCreate(stream.sn);
            if (chatData) {
                chatData.isStreaming = false;
            }
        } catch(e) {}
    }

    // 仅当此 stream 仍是活跃 stream 时，才操作全局 UI
    // 避免后台流完成时错误地影响当前 active chat 的 UI 状态
    const isActiveStream = chats && chats.active && chats.active.sn === stream.sn;
    if (isActiveStream) {
        applyStreamingState(false);
        document.getElementById('messageInput').focus();
    }

    // 移除 streaming 类
    const contentDiv = stream.contentDiv;
    if (contentDiv) {
        contentDiv.classList.remove('streaming');
    }

    // 清理 reasoning 区域的节流渲染定时器（防止取消请求后定时器残留）
    const assistantBubble = stream.assistantBubble;
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
 *   - 仅在第一组消息后触发（groups.length <= 1）
 */
async function getCurrentChatIfNeeded(wasAborted) {
    // 被中断或非第一轮对话，跳过
    var chats = window.Alpine.store('chats');
    var activeChat = chats ? chats.active : null;
    if (wasAborted || !activeChat || activeChat.groups.length > 1) return;

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
 * sendMessage 发送用户消息并启动 SSE 流式接收。
 *
 * 新对话时 blankItem 保持原位（activeIndex === -1），
 * 后端 ensureDBSession 生成 SN 后通过 SSE chat_created 事件推送，
 * 前端 onChatCreated 处理器负责更新 blankItem.sn 并 promoteBlankItem()。
 */
export async function sendMessage() {
    const chatData = prepareChat();
    if (!chatData) return;

    const { content, createdAt, stream } = chatData;

    try {
        await fetchStream(stream, content, createdAt);
    } catch (err) {
        handleStreamError(err, stream);
    } finally {
        cleanupAfterStream(stream, stream.wasAborted);
    }
}
