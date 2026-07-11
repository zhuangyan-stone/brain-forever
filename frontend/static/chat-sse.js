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
import { addMessage, applyStreamingState, showError, showSources, showTokenUsage, autoScrollToBottom, updateHeaderTitle, showToast, showToastHTML, isScrolledToBottom, restoreInputArea } from './chat-ui.js';
import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { updateTickNav } from './chat-ticknav.js';
import { TITLE_STATE, fetchChatTitle, truncateTitle, sendChatMessage } from './chat-api.js';
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
 * 准备发送：验证输入、清理 UI、添加用户消息（立即执行，让用户消息尽快渲染）
 * 返回准备好的数据对象，供后续 initStreaming 和网络请求使用。
 * @returns {{ content: string, createdAt: string, stream: ChatStream, chats: object, sn: string } | null}
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
    }

    // 获取当前对话的 SN（promoteBlankItem 后已有临时 SN）
    var activeChat = chats ? chats.active : null;
    const sn = activeChat ? activeChat.sn : '';
    const stream = chatStreamMgr.getOrCreate(sn);

    // ★ 立即添加用户消息，让 Alpine 在当前 tick 收集 groups[] 变更
    addUserMessage(content, createdAt);

    return { content, createdAt, stream, chats, sn };
}

/**
 * 初始化流式状态：标记 isStreaming、创建 assistant 占位、准备网络请求。
 * 应在 Alpine.nextTick 中执行，确保用户消息气泡已渲染到 DOM。
 * @param {{ content: string, createdAt: string, stream: ChatStream, chats: object, sn: string }} data - prepareChat 返回的数据
 */
function initStreaming(data) {
    const { content, createdAt, stream, chats, sn } = data;

    // 标记为流式状态（startStreaming 会设置 isStreaming=true 并创建 streamingMsg）
    chats.startStreaming(sn);

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
    const response = await sendChatMessage({
        content,
        createdAt,
        stream: true,
        deepThink: settings ? settings.deepThink : false,
        traitSearch: settings ? settings.traitSearch : false,
        webSearch: settings ? settings.webSearch : false,
        frontSn: stream.sn,
        signal: stream.abortController.signal,
    });

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

    try {
        var chats = window.Alpine.store('chats');
        if (!chats) return;
        var chatData = chats.getOrCreate(stream.sn);
        var sm = chatData?.streamingMsg;
        var groups = chatData?.groups;
        var assistant = groups?.[groups.length - 1]?.assistant;
        if (!sm || !assistant) return;

        // ============================================================
        // 1. 设置 reasoningState = 'done'
        //    中断与完成在角色标签上统一显示"思考完成"。
        // ============================================================
        sm.reasoningState = 'done';
        assistant.reasoningState = 'done';

        // ============================================================
        // 2. 补全 createdAt 时间戳
        //    中断时 SSE done 事件未到达，onDone 不会设置 createdAt，
        //    但角色标签（assistantLabel）依赖 assistant.createdAt 显示时间。
        //    若缺失则显示空括号"思考完成 ()"，刷新后才正常。
        //    此处用本地当前时间补充，确保中断后立即显示正确时间。
        // ============================================================
        if (!sm.createdAt) {
            sm.createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
        }
        if (!assistant.createdAt) {
            assistant.createdAt = sm.createdAt;
        }

        // ============================================================
        // 3. 设置 interrupted = 1，触发 Alpine 模板中的 :class 绑定
        //    <div class="message assistant" :class="{
        //        interrupted: group.assistant.interrupted === 1,
        //        failed: group.assistant.interrupted === 2
        //    }">
        //    此标志位仅用于前端样式控制（虚线边框 + 浅色文字），
        //    不再追加额外的中断提示文字。
        // ============================================================
        assistant.interrupted = 1;

        // ============================================================
        // 4. 将 reasoning 区域从 active 切换为 done 状态
        // ============================================================
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

        // ============================================================
        // 5. 立即渲染中断时的最新内容
        //    清除所有节流定时器 + 强制 renderMarkdown，
        //    确保用户看到的是全部已累积内容的最新渲染，而非 180ms 前的快照。
        // ============================================================
        if (stream.responser?._renderTimer) {
            clearTimeout(stream.responser._renderTimer);
            stream.responser._renderTimer = null;
        }
        assistant.contentHTML = renderMarkdown(sm.content || '');

        if (stream.responser?._reasoningRenderTimer) {
            clearTimeout(stream.responser._reasoningRenderTimer);
            stream.responser._reasoningRenderTimer = null;
        }
        if (sm.reasoning) {
            assistant.reasoningHTML = renderMarkdown(sm.reasoning);
        }

        // ============================================================
        // 6. 更新 DOM 状态：移除 streaming 类，添加 interrupted 类
        //    触发 CSS 样式切换：
        //      - 移除流式输出时的隐藏样式（如复制按钮隐藏）
        //      - 添加中断样式（虚线边框、浅色文字）
        // ============================================================
        const contentDiv = stream.contentDiv;
        if (contentDiv) {
            contentDiv.classList.remove('streaming');
        }
        assistantBubble.classList.add('interrupted');

        // ============================================================
        // 7. 自动滚动到底部（让用户看到中断时的完整内容）
        // ============================================================
        autoScrollToBottom();

    } catch(e) {}
}

/**
 * ★ 判断是否为网络错误（连接断开、网络不可达等）
 * 电脑休眠恢复后，浏览器 fetch 连接断开会抛出 TypeError（"Failed to fetch" 等）。
 * @param {Error} err
 * @returns {boolean}
 */
function isNetworkError(err) {
    // AbortError 不视为网络错误（用户主动中止）
    if (err.name === 'AbortError') return false;
    // fetch 网络错误通常是 TypeError
    if (err.name === 'TypeError') return true;
    // 某些浏览器可能抛出含 network/fetch 关键词的错误
    var msg = (err.message || '').toLowerCase();
    return msg.includes('network') || msg.includes('fetch') || msg.includes('econnrefused');
}

/**
 * ★ 处理网络错误：显示"连接已断开"的重试提示 Toast
 * @param {ChatStream} stream
 */
function handleNetworkError(stream) {
    console.warn('[SSE] 网络连接断开，显示重试提示');

    // 显示可点击的 Toast，点击后调用 retryStream 重试
    // 使用长时间（30s）确保用户有时间操作
    showToastHTML(
        '⚠ 连接已断开，<span class="toast-link">点击重新发送</span>',
        'error',
        30000,
        function() {
            retryStream(stream);
        }
    );
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
    } else if (isNetworkError(err) && stream.hasRetryData()) {
        // ★ 网络错误（休眠恢复等导致 SSE 连接断开）：显示重试提示
        //    不清除 retryData，供 retryStream 使用
        handleNetworkError(stream);
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
 * applyAIAutoTitle：仅在第一组消息完成后，自动请求 AI 为对话生成/优化标题。
 *
 * ★ 修复：通过 sn 参数指定目标对话，而非依赖 chats.active。
 *   用户在流式过程中可能已切换到其他对话，activeChat 不再指向流式对话。
 *
 * @param {boolean} wasAborted  是否被用户中断
 * @param {string} sn - 目标对话 SN（来自 ChatStream）
 */
function applyAIAutoTitle(wasAborted, sn) {
    if (wasAborted || !sn) return;

    // 条件：
    //   - 未被中断（AI 回复被掐断时不取标题，因为回复不完整）
    //   - 标题未被修改过（titleState >= 1 表示已被 AI 或用户修改，不再请求）
    //   - 仅在第一组消息时允许（groups.length <= 1，即第一轮对话）
    // ★ 通过 getOrCreate(sn) 获取目标对话，而非依赖 chats.active
    var chats = window.Alpine.store('chats');
    if (!chats) return;
    var chatData = chats.getOrCreate(sn);
    if (!chatData) return;
    if (chatData.titleState >= TITLE_STATE.AI || chatData.groups.length > 1) return;

    // 原标题：使用当前已有的标题（可能是 AI 已修改过的），而不是重新从第一条消息截取
    // 这样后端可以基于当前标题判断是否需要更新
    // 如果 title 为空（新对话首次发送消息），传空字符串让后端基于对话历史生成
    const originalTitle = chatData.title || '';
    // 异步调用，不阻塞后续操作
    // 传递当前 chat SN，确保后端返回时前端能精确定位到正确的对话
    fetchChatTitle(originalTitle, false, sn);
}

/**
 * 流结束清理：重置状态、恢复 UI、清理定时器、自动修改标题
 *
 * ★ 修复：将 stream.sn 传递给 autoUpdateTitle 和 getCurrentChatIfNeeded，
 *   确保它们操作的是正确的对话（流式对话），而非依赖可能已改变的 chats.active。
 *
 * @param {ChatStream} stream
 * @param {boolean} wasAborted  是否被用户中断
 */
function cleanupAfterStream(stream, wasAborted) {
    stream.abortController = null;

    // ★ 在清理前捕获流式对话的 SN（后续可能被其他操作改变）
    const streamSN = stream.sn;

    // 更新 Alpine store 中对应 chat 的 isStreaming（无论活跃/后台都必须重置）
    // ★ 异常/中断/断连等非 done 事件路径下，finalizeStreaming 不会被调用，
    //    isStreaming 会永远卡在 true，导致登录按钮等依赖 :disabled 绑定的 UI 永久不可用。
    //    此处统一重置，与 onDone() 中的 finalizeStreaming() 形成互补。
    const chats = window.Alpine.store('chats');
    if (chats) {
        try {
            var chatData = chats.getOrCreate(streamSN);
            if (chatData) {
                chatData.isStreaming = false;
            }
        } catch(e) {}
    }

    // 仅当此 stream 仍是活跃 stream 时，才操作全局 UI
    // 避免后台流完成时错误地影响当前 active chat 的 UI 状态
    const isActiveStream = chats && chats.active && chats.active.sn === streamSN;
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

    // ★ 兜底清理：清除 SSEResponser 上的节流定时器引用，
    //   防止 handleAbortError 已设置的最终渲染被后续 setTimeout 覆盖。
    if (stream.responser) {
        if (stream.responser._renderTimer) {
            clearTimeout(stream.responser._renderTimer);
            stream.responser._renderTimer = null;
        }
        if (stream.responser._reasoningRenderTimer) {
            clearTimeout(stream.responser._reasoningRenderTimer);
            stream.responser._reasoningRenderTimer = null;
        }
    }

    // 自动请求 AI 生成标题 — 传递 streamSN 确保操作正确的对话
    applyAIAutoTitle(wasAborted, streamSN);

    // 第一轮对话完成后，同步 Alpine store 的 titleState 到侧边栏条目 — 传递 streamSN
    syncSidebarChatEntry(wasAborted, streamSN);
}

/**
 * syncSidebarChatEntry 在第一轮对话完成后，确保侧边栏中有该对话的条目。
 * 同步 Alpine store 的 titleState 到侧边栏条目（而非刷新整个列表）。
 * 这避免了 refreshChatListIfNeeded 的"全量替换"方式可能导致的重复条目和标题覆盖问题。
 *
 * ★ 修复：不再依赖 /api/chat/current（该 API 返回的是后端当前活跃对话的数据，
 *   用户在流式过程中可能已切换到其他对话，此时返回的是错误对话的信息）。
 *   改用本地 Alpine store 中的数据（title/titleState 已在 addUserMessage 时设置）。
 *
 * 条件：
 *   - 未被中断（AI 回复不完整时列表无意义）
 *   - 仅在第一组消息后触发（groups.length <= 1）
 *
 * @param {boolean} wasAborted  是否被用户中断
 * @param {string} sn - 目标对话 SN（来自 ChatStream）
 */
async function syncSidebarChatEntry(wasAborted, sn) {
    // 被中断或缺少 SN，跳过
    if (wasAborted || !sn) return;

    // 通过 getOrCreate(sn) 获取目标对话，而非依赖 chats.active
    var chats = window.Alpine.store('chats');
    if (!chats) return;
    var chatData = chats.getOrCreate(sn);
    if (!chatData || chatData.groups.length > 1) return;

    // 使用本地 Alpine store 数据更新侧边栏条目
    // title 传 undefined 保留 addDirtyChat 已设置的正确标题，不覆盖
    const { updateChatEntry } = await import('./chat-list.js');
    updateChatEntry(sn, undefined, chatData.titleState);
}

// ============================================================
// 主入口
// ============================================================

/**
 * sendMessage 发送用户消息并启动 SSE 流式接收。
 *
 * 分两阶段执行：
 *   阶段一（同步）：验证输入、清理 UI、添加用户消息 → 让 Alpine 尽快渲染用户气泡
 *   阶段二（nextTick）：初始化流式状态、创建 assistant 占位、发起网络请求
 *
 * 新对话时 blankItem 保持原位（activeIndex === -1），
 * 后端 ensureDBSession 生成 SN 后通过 SSE chat_created 事件推送，
 * 前端 onChatCreated 处理器负责更新 blankItem.sn 并 promoteBlankItem()。
 */
export async function sendMessage() {
    const data = prepareChat();
    if (!data) return;

    const { content, createdAt, stream } = data;

    // ★ 保存重试数据：当网络错误（如休眠恢复后连接断开）时，
    //   用户可点击重试重新发送这条消息。
    //   在 retryStream 中使用，新 sendMessage 调用时会自动覆盖。
    stream.saveRetryData(content, createdAt);

    // 使用 Alpine.nextTick 让 Alpine 先完成用户消息的 DOM 渲染，
    // 再执行流式初始化和网络请求，减少用户感知的"消息气泡延迟"
    window.Alpine.nextTick(function() {
        initStreaming(data);

        // 发起网络请求（流式读取）
        (async function() {
            try {
                await fetchStream(stream, content, createdAt);
            } catch (err) {
                handleStreamError(err, stream);
            } finally {
                cleanupAfterStream(stream, stream.wasAborted);
            }
        })();
    });
}

// ============================================================
// ★ SSE 重连 — 网络断开后的恢复机制
// ============================================================

/**
 * ★ retryStream — 在 SSE 连接因网络错误（如电脑休眠恢复）断开后，重新发送消息。
 *
 * 触发条件：
 *   1. sendMessage() 已保存重试数据（stream._retryContent/_retryCreatedAt）
 *   2. handleStreamError 检测到网络错误（TypeError / 含 network/fetch 关键词）
 *   3. 用户点击 Toast 中的"重新发送"链接
 *
 * 流程：
 *   1. 重置 stream 状态（新 AbortController、清除 wasAborted）
 *   2. 刷新 Alpine store 的 streamingMsg（丢弃旧的不完整数据）
 *   3. 重置 assistant group 的内容（准备接收新数据）
 *   4. 重新获取 DOM 引用
 *   5. 重新发起 fetchStream()
 *
 * @param {ChatStream} stream
 */
async function retryStream(stream) {
    if (!stream.hasRetryData()) {
        console.warn('[SSE] 无重试数据，无法重试');
        return;
    }

    const content = stream._retryContent;
    const createdAt = stream._retryCreatedAt;
    console.log('[SSE] 开始重试，SN:', stream.sn);

    // ---- 1. 重置 stream 状态 ----
    stream.wasAborted = false;
    stream.abortController = new AbortController();

    // ---- 2. 刷新 Alpine store 的流式状态 ----
    var chats = window.Alpine.store('chats');
    if (!chats) return;
    var chatData = chats.getOrCreate(stream.sn);
    if (!chatData) return;

    // 重新创建 streamingMsg（丢弃之前的不完整数据，从空白开始接收）
    chatData.isStreaming = true;
    chatData.streamingMsg = {
        reasoning: '',
        content: '',
        sources: [],
        usage: null,
        msgId: 0,
        createdAt: null,
        isDone: false,
        error: null,
        reasoningState: 'thinking',
    };

    // ---- 3. 重置最后一个 group 的 assistant 内容 ----
    var groups = chatData.groups;
    if (groups && groups.length > 0) {
        var lastGroup = groups[groups.length - 1];
        if (lastGroup && lastGroup.assistant) {
            lastGroup.assistant.content = '';
            lastGroup.assistant.contentHTML = '';
            lastGroup.assistant.reasoning = '';
            lastGroup.assistant.reasoningHTML = '';
            lastGroup.assistant.reasoningState = 'thinking';
            lastGroup.assistant.sources = [];
            lastGroup.assistant.createdAt = null;
            // 清除可能被 handleAbortError 设置的中断标记
            lastGroup.assistant.interrupted = 0;
        }
    }

    // ---- 4. 恢复 UI 流式状态 ----
    applyStreamingState(true);

    // ---- 5. 重新获取 DOM 引用 ----
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

            // 清除气泡上的错误/中断样式
            assistantMsgEl.classList.remove('interrupted');
            if (stream.contentDiv) {
                stream.contentDiv.classList.remove('streaming');
                stream.contentDiv.classList.add('streaming');
            }
        }
    });

    autoScrollToBottom();

    // ---- 6. 重新发起 SSE 请求 ----
    try {
        await fetchStream(stream, content, createdAt);
        // ★ 重试成功：清除重试数据（后续正常 cleanupAfterStream 中 wasAborted=false）
    } catch (err) {
        // ★ 重试仍然失败：更新错误提示
        if (err.name === 'AbortError') {
            // 用户主动中断
            stream.wasAborted = true;
            handleAbortError(stream);
        } else if (isNetworkError(err)) {
            // 网络仍然不可达：再次显示重试提示
            handleNetworkError(stream);
            // 不执行 cleanupAfterStream，保持 isStreaming 状态供再次重试
            return;
        } else {
            // 其他错误
            console.error('[SSE] 重试请求失败:', err);
            if (stream.assistantBubble) {
                showError(stream.assistantBubble, err.message);
            }
        }
    }

    // ★ 只有在非网络错误（或重试成功）时才 cleanup
    cleanupAfterStream(stream, stream.wasAborted);
}
