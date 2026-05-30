// ============================================================
// buttons.js — 所有按钮 Alpine 组件
// ============================================================
// 分类：
//   1. iconBtn    — 纯图标按钮，支持 normal / small 两种尺寸
//   2. textBtn    — 带文字、带边框的按钮，可选左侧图标
//   3. toggleBtn  — 开关型按钮，点击切换选中/未选中状态
//   4. sendBtn    — 发送/停止按钮，支持两种视觉状态切换
//   5. attachBtn  — 附件上传触发按钮
//   6. deleteDialog — 删除确认对话框
//   7. titleEditDialog — 修改标题对话框
//   8. chatContainer — 聊天容器 Alpine 组件
//   9. formatTime — 时间格式化函数
//
// ⚠️ 此文件以普通 <script> 加载（非 ES Module），
//    在 alpine:init 事件中通过 Alpine.data() 注册所有组件，
//    使 HTML 中 x-data="iconBtn(config)" 在 Alpine 处理 DOM 时立即可用。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 1. iconBtn — IconButton
    // ============================================================
    // 用途：纯图标按钮，支持两种尺寸
    // 适用：themeToggle（normal 尺寸）、aiTitleBtn（small 尺寸）、
    //       sidebarCloseBtn（normal 尺寸）、menu-toggle-btn（small 尺寸）
    //
    // 使用方式：
    //   <button class="icon-btn icon-btn--normal"
    //           x-data="iconBtn({ size: 'normal' })"
    //           @click="$store.settings.toggleTheme()"
    //           :data-tooltip="$store.settings.theme === 1 ? '亮色' : '暗色'">
    //       <svg>...</svg>
    //   </button>
    Alpine.data('iconBtn', (config = {}) => ({
        /**
         * 尺寸变体：'normal' | 'small'
         * - normal: 34×34 容器，20×20 图标
         * - small:  20×20 容器，14×14 图标
         */
        size: config.size || 'normal',

        /**
         * disabled 状态由外部注入，支持函数或布尔值
         */
        get disabled() {
            if (typeof config.disabled === 'function') {
                return config.disabled();
            }
            return config.disabled === true;
        },
    }));


    // ============================================================
    // 2. textBtn — TextButton
    // ============================================================
    // 用途：带文字、带边框的按钮，可选左侧图标
    // 适用：newChatBtn（图标+文字）、loginBtn（纯文字）
    //
    // 使用方式：
    //   <button class="text-btn"
    //           x-data="textBtn({ disabled: () => $store.chats.active?.isStreaming })"
    //           :disabled="disabled">
    //       <svg><use href="#icon-new-chat"/></svg>
    //       <span>新对话</span>
    //   </button>
    Alpine.data('textBtn', (config = {}) => ({
        /**
         * disabled 状态由外部通过 config.disabled 注入，
         * 支持传入 getter 函数（保持 Alpine 响应式）或布尔值。
         */
        get disabled() {
            if (typeof config.disabled === 'function') {
                return config.disabled();
            }
            return config.disabled === true;
        },
    }));


    // ============================================================
    // 3. toggleBtn — ToggleButton
    // ============================================================
    // 用途：开关型按钮，点击切换选中/未选中状态
    // 适用：deepThinkBtn、webSearchBtn
    //
    // 使用方式：
    //   <button class="toggle-btn"
    //           x-data="toggleBtn({
    //               active: () => $store.settings.deepThink,
    //               onToggle: () => $store.settings.toggleDeepThink(),
    //           })"
    //           :data-active="active ? 'true' : 'false'"
    //           @click="onToggle">
    //       <svg>...</svg>
    //       <span>深度思考</span>
    //   </button>
    Alpine.data('toggleBtn', (config = {}) => ({
        /**
         * 是否激活，由外部注入响应式 getter
         */
        get active() {
            if (typeof config.active === 'function') {
                return config.active();
            }
            return config.active === true;
        },

        /**
         * 点击时的切换函数，由外部注入
         */
        onToggle: config.onToggle || function() {},
    }));


    // ============================================================
    // 4. sendBtn — SendButton
    // ============================================================
    // 用途：发送/停止按钮，支持两种视觉状态切换
    // - active=false：默认状态（发送态）
    // - active=true：备选状态（停止态）
    //
    // 使用方式：
    //   <button id="sendBtn" class="send-btn"
    //           x-data="sendBtn({ active: () => $store.chats.active?.isStreaming })"
    //           :class="{ 'stop-btn': active }"
    //           :data-tooltip="active ? '停止生成' : '发送'">
    //       <template x-if="!active"><svg><!-- 纸飞机 --></svg></template>
    //       <template x-if="active"><svg><!-- 停止方块 --></svg></template>
    //   </button>
    Alpine.data('sendBtn', (config = {}) => ({
        /**
         * active 状态由外部注入，控制按钮的视觉模式：
         * - false → 默认状态（如发送）
         * - true  → 备选状态（如停止）
         *
         * 支持传入 getter 函数（保持 Alpine 响应式）或布尔值
         */
        get active() {
            if (typeof config.active === 'function') {
                return config.active();
            }
            return config.active === true;
        },
    }));


    // ============================================================
    // 5. attachBtn — AttachButton
    // ============================================================
    // 用途：附件上传触发按钮，点击打开文件选择框
    //
    // 使用方式：
    //   <button id="attachBtn" class="attach-btn" x-data="attachBtn()">
    //       <svg>...</svg>
    //   </button>
    Alpine.data('attachBtn', () => ({
        /**
         * 当前无特殊状态逻辑，仅为 Alpine 组件占位，
         * 以便将来扩展（如拖拽上传状态、文件数量徽标等）。
         */
    }));


    // ============================================================
    // 6. deleteDialog — 删除确认对话框
    // ============================================================
    // 用途：消息删除确认对话框的状态管理
    //
    // 使用方式：
    //   <div class="dialog-overlay" id="deleteModal"
    //        x-data="deleteDialog()"
    //        :class="{ show: show }">
    //   </div>
    //
    //   在 JS 中通过 Alpine.$data(deleteModal) 操作：
    //     Alpine.$data(deleteModal).open(deleteIndex)
    //     Alpine.$data(deleteModal).close()
    Alpine.data('deleteDialog', () => ({
        show: false,
        deleteIndex: -1,

        /**
         * 打开删除对话框
         * @param {number} index - 要删除的消息索引
         */
        open: function(index) {
            this.deleteIndex = index;
            this.show = true;
            // 内容填充由 showDeleteModal() 在 JS 中完成
            // （内容从 DOM 动态提取，不适合 Alpine 模板）
        },

        /**
         * 关闭删除对话框
         */
        close: function() {
            this.show = false;
            this.deleteIndex = -1;
        },
    }));


    // ============================================================
    // 7. titleEditDialog — 修改标题对话框
    // ============================================================
    // 用途：修改对话标题对话框，提供输入编辑+确认/取消操作
    //
    // 使用方式：
    //   <div class="dialog-overlay" id="titleEditDialog"
    //        x-data="titleEditDialog()"
    //        :class="{ show: show }">
    //       ...
    //       <input type="text" x-model="editingTitle" maxlength="50">
    //       <button @click="confirm" :disabled="!editingTitle.trim() || submitting"
    //               x-text="submitting ? '提交中…' : '确认'">确认</button>
    //   </div>
    //
    //   在 JS 中通过 Alpine.$data(titleEditDialog).open(options) 调用
    Alpine.data('titleEditDialog', () => ({
        show: false,
        editingTitle: '',
        originalTitle: '',
        submitting: false,

        /** @type {function(string): Promise<boolean>|null} */
        _onConfirm: null,
        /** @type {function(): void|null} */
        _onCancel: null,

        /**
         * 由原始标题派生显示的标题（空编辑时回退到原标题）
         */
        get displayTitle() {
            return this.editingTitle || this.originalTitle;
        },

        /**
         * 打开标题编辑对话框
         * @param {{ currentTitle: string, onConfirm: function, onCancel?: function }} options
         */
        open: function(options) {
            this.originalTitle = options.currentTitle;
            this.editingTitle = options.currentTitle;
            this._onConfirm = options.onConfirm || null;
            this._onCancel = options.onCancel || null;
            this.submitting = false;
            this.show = true;

            // 自动聚焦编辑框
            var self = this;
            this.$nextTick(function() {
                var input = self.$el.querySelector('.title-edit-input');
                if (input) {
                    input.focus();
                    input.select();
                }
            });
        },

        /**
         * 取消编辑
         */
        cancel: function() {
            if (typeof this._onCancel === 'function') this._onCancel();
            this.show = false;
            this._onConfirm = null;
            this._onCancel = null;
        },

        /**
         * 确认编辑
         */
        confirm: function() {
            var self = this;
            var newTitle = this.editingTitle.trim();
            if (!newTitle || this.submitting) return;

            if (typeof this._onConfirm !== 'function') {
                this.show = false;
                return;
            }

            this.submitting = true;
            var result = this._onConfirm(newTitle);

            if (result && typeof result.then === 'function') {
                result.then(function(success) {
                    if (success) {
                        self.show = false;
                        self._onConfirm = null;
                        self._onCancel = null;
                    } else {
                        self.submitting = false;
                    }
                }).catch(function(e) {
                    console.error('修改标题出错:', e);
                    self.submitting = false;
                });
            } else {
                if (result) {
                    self.show = false;
                    self._onConfirm = null;
                    self._onCancel = null;
                } else {
                    self.submitting = false;
                }
            }
        },
    }));


    // ============================================================
    // 8. chatContainer — 聊天容器 Alpine 组件
    // ============================================================
    // 用途：管理 #chatContainer 的 Alpine 状态，提供 x-for 模板中
    //       使用的辅助方法（formatTime、deleteGroup 等）。
    //
    // 使用方式：
    //   <main class="chat-container" id="chatContainer"
    //         x-data="chatContainer()"
    //         x-init="init($el)">
    //       <template x-for="(group, idx) in $store.chats.active?.groups ?? []">
    //           ...
    //       </template>
    //   </main>
    Alpine.data('chatContainer', () => ({
        /**
         * 初始化：保存容器 DOM 引用
         * @param {HTMLElement} el
         */
        init: function(el) {
            // 保存容器引用供外部 JS 使用
            window._chatContainerEl = el;
        },

        /**
         * 显示删除确认对话框
         * 由 Alpine x-for 模板中的 @click 调用
         * @param {number} idx - groups 数组中的索引
         */
        showDeleteModal: function(idx) {
            var chats = window.Alpine.store('chats');
            if (!chats || !chats.active) return;
            var group = chats.active.groups[idx];
            if (!group) return;

            // 设置活动刻度索引（用于刻度导航高亮）
            try {
                var tickNav = Alpine.store('tickNav');
                if (tickNav) tickNav.activeTickIndex = idx;
            } catch(e) {}

            // 通过 Alpine 打开删除确认对话框
            var deleteModal = document.getElementById('deleteModal');
            if (!deleteModal) return;
            Alpine.$data(deleteModal).open(idx);
        },

        /**
         * 确认删除指定索引的消息组（由 confirmDelete 调用）
         * @param {number} idx - groups 数组中的索引
         */
        confirmDeleteGroup: function(idx) {
            var chats = window.Alpine.store('chats');
            if (chats) {
                chats.deleteGroup(idx);
            }
        },
    }));


    // ============================================================
    // 9. formatTime — 时间格式化函数
    // ============================================================
    // 在 Alpine x-text 表达式中使用：formatTime(group.user.createdAt)
    //
    // 由于 Alpine.data 中可直接引用全局函数，此函数作为 Alpine magic
    // 注册，或保持为普通函数均可。当前保持为 Alpine.data 内联。
    // 同时保留 window.formatTime 供 Alpine 模板直接引用。
    // ============================================================
    window.formatTime = function(isoStr) {
        if (!isoStr) return '';
        try {
            var d = new Date(isoStr);
            var hh = String(d.getHours()).padStart(2, '0');
            var mm = String(d.getMinutes()).padStart(2, '0');
            var ss = String(d.getSeconds()).padStart(2, '0');
            return hh + ':' + mm + ':' + ss;
        } catch(e) {
            return '';
        }
    };

});
