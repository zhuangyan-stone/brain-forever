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
    // ES Module 通过 Alpine.store('settings') 直接访问。
    // ============================================================
    Alpine.store('settings', {
        deepThink: typeof _bfSettings.deepThink === 'boolean' ? _bfSettings.deepThink : false,
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
         * _addToast — 内部方法：创建 toast 并管理其生命周期
         * 所有消息统一使用 x-html 渲染（showToast 已对纯文本做 HTML 转义）
         * @param {string} message
         * @param {'error'|'success'|'info'} type
         * @param {number} duration
         * @param {function|null} onClick - 点击回调
         * @returns {number} toast id
         */
        _addToast: function(message, type, duration, onClick) {
            var id = ++this._nextToastId;
            var self = this;
            var toast = {
                id: id,
                message: message,
                type: type,
                visible: false,
                onClick: (typeof onClick === 'function') ? onClick : null,
            };
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

            return id;
        },

        /**
         * showToast — 纯文本 Toast
         * 会对 message 做 HTML 转义，防止 XSS
         * @param {string} message
         * @param {'error'|'success'|'info'} [type='error']
         * @param {number} [duration=4000]
         */
        showToast: function(message, type, duration) {
            if (!type) type = 'error';
            if (!duration) duration = 4000;
            // HTML 转义：& < > 三个危险字符
            var safe = String(message).replace(/&/g, '&').replace(/</g, '<').replace(/>/g, '>');
            this._addToast(safe, type, duration, null);
        },

        /**
         * showToastHTML — HTML 内容 Toast（支持点击回调）
         * 不会对 html 做转义，调用方需确保 HTML 安全性
         * @param {string} html - 支持 HTML 标签的消息内容
         * @param {'error'|'success'|'info'} [type='error']
         * @param {number} [duration=4000]
         * @param {function} [onClick] - 点击 toast 时的回调函数
         */
        showToastHTML: function(html, type, duration, onClick) {
            if (!type) type = 'error';
            if (!duration) duration = 4000;
            this._addToast(html, type, duration, onClick || null);
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
        chatsTimeline: [],       // 时间线 tab 的分组数据（按时间/置顶加工）
        chats: [],               // 原始对话列表（单一数据源，替代 chat-list.js 的 currentChats）
        activeChatSN: null,      // 当前选中的对话 SN，供侧边栏高亮
        sidebarTab: 'timeline',  // 侧边栏当前 tab: 'timeline' | 'category'
        collapsedGroups: {},     // 折叠状态: { 'groupLabel': true/false }
        chatCategories: [],      // 分类 tab 的分组数据
        currentUserNo: '',       // 当前登录用户号，由 initPage / onChatLogin 设置，供登录按钮 Alpine 模板渲染
        currentUserAvatar: '',   // 当前登录用户头像 URL，由 onChatLogin 设置
        deletedChats: [],        // 回收站中的对话列表（已逻辑删除）
        trashExpanded: false,    // 回收站是否展开
        trashLoaded: false,      // 回收站是否已从服务端全量加载

        // ---- 计算属性 ----
        get active() {
            if (this.activeIndex === -1) {
                return this.blankItem;
            }
            return this.items[this.activeIndex] || null;
        },

        // ---- 方法 ----

        /**
         * resetToBlank — 重置为空白对话状态（清除所有数据并创建 blankItem）
         *
         * 与 reset() 的区别：
         *   - reset() 设置 blankItem = null（不保留空白对话）
         *   - resetToBlank() 创建新的 blankItem（保留空白对话）
         *
         * 切换用户/退出登录后必须使用 resetToBlank() 而非 reset()，确保 blankItem 存在，
         * 否则 prepareChat() 中的 promoteBlankItem() 不会执行，
         * 导致 chats.active 为 null，消息添加和流式操作全部静默失败。
         *
         * 页面初始化或点击"新对话"时调用。
         */
        resetToBlank: function() {
            this.items = [];
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
            this.chatsTimeline = [];
            this.chats = [];
            this.chatCategories = [];
            // 重置输入面板折叠状态
            this.inputCollapsed = false;
            // 重置侧边栏选中状态
            this.activeChatSN = null;
            // 重置回收站状态
            this.deletedChats = [];
            this.trashExpanded = false;
            this.trashLoaded = false;
        },

        /**
         * restructChatLists — 对原始 chat 列表按时间、分类、置顶等规则加工，
         * 生成结构化的分组数据存入 this.chatsTimeline / this.chatCategories，供侧边栏 Alpine 模板渲染。
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

        /**
         * 切换回收站展开/折叠状态
         */
        toggleTrash: function() {
            this.trashExpanded = !this.trashExpanded;
            // 如果展开且尚未全量加载过，从服务端拉取
            if (this.trashExpanded && !this.trashLoaded) {
                var self = this;
                fetch('/api/chat/deleted')
                    .then(function(resp) { return resp.json(); })
                    .then(function(data) {
                        self.deletedChats = data.chats || [];
                        self.trashLoaded = true;
                        // 重新渲染列表（更新回收站分组中的 items）
                        self.restructChatLists();
                    })
                    .catch(function(err) {
                        console.warn('加载回收站列表失败:', err);
                    });
            }
        },

        /**
         * 设置回收站中的对话列表（由 chat-list.js 调用）
         * @param {Array} chats
         */
        setDeletedChats: function(chats) {
            this.deletedChats = chats || [];
            this.restructChatLists();
        },

        restructChatLists: function(chats, activeSN) {
            // 如果传入了 chats 参数，同步保存到 this.chats；
            // 否则从 this.chats 读取（支持调用方不传参的场景）
            if (chats !== undefined) {
                this.chats = chats;
            } else {
                chats = this.chats;
            }
            this.activeChatSN = activeSN || null;
            if (!chats || chats.length === 0) {
                this.chatsTimeline = [];
                this.chatCategories = [];
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
                // 已分类 — 同时加入分类分组和时间线分组
                if (chat.category && chat.category > 0) {
                    var catKey = String(chat.category);
                    if (!categorized[catKey]) {
                        categorized[catKey] = [];
                    }
                    categorized[catKey].push(chat);
                    // 不 continue，继续进入时间线分组逻辑
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

        // 7. 回收站 — 始终在时间线最底部
        var deletedCount = this.deletedChats ? this.deletedChats.length : 0;
        groups.push({
            label: '🗑️ 回收站',
            type: 'trash',
            count: deletedCount,
            items: this.deletedChats || [],
        });

        this.chatsTimeline = groups;

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
            this.chatCategories = catGroups;
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
         *
         * 当 sn 为空时（新对话尚未获得后端 SN），返回 this.active。
         * 此时 activeIndex === -1，this.active 返回 blankItem，
         * 使 startStreaming() 和 SSEResponser 能在 blankItem 上创建/读取 streamingMsg。
         * 待后端通过 SSE chat_created 事件推送真实 SN 后，
         * onChatCreated 处理器会更新 blankItem.sn 并 promoteBlankItem() 将其移入 items[]。
         *
         * @param {string} sn
         * @returns {object|null} ChatData
         */
        getOrCreate: function(sn) {
            if (!sn) {
                // 新对话时 sn 为空，返回 blankItem（即 this.active）
                // 此时 activeIndex === -1，active getter 返回 blankItem
                return this.active;
            }
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
         * 将 blankItem 提升到 items[] 中。
         *
         * 新流程（用户发出第一条消息时调用）：
         *   1. 如果 blankItem.sn 为空，生成临时 SN（new_ + 当前时间戳）
         *   2. 将 blankItem 移入 items[]
         *   3. blankItem 置 null，activeIndex 指向新加入的元素
         *
         * 这样在收到后端真实 SN 之前，items[] 中已有该 chat 的条目，
         * 用户切换到其他 chat 后仍可通过临时 SN 切回来。
         *
         * @returns {object|null} 提升后的 chat item
         */
        promoteBlankItem: function() {
            if (!this.blankItem) return null;
            var item = this.blankItem;
            // 生成临时 SN：new_ + 当前时间（精确到秒）
            if (!item.sn) {
                item.sn = 'new_' + new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
            }
            this.items.push(item);
            this.activeIndex = this.items.length - 1;
            this.blankItem = null;
            return item;
        },

        /**
         * isDirtyChat — 判断指定 chat 是否是一个"脏"对话（临时 SN，尚未被后端确认）。
         * 脏对话的 SN 以 "new_" 前缀开头，表示它只是前端生成的临时标识，
         * 尚未从后端获得真实的 SN。
         *
         * 用于拦截标题修改等操作：脏对话不允许修改标题（包括手动和 AI 推荐）。
         *
         * @param {object|null} chat - ChatData 对象，不传则检查当前活跃 chat
         * @returns {boolean}
         */
        isDirtyChat: function(chat) {
            if (!chat) {
                chat = this.active;
            }
            if (!chat || !chat.sn) return true;
            return chat.sn.startsWith('new_');
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
                    reasoningState: undefined,
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
                // 历史消息中的 reasoning：中断由后端标记，前端统一 'done'
                group.assistant.reasoningState = assistantData.reasoning ? 'done' : undefined;
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
            // 排序：URL 非空的排在前面（与流式期间 _syncWebSourcesToGroup 一致）
            if (sm.sources && sm.sources.length > 0) {
                var allSrcs = sm.sources.slice();
                var withUrl = allSrcs.filter(function(s) { return s.url; });
                var withoutUrl = allSrcs.filter(function(s) { return !s.url; });
                lastGroup.assistant.sources = withUrl.concat(withoutUrl);
            } else {
                lastGroup.assistant.sources = undefined;
            }
            lastGroup.assistant.usage = sm.usage || undefined;
            // 从 streamingMsg 拷贝 reasoningState（正常完成→'done'，中断→'interrupted'）
            lastGroup.assistant.reasoningState = sm.reasoningState || undefined;
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
                            reasoningState: undefined,
                            sources: null,
                            usage: null,
                            contentHTML: '',
                            reasoningHTML: undefined,
                            interrupted: 0, // 0=done, 1=user-interrupted, 2=backend-error
                        },
                    });
                } else if (msg.role === 'assistant' && groups.length > 0) {
                    var lastGroup = groups[groups.length - 1];
                    lastGroup.assistant.content = msg.content || '';
                    lastGroup.assistant.createdAt = msg.created_at || null;
                    lastGroup.assistant.reasoning = msg.reasoning || null;
                    // 历史消息中的 reasoning：中断消息由后端追加 broken message 标记，
                    // 前端统一显示为"思考完成"（interrupted 与 done 同态）
                    lastGroup.assistant.reasoningState = msg.reasoning ? 'done' : undefined;
                    lastGroup.assistant.sources = msg.sources || null;
                    lastGroup.assistant.usage = msg.usage || null;
                    lastGroup.assistant.contentHTML = render(msg.content || '');
                    lastGroup.assistant.reasoningHTML = msg.reasoning ? render(msg.reasoning) : undefined;
                    lastGroup.assistant.interrupted = msg.interrupted || 0;
                    lastGroup.msgId = msg.id || lastGroup.msgId;
                }
            }
            chat.groups = groups;
            chat._groupSeq = seq;
        },

        /**
         * 判断指定 SN 的对话是否正在流式输出
         * 供侧边栏 Alpine 模板调用，用于显示旋转加载图标
         * @param {string} sn
         * @returns {boolean}
         */
        isStreamingBySN: function(sn) {
            if (!sn) return false;
            var item = this.items.find(function(c) { return c.sn === sn; });
            return item ? !!item.isStreaming : false;
        },

        /**
         * 重置所有数据（切换用户时）
         * 清除 items、chatsTimeline、chatCategories 等所有缓存数据，
         * 确保 Alpine 响应式模板立即清空侧边栏。
         * 后续由 renderChatList() 通过 restructChatLists() 重新填充。
         */
        reset: function() {
            this.items = [];
            this.activeIndex = -1;
            this.blankItem = null;
            this.chatsTimeline = [];
            this.chats = [];
            this.chatCategories = [];
            this.activeChatSN = null;
            this.deletedChats = [];
            this.trashExpanded = false;
            this.trashLoaded = false;
        },
    });

    // ============================================================
    // Tick Nav store — 刻度导航状态
    // ============================================================
    // 由 tick-state.js（ES Module）的 setter 函数同步更新，
    // 供 buttons.js 中的 chatContainer 组件读取。
    // ============================================================
    Alpine.store('tickNav', {
        activeTickIndex: -1,
        tickScrollOffset: 0,
        targetTickIndex: -1,
        pendingHighlightIndex: -1,
        MAX_VISIBLE_TICKS: 9,
    });

    // ---- ES Module 直接通过 Alpine.store('settings') 访问 ----
});
