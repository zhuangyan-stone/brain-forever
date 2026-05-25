// ============================================================
// chat-session.js — 对话级 SSE 会话管理
// ============================================================
// 每个 ChatSession 实例代表一个对话的 SSE 生命周期。
// 包含：abortController、streamingMsg（累积数据）、
// DOM 引用（assistantBubble, contentDiv）、renderTimer。
//
// 由 ChatSessionManager 统一管理，不直接对外暴露。
// ============================================================

'use strict';

/**
 * ChatSession — 单个对话的 SSE 会话
 */
export class ChatSession {
    /**
     * @param {string} sn - 对话 SN
     */
    constructor(sn) {
        this.sn = sn;

        // SSE 连接控制
        this.abortController = null;
        this.isStreaming = false;

        // 渲染节流定时器
        this.renderTimer = null;

        // 流式累积数据
        this.streamingMsg = {
            reasoning: '',
            content: '',
            webSources: [],
            usage: null,
            msgId: 0,
            createdAt: null,
            error: null,
            isDone: false,
        };

        // DOM 引用（由 UI 层设置，SSEResponser 通过它更新 DOM）
        this.assistantBubble = null;
        this.contentDiv = null;

        // 内部标记：当前 session 是否是活跃会话
        // 由 ChatSessionManager.switchTo() 管理
        this._isActive = false;
    }

    /**
     * 重置流式状态（新一次 SSE 开始前调用）
     */
    resetStreaming() {
        this.streamingMsg = {
            reasoning: '',
            content: '',
            webSources: [],
            usage: null,
            msgId: 0,
            createdAt: null,
            error: null,
            isDone: false,
        };
        this.renderTimer = null;
    }

    /**
     * 清理渲染定时器
     */
    clearRenderTimer() {
        if (this.renderTimer) {
            clearTimeout(this.renderTimer);
            this.renderTimer = null;
        }
    }

    /**
     * 释放 DOM 引用（切换对话或删除对话时调用）
     */
    releaseDOM() {
        this.assistantBubble = null;
        this.contentDiv = null;
    }
}
