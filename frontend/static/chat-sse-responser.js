// ============================================================
// chat-sse-responser.js — SSE 事件响应器抽象层
// ============================================================
//
// 对标后端 infra/llm/sse_responser.go 的 SSEResponser 接口。
// 每个 ChatSession 拥有一个 SSEResponser 实例，负责将 SSE 事件
// 转化为对 Alpine store 中 ChatData.streamingMsg 的累积。
//
// ★ 方案B：流式数据直接写入 group.assistant 的同一字段，
//   流式期间 content/contentHTML/reasoning/reasoningHTML 持续增长，
//   完成时 finalizeStreamingToGroup 只补 metadata（createdAt 等）。
//   模板无需感知 isStreaming，统一渲染 group.assistant。
//
//   当 session 是"活跃的"（chats.active.sn === session.sn）时，
//   DOM 更新立即生效（Alpine 响应式自动渲染）；否则仅累积数据。
// ============================================================

import { renderMarkdown, enableCopyButtons } from './chat-markdown.js';
import { showSources, showTokenUsage, autoScrollToBottom, showError, restoreInputArea, showToast } from './chat-ui.js';

'use strict';

/** SSE 渲染节流间隔（毫秒） */
const SSE_RENDER_INTERVAL = 180;

/** 工具名称 → 图标映射 */
const TOOL_ICONS = {
    web_search: '🔍',
    current_time: '🕐',
    personal_trait_search: '🧑',
};

/**
 * 获取最后一个 group 的 assistant 对象的辅助函数
 * @param {string} sn
 * @returns {object|null} group.assistant
 */
function _getAssistant(sn) {
    try {
        var chats = window.Alpine.store('chats');
        if (!chats) return null;
        var chatData = chats.getOrCreate(sn);
        if (!chatData) return null;
        var groups = chatData.groups;
        if (!groups || groups.length === 0) return null;
        return groups[groups.length - 1].assistant;
    } catch(e) {
        return null;
    }
}

/**
 * SSEResponser — 每个 ChatSession 的 SSE 事件处理器
 *
 * 职责：
 *   1. 将 SSE 事件数据累积到 Alpine store 的 ChatData.streamingMsg
 *   2. 同步到 group.assistant 的同名字段（内容/推理持续增长）
 *   3. 节流渲染 Markdown → HTML
 *
 * 数据源：Alpine.store('chats').getOrCreate(session.sn).streamingMsg
 * 渲染目标：group.assistant.contentHTML / group.assistant.reasoningHTML
 */
export class SSEResponser {
    /**
     * @param {import('./chat-session.js').ChatSession} session
     */
    constructor(session) {
        this.session = session;
        // 节流渲染定时器（content）
        this._renderTimer = null;
        // 节流渲染定时器（reasoning）
        this._reasoningRenderTimer = null;
    }

    /** 判断当前 session 是否是活跃会话 */
    get isActive() {
        try {
            var chats = window.Alpine.store('chats');
            return chats && chats.active && chats.active.sn === this.session.sn;
        } catch(e) {
            return false;
        }
    }

    // ---- Alpine store 辅助 ----

    /**
     * 获取 Alpine store 中当前 session 对应的 streamingMsg。
     * @returns {object|null}
     */
    _getStreamingMsg() {
        try {
            var chats = window.Alpine.store('chats');
            if (!chats) return null;
            var chatData = chats.getOrCreate(this.session.sn);
            return chatData ? chatData.streamingMsg : null;
        } catch(e) {
            return null;
        }
    }

    // ---- 节流渲染（Content）- 直接写入 group.assistant ----

    /**
     * 将 streamingMsg.content 同步到 group.assistant.content，
     * 并触发节流渲染更新 group.assistant.contentHTML。
     */
    _syncContentToAssistant() {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        var assistant = _getAssistant(this.session.sn);
        if (!assistant) return;
        // 直接更新原始文本（让 Alpine 感知变化，但 throttle 控制渲染频率）
        assistant.content = sm.content;
        this._throttleContentRender();
    }

    /**
     * 节流渲染：将 streamingMsg.content 渲染为 HTML 写入 group.assistant.contentHTML。
     */
    _throttleContentRender() {
        if (this._renderTimer) return;
        var self = this;
        this._renderTimer = setTimeout(function() {
            self._renderTimer = null;
            var sm = self._getStreamingMsg();
            if (!sm) return;
            var assistant = _getAssistant(self.session.sn);
            if (!assistant) return;
            assistant.contentHTML = renderMarkdown(sm.content || '');
            autoScrollToBottom();
        }, SSE_RENDER_INTERVAL);
    }

    // ---- 节流渲染（Reasoning）- 直接写入 group.assistant ----

    /**
     * 将 streamingMsg.reasoning 同步到 group.assistant.reasoning，
     * 并触发节流渲染更新 group.assistant.reasoningHTML。
     */
    _syncReasoningToAssistant() {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        var assistant = _getAssistant(this.session.sn);
        if (!assistant) return;
        assistant.reasoning = sm.reasoning;
        this._throttleReasoningRender();
    }

    /**
     * 节流渲染：将 streamingMsg.reasoning 渲染为 HTML 写入 group.assistant.reasoningHTML。
     */
    _throttleReasoningRender() {
        if (this._reasoningRenderTimer) return;
        var self = this;
        this._reasoningRenderTimer = setTimeout(function() {
            self._reasoningRenderTimer = null;
            var sm = self._getStreamingMsg();
            if (!sm) return;
            var assistant = _getAssistant(self.session.sn);
            if (!assistant) return;
            assistant.reasoningHTML = renderMarkdown(sm.reasoning || '');
            autoScrollToBottom();
        }, SSE_RENDER_INTERVAL);
    }

    // ---- 事件处理方法 ----

    /**
     * 处理 reasoning 事件
     * @param {object} event
     */
    onReasoning(event) {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        // 处理 tool-pending：带图标格式
        if (event.subject === 'tool-pending') {
            var icon = TOOL_ICONS[event.tool] || '⚙';
            sm.reasoning += '\n> ' + icon + ' ' + (event.content || '') + '\n';
        } else {
            sm.reasoning += event.content || '';
        }
        // 同步到 group.assistant + 节流渲染 HTML
        this._syncReasoningToAssistant();
    }

    /**
     * 处理 reasoning_end 事件
     */
    onReasoningEnd() {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        sm.reasoningState = 'done';
        // 同步 reasoningState 到 group.assistant
        var assistant = _getAssistant(this.session.sn);
        if (assistant) {
            assistant.reasoningState = 'done';
        }
        // 强制最终渲染 reasoning（清空节流 timer 并立即渲染）
        if (this._reasoningRenderTimer) {
            clearTimeout(this._reasoningRenderTimer);
            this._reasoningRenderTimer = null;
        }
        if (sm.reasoning) {
            var assistant = _getAssistant(this.session.sn);
            if (assistant) {
                assistant.reasoningHTML = renderMarkdown(sm.reasoning);
            }
        }
        autoScrollToBottom();
    }

    /**
     * 处理 text 事件
     * @param {object} event
     */
    onText(event) {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        sm.content += event.content || '';
        // ★ 方案B：直接写入 group.assistant.content + 节流渲染 contentHTML
        this._syncContentToAssistant();
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
        var sm = this._getStreamingMsg();
        if (!sm) return;

        if (event.sources) {
            const newSources = dedupeSources(event.sources, sm.sources || []);
            if (newSources.length > 0) {
                if (!sm.sources) sm.sources = [];
                sm.sources.push(...newSources);
                if (this.isActive) {
                    showSources(newSources, 'rag');
                }
            }
        }
        if (event.web_sources) {
            const newWebSources = dedupeSources(event.web_sources, sm.sources || []);
            if (newWebSources.length > 0) {
                if (!sm.sources) sm.sources = [];
                sm.sources.push(...newWebSources);
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
        var sm = this._getStreamingMsg();
        if (!sm) return;
        sm.isDone = true;
        sm.msgId = event.msg_id || 0;
        sm.createdAt = event.created_at || null;
        sm.usage = event.usage || null;
        sm.reasoningState = 'done';

        // ★ 方案B：finalizeStreamingToGroup 只补 metadata（createdAt/sources/usage），
        //   content/contentHTML/reasoning/reasoningHTML 已在流式期间持续维护。
        try {
            window.Alpine.store('chats').finalizeStreamingToGroup();
        } catch(e) {}

        // 清理 streamingMsg
        try {
            window.Alpine.store('chats').finalizeStreaming(this.session.sn);
        } catch(e) {}

        if (this.isActive) {
            this._applyDoneToDOM(event);
        }

        // 清理节流定时器
        this.session.clearRenderTimer();
        if (this._renderTimer) {
            clearTimeout(this._renderTimer);
            this._renderTimer = null;
        }
        if (this._reasoningRenderTimer) {
            clearTimeout(this._reasoningRenderTimer);
            this._reasoningRenderTimer = null;
        }
    }

    /**
     * 处理 error 事件
     * @param {object} event
     */
    onError(event) {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        sm.error = event.message || '未知错误';
        if (this.isActive) {
            showError(null, sm.error);
        }
    }

    // ---- 内部方法 ----

    /**
     * 将 done 事件应用到 DOM。
     * ★ 方案B：contentHTML/reasoningHTML 已由 Alpine 响应式更新，
     *   此处只处理 Alpine 无法覆盖的 UI 操作（copy buttons、token usage、autoScroll）。
     *
     * @param {object} event
     */
    _applyDoneToDOM(event) {
        const bubble = this.session.assistantBubble;

        // 1. 启用复制按钮
        if (bubble) {
            enableCopyButtons(bubble);
        }

        // 2. 更新 msgId
        const msgId = event.msg_id || 0;
        if (msgId) {
            try {
                var chats = window.Alpine.store('chats');
                if (chats && chats.active) {
                    var lastGroup = chats.active.groups[chats.active.groups.length - 1];
                    if (lastGroup) {
                        lastGroup.msgId = msgId;
                    }
                }
            } catch(e) {}
        }

        // 3. 显示 token 用量
        if (event.usage) {
            try {
                var chats = window.Alpine.store('chats');
                if (chats && chats.active) {
                    var lastGroup = chats.active.groups[chats.active.groups.length - 1];
                    if (lastGroup && lastGroup.assistant) {
                        if (bubble) {
                            showTokenUsage(bubble, event.usage);
                        }
                    }
                }
            } catch(e) {}
        }

        // 4. 滚动 + 恢复输入区
        autoScrollToBottom();
        restoreInputArea();
        setTimeout(function() {
            autoScrollToBottom();
        }, 480);

        // 5. 用户向上滚动时提示
        try {
            var chats = window.Alpine.store('chats');
            if (chats && chats.active && chats.active.userScrolledUp) {
                setTimeout(function() {
                    showToast('AI 回复完毕', 'info');
                }, 500);
            }
        } catch(e) {}
    }

    /**
     * 当 session 从非活跃变为活跃时，确保 group.assistant 中的数据
     * 已经同步到 Alpine 模板可渲染的状态。
     *
     * ★ 方案B：数据始终在 group.assistant 中，Alpine 自动渲染。
     *   此处只需确保 reasoningHTML 已渲染（后台流可能未来得及节流渲染），
     *   以及 sources/usage 等 JS 管理元素的显示。
     *
     * 由 ChatSessionManager.switchTo() 在切换回对话时调用。
     */
    flushToDOM() {
        var sm = this._getStreamingMsg();
        if (!sm) return;

        var assistant = _getAssistant(this.session.sn);
        if (!assistant) return;

        // 确保 contentHTML 已渲染（后台流可能 throttle 未触发）
        if (sm.content && !assistant.contentHTML) {
            assistant.content = sm.content;
            assistant.contentHTML = renderMarkdown(sm.content);
        } else if (sm.content) {
            // 确保原始文本 sync（Alpine 响应式可能检测到 content 变化但 throttle 未触发）
            assistant.content = sm.content;
        }

        // 确保 reasoningHTML 已渲染
        if (sm.reasoning && !assistant.reasoningHTML) {
            assistant.reasoning = sm.reasoning;
            assistant.reasoningHTML = renderMarkdown(sm.reasoning);
        } else if (sm.reasoning) {
            assistant.reasoning = sm.reasoning;
        }

        // 如果已完成但 assistantBubble 存在，显示 sources/usage
        if (this.session.assistantBubble) {
            if (sm.sources && sm.sources.length > 0) {
                showSources(sm.sources, 'web');
            }
            if (sm.isDone && sm.usage) {
                showTokenUsage(this.session.assistantBubble, sm.usage);
            }
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

    const existingUrls = new Set();
    for (const src of existingSources) {
        if (src.url) {
            existingUrls.add(src.url);
        } else if (src.title) {
            existingUrls.add(src.title);
        }
    }

    return newSources.filter(src => {
        const key = src.url || src.title;
        if (!key) return true;
        if (existingUrls.has(key)) return false;
        existingUrls.add(key);
        return true;
    });
}
