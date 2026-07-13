// ============================================================
// dialogs/title-edit-dialog.js — 修改标题对话框 Alpine 组件
// ============================================================
// 包含：
//   1. titleEditDialog     — 修改标题对话框的 Alpine 状态管理
//   2. showTitleEditDialog — 打开入口（注册到 window）
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // titleEditDialog — 修改标题对话框
    // ============================================================
    // 用途：修改对话标题对话框，提供输入编辑 + 确认/取消操作
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
    //   在 JS 中通过 window.showTitleEditDialog(options) 调用
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

// ============================================================
// showTitleEditDialog — 打开标题编辑对话框的入口函数
// ============================================================
// UI 由 Alpine 组件 titleEditDialog 渲染，本函数只负责传递数据。
//
// @param {object} options
// @param {string} options.currentTitle - 当前对话标题
// @param {(newTitle: string) => Promise<boolean>} options.onConfirm - 确认回调
// @param {() => void} [options.onCancel] - 取消回调（可选）
window.showTitleEditDialog = function({ currentTitle, onConfirm, onCancel } = {}) {
    if (!currentTitle) return;

    const titleEditDialog = document.getElementById('titleEditDialog');
    if (!titleEditDialog) return;

    Alpine.$data(titleEditDialog).open({
        currentTitle: currentTitle,
        onConfirm: onConfirm,
        onCancel: onCancel,
    });
};
