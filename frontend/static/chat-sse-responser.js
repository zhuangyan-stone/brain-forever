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
import { showSources, showTokenUsage, autoScrollToBottom, showError, restoreInputArea, showToast, updateSourcesPagerInDOM } from './chat-ui.js';
import { addDirtyChat } from './chat-list.js';
import { chatStreamMgr } from './chat-stream-mgr.js';

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
 * 将 streamingMsg.sources 同步到 group.assistant.sources（Alpine 响应式数据）
 * 同时更新 SwipePager。
 * 仅在活跃 session 且 sources 有数据时调用。
 * @param {string} sn
 */
function _syncWebSourcesToGroup(sn) {
    try {
        var chats = window.Alpine.store('chats');
        if (!chats) return;
        var chatData = chats.getOrCreate(sn);
        if (!chatData || !chatData.streamingMsg) return;
        var groups = chatData.groups;
        if (!groups || groups.length === 0) return;
        var assistant = groups[groups.length - 1].assistant;
        if (!assistant) return;

        // 分组排序：URL 非空的排在前面
        var sources = chatData.streamingMsg.sources || [];
        var withUrl = sources.filter(function(s) { return s.url; });
        var withoutUrl = sources.filter(function(s) { return !s.url; });
        assistant.sources = withUrl.concat(withoutUrl);

        // 使用 Alpine.nextTick 确保 Alpine 完成 DOM 更新后
        // 再操作 SwipePager（SwipePager 需要 DOM 容器已就绪）
        window.Alpine.nextTick(function() {
            if (typeof updateSourcesPagerInDOM === 'function') {
                updateSourcesPagerInDOM(assistant);
            }
        });
    } catch(e) {
        // Alpine store 尚未就绪时静默跳过
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
     * @param {import('./chat-stream.js').ChatStream} stream
     */
    constructor(stream) {
        this.stream = stream;
        // 节流渲染定时器（content）
        this._renderTimer = null;
        // 节流渲染定时器（reasoning）
        this._reasoningRenderTimer = null;
    }

    /** 判断当前 stream 是否是活跃会话 */
    get isActive() {
        try {
            var chats = window.Alpine.store('chats');
            return chats && chats.active && chats.active.sn === this.stream.sn;
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
            var chatData = chats.getOrCreate(this.stream.sn);
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
        var assistant = _getAssistant(this.stream.sn);
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
            var assistant = _getAssistant(self.stream.sn);
            if (!assistant) return;
            console.log('[throttle] 🅲 contentHTML 设置前', `content长度=${(sm.content||'').length} reasoning长度=${(sm.reasoning||'').length}`);
            assistant.contentHTML = renderMarkdown(sm.content || '');
            // 使用 Alpine.nextTick 确保 Alpine 已异步更新 DOM 后再滚动
            // （参考 html_demo/alpine-demo/alpine-throttled-demo2-markdown.html 的 $nextTick 模式）
            window.Alpine.nextTick(function() {
                console.log('[throttle] 🅲 Alpine.nextTick→autoScrollToBottom (content)');
                autoScrollToBottom();
            });
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
        var assistant = _getAssistant(this.stream.sn);
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
            var assistant = _getAssistant(self.stream.sn);
            if (!assistant) return;
            console.log('[throttle] 🅡 reasoningHTML 设置前', `reasoning长度=${(sm.reasoning||'').length} content长度=${(sm.content||'').length}`);
            assistant.reasoningHTML = renderMarkdown(sm.reasoning || '');
            // 使用 Alpine.nextTick 确保 Alpine 已异步更新 DOM 后再滚动
            // （参考 html_demo/alpine-demo/alpine-throttled-demo2-markdown.html 的 $nextTick 模式）
            window.Alpine.nextTick(function() {
                console.log('[throttle] 🅡 Alpine.nextTick→autoScrollToBottom (reasoning)');
                autoScrollToBottom();
            });
        }, SSE_RENDER_INTERVAL);
    }

    // ---- 事件处理方法 ----

    /**
     * 处理 chat_created 事件：后端为新对话生成 SN 后推送给前端。
     * 前端将 blankItem.sn 更新为真实 SN，然后 promoteBlankItem() 将其移入 items[]，
     * 同时更新 session.sn 使后续 SSE 事件能通过 getOrCreate(sn) 找到正确的 ChatData。
     * @param {object} event
     */
    onChatCreated(event) {
        if (!event.sn) return;
        var frontSN = event.front_sn;
        try {
            var chats = window.Alpine.store('chats');
            if (!chats) return;

            if (frontSN) {
                // 1. 先迁移 ChatStreamMgr 的 Map key（避免 Alpine store 先更新后查找不到）
                var stream = chatStreamMgr.streams.get(frontSN);
                if (stream) {
                    chatStreamMgr.streams.delete(frontSN);
                    chatStreamMgr.streams.set(event.sn, stream);
                    stream.sn = event.sn;
                }

                // 2. 再更新 Alpine store items[].sn（原地更新，active.sn 自动反映新值）
                var idx = chats.items.findIndex(function(c) { return c.sn === frontSN; });
                if (idx >= 0) {
                    chats.items[idx].sn = event.sn;
                }

                // 3. 更新侧边栏 currentChats 中的 SN
                try {
                    var chatListModule = window.__chatListModule;
                    if (!chatListModule) {
                        // 动态导入 chat-list.js 获取 currentChats 引用
                        import('./chat-list.js').then(function(mod) {
                            var currentChats = mod.currentChats;
                            if (currentChats) {
                                var chatIdx = currentChats.findIndex(function(c) { return c.sn === frontSN; });
                                if (chatIdx >= 0) {
                                    currentChats[chatIdx].sn = event.sn;
                                    mod.renderChatList(currentChats, event.sn);
                                }
                            }
                        });
                    } else {
                        var currentChats = chatListModule.currentChats;
                        if (currentChats) {
                            var chatIdx = currentChats.findIndex(function(c) { return c.sn === frontSN; });
                            if (chatIdx >= 0) {
                                currentChats[chatIdx].sn = event.sn;
                                chatListModule.renderChatList(currentChats, event.sn);
                            }
                        }
                    }
                } catch(e) {
                    // 侧边栏更新失败不阻塞主流程
                }

                // 4. stream.sn 已在第 1 步中更新，无需重复赋值
            } else {
                // 没有 frontSN 时走旧逻辑：直接更新 session SN
                this.session.sn = event.sn;
            }

            // 更新 Alpine store 的 activeChatSN
            chats.activeChatSN = event.sn;
        } catch(e) {
            console.warn('[SSE] onChatCreated 处理失败:', e);
        }
    }

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
        console.log('[SSE] 🧠 onReasoning', `content长度=${sm.reasoning.length} subject=${event.subject||''}`);
        // 同步到 group.assistant + 节流渲染 HTML
        this._syncReasoningToAssistant();
    }

    /**
     * 处理 reasoning_end 事件
     */
    onReasoningEnd() {
        var sm = this._getStreamingMsg();
        if (!sm) return;
        console.log('[SSE] 🧠 onReasoningEnd');
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
        // 使用 Alpine.nextTick 确保 Alpine 已异步更新 DOM 后再滚动
        window.Alpine.nextTick(function() {
            console.log('[SSE] 🧠 Alpine.nextTick→autoScrollToBottom (reasoningEnd)');
            autoScrollToBottom();
        });
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
                    // ★ Alpine 响应式：同步全量 sources 到 group.assistant，
                    //    由 Alpine 模板 + SwipePager 渲染，不再调用 showSources()。
                    _syncWebSourcesToGroup(this.stream.sn);
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
        // ★ Alpine 响应式：同步全量 sources 到 group.assistant，不再调用 showSources()
        if (this.session.assistantBubble) {
            if (sm.sources && sm.sources.length > 0) {
                _syncWebSourcesToGroup(this.session.sn);
            }
            if (sm.isDone && sm.usage) {
                showTokenUsage(this.session.assistantBubble, sm.usage);
            }
        }

        console.log('[flushToDOM] ✅ 数据同步完成，准备 autoScrollToBottom');
        window.Alpine.nextTick(function() {
            console.log('[flushToDOM] Alpine.nextTick→autoScrollToBottom');
            autoScrollToBottom();
        });
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
