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

// ---- Markdown 渲染器引用 ----
// renderMarkdown 函数在 chat-markdown.js 中定义（ES Module），
// 通过 window._alpineRenderMarkdown 注入。alpine-store.js 是普通 <script>
// 无法直接 import，但 store 方法（addGroup 等）在用户交互时才执行，
// 此时 ES Module 已加载完毕，window._alpineRenderMarkdown 一定可用。
// Alpine 模板不再直接调用渲染器——改用预渲染的 contentHTML 字段。

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 全局设置 store — settings
    // ============================================================
    // 直接操作 localStorage 实现持久化。
    // ES Module 通过 window.__userSettings 访问 load/save 方法。
    // ============================================================
    Alpine.store('settings', {
        deepThink: typeof _bfSettings.deepThink === 'boolean' ? _bfSettings.deepThink : true,
        webSearch: typeof _bfSettings.webSearch === 'boolean' ? _bfSettings.webSearch : true,
        sendMode: typeof _bfSettings.sendMode === 'number' ? _bfSettings.sendMode : 0,
        theme: typeof _bfSettings.theme === 'number' ? _bfSettings.theme : 0,

        // ---- 持久化 ----
        _save: function() {
            localStorage.setItem('brainforever_settings', JSON.stringify({
                sendMode: this.sendMode,
                deepThink: this.deepThink,
                webSearch: this.webSearch,
                theme: this.theme,
            }));
        },

        /**
         * 从 localStorage 加载设置（供 ES Module 调用）
         */
        load: function() {
            try {
                var raw = localStorage.getItem('brainforever_settings');
                if (raw) {
                    var parsed = JSON.parse(raw);
                    if (typeof parsed.sendMode === 'number') this.sendMode = parsed.sendMode;
                    if (typeof parsed.deepThink === 'boolean') this.deepThink = parsed.deepThink;
                    if (typeof parsed.webSearch === 'boolean') this.webSearch = parsed.webSearch;
                    if (typeof parsed.theme === 'number') this.theme = parsed.theme;
                }
            } catch(_) {}
        },

        /**
         * 保存设置到 localStorage（供 ES Module 调用）
         */
        save: function() {
            this._save();
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
    // 数据模型 ChatData（主要数据结构为 groups）：
    //   {
    //     sn: string,                    // 对话 SN
    //     title: string,                 // 对话标题
    //     titleState: 0 | 1 | 2,         // 标题修改状态
    //     isStreaming: boolean,          // 是否正在流式接收
    //     streamingMsg: { reasoning, content, sources, usage, msgId, createdAt, isDone, error } | null,
    //     groups: [{                     // 消息组列表（Alpine x-for 驱动，唯一数据源）
    //         id: number,                // 组内唯一 ID（用于 x-for :key）
    //         msgId: number,             // 后端消息 ID（实际为 group_index）
    //         user: { content, createdAt, contentHTML } | null,
    //         assistant: {               // ★ 方案B：始终存在，流式中→已完成持续增长
    //             content: string,       //    原始 Markdown 文本
    //             createdAt: string|null,//    null 表示流式中，设置后表示已完成
    //             reasoning: string|null,
    //             sources?, usage?,
    //             contentHTML: string,   //    节流渲染的 HTML（流式中持续更新）
    //             reasoningHTML: string, //    节流渲染的 reasoning HTML
    //         },
    //     }],
    //     _groupSeq: number,             // 自增序列，用于生成 group.id
    //   }
    // ============================================================
    Alpine.store('chats', {
        // ---- 数据 ----
        items: [],
        activeIndex: -1,
        blankItem: null,         // 空白对话（新对话状态），activeIndex===-1 时 active 返回此对象
        inputCollapsed: false,   // 输入面板是否折叠，由 chat-ui.js 的 collapseInputArea/restoreInputArea 更新
        welcomeMessage: '',      // 欢迎消息文本，非空时显示欢迎页（由 Alpine x-show 驱动）
        chatsLists: [],          // 按时间/分类加工后的分组数据，供侧边栏 Alpine 模板渲染
        activeChatSN: null,      // 当前选中的对话 SN，供侧边栏高亮
        sidebarTab: 'timeline',  // 侧边栏当前 tab: 'timeline' | 'category'
        collapsedGroups: {},     // 折叠状态: { 'groupLabel': true/false }
        categoryGroups: [],      // 分类 tab 的分组数据
        currentUserNo: '',       // 当前登录用户号，由 initPage / onChatLogin 设置，供登录按钮 Alpine 模板渲染

        // ---- 计算属性 ----
        get active() {
            if (this.activeIndex === -1) {
                return this.blankItem;
            }
            return this.items[this.activeIndex] || null;
        },

        // ---- 方法 ----

        /**
         * resetToBlank — 重置为空白对话状态
         * 设置 activeIndex = -1，创建 blankItem。
         * 页面初始化或点击"新对话"时调用。
         */
        resetToBlank: function() {
            this.activeIndex = -1;
            this.blankItem = {
                sn: '',
                title: '',
                titleState: 0,
                isStreaming: false,
                userScrolledUp: false,
                streamingMsg: null,
                groups: [],
                _groupSeq: 0,
            };
        },

        /**
         * restructChatLists — 对原始 chat 列表按时间、分类、置顶等规则加工，
         * 生成结构化的分组数据存入 this.chatsLists，供侧边栏 Alpine 模板渲染。
         *
         * 分组规则（与 chat-list.js 的 groupChats 一致）：
         *   - 已分类（category > 0）→ categorized 分组
         *   - 置顶（pinned）→ pinned 分组
         *   - 按 update_at 时间 → today / yesterday / within7Days / within30Days / earlier
         *
         * @param {Array} chats - 原始对话数组
         * @param {string} [activeSN] - 当前选中的对话 SN
         */
        /**
         * 切换侧边栏 tab
         * @param {'timeline'|'category'} tab
         */
        switchSidebarTab: function(tab) {
            this.sidebarTab = tab;
        },

        /**
         * 切换分组的折叠/展开状态
         * @param {string} groupKey - 分组的唯一标识 key
         */
        toggleCollapse: function(groupKey) {
            var current = this.collapsedGroups[groupKey];
            if (current === undefined) {
                // 默认展开，首次点击折叠
                this.collapsedGroups[groupKey] = true;
            } else {
                this.collapsedGroups[groupKey] = !current;
            }
            // 触发响应式更新
            this.collapsedGroups = Object.assign({}, this.collapsedGroups);
        },

        /**
         * 判断分组是否折叠
         * @param {string} groupKey
         * @returns {boolean}
         */
        isCollapsed: function(groupKey) {
            return !!this.collapsedGroups[groupKey];
        },

        restructChatLists: function(chats, activeSN) {
            this.activeChatSN = activeSN || null;
            if (!chats || chats.length === 0) {
                this.chatsLists = [];
                this.categoryGroups = [];
                return;
            }

            var now = new Date();
            var todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
            var yesterdayStart = new Date(todayStart);
            yesterdayStart.setDate(yesterdayStart.getDate() - 1);
            var weekAgoStart = new Date(todayStart);
            weekAgoStart.setDate(weekAgoStart.getDate() - 7);
            var monthAgoStart = new Date(todayStart);
            monthAgoStart.setDate(monthAgoStart.getDate() - 30);

            var pinned = [];
            var today = [];
            var yesterday = [];
            var within7Days = [];
            var within30Days = [];
            var earlier = {};       // { '2026/3/25': [...] }
            var categorized = {};   // { categoryId: [...] }

            for (var i = 0; i < chats.length; i++) {
                var chat = chats[i];
                // 已分类
                if (chat.category && chat.category > 0) {
                    var catKey = String(chat.category);
                    if (!categorized[catKey]) {
                        categorized[catKey] = [];
                    }
                    categorized[catKey].push(chat);
                    continue;
                }
                // 置顶
                if (chat.pinned) {
                    pinned.push(chat);
                    continue;
                }
                // 按时间
                var updateDate = new Date(chat.update_at);
                if (updateDate >= todayStart) {
                    today.push(chat);
                } else if (updateDate >= yesterdayStart) {
                    yesterday.push(chat);
                } else if (updateDate >= weekAgoStart) {
                    within7Days.push(chat);
                } else if (updateDate >= monthAgoStart) {
                    within30Days.push(chat);
                } else {
                    var dateKey = this._getDateStr(chat.update_at);
                    if (!earlier[dateKey]) {
                        earlier[dateKey] = [];
                    }
                    earlier[dateKey].push(chat);
                }
            }

            // ---- 构建时间线分组（timeline tab） ----
            var groups = [];

            // 1. 置顶
            if (pinned.length > 0) {
                groups.push({ label: '📌 置顶', type: 'normal', items: pinned });
            }

            // 2. 今天
            if (today.length > 0) {
                groups.push({ label: '今天', type: 'normal', items: today });
            }

            // 3. 昨天
            if (yesterday.length > 0) {
                groups.push({ label: '昨天', type: 'normal', items: yesterday });
            }

            // 4. 7天内
            if (within7Days.length > 0) {
                groups.push({ label: '7天内', type: 'normal', items: within7Days });
            }

            // 5. 30天内
            if (within30Days.length > 0) {
                groups.push({ label: '30天内', type: 'normal', items: within30Days });
            }

            // 6. 更早 — 按日期降序
            var earlierDates = Object.keys(earlier).sort(function(a, b) {
                return new Date(b) - new Date(a);
            });
            if (earlierDates.length > 0) {
                var earlierItems = [];
                for (var j = 0; j < earlierDates.length; j++) {
                    earlierItems.push({
                        dateLabel: earlierDates[j],
                        items: earlier[earlierDates[j]],
                    });
                }
                groups.push({ label: '更早', type: 'earlier', subGroups: earlierItems });
                }
    
                this.chatsLists = groups;

            // ---- 构建分类分组（category tab）- 只保留一级分类 ----
            var catKeys = Object.keys(categorized);
            var catGroups = [];
            if (catKeys.length > 0) {
                for (var k = 0; k < catKeys.length; k++) {
                    catGroups.push({
                        label: '分类 ' + catKeys[k],
                        type: 'normal',
                        items: categorized[catKeys[k]],
                    });
                }
            }
            this.categoryGroups = catGroups;
        },

        /**
         * _getDateStr — 获取日期字符串（YYYY/M/D 格式）
         * @param {string} isoStr
         * @returns {string}
         */
        _getDateStr: function(isoStr) {
            if (!isoStr) return '';
            var d = new Date(isoStr);
            return d.getFullYear() + '/' + (d.getMonth() + 1) + '/' + d.getDate();
        },

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
                    userScrolledUp: false,
                    streamingMsg: null,
                    groups: [],
                    _groupSeq: 0,
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
                this.blankItem = null;  // 切换到已有对话时，清除空白对话
            }
        },

        /**
         * 将 blankItem 提升到 items[] 中（用户发出第一条消息时调用）
         * 提升后 blankItem 置 null，activeIndex 指向新加入的元素
         */
        promoteBlankItem: function() {
            if (!this.blankItem) return null;
            var item = this.blankItem;
            this.items.push(item);
            this.activeIndex = this.items.length - 1;
            this.blankItem = null;
            return item;
        },

        /**
         * 按 SN 从 items[] 中移除 ChatData（删除对话时调用）
         * 如果移除的是当前活跃对话，重置为空白状态
         * @param {string} sn
         */
        removeChat: function(sn) {
            if (!sn) return;
            var idx = this.items.findIndex(function(c) { return c.sn === sn; });
            if (idx < 0) return;
            this.items.splice(idx, 1);
            // 如果删除的是当前活跃对话，重置为空白状态
            if (this.activeIndex === idx) {
                this.activeIndex = -1;
                this.blankItem = null;
            } else if (this.activeIndex > idx) {
                // 删除的元素在当前活跃之前，修正索引
                this.activeIndex--;
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
                reasoningState: 'thinking',
            };
        },

        /**
         * 结束流式，清理流式状态。
         * 实际消息内容已由 finalizeStreamingToGroup() 归档到 group.assistant。
         * @param {string} sn
         */
        finalizeStreaming: function(sn) {
            var chat = this.getOrCreate(sn);
            if (!chat || !chat.streamingMsg) return;
            chat.isStreaming = false;
            chat.streamingMsg = null;
        },

        // ============================================================
        // groups 操作方法 — 用于 Alpine x-for 数据驱动渲染
        // ============================================================

        /**
         * 添加一个消息组（用户消息 + 可选的助手消息占位）
         * @param {string} userContent
         * @param {string|null} userCreatedAt
         * @param {object|null} [assistantData] - 可选的助手消息数据（历史恢复时使用）
         * @returns {number} group.id
         */
        /**
         * 添加一个消息组（用户消息 + 助手消息）。
         *
         * ★ 方案B：assistant 始终提前初始化（内容为空），
         *   流式期间 SSEResponser 直接更新 assistant.content/contentHTML，
         *   完成时 finalizeStreamingToGroup 只添加 metadata（createdAt 等）。
         *   模板因此无需感知 isStreaming 状态，统一渲染 group.assistant。
         *
         * @param {string} userContent
         * @param {string|null} userCreatedAt
         * @param {object|null} [assistantData] - 可选的助手消息数据（历史恢复时使用）
         * @returns {number} group.id
         */
        addGroup: function(userContent, userCreatedAt, assistantData) {
            var chat = this.active;
            if (!chat) return -1;
            var id = ++chat._groupSeq;
            var render = window._alpineRenderMarkdown || function(s) { return s || ''; };
            var group = {
                id: id,
                msgId: assistantData ? (assistantData.msgId || 0) : 0,
                user: {
                    content: userContent,
                    createdAt: userCreatedAt || null,
                    contentHTML: render(userContent),
                },
                assistant: {
                    content: '',
                    createdAt: null,
                    reasoning: null,
                    sources: null,
                    usage: null,
                    contentHTML: '',
                    reasoningHTML: undefined,
                },
            };
            if (assistantData) {
                group.assistant.content = assistantData.content || '';
                group.assistant.createdAt = assistantData.createdAt || null;
                group.assistant.reasoning = assistantData.reasoning || null;
                group.assistant.sources = assistantData.sources || null;
                group.assistant.usage = assistantData.usage || null;
                group.assistant.contentHTML = render(assistantData.content || '');
                group.assistant.reasoningHTML = assistantData.reasoning ? render(assistantData.reasoning) : undefined;
            }
            chat.groups.push(group);
            return id;
        },

        /**
         * 获取最后一个消息组
         * @returns {object|null}
         */
        getLastGroup: function() {
            var chat = this.active;
            if (!chat || chat.groups.length === 0) return null;
            return chat.groups[chat.groups.length - 1];
        },

        /**
         * 删除指定索引的消息组
         * @param {number} index
         */
        deleteGroup: function(index) {
            var chat = this.active;
            if (!chat || index < 0 || index >= chat.groups.length) return;
            chat.groups.splice(index, 1);
        },

        /**
         * 清空所有消息组
         */
        clearGroups: function() {
            var chat = this.active;
            if (!chat) return;
            chat.groups = [];
            chat._groupSeq = 0;
        },

        /**
         * 流式完成时，为 group.assistant 补充 metadata。
         * ★ 方案B：assistant.content / contentHTML / reasoning / reasoningHTML
         *   已在流式期间由 SSEResponser 直接维护，此处只添加 createdAt/sources/usage。
         */
        finalizeStreamingToGroup: function() {
            var chat = this.active;
            if (!chat || !chat.streamingMsg) return;
            var lastGroup = chat.groups[chat.groups.length - 1];
            if (!lastGroup || !lastGroup.assistant) return;
            var sm = chat.streamingMsg;
            var render = window._alpineRenderMarkdown || function(s) { return s || ''; };
            lastGroup.assistant.createdAt = sm.createdAt || null;
            lastGroup.assistant.sources = sm.sources && sm.sources.length > 0 ? sm.sources.slice() : undefined;
            lastGroup.assistant.usage = sm.usage || undefined;
            // 确保 reasoningHTML 已渲染（可能流式期间没有 reasoning）
            if (sm.reasoning && !lastGroup.assistant.reasoningHTML) {
                lastGroup.assistant.reasoningHTML = render(sm.reasoning);
            }
            lastGroup.msgId = sm.msgId || 0;
        },

        /**
         * setChatMessageGroups — 将后端返回的扁平 messages 数组转换为 groups
         * 并设置到指定 SN 的 ChatData 上。
         *
         * 通过 SN 查找目标 chat（而非假定 active），支持后台 chat 的数据更新。
         *
         * @param {string} sn - 目标对话的 SN
         * @param {Array} messages - 后端返回的消息数组 [{ id, role, content, ... }]
         */
        setChatMessageGroups: function(sn, messages) {
            var chat = this.getOrCreate(sn);
            if (!chat) return;
            var render = window._alpineRenderMarkdown || function(s) { return s || ''; };
            var groups = [];
            var seq = 0;
            for (const msg of messages || []) {
                if (msg.role === 'user') {
                    seq++;
                    groups.push({
                        id: seq,
                        msgId: msg.id || 0,
                        user: {
                            content: msg.content,
                            createdAt: msg.created_at || null,
                            contentHTML: render(msg.content),
                        },
                        assistant: {
                            content: '',
                            createdAt: null,
                            reasoning: null,
                            sources: null,
                            usage: null,
                            contentHTML: '',
                            reasoningHTML: undefined,
                        },
                    });
                } else if (msg.role === 'assistant' && groups.length > 0) {
                    var lastGroup = groups[groups.length - 1];
                    lastGroup.assistant.content = msg.content || '';
                    lastGroup.assistant.createdAt = msg.created_at || null;
                    lastGroup.assistant.reasoning = msg.reasoning || null;
                    lastGroup.assistant.sources = msg.sources || null;
                    lastGroup.assistant.usage = msg.usage || null;
                    lastGroup.assistant.contentHTML = render(msg.content || '');
                    lastGroup.assistant.reasoningHTML = msg.reasoning ? render(msg.reasoning) : undefined;
                    lastGroup.msgId = msg.id || lastGroup.msgId;
                }
            }
            chat.groups = groups;
            chat._groupSeq = seq;
        },

        /**
         * 重置所有数据（切换用户时）
         */
        reset: function() {
            this.items = [];
            this.activeIndex = -1;
            this.blankItem = null;
        },
    });

    // ---- 暴露给 ES Module 使用 ----
    // alpine-store.js 是普通 <script>（非 ES Module），
    // ES Module 通过 window.__settingsStore 访问 Alpine.store('settings')
    window.__settingsStore = Alpine.store('settings');
});
