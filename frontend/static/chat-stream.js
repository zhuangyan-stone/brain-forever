// ============================================================
// chat-stream.js — 对话级 SSE 流管理
// ============================================================
// 每个 ChatStream 实例代表一个对话的 SSE 生命周期。
// 包含：abortController、DOM 引用（assistantBubble, contentDiv）、
// wasAborted。
//
// 流式数据（streamingMsg）和流式状态（isStreaming）已迁移到
// Alpine.store('chats') 的 ChatData 中，ChatStream 不再持有。
//
// 由 ChatStreamMgr 统一管理，不直接对外暴露。
// ============================================================

'use strict';

/**
 * ChatStream — 单个对话的 SSE 流
 *
 * 精简后仅保留：
 *   - sn              — 对话 SN
 *   - abortController — SSE 连接控制
 *   - assistantBubble — DOM 引用
 *   - contentDiv      — DOM 引用
 *   - wasAborted      — 中断标记（瞬态，不持久化）
 *
 * isStreaming、streamingMsg、_isActive、renderTimer 已删除，
 * 这些数据现在由 Alpine.store('chats') 的 ChatData 统一管理。
 */
export class ChatStream {
    /**
     * @param {string} sn - 对话 SN
     */
    constructor(sn) {
        this.sn = sn;

        // SSE 连接控制
        this.abortController = null;

        // DOM 引用（由 UI 层设置，SSEResponser 通过它更新 DOM）
        this.assistantBubble = null;
        this.contentDiv = null;

        // 中断标记：当前 SSE 流是否被用户中断（AbortError）
        // 瞬态标记，不持久化，仅用于 cleanupAfterStream 判断
        this.wasAborted = false;

        // ★ 重试数据：保存最后一次发送的消息内容和创建时间，
        //   当网络错误（休眠恢复等）导致 SSE 连接断开时，用户可点击重试重新发送。
        //   在 sendMessage() 中设置，在 retryStream() 中使用。
        //   正常完成后（onDone）会被清除，避免后续误重试。
        /** @type {string|null} */
        this._retryContent = null;
        /** @type {string|null} */
        this._retryCreatedAt = null;
    }

    /**
     * 释放 DOM 引用（切换对话或删除对话时调用）
     */
    releaseDOM() {
        this.assistantBubble = null;
        this.contentDiv = null;
    }

    /**
     * ★ 保存重试数据，供网络断开后重试使用
     * @param {string} content
     * @param {string} createdAt
     */
    saveRetryData(content, createdAt) {
        this._retryContent = content;
        this._retryCreatedAt = createdAt;
    }

    /**
     * ★ 清除重试数据（正常完成后调用）
     */
    clearRetryData() {
        this._retryContent = null;
        this._retryCreatedAt = null;
    }

    /**
     * ★ 判断是否有可重试的数据
     * @returns {boolean}
     */
    hasRetryData() {
        return this._retryContent !== null && this._retryCreatedAt !== null;
    }
}
