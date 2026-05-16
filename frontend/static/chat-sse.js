// ============================================================
// chat-sse.js — SSE 流处理 + 事件分发
// ============================================================

import { state, resetStreamingState } from './chat-state.js';
import { addMessage, setInputEnabled, updateDeleteButtons, showError, showSources, showTokenUsage, scrollToBottom, stopSearchHintsAnimation, updateHeaderTitle } from './chat-ui.js';
import { handleReasoningEvent, finalizeReasoningArea } from './chat-reasoning.js';
import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { updateTickNav } from './chat-ticknav.js';
import { TITLE_STATE, fetchSessionTitle, truncateTitle } from './chat-api.js';

'use strict';

/**
 * 对 contentDiv 执行节流渲染（累积 Markdown → HTML）
 * @param {HTMLElement} contentDiv
 */
function scheduleContentRender(contentDiv) {
    if (!state.renderTimer) {
        state.renderTimer = setTimeout(() => {
            state.renderTimer = null;
            contentDiv.innerHTML = renderMarkdown(state.accumulatedMarkdown);
            scrollToBottom();
        }, state.RENDER_INTERVAL);
    }
}

/**
 * 处理 text 事件
 * @param {object} event
 * @param {HTMLElement} assistantBubble
 * @param {HTMLElement} contentDiv
 */
function handleTextEvent(event, assistantBubble, contentDiv) {
    // 停止搜索提示的闪烁动画
    stopSearchHintsAnimation(assistantBubble);

    // 累积原始 Markdown，实时渲染为 HTML（带节流）
    if (contentDiv) {
        state.accumulatedMarkdown += event.content;
        contentDiv.classList.add('streaming');
        scheduleContentRender(contentDiv);
    }
}

/**
 * 处理 sources 事件
 * @param {object} event
 */
function handleSourcesEvent(event) {
    if (event.sources) {
        showSources(event.sources, 'rag');
    }
    if (event.web_sources) {
        showSources(event.web_sources, 'web');
    }
}

/**
 * 处理 done 事件
 * @param {object} event
 * @param {HTMLElement} assistantBubble
 * @param {HTMLElement} contentDiv
 */
function handleDoneEvent(event, assistantBubble, contentDiv) {
    // 流结束：如果 reasoning 区域仍处于 active 状态（没有 text 事件），标记为完成
    finalizeReasoningArea(assistantBubble);

    // 停止搜索提示的闪烁动画（安全兜底）
    stopSearchHintsAnimation(assistantBubble);

    // 流结束：确保最后一次渲染完成，保存纯文本到 messages
    if (!contentDiv) return;

    contentDiv.classList.remove('streaming');
    // 清除未执行的节流定时器，立即做最终渲染
    if (state.renderTimer) {
        clearTimeout(state.renderTimer);
        state.renderTimer = null;
    }
    // 最终渲染为 HTML
    contentDiv.innerHTML = renderMarkdown(state.accumulatedMarkdown);
    // 启用所有复制按钮（流已结束）
    enableCopyButtons(assistantBubble);
    // 后端返回的消息 ID（前端之前传 0，由后端分配）
    const msgId = event.msg_id || 0;
    if (msgId) {
        // 更新本地 messages 数组中最新一条 id===0 的用户消息的 ID
        for (let i = state.messages.length - 1; i >= 0; i--) {
            if (state.messages[i].role === 'user' && state.messages[i].id === 0) {
                state.messages[i].id = msgId;
                break;
            }
        }
        // 将 data-msg-id 设置在 message-group 上（同一组的 user 和 assistant 共享同一 ID）
        const group = assistantBubble.closest('.message-group');
        if (group) {
            group.dataset.msgId = msgId;
        }
    }
    // AI 回复复用用户消息的 ID（source ID）
    const usage = event.usage || null;
    state.messages.push({ role: 'assistant', content: state.accumulatedMarkdown, id: msgId, usage });
    // 显示 token 用量信息
    if (event.usage) {
        showTokenUsage(assistantBubble, event.usage);
    }
    // 重置累积变量，为下一次流式做准备
    state.accumulatedMarkdown = '';
    scrollToBottom();
}

/**
 * 处理 reasoning_end 事件：标记 reasoning 区域为"思考完成"
 * @param {object} event
 * @param {HTMLElement} assistantBubble
 */
function handleReasoningEndEvent(event, assistantBubble) {
    finalizeReasoningArea(assistantBubble);
}

/**
 * handleSSEEvent 根据事件类型分发到对应的处理函数
 * @param {object} event
 * @param {HTMLElement} assistantBubble
 */
function handleSSEEvent(event, assistantBubble) {
    const contentDiv = assistantBubble.querySelector('.bubble');

    switch (event.type) {
        case 'reasoning':
            handleReasoningEvent(event, assistantBubble);
            break;

        case 'reasoning_end':
            handleReasoningEndEvent(event, assistantBubble);
            break;

        case 'text':
            handleTextEvent(event, assistantBubble, contentDiv);
            break;

        case 'sources':
            handleSourcesEvent(event);
            break;

        case 'done':
            handleDoneEvent(event, assistantBubble, contentDiv);
            break;

        case 'error':
            showError(assistantBubble, event.message);
            break;

        default:
            console.warn('未知事件类型:', event.type);
    }
}

/**
 * sendMessage 发送用户消息并启动 SSE 流式接收
 */
export async function sendMessage() {
    const messageInput = document.getElementById('messageInput');
    const chatContainer = document.getElementById('chatContainer');
    const content = messageInput.value.trim();
    if (!content || state.isStreaming) return;

    // 清空输入框
    messageInput.value = '';
    messageInput.style.height = 'auto';

    // 删除欢迎消息（用户发出第一条消息时）
    const welcomeEl = chatContainer.querySelector('.welcome-message');
    if (welcomeEl) {
        // 将输入区域移回 main-content 底部（其原始位置）
        const inputArea = welcomeEl.querySelector('.input-area');
        if (inputArea) {
            const mainContent = document.querySelector('.main-content');
            if (mainContent) {
                mainContent.appendChild(inputArea);
            }
        }
        welcomeEl.remove();
        // 移除欢迎状态标记
        const scrollContainer = document.getElementById('scrollContainer');
        if (scrollContainer) {
            scrollContainer.classList.remove('welcome-state');
        }
    }

    // 生成 UTC 时间
    const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'); // UTC, e.g. "2026-05-02T16:30:00Z"

    // 添加用户消息到界面（ID 由后端分配，先传 0）
    const userDiv = addMessage('user', content);
    const userEntry = { role: 'user', content, id: 0, created_at: createdAt };
    state.messages.push(userEntry);

    // 如果当前没有标题（新对话首次发送消息），从第一条用户消息截取标题
    if (!state.dialogTitle) {
        const title = truncateTitle(content);
        if (title) {
            updateHeaderTitle(title);
            state.titleState = TITLE_STATE.ORIGINAL;
        }
    }

    // 更新刻度导航
    updateTickNav();

    // 禁用输入
    setInputEnabled(false);

    // 创建空的 assistant 消息占位
    const assistantBubble = addMessage('assistant', '', true);
    state.isStreaming = true;
    // 禁用所有删除按钮（流式请求进行中禁止删除）
    updateDeleteButtons();

    // 创建 AbortController
    state.abortController = new AbortController();

    try {
        // 锁定本轮会话的深度思考状态（防止流式过程中用户乱点按钮导致状态漂移）
        state.sessionDeepThinkingEnabled = state.deepThinkActive;

        // 发送请求 — 只传用户最新的一句话，历史由后端维护
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                message: { id: 0, role: 'user', content: content, created_at: createdAt },
                stream: true,
                deep_think: state.deepThinkActive,
                web_search_enabled: state.webSearchActive
            }),
            signal: state.abortController.signal
        });

        if (!response.ok) {
            const errText = await response.text();
            throw new Error(`服务器错误 [${response.status}]: ${errText}`);
        }

        // 读取 SSE 流
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
                    handleSSEEvent(event, assistantBubble);
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
                handleSSEEvent(event, assistantBubble);
            } catch (e) {
                console.warn('解析剩余 SSE 事件失败:', jsonStr);
            }
        }

    } catch (err) {
        if (err.name === 'AbortError') {
            // 请求已取消，不做处理
        } else {
            console.error('请求失败:', err);
            showError(assistantBubble, err.message);
        }
    } finally {
        state.isStreaming = false;
        state.abortController = null;
        setInputEnabled(true);
        updateDeleteButtons();
        messageInput.focus();

        // 移除 streaming 类
        const contentDiv = assistantBubble.querySelector('.bubble');
        if (contentDiv) {
            contentDiv.classList.remove('streaming');
        }

        // 清理渲染状态（防止取消请求后定时器残留）
        resetStreamingState();

        // 清理 reasoning 区域的节流渲染定时器（防止取消请求后定时器残留）
        const reasoningContentEl = assistantBubble.querySelector('.reasoning-content');
        if (reasoningContentEl && reasoningContentEl.renderTimer) {
            clearTimeout(reasoningContentEl.renderTimer);
            reasoningContentEl.renderTimer = null;
        }

        // ---- 标题自动修改逻辑 ----
        // 条件：标题未被用户手动修改（titleState !== 2），且对话未超过 3 轮
        // 一轮指一对 user+assistant 消息，所以消息总数 ≤ 6 即不超过 3 轮
        // 前三轮每次 AI 回复后都尝试优化标题，让 AI 基于更多对话内容生成更准确的标题
        if (state.titleState !== TITLE_STATE.USER && state.messages.length <= 6) {
            // 原标题：使用当前已有的标题（可能是 AI 已修改过的），而不是重新从第一条消息截取
            // 这样后端可以基于当前标题判断是否需要更新
            // 如果 dialogTitle 为空（新对话首次发送消息），传空字符串让后端基于对话历史生成
            const originalTitle = state.dialogTitle || '';
            // 异步调用，不阻塞后续操作
            fetchSessionTitle(originalTitle);
        }
    }
}
