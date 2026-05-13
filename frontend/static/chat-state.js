// ============================================================
// chat-state.js — 全局状态管理
// ============================================================
// 所有模块共享的状态变量集中管理，避免循环依赖。
// 模块通过 import { state } from './chat-state.js' 引用。

'use strict';

// ============================================================
// Cookie 工具函数 — 持久化用户偏好（深度思考 / 智能搜索）
// ============================================================

/**
 * Cookie 名称常量
 */
const COOKIE_DEEP_THINK = 'brainforever_deep_think';
const COOKIE_WEB_SEARCH = 'brainforever_web_search';

/**
 * 读取指定名称的 cookie 值
 * @param {string} name - cookie 名称
 * @returns {string|null} cookie 值，不存在则返回 null
 */
export function getCookie(name) {
    const match = document.cookie.match(new RegExp('(?:^|;\\s*)' + encodeURIComponent(name) + '=([^;]*)'));
    return match ? decodeURIComponent(match[1]) : null;
}

/**
 * 设置 cookie（默认 365 天过期，路径为 /）
 * @param {string} name - cookie 名称
 * @param {string} value - cookie 值
 * @param {number} [days=365] - 过期天数
 */
export function setCookie(name, value, days = 365) {
    const expires = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toUTCString();
    document.cookie = encodeURIComponent(name) + '=' + encodeURIComponent(value) +
        '; expires=' + expires + '; path=/';
}

/**
 * 从 cookie 恢复深度思考状态，返回 boolean
 * @returns {boolean}
 */
export function loadDeepThinkFromCookie() {
    const val = getCookie(COOKIE_DEEP_THINK);
    if (val === null) return false; // 默认关闭
    return val === 'true';
}

/**
 * 将深度思考状态保存到 cookie
 * @param {boolean} active
 */
export function saveDeepThinkToCookie(active) {
    setCookie(COOKIE_DEEP_THINK, active ? 'true' : 'false');
}

/**
 * 从 cookie 恢复智能搜索状态，返回 boolean
 * @returns {boolean}
 */
export function loadWebSearchFromCookie() {
    const val = getCookie(COOKIE_WEB_SEARCH);
    if (val === null) return true; // 默认开启
    return val === 'true';
}

/**
 * 将智能搜索状态保存到 cookie
 * @param {boolean} active
 */
export function saveWebSearchToCookie(active) {
    setCookie(COOKIE_WEB_SEARCH, active ? 'true' : 'false');
}

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
    MAX_VISIBLE_TICKS: 9,

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
