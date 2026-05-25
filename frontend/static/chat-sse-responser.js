// ============================================================
// chat-sse-responser.js — SSE 事件响应器抽象层
// ============================================================
//
// 对标后端 infra/llm/sse_responser.go 的 SSEResponser 接口。
// 每个 ChatSession 拥有一个 SSEResponser 实例，负责将 SSE 事件
// 转化为对 streamingMsg 的累积 + DOM 更新。
//
// 当 session 是"活跃的"（activeSessionSN === session.sn）时，
// DOM 更新立即生效；否则仅累积数据，不操作 DOM。
// ============================================================

import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { handleReasoningEvent, finalizeReasoningArea, restoreReasoningArea } from './chat-reasoning.js';
import { showSources, showTokenUsage, autoScrollToBottom, showError, restoreInputArea, showToast, throttleRender } from './chat-ui.js';
import { state } from './chat-state.js';

'use strict';

/**
 * SSEResponser — 每个 ChatSession 的 SSE 事件处理器
 *
 * 职责：
 *   1. 将 SSE 事件数据累积到 session.streamingMsg
 *   2. 更新关联的 DOM（assistantBubble）
 *
 * 当 session 是"活跃的"（_isActive === true）时，
 * DOM 更新立即生效；否则仅累积数据，不操作 DOM。
 */
export class SSEResponser {
    /**
     * @param {import('./chat-session.js').ChatSession} session
     */
    constructor(session) {
        this.session = session;
    }

    /** 判断当前 session 是否是活跃会话 */
    get isActive() {
        return this.session._isActive;
    }

    // ---- 事件处理方法 ----

    /**
     * 处理 reasoning 事件
     * @param {object} event
     */
    onReasoning(event) {
        this.session.streamingMsg.reasoning += event.content || '';
        if (this.isActive && this.session.assistantBubble) {
            handleReasoningEvent(event, this.session.assistantBubble);
        }
    }

    /**
     * 处理 reasoning_end 事件
     */
    onReasoningEnd() {
        if (this.isActive && this.session.assistantBubble) {
            finalizeReasoningArea(this.session.assistantBubble);
        }
    }

    /**
     * 处理 text 事件
     * @param {object} event
     */
    onText(event) {
        this.session.streamingMsg.content += event.content || '';
        if (this.isActive && this.session.contentDiv) {
            const contentDiv = this.session.contentDiv;
            contentDiv.classList.add('streaming');
            throttleRender(this.session, contentDiv, () => this.session.streamingMsg.content);
        }
    }

    /**
     * 处理 sources 事件
     * @param {object} event
     */
    onSources(event) {
        if (event.sources) {
            this.session.streamingMsg.webSources.push(...(event.sources || []));
            if (this.isActive) {
                showSources(event.sources, 'rag');
            }
        }
        if (event.web_sources) {
            this.session.streamingMsg.webSources.push(...(event.web_sources || []));
            if (this.isActive) {
                showSources(event.web_sources, 'web');
            }
        }
    }

    /**
     * 处理 done 事件
     * @param {object} event
     */
    onDone(event) {
        const msg = this.session.streamingMsg;
        msg.isDone = true;
        msg.msgId = event.msg_id || 0;
        msg.createdAt = event.created_at || null;
        msg.usage = event.usage || null;

        if (this.isActive && this.session.assistantBubble) {
            this._applyDoneToDOM(event);
        }
    }

    /**
     * 处理 error 事件
     * @param {object} event
     */
    onError(event) {
        this.session.streamingMsg.error = event.message || '未知错误';
        if (this.isActive && this.session.assistantBubble) {
            showError(this.session.assistantBubble, this.session.streamingMsg.error);
        }
    }

    // ---- 内部方法 ----

    /**
     * 将 done 事件应用到 DOM
     * @param {object} event
     */
    _applyDoneToDOM(event) {
        const bubble = this.session.assistantBubble;
        const contentDiv = this.session.contentDiv;
        if (!contentDiv) return;

        // 1. 完成 reasoning
        finalizeReasoningArea(bubble);

        // 2. 最终渲染 content
        contentDiv.classList.remove('streaming');
        this.session.clearRenderTimer();
        contentDiv.innerHTML = renderMarkdown(this.session.streamingMsg.content);
        enableCopyButtons(bubble);

        // 3. 更新 msgId
        const msgId = event.msg_id || 0;
        if (msgId) {
            for (let i = state.messages.length - 1; i >= 0; i--) {
                if (state.messages[i].role === 'user' && state.messages[i].id === 0) {
                    state.messages[i].id = msgId;
                    break;
                }
            }
            const group = bubble.closest('.message-group');
            if (group) {
                group.dataset.msgId = msgId;
            }
        }

        // 4. 推入 messages
        const usage = event.usage || null;
        const assistantCreatedAt = event.created_at || null;
        state.messages.push({
            role: 'assistant',
            content: this.session.streamingMsg.content,
            id: msgId,
            usage,
            created_at: assistantCreatedAt,
        });

        // 5. 显示时间
        if (assistantCreatedAt) {
            const d = new Date(assistantCreatedAt);
            const hh = String(d.getHours()).padStart(2, '0');
            const mm = String(d.getMinutes()).padStart(2, '0');
            const ss = String(d.getSeconds()).padStart(2, '0');
            const timeText = `(${hh}:${mm}:${ss})`;

            const reasoningBadge = bubble.querySelector('.reasoning-role-badge');
            if (reasoningBadge) {
                const badgeText = reasoningBadge.textContent || 'AI';
                const cleanBadge = badgeText.replace(/\s*\(\d{2}:\d{2}:\d{2}\)/, '');
                reasoningBadge.textContent = `${cleanBadge} ${timeText}`;
            } else {
                const roleLabel = bubble.querySelector('.role-label');
                if (roleLabel) {
                    const roleText = '🤖 AI';
                    roleLabel.textContent = `${roleText} ${timeText}`;
                }
            }
        }

        // 6. 显示 token 用量
        if (event.usage) {
            showTokenUsage(bubble, event.usage);
        }

        // 7. 清理 + 滚动
        autoScrollToBottom();
        restoreInputArea();
        setTimeout(() => {
            autoScrollToBottom();
        }, 480);

        if (state.userScrolledUp) {
            setTimeout(() => {
                showToast('AI 回复完毕', 'info');
            }, 500);
        }
    }

    /**
     * 当 session 从非活跃变为活跃时，将累积的 streamingMsg 渲染到 DOM。
     * 由 ChatSessionManager.switchTo() 在切换回对话时调用。
     *
     * 支持两种场景：
     *   1. isDone === true  — 流已完成，完整渲染（含 usage、最终 markdown 等）
     *   2. isDone === false — 流仍在后台进行，渲染已有累积内容（不含 usage、最终样式）
     */
    flushToDOM() {
        const msg = this.session.streamingMsg;

        // 如果还没有 assistantBubble，无法渲染
        if (!this.session.assistantBubble) {
            return;
        }

        // 渲染 reasoning
        if (msg.reasoning) {
            restoreReasoningArea(this.session.assistantBubble, msg.reasoning);
        }

        // 渲染 content
        if (msg.content && this.session.contentDiv) {
            if (msg.isDone) {
                // 流已完成：最终渲染（markdown + 移除 streaming 类）
                this.session.contentDiv.innerHTML = renderMarkdown(msg.content);
                this.session.contentDiv.classList.remove('streaming');
                enableCopyButtons(this.session.assistantBubble);
            } else {
                // 流未完成：渲染已有纯文本内容，保留 streaming 类
                this.session.contentDiv.innerHTML = msg.content;
                this.session.contentDiv.classList.add('streaming');
            }
        }

        // 渲染 sources
        if (msg.webSources.length > 0) {
            showSources(msg.webSources, 'web');
        }

        // 仅流已完成时渲染 usage
        if (msg.isDone && msg.usage) {
            showTokenUsage(this.session.assistantBubble, msg.usage);
        }

        autoScrollToBottom();
    }
}
