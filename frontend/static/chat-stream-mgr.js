// ============================================================
// chat-stream-mgr.js — 多对话 SSE 流管理器
// ============================================================
//
// 管理所有对话的 ChatStream 实例。
// 切换对话时，旧对话的 SSE 连接继续在后台接收数据，
// 数据累积到 Alpine store 的 ChatData.streamingMsg 中，不丢失。
//
// ★ 不再追踪"活跃"状态 — 统一由 Alpine.store('chats').active 负责。
//
// 全局单例：export const chatStreamMgr
// ============================================================

import { ChatStream } from './chat-stream.js';
import { SSEResponser } from './chat-sse-responser.js';

'use strict';

class ChatStreamMgr {
    constructor() {
        /** @type {Map<string, ChatStream>} */
        this.streams = new Map();
    }

    /**
     * 获取或创建指定 SN 的 ChatStream
     * @param {string} sn
     * @returns {ChatStream}
     */
    getOrCreate(sn) {
        if (!this.streams.has(sn)) {
            const stream = new ChatStream(sn);
            stream.responser = new SSEResponser(stream);
            this.streams.set(sn, stream);
        }
        return this.streams.get(sn);
    }

    /**
     * 获取指定 SN 的 ChatStream（不存在时返回 null）
     * @param {string} sn
     * @returns {ChatStream|null}
     */
    get(sn) {
        return this.streams.get(sn) || null;
    }

    /**
     * 移除指定 SN 的 ChatStream（对话被删除时调用）
     * @param {string} sn
     */
    remove(sn) {
        const stream = this.streams.get(sn);
        if (stream) {
            // 如果有正在进行的 SSE 流，abort 它
            if (stream.abortController) {
                stream.abortController.abort();
            }
            // 释放 DOM 引用
            stream.releaseDOM();
            this.streams.delete(sn);
        }
    }

    /**
     * 清理所有已完成的非活跃 stream（释放内存）
     * 从 Alpine store 读取 active SN（方案B）
     * 最大保留 MAX_INACTIVE 个非活跃 stream
     */
    cleanup() {
        const MAX_INACTIVE = 5;
        const activeSN = (() => {
            try {
                return window.Alpine.store('chats')?.active?.sn;
            } catch(e) { return null; }
        })();

        const inactiveStreams = [];
        for (const [sn, stream] of this.streams) {
            if (sn === activeSN) continue;
            // 从 Alpine store 检查 streamingMsg 是否已完成
            var isDone = false;
            try {
                var chats = window.Alpine.store('chats');
                if (chats) {
                    var chatData = chats.getOrCreate(sn);
                    if (chatData && chatData.streamingMsg) {
                        isDone = chatData.streamingMsg.isDone;
                    }
                }
            } catch(e) {}
            if (isDone) {
                inactiveStreams.push({ sn, stream });
            }
        }

        // 按创建顺序排序（先创建的先清理）
        if (inactiveStreams.length > MAX_INACTIVE) {
            const toRemove = inactiveStreams.slice(0, inactiveStreams.length - MAX_INACTIVE);
            for (const { sn } of toRemove) {
                const stream = this.streams.get(sn);
                if (stream) {
                    stream.releaseDOM();
                    this.streams.delete(sn);
                }
            }
        }
    }

    /**
     * 为即将激活的 chat 做准备工作：
     * 获取/创建 ChatStream + 检查 streamingMsg 完成状态并刷 DOM
     * 不设置任何"活跃"状态（由 Alpine.store('chats').switchTo() 负责）
     *
     * @param {string} sn - 目标对话 SN
     * @returns {ChatStream} 目标对话的 ChatStream
     */
    activateSession(sn) {
        const stream = this.getOrCreate(sn);

        // 从 Alpine store 检查 streamingMsg 是否已完成
        try {
            var chats = window.Alpine.store('chats');
            if (chats) {
                var chatData = chats.getOrCreate(sn);
                if (chatData && chatData.streamingMsg && chatData.streamingMsg.isDone) {
                    stream.responser.flushToDOM();
                }
            }
        } catch(e) {}

        return stream;
    }
}

/** 全局单例 */
export const chatStreamMgr = new ChatStreamMgr();
