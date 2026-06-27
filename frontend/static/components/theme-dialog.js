// ============================================================
// theme-dialog.js — 主题选择对话框 Alpine 组件
// ============================================================
// 在用户头像下拉菜单中点击"选择颜色主题"时打开。
// 从 /api/themes 加载可用主题列表，亮/暗两组 ComboBox，
// 每组首项为"内置亮色"/"内置暗色"。
// ============================================================

'use strict';

document.addEventListener('alpine:init', function() {

    Alpine.data('themeDialog', function() {
        return {
            // ---- 响应式状态 ----
            show: false,
            availableLight: [],   // 亮色主题列表 [{ id, name, name_zh }]
            availableDark: [],    // 暗色主题列表 [{ id, name, name_zh }]
            selectedLight: '',    // 当前选中的亮色主题 ID
            selectedDark: '',     // 当前选中的暗色主题 ID

            /**
             * open — 打开对话框
             * 1. 从服务端加载 manifest
             * 2. 构建亮/暗两组列表（首项为"内置"默认项）
             * 3. 设置当前选中值
             */
            open: async function() {
                var data = await window.ThemeLoader.loadManifest();

                var builtinLight = { id: '', name: '内置亮色', name_zh: '内置亮色' };
                var builtinDark  = { id: '', name: '内置暗色', name_zh: '内置暗色' };

                var allThemes = (data && data.themes) || [];

                this.availableLight = [builtinLight].concat(
                    allThemes.filter(function(t) { return t.mode === 'light'; })
                );
                this.availableDark = [builtinDark].concat(
                    allThemes.filter(function(t) { return t.mode === 'dark'; })
                );

                this.selectedLight = localStorage.getItem('brainforever_theme_light') || '';
                this.selectedDark  = localStorage.getItem('brainforever_theme_dark') || '';

                this.show = true;
            },

            /**
             * close — 关闭对话框
             */
            close: function() {
                this.show = false;
            },

            /**
             * confirm — 确认选择
             * 通过 Alpine.store('settings').setThemeSelection 保存并同步
             */
            confirm: function() {
                Alpine.store('settings').setThemeSelection(this.selectedLight, this.selectedDark);
                this.close();
            },
        };
    });

});
