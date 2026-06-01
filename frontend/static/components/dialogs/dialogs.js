// ============================================================
// components/dialogs/dialogs.js — 对话框 Alpine 组件
// ============================================================
// 包含：
//   1. deleteDialog     — 删除确认对话框
//   2. titleEditDialog  — 修改标题对话框
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册所有组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 1. deleteDialog — 删除确认对话框
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
    // 2. titleEditDialog — 修改标题对话框
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

});
