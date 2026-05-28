// ============================================================
// chat-session-manager.js — 多会话管理器
// ============================================================
//
// 管理所有对话的 ChatSession 实例。
// 切换对话时，旧对话的 SSE 连接继续在后台接收数据，
// 数据累积到 Alpine store 的 ChatData.streamingMsg 中，不丢失。
//
// 全局单例：export const sessionManager
// ============================================================

import { ChatSession } from './chat-session.js';
import { SSEResponser } from './chat-sse-responser.js';

'use strict';

class ChatSessionManager {
    constructor() {
        /** @type {Map<string, ChatSession>} */
        this.sessions = new Map();
        /** 当前活跃的对话 SN */
        this.activeSessionSN = null;
    }

    /**
     * 获取或创建指定 SN 的 ChatSession
     * @param {string} sn
     * @returns {ChatSession}
     */
    getOrCreate(sn) {
        if (!this.sessions.has(sn)) {
            const session = new ChatSession(sn);
            session.responser = new SSEResponser(session);
            this.sessions.set(sn, session);
        }
        return this.sessions.get(sn);
    }

    /**
     * 获取当前活跃的 ChatSession
     * @returns {ChatSession|null}
     */
    getActive() {
        return this.activeSessionSN ? this.sessions.get(this.activeSessionSN) || null : null;
    }

    /**
     * 切换活跃对话
     *
     * 切换时：
     *   - 旧 session 的 SSEResponser 不再更新 DOM（通过 isActive 判断），
     *     但继续在后台接收数据到 Alpine store
     *   - 新 session 的 SSEResponser 开始更新 DOM
     *   - 如果新 session 有已完成的 streamingMsg，调用 flushToDOM() 渲染
     *
     * @param {string} newSN - 目标对话 SN
     * @returns {ChatSession} 目标对话的 ChatSession
     */
    switchTo(newSN) {
        // 更新活跃 SN
        this.activeSessionSN = newSN;
        const newSession = this.getOrCreate(newSN);

        // 从 Alpine store 检查 streamingMsg 是否已完成
        try {
            var chats = window.Alpine.store('chats');
            if (chats) {
                var chatData = chats.getOrCreate(newSN);
                if (chatData && chatData.streamingMsg && chatData.streamingMsg.isDone) {
                    newSession.responser.flushToDOM();
                }
            }
        } catch(e) {}

        return newSession;
    }

    /**
     * 移除指定 SN 的 ChatSession（对话被删除时调用）
     * @param {string} sn
     */
    remove(sn) {
        const session = this.sessions.get(sn);
        if (session) {
            // 如果有正在进行的 SSE 流，abort 它
            if (session.abortController) {
                session.abortController.abort();
            }
            // 清理渲染定时器
            session.clearRenderTimer();
            // 释放 DOM 引用
            session.releaseDOM();
            this.sessions.delete(sn);
        }
        if (this.activeSessionSN === sn) {
            this.activeSessionSN = null;
        }
    }

    /**
     * 清理所有已完成的非活跃 session（释放内存）
     * 最大保留 MAX_INACTIVE 个非活跃 session
     */
    cleanup() {
        const MAX_INACTIVE = 5;
        const inactiveSessions = [];

        for (const [sn, session] of this.sessions) {
            if (sn === this.activeSessionSN) continue;
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
                inactiveSessions.push({ sn, session });
            }
        }

        // 按创建顺序排序（先创建的先清理）
        if (inactiveSessions.length > MAX_INACTIVE) {
            const toRemove = inactiveSessions.slice(0, inactiveSessions.length - MAX_INACTIVE);
            for (const { sn } of toRemove) {
                const session = this.sessions.get(sn);
                if (session) {
                    session.clearRenderTimer();
                    session.releaseDOM();
                    this.sessions.delete(sn);
                }
            }
        }
    }

    /**
     * 获取当前活跃 session 的 isStreaming 状态
     * 从 Alpine.store('chats') 读取，ChatSession 不再持有此字段。
     * @returns {boolean}
     */
    get isStreaming() {
        try {
            var chats = window.Alpine.store('chats');
            if (chats && chats.active) {
                return !!chats.active.isStreaming;
            }
        } catch(e) {}
        return false;
    }

    /**
     * 获取当前活跃 session 的 abortController
     * @returns {AbortController|null}
     */
    get abortController() {
        const active = this.getActive();
        return active ? active.abortController : null;
    }

    /**
     * 设置当前活跃 session 的 abortController
     * @param {AbortController|null} controller
     */
    set abortController(controller) {
        const active = this.getActive();
        if (active) {
            active.abortController = controller;
        }
    }
}

/** 全局单例 */
export const sessionManager = new ChatSessionManager();
