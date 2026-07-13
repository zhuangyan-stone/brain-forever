// ============================================================
// about-dialog.js — 关于"第2大脑"对话框 Alpine 组件
// ============================================================
// 依赖：dialog.css, about-dialog.css（对话框公共样式）
// 通过 Alpine.data('aboutDialog') 注册组件，
// 在 index.html 中通过 x-data="aboutDialog()" 使用。
//
// 数据来源：页面加载时通过 fetchProviderInfo() 获取第三方提供商信息，
// 存储在 this.providers 中，供对话框展示。
// ============================================================

'use strict';

document.addEventListener('alpine:init', function() {
    Alpine.data('aboutDialog', function() {
        return {
            // ---- 显示状态 ----
            show: false,

            // ---- 提供商信息（由 fetchProviderInfo 填充） ----
            providers: null,

            // ---- 生命周期钩子 ----
            init: function() {
                // 尝试从 window.__providerInfo 读取已缓存的数据，
                // 该数据由 chat.js 中的 fetchProviderInfo 调用后设置。
                var cached = window.__providerInfo;
                if (cached) {
                    this.providers = cached;
                }
            },

            // ---- 打开对话框 ----
            open: function() {
                this.show = true;
                // 如果尚未加载提供商数据，尝试获取
                if (!this.providers) {
                    var self = this;
                    // 使用动态 import 避免循环依赖
                    import('/static/chat-api.js').then(function(mod) {
                        if (mod && typeof mod.fetchProviderInfo === 'function') {
                            mod.fetchProviderInfo().then(function(data) {
                                if (data) {
                                    self.providers = data;
                                    window.__providerInfo = data;
                                }
                            });
                        }
                    }).catch(function(e) {
                        console.warn('about-dialog: 加载提供商信息失败', e);
                    });
                }
            },

            // ---- 关闭对话框 ----
            close: function() {
                this.show = false;
            },

            // ---- 获取提供商展示文本 ----
            getProviderName: function(provider) {
                if (!provider || !provider.name) return '';
                if (provider.website) {
                    return '<a href="' + provider.website + '" target="_blank" rel="noopener noreferrer">'
                        + this._escapeHtml(provider.name) + '</a>';
                }
                return this._escapeHtml(provider.name);
            },

            getProviderModel: function(provider) {
                if (!provider || !provider.model) return '';
                return provider.model;
            },

            getEmbedderDimension: function(em) {
                if (!em || !em.dimension) return '';
                return '维度：' + em.dimension;
            },

            // ---- 内部工具 ----
            _escapeHtml: function(str) {
                if (!str) return '';
                var div = document.createElement('div');
                div.textContent = str;
                return div.innerHTML;
            },
        };
    });
});

// ============================================================
// onOpenAboutDialog — 打开关于对话框（注册到 window，供 @click 调用）
// ============================================================
window.onOpenAboutDialog = function() {
    try {
        var dialogEl = document.getElementById('aboutDialog');
        if (dialogEl) {
            var data = Alpine.$data(dialogEl);
            if (data && typeof data.open === 'function') {
                data.open();
                return;
            }
        }
        console.warn('关于对话框组件未找到或未初始化');
    } catch (e) {
        console.error('打开关于对话框失败:', e);
    }
};
