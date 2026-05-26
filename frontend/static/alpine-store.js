// ============================================================
// alpine-store.js — Alpine.js Store 注册
// ============================================================
// 职责：在 Alpine.js 初始化 DOM 之前注册全局 store（settings, ui），
//       使 HTML 模板中的 $store.* 引用在 Alpine 处理 DOM 时立即可用。
//
// ⚠️ 此文件以普通 <script> 加载（非 ES Module），因为 Alpine 需要在处理
//    DOM 前找到已注册的 store。ES Module 的执行时机晚于 Alpine 的 DOM 扫描，
//    无法满足时序要求。
//
// 加载顺序（index.html 中）：
//   1. alpine-store.js        ← 普通 <script>，注册 Alpine store
//   2. toggle-btn.js 等组件    ← 普通 <script>，注册全局组件函数
//   3. alpine.min.js          ← <script defer>，触发 alpine:init → 扫描 DOM
//   4. chat.js                ← <script type="module">，异步加载
//
// 注意：只有被 HTML 模板中 $store.* 直接引用的状态才需要在此注册。
//       业务逻辑状态（state.messages, state.dialogTitle 等）保留在
//       ES Module 中管理，不需迁移至此。
// ============================================================

'use strict';

// ---- 从 localStorage 读取设置（供下方 Alpine store 初始化使用） ----
var _bfSettings = (function() {
    try {
        var raw = localStorage.getItem('brainforever_settings');
        return raw ? JSON.parse(raw) : {};
    } catch(e) { return {}; }
})();

// ---- 注册 Alpine 模板可调用的 Markdown 渲染器 ----
// 实际的 renderMarkdown 函数在 chat-markdown.js 中定义（ES Module），
// 会通过 window._alpineRenderMarkdown 注入。由于 Alpine 的 x-html/x-text
// 表达式是懒评估的（在模板实际渲染时才执行），此时 chat-markdown.js
// 已作为 ES Module 加载完成，因此函数一定可用。
// 此处定义的是"桩函数"，确保 Alpine 扫描 DOM 时表达式可解析。
window.AlpineRenderMarkdown = function(content) {
    if (typeof window._alpineRenderMarkdown === 'function') {
        return window._alpineRenderMarkdown(content || '');
    }
    // 降级：直接返回纯文本（不渲染 Markdown）
    return content || '';
};

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 全局设置 store — settings
    // ============================================================
    // 不依赖 ES Module 中的 UserSettings（此时尚未加载），
    // 直接操作 localStorage 实现持久化。
    // ES Module 中的 UserSettings.load() 后续执行时会从同一
    // localStorage 键读取，因此两者始终一致。
    // ============================================================
    Alpine.store('settings', {
        deepThink: typeof _bfSettings.deepThink === 'boolean' ? _bfSettings.deepThink : true,
        webSearch: typeof _bfSettings.webSearch === 'boolean' ? _bfSettings.webSearch : true,
        sendMode: typeof _bfSettings.sendMode === 'number' ? _bfSettings.sendMode : 0,
        theme: typeof _bfSettings.theme === 'number' ? _bfSettings.theme : 0,

        // 流式状态（由 chat-session-manager 驱动更新）
        // isStreaming 改为 getter/setter，数据源为 $store.chats.active
        // 过渡期策略：所有读写最终指向 $store.chats.active.isStreaming
        get isStreaming() {
            try {
                var chats = Alpine.store('chats');
                return chats.active ? chats.active.isStreaming : false;
            } catch(e) {
                return false;
            }
        },
        set isStreaming(val) {
            try {
                var chats = Alpine.store('chats');
                if (chats.active) chats.active.isStreaming = val;
            } catch(e) {}
        },

        // ---- 持久化 ----
        _save: function() {
            localStorage.setItem('brainforever_settings', JSON.stringify({
                sendMode: this.sendMode,
                deepThink: this.deepThink,
                webSearch: this.webSearch,
                theme: this.theme,
            }));
        },

        // ---- 操作函数 ----

        toggleDeepThink: function() {
            this.deepThink = !this.deepThink;
            this._save();
        },

        toggleWebSearch: function() {
            this.webSearch = !this.webSearch;
            this._save();
        },

        toggleTheme: function() {
            var newTheme = this.theme === 0 ? 1 : 0;
            this.theme = newTheme;
            this._save();
            document.dispatchEvent(new CustomEvent('theme-changed', {
                detail: { theme: newTheme }
            }));
        },
    });

    // ============================================================
    // UI store — Toast 等组件状态
    // ============================================================
    // 必须在 Alpine 处理 DOM 前注册，否则 x-for="$store.ui.toasts" 无法解析
    // ============================================================
    Alpine.store('ui', {
        toasts: [],
        _nextToastId: 0,

        /**
         * 添加一条 Toast
         * @param {string} message
         * @param {'error'|'success'|'info'} [type='error']
         * @param {number} [duration=4000]
         */
        showToast: function(message, type, duration) {
            if (!type) type = 'error';
            if (!duration) duration = 4000;
            var id = ++this._nextToastId;
            var self = this;
            var toast = { id: id, message: message, type: type, visible: false };
            this.toasts.push(toast);

            // 下一帧触发进入动画
            requestAnimationFrame(function() {
                var t = self.toasts.find(function(t) { return t.id === id; });
                if (t) t.visible = true;
            });

            // 自动移除
            setTimeout(function() {
                var t = self.toasts.find(function(t) { return t.id === id; });
                if (t) t.visible = false;
                setTimeout(function() {
                    self.toasts = self.toasts.filter(function(t) { return t.id !== id; });
                }, 300);
            }, duration);
        },
    });

    // ============================================================
    // Chat store — 多会话数据模型
    // ============================================================
    // 用于支持多个对话并发流式输出，每个对话独立管理消息和流式状态。
    // HTML 模板通过 $store.chats.active 访问当前活跃对话的数据。
    //
    // 数据模型 ChatData：
    //   {
    //     sn: string,                    // 对话 SN
    //     title: string,                 // 对话标题
    //     titleState: 0 | 1 | 2,         // 标题修改状态
    //     isStreaming: boolean,          // 是否正在流式接收
    //     messages: [{ id, role, content, reasoning?, sources?, usage?, createdAt? }],
    //     streamingMsg: { reasoning, content, sources, usage, msgId, createdAt, isDone, error } | null,
    //   }
    // ============================================================
    Alpine.store('chats', {
        // ---- 数据 ----
        items: [],
        activeIndex: -1,

        // ---- 计算属性 ----
        get active() {
            return this.items[this.activeIndex] || null;
        },

        // ---- 方法 ----

        /**
         * 按 SN 获取或创建 ChatData
         * @param {string} sn
         * @returns {object} ChatData
         */
        getOrCreate: function(sn) {
            if (!sn) return null;
            var item = this.items.find(function(c) { return c.sn === sn; });
            if (!item) {
                item = {
                    sn: sn,
                    title: '',
                    titleState: 0,
                    isStreaming: false,
                    messages: [],
                    streamingMsg: null,
                };
                this.items.push(item);
                if (this.activeIndex < 0) this.activeIndex = 0;
            }
            return item;
        },

        /**
         * 按 SN 切换活跃对话
         * @param {string} sn
         */
        switchTo: function(sn) {
            var idx = this.items.findIndex(function(c) { return c.sn === sn; });
            if (idx >= 0) {
                this.activeIndex = idx;
            }
        },

        /**
         * 开始流式，创建 streamingMsg 对象
         * @param {string} sn
         */
        startStreaming: function(sn) {
            var chat = this.getOrCreate(sn);
            if (!chat) return;
            chat.isStreaming = true;
            chat.streamingMsg = {
                reasoning: '',
                content: '',
                sources: [],
                usage: null,
                msgId: 0,
                createdAt: null,
                isDone: false,
                error: null,
            };
        },

        /**
         * 结束流式，将 streamingMsg 归档到 messages
         * @param {string} sn
         */
        finalizeStreaming: function(sn) {
            var chat = this.getOrCreate(sn);
            if (!chat || !chat.streamingMsg) return;
            var sm = chat.streamingMsg;
            if (sm.content || sm.reasoning) {
                chat.messages.push({
                    id: sm.msgId,
                    role: 'assistant',
                    content: sm.content,
                    reasoning: sm.reasoning || undefined,
                    sources: sm.sources.length > 0 ? sm.sources.slice() : undefined,
                    usage: sm.usage || undefined,
                    createdAt: sm.createdAt || undefined,
                });
            }
            chat.isStreaming = false;
            chat.streamingMsg = null;
        },

        /**
         * 重置所有数据（切换用户时）
         */
        reset: function() {
            this.items = [];
            this.activeIndex = -1;
        },
    });
});
