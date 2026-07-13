// ============================================================
// dialogs/favorite-edit-dialog.js — 收藏夹编辑对话框 Alpine 组件
// ============================================================
// 包含：
//   1. favoriteEditDialog     — 收藏夹编辑对话框的 Alpine 状态管理
//   2. showFavoriteEditDialog — 打开入口（注册到 window）
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // favoriteEditDialog — 收藏夹编辑对话框
    // ============================================================
    // 用途：收藏夹编辑对话框，提供标签输入 + 已有标签下拉选择 + 确认/取消操作
    //
    // 使用方式：
    //   <div class="dialog-overlay" id="favoriteEditDialog"
    //        x-data="favoriteEditDialog()"
    //        :class="{ show: show }">
    //       ...
    //   </div>
    //
    //   在 JS 中通过 window.showFavoriteEditDialog(options) 调用
    Alpine.data('favoriteEditDialog', () => ({
        show: false,
        customTag: '',
        existingTags: [],
        submitting: false,
        dropdownOpen: false,
        _onConfirm: null,

        open: function(options) {
            this.existingTags = options.existingTags || [];
            this.customTag = options.defaultTag || '';
            this._onConfirm = options.onConfirm || null;
            this.submitting = false;
            this.dropdownOpen = false;
            this.show = true;
        },

        cancel: function() {
            this.show = false;
            this.dropdownOpen = false;
            this._onConfirm = null;
        },

        toggleDropdown: function() {
            this.dropdownOpen = !this.dropdownOpen;
        },

        closeDropdown: function() {
            this.dropdownOpen = false;
        },

        selectTag: function(tag) {
            this.customTag = tag;
            this.dropdownOpen = false;
        },

        confirm: function() {
            if (this.submitting) return;
            if (typeof this._onConfirm !== 'function') {
                this.show = false;
                return;
            }
            this.submitting = true;
            var tag = this.customTag.trim();
            var self = this;
            var result = this._onConfirm(tag);
            if (result && typeof result.then === 'function') {
                result.then(function(success) {
                    if (success) {
                        self.show = false;
                        self._onConfirm = null;
                    } else {
                        self.submitting = false;
                    }
                }).catch(function(e) {
                    console.error('收藏操作出错:', e);
                    self.submitting = false;
                });
            } else {
                if (result) {
                    self.show = false;
                    self._onConfirm = null;
                } else {
                    self.submitting = false;
                }
            }
        },
    }));

});

// ============================================================
// showFavoriteEditDialog — 打开收藏夹编辑对话框的入口函数
// ============================================================
// UI 由 Alpine 组件 favoriteEditDialog 渲染，本函数只负责传递数据。
//
// @param {object} options
// @param {string[]} options.existingTags - 已有的收藏夹目录名列表
// @param {string} [options.defaultTag] - 默认收藏夹目录名
// @param {(customTag: string) => Promise<boolean>} options.onConfirm - 确认回调
window.showFavoriteEditDialog = function({ existingTags, defaultTag, onConfirm } = {}) {
    if (!Array.isArray(existingTags)) existingTags = [];

    const dialogEl = document.getElementById('favoriteEditDialog');
    if (!dialogEl) return;

    Alpine.$data(dialogEl).open({
        existingTags: existingTags,
        defaultTag: defaultTag || '',
        onConfirm: onConfirm,
    });
};
