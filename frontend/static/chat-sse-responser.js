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

    // ---- Alpine store 同步辅助 ----

    /**
     * 获取 Alpine store 中当前 session 对应的 chatData。
     * 用于将 SSE 数据同步到响应式数据模型。
     * @returns {object|null}
     */
    _getChatData() {
        try {
            return window.Alpine.store('chats').getOrCreate(this.session.sn);
        } catch(e) {
            return null;
        }
    }

    /**
     * 将当前 session 的 streamingMsg 同步到 Alpine store。
     * 保持 ChatSession（旧路径）和 Alpine store（新路径）数据一致。
     */
    _syncStreamingToAlpine() {
        var chatData = this._getChatData();
        if (!chatData) return;
        var sm = this.session.streamingMsg;
        if (!chatData.streamingMsg && sm) {
            // ChatSession 已有 streamingMsg 但 Alpine store 尚未建立
            chatData.isStreaming = true;
            chatData.streamingMsg = {
                reasoning: sm.reasoning,
                content: sm.content,
                sources: sm.webSources ? sm.webSources.slice() : [],
                usage: sm.usage,
                msgId: sm.msgId,
                createdAt: sm.createdAt,
                isDone: sm.isDone,
                error: sm.error,
            };
        } else if (chatData.streamingMsg) {
            chatData.streamingMsg.reasoning = sm.reasoning;
            chatData.streamingMsg.content = sm.content;
            chatData.streamingMsg.sources = sm.webSources ? sm.webSources.slice() : [];
            chatData.streamingMsg.isDone = sm.isDone;
            chatData.streamingMsg.error = sm.error;
        }
    }

    // ---- 事件处理方法 ----

    /**
     * 处理 reasoning 事件
     * @param {object} event
     */
    onReasoning(event) {
        this.session.streamingMsg.reasoning += event.content || '';
        this._syncStreamingToAlpine();
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
        this._syncStreamingToAlpine();
        if (this.isActive && this.session.contentDiv) {
            const contentDiv = this.session.contentDiv;
            contentDiv.classList.add('streaming');
            throttleRender(this.session, contentDiv, () => this.session.streamingMsg.content);
        }
    }

    /**
     * 处理 sources 事件
     *
     * 幂等保证：基于 URL 对 sources 做 Set 去重，避免 SSE 推送重复数据。
     * 同时 showSources() 内部也已改为幂等（先移除同类型 section 再重建），
     * 双重防护确保 sources 不会重复显示。
     *
     * @param {object} event
     */
    onSources(event) {
        if (event.sources) {
            const newSources = dedupeSources(event.sources, this.session.streamingMsg.webSources);
            if (newSources.length > 0) {
                this.session.streamingMsg.webSources.push(...newSources);
                this._syncStreamingToAlpine();
                if (this.isActive) {
                    showSources(newSources, 'rag');
                }
            }
        }
        if (event.web_sources) {
            const newWebSources = dedupeSources(event.web_sources, this.session.streamingMsg.webSources);
            if (newWebSources.length > 0) {
                this.session.streamingMsg.webSources.push(...newWebSources);
                this._syncStreamingToAlpine();
                if (this.isActive) {
                    showSources(newWebSources, 'web');
                }
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

        // 同步到 Alpine store：归档 streamingMsg → messages
        this._syncStreamingToAlpine();
        try {
            window.Alpine.store('chats').finalizeStreaming(this.session.sn);
        } catch(e) {}

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
        this._syncStreamingToAlpine();
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

        // 同步到 Alpine store（确保切换回对话时数据一致）
        this._syncStreamingToAlpine();

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

// ============================================================
// 辅助函数 — sources 去重
// ============================================================

/**
 * 对 sources 数组做 Set 去重，基于 URL 判断重复。
 * 仅返回 newSources 中尚未存在于 existingSources 的条目。
 *
 * @param {Array} newSources - 新收到的 sources
 * @param {Array} existingSources - 已累积的 sources
 * @returns {Array} 去重后的新 sources（仅包含真正新增的条目）
 */
function dedupeSources(newSources, existingSources) {
    if (!newSources || newSources.length === 0) return [];
    if (!existingSources || existingSources.length === 0) return newSources;

    // 用 Set 记录已有 sources 的 URL（以 URL 为唯一标识）
    const existingUrls = new Set();
    for (const src of existingSources) {
        if (src.url) {
            existingUrls.add(src.url);
        } else if (src.title) {
            // 无 URL 时用 title 作为兜底标识
            existingUrls.add(src.title);
        }
    }

    return newSources.filter(src => {
        const key = src.url || src.title;
        if (!key) return true; // 无 URL 也无 title，无法去重，保留
        if (existingUrls.has(key)) return false;
        existingUrls.add(key);
        return true;
    });
}
