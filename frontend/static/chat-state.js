// ============================================================
// chat-state.js — 全局状态管理
// ============================================================
// 所有模块共享的状态变量集中管理，避免循环依赖。
// 模块通过 import { state } from './chat-state.js' 引用。

'use strict';

/**
 * 全局状态对象
 * 所有可变状态集中在此，各模块通过 state.xxx 读写。
 */
export const state = {
    // 消息历史 [{role, content, id, usage}]
    messages: [],

    // 是否正在流式接收
    isStreaming: false,

    // 用于取消请求的 AbortController
    abortController: null,

    // 深度思考按钮状态
    deepThinkActive: false,

    // 智能搜索按钮状态
    webSearchActive: true,

    // 当前会话的快照：发送消息时锁定，SSE 处理期间基于此判断
    sessionDeepThinkingEnabled: false,

    // 用户消息计数（用于生成 data-msg-index）
    userMsgCount: 0,

    // 当前活动刻度的索引，-1 表示无活动刻度
    activeTickIndex: -1,

    // 当前消息组，用于将同一问答对的 user + assistant 包裹在 .message-group 内
    currentGroup: null,

    // 流式渲染相关
    accumulatedMarkdown: '',
    renderTimer: null,

    // 渲染节流间隔（毫秒）
    RENDER_INTERVAL: 120,

    // 最多同时显示的刻度数
    MAX_VISIBLE_TICKS: 10,

    // 刻度滚动偏移量（0 表示从第一条开始显示）
    tickScrollOffset: 0,

    // 发送模式状态: false = Enter发送/Shift+Enter换行, true = Enter换行/Shift+Enter发送
    sendModeAlternate: false,
};

/**
 * 重置流式渲染状态（取消请求后清理）
 */
export function resetStreamingState() {
    state.accumulatedMarkdown = '';
    if (state.renderTimer) {
        clearTimeout(state.renderTimer);
        state.renderTimer = null;
    }
}
