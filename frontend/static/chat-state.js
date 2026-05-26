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
 *
 * 注意：流式相关的状态（isStreaming, abortController 等）已迁移到
 * ChatSession / ChatSessionManager 中管理。state 中的 isStreaming
 * 和 abortController 改为委托给 sessionManager 的 getter/setter，
 * 以保持对现有代码的向后兼容。
 */
export const state = {
    // 消息历史 [{role, content, id, usage}]
    messages: [],

    // 用户是否向上滚动离开底部（流式输出中用于控制自动滚动）
    // true  = 用户已向上滚动，停止自动滚动
    // false = 页面在底部或尚未手动滚动，继续自动滚动
    userScrolledUp: false,

    // 当前对话标题（显示在 header 左侧）
    // 欢迎状态: "欢迎开始新对话"
    // 用户发出第一条消息后: 通过 GET /api/session/title 由后端 AI 生成
    // 页面刷新时: 由后端 OnRestoreSession 返回已保存的 session.Title
    dialogTitle: '',

    // 标题修改状态
    // 0: 原始标题（新对话为"新对话"）
    // 1: AI 修改标题
    // 2: 用户手动修改标题
    // 状态只能从低往高变，不能从高往低变
    titleState: 0,

    // ---- 以下字段委托给 $store.chats（过渡期） ----
    // 最终目标：所有代码改为直接读取 $store.chats.active
    // 此处的 getter/setter 仅用于兼容现有 JS 代码

    /**
     * 是否正在流式接收
     * 委托给 $store.chats.active.isStreaming
     */
    get isStreaming() {
        try {
            var chats = window.Alpine.store('chats');
            return chats.active ? chats.active.isStreaming : false;
        } catch(e) {
            return false;
        }
    },
    set isStreaming(val) {
        try {
            var chats = window.Alpine.store('chats');
            if (chats.active) chats.active.isStreaming = val;
        } catch(e) {}
    },

    /**
     * 用于取消请求的 AbortController
     * 委托给 sessionManager.getActive()?.abortController
     */
    get abortController() {
        if (!this._sessionManager) return null;
        return this._sessionManager.abortController;
    },
    set abortController(val) {
        if (this._sessionManager) {
            this._sessionManager.abortController = val;
        }
    },

    /**
     * 深度思考按钮状态（委托给 UserSettings，为 Alpine store 和旧代码的共享数据源）
     * 注意：这是一个 getter，每次读取都从 UserSettings 获取最新值。
     * Alpine store 的 toggleDeepThink() 会更新 UserSettings，因此这里无需手动同步。
     */
    get deepThinkActive() {
        return UserSettings.deepThink;
    },

    /**
     * 智能搜索按钮状态（同上）
     */
    get webSearchActive() {
        return UserSettings.webSearch;
    },

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

    // 渲染节流间隔（毫秒）
    RENDER_INTERVAL: 180,

    // 最多同时显示的刻度数
    MAX_VISIBLE_TICKS: 9,
    
    // 刻度滚动偏移量（0 表示从第一条开始显示）
    tickScrollOffset: 0,

    // 发送模式状态: false = Enter发送/Shift+Enter换行, true = Enter换行/Shift+Enter发送
    // 由 UserSettings.sendMode 驱动（0→false, 1→true）
    sendModeAlternate: false,

    // 当前对话的 SN（序列号），由后端分配
    // 新对话初始为空字符串，首次发送消息前通过 POST /api/chat/new 获取
    // 页面刷新时由 restoreChat 从后端恢复
    currentChatSN: '',

    // ChatSessionManager 引用（由 chat.js 初始化时设置）
    _sessionManager: null,
};

/**
 * 重置流式渲染状态（取消请求后清理）
 * 注意：accumulatedMarkdown 和 renderTimer 已迁移到 ChatSession，
 * 此函数保留以兼容旧代码调用，但实际不再操作 state 上的字段。
 */
export function resetStreamingState() {
    // 不再操作 state.accumulatedMarkdown 和 state.renderTimer
    // 这些字段已由 ChatSession 管理
}
