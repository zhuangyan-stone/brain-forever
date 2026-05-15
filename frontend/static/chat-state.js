// ============================================================
// chat-state.js — 全局状态管理
// ============================================================
// 所有模块共享的状态变量集中管理，避免循环依赖。
// 模块通过 import { state } from './chat-state.js' 引用。

'use strict';

// ============================================================
// localStorage 键名
// ============================================================

const LS_KEY_SETTINGS = 'brainforever_settings';

// ============================================================
// UserSettings — 统一用户配置（JSON 序列化到 localStorage）
// ============================================================
// 字段说明：
//   sendMode   — 0: Enter发送/Shift+Enter换行, 1: Enter换行/Shift+Enter发送
//   deepThink  — 是否深度思考
//   webSearch  — 是否智能搜索
//   theme      — 0: 明亮, 1: 暗色, 2: 跟随系统（保留值，主页切换仅用 0/1）

const DEFAULT_SETTINGS = {
    sendMode: 0,
    deepThink: true,
    webSearch: true,
    theme: 0,
};

export const UserSettings = {
    /** 当前运行时设置（内存副本） */
    sendMode: DEFAULT_SETTINGS.sendMode,
    deepThink: DEFAULT_SETTINGS.deepThink,
    webSearch: DEFAULT_SETTINGS.webSearch,
    theme: DEFAULT_SETTINGS.theme,

    /**
     * 从 localStorage 加载设置，合并到内存
     */
    load() {
        try {
            const raw = localStorage.getItem(LS_KEY_SETTINGS);
            if (raw) {
                const parsed = JSON.parse(raw);
                this.sendMode = typeof parsed.sendMode === 'number' ? parsed.sendMode : DEFAULT_SETTINGS.sendMode;
                this.deepThink = typeof parsed.deepThink === 'boolean' ? parsed.deepThink : DEFAULT_SETTINGS.deepThink;
                this.webSearch = typeof parsed.webSearch === 'boolean' ? parsed.webSearch : DEFAULT_SETTINGS.webSearch;
                this.theme = typeof parsed.theme === 'number' ? parsed.theme : DEFAULT_SETTINGS.theme;
            }
        } catch (_) {
            // localStorage 数据损坏时使用默认值
        }
    },

    /**
     * 将当前设置序列化保存到 localStorage
     */
    save() {
        const data = {
            sendMode: this.sendMode,
            deepThink: this.deepThink,
            webSearch: this.webSearch,
            theme: this.theme,
        };
        localStorage.setItem(LS_KEY_SETTINGS, JSON.stringify(data));
    },
};

/**
 * 全局状态对象
 * 所有可变状态集中在此，各模块通过 state.xxx 读写。
 */
export const state = {
    // 消息历史 [{role, content, id, usage}]
    messages: [],

    // 当前对话标题（显示在 header 左侧）
    // 欢迎状态: "欢迎开始新对话"
    // 用户发出第一条消息后: 通过 GET /api/session/title 由后端 AI 生成
    // 页面刷新时: 由后端 OnRestoreSession 返回已保存的 session.Title
    dialogTitle: '欢迎开始新对话',

    // 是否正在流式接收
    isStreaming: false,

    // 用于取消请求的 AbortController
    abortController: null,

    // 深度思考按钮状态（同步自 UserSettings.deepThink）
    deepThinkActive: false,

    // 智能搜索按钮状态（同步自 UserSettings.webSearch）
    webSearchActive: true,

    // 当前会话的快照：发送消息时锁定，SSE 处理期间基于此判断
    sessionDeepThinkingEnabled: false,

    // 用户消息计数（用于生成 data-msg-index）
    userMsgCount: 0,

    // 当前活动刻度的索引，-1 表示无活动刻度
    activeTickIndex: -1,

    // 用户点击刻度后的目标索引，用于平滑滚动到位后精确解锁面板
    // 与 activeTickIndex 分离，避免滚动过程中被中间过渡值覆盖
    targetTickIndex: -1,

    // 待高亮消息索引：目标消息进入视口后不立即触发高亮动画，
    // 而是标记此值，等滚动完全停止后再触发（由 scrollDebounceTimer 处理）
    pendingHighlightIndex: -1,

    // 当前消息组，用于将同一问答对的 user + assistant 包裹在 .message-group 内
    currentGroup: null,

    // 流式渲染相关
    accumulatedMarkdown: '',
    renderTimer: null,

    // 渲染节流间隔（毫秒）
    RENDER_INTERVAL: 240,

    // 最多同时显示的刻度数
    MAX_VISIBLE_TICKS: 9,
// 刻度滚动偏移量（0 表示从第一条开始显示）
tickScrollOffset: 0,


    // 发送模式状态: false = Enter发送/Shift+Enter换行, true = Enter换行/Shift+Enter发送
    // 由 UserSettings.sendMode 驱动（0→false, 1→true）
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
