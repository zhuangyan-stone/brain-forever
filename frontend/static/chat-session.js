// ============================================================
// chat-session.js — 对话级 SSE 会话管理
// ============================================================
// 每个 ChatSession 实例代表一个对话的 SSE 生命周期。
// 包含：abortController、DOM 引用（assistantBubble, contentDiv）、
// renderTimer、wasAborted。
//
// 流式数据（streamingMsg）和流式状态（isStreaming）已迁移到
// Alpine.store('chats') 的 ChatData 中，ChatSession 不再持有。
//
// 由 ChatSessionManager 统一管理，不直接对外暴露。
// ============================================================

'use strict';

/**
 * ChatSession — 单个对话的 SSE 会话
 *
 * 精简后仅保留：
 *   - sn              — 对话 SN
 *   - abortController — SSE 连接控制
 *   - renderTimer     — 渲染节流定时器
 *   - assistantBubble — DOM 引用
 *   - contentDiv      — DOM 引用
 *   - wasAborted      — 中断标记（瞬态，不持久化）
 *
 * isStreaming、streamingMsg、_isActive 已删除，
 * 这些数据现在由 Alpine.store('chats') 的 ChatData 统一管理。
 */
export class ChatSession {
    /**
     * @param {string} sn - 对话 SN
     */
    constructor(sn) {
        this.sn = sn;

        // SSE 连接控制
        this.abortController = null;

        // 渲染节流定时器
        this.renderTimer = null;

        // DOM 引用（由 UI 层设置，SSEResponser 通过它更新 DOM）
        this.assistantBubble = null;
        this.contentDiv = null;

        // 中断标记：当前 SSE 流是否被用户中断（AbortError）
        // 瞬态标记，不持久化，仅用于 cleanupAfterStream 判断
        this.wasAborted = false;
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
