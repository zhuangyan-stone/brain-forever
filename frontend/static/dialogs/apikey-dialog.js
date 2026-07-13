// ============================================================
// apikey-dialog.js — API-Key 设置对话框（Alpine 组件）
// ============================================================
// 用途：管理 API-Key 设置对话框的状态、交互逻辑和费用计算。
//
// 数据模型（对应后端 store.UserSettingsAPIKey）：
//   settings = {
//     llm:      { provider: "deepseek", api_key: "", private: false },
//     search:   { provider: "zhipu",    api_key: "", private: false },
//     embedder: { provider: "zhipu",    api_key: "", private: false },
//   }
//
// 使用方式：
//   <div class="dialog-overlay" id="apikeyDialog"
//        x-data="apikeyDialog()"
//        :class="{ show: show }">
//   </div>
//
//   在 JS 中通过 Alpine.$data(apikeyDialog).open(initialData) 调用
// ============================================================

'use strict';

document.addEventListener('alpine:init', function() {

    Alpine.data('apikeyDialog', () => ({

        // ============================================================
        // 状态
        // ============================================================
        show: false,
        showHelp: false,

        // API-Key 设置数据
        settings: {
            llm:      { provider: 'deepseek', api_key: '', private: false },
            search:   { provider: 'zhipu',    api_key: '', private: false },
            embedder: { provider: 'zhipu',    api_key: '', private: false },
        },

        // 保存回调（兼容模式：有则用回调，无则内部直接保存）
        _onConfirm: null,

        // 防止重复提交
        _submitting: false,

        // 保存失败时的错误消息
        _saveError: '',

        // ============================================================
        // 计算属性
        // ============================================================

        /**
         * LLM 是否使用私有 key
         * getter 返回布尔值供 :checked 绑定使用；
         * setter 切换时自动清空 api_key（切回公共时）。
         */
        get llmPrivate() {
            return this.settings.llm.private;
        },

        set llmPrivate(val) {
            var isPrivate = !!val;
            this.settings.llm.private = isPrivate;
            if (!isPrivate) this.settings.llm.api_key = '';
            this._saveError = '';
        },

        /**
         * Search 是否使用私有 key
         */
        get searchPrivate() {
            return this.settings.search.private;
        },

        set searchPrivate(val) {
            var isPrivate = !!val;
            this.settings.search.private = isPrivate;
            if (!isPrivate) this.settings.search.api_key = '';
            this._saveError = '';
        },

        /**
         * Embedder 是否使用私有 key
         */
        get embedderPrivate() {
            return this.settings.embedder.private;
        },

        set embedderPrivate(val) {
            var isPrivate = !!val;
            this.settings.embedder.private = isPrivate;
            if (!isPrivate) this.settings.embedder.api_key = '';
            this._saveError = '';
        },

        // ============================================================
        // 方法
        // ============================================================

        /**
         * 打开对话框
         * @param {object} options
         * @param {object} [options.settings] - 当前 API-Key 设置（可选）
         * @param {function} [options.onConfirm] - 确认回调，接收 settings 对象
         */
        open: function(options) {
            options = options || {};

            // 合并初始设置
            if (options.settings) {
                this.settings.llm = Object.assign(
                    { provider: 'deepseek', api_key: '', private: false },
                    options.settings.llm || {}
                );
                this.settings.search = Object.assign(
                    { provider: 'zhipu', api_key: '', private: false },
                    options.settings.search || {}
                );
                this.settings.embedder = Object.assign(
                    { provider: 'zhipu', api_key: '', private: false },
                    options.settings.embedder || {}
                );
            } else {
                this.settings = {
                    llm:      { provider: 'deepseek', api_key: '', private: false },
                    search:   { provider: 'zhipu',    api_key: '', private: false },
                    embedder: { provider: 'zhipu',    api_key: '', private: false },
                };
            }

            this._onConfirm = options.onConfirm || null;
            this._submitting = false;
            this._saveError = '';
            this.show = true;
        },

        /**
         * 关闭对话框
         */
        close: function() {
            this.show = false;
            this._onConfirm = null;
            this._submitting = false;
            this._saveError = '';
        },

        /**
         * 内部保存：将当前设置 POST 到后端
         * @returns {Promise<boolean>}
         */
        _saveToServer: async function() {
            try {
                var response = await fetch('/api/user/settings/apikey', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json; charset=utf-8' },
                    body: JSON.stringify({
                        llm: {
                            provider: this.settings.llm.provider,
                            api_key: this.settings.llm.api_key,
                            private: this.settings.llm.private,
                        },
                        search: {
                            provider: this.settings.search.provider,
                            api_key: this.settings.search.api_key,
                            private: this.settings.search.private,
                        },
                        embedder: {
                            provider: this.settings.embedder.provider,
                            api_key: this.settings.embedder.api_key,
                            private: this.settings.embedder.private,
                        },
                    }),
                });
                if (!response.ok) {
                    const t = await response.text();
                    showToast('保存API-Key失败：' + t, 'error');
                    return false;
                }
                return true;
            } catch (e) {
                console.error('保存API-Key设置出错:', e);
                return false;
            }
        },

        /**
         * 确认保存
         * 如有外部 onConfirm 回调则使用回调，否则内部直接保存到后端。
         */
        confirm: async function() {
            if (this._submitting) return;
            this._submitting = true;
            this._saveError = '';

            var success = false;

            if (typeof this._onConfirm === 'function') {
                // 回调模式
                try {
                    var result = this._onConfirm({
                        llm: {
                            provider: this.settings.llm.provider,
                            api_key: this.settings.llm.api_key,
                            private: this.settings.llm.private,
                        },
                        search: {
                            provider: this.settings.search.provider,
                            api_key: this.settings.search.api_key,
                            private: this.settings.search.private,
                        },
                        embedder: {
                            provider: this.settings.embedder.provider,
                            api_key: this.settings.embedder.api_key,
                            private: this.settings.embedder.private,
                        },
                    });

                    if (result && typeof result.then === 'function') {
                        success = await result;
                    } else {
                        success = !!result;
                    }
                } catch (e) {
                    console.error('保存 API-Key 设置出错:', e);
                    success = false;
                }
            } else {
                // 内部保存模式
                success = await this._saveToServer();
            }

            this._submitting = false;

            if (success) {
                this.show = false;
                this._onConfirm = null;
                // 显示成功提示
                try {
                    if (Alpine.store('ui') && Alpine.store('ui').showToast) {
                        Alpine.store('ui').showToast('API-Keys 设置已保存', 'success', 2000);
                    }
                } catch(e) {}
            } else {
                this._saveError = '保存失败，请稍后重试';
            }
        },

    }));

});

// ============================================================
// onOpenApiKeyDialog — 打开 API-Key 设置对话框（注册到 window，供 @click 调用）
// ============================================================
window.onOpenApiKeyDialog = async function() {
    try {
        var dialogEl = document.getElementById('apikeyDialog');
        if (!dialogEl) {
            console.warn('API-Key 设置对话框组件未找到或未初始化');
            return;
        }
        var dialogData = Alpine.$data(dialogEl);
        if (!dialogData || typeof dialogData.open !== 'function') {
            console.warn('API-Key 设置对话框组件未初始化');
            return;
        }

        // API 调用委托给 alpine-api.js 中的 window.fetchApiKeySettings
        var settings = typeof window.fetchApiKeySettings === 'function'
            ? await window.fetchApiKeySettings()
            : null;

        dialogData.open({ settings: settings });
    } catch (e) {
        console.error('打开API-Key设置对话框失败:', e);
    }
};
