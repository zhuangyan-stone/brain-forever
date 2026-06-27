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
            linkThemes: true,     // 亮暗联动开关（默认开启）

            /**
             * init — Alpine 初始化时注册 watcher，监听亮/暗主题选择变化
             */
            init: function() {
                var self = this;
                this.$watch('selectedLight', function(value) {
                    if (self.linkThemes && value) {
                        var linked = self._getLinkedId(value, 'dark');
                        if (linked && linked !== self.selectedDark) {
                            self.selectedDark = linked;
                        }
                    }
                });
                this.$watch('selectedDark', function(value) {
                    if (self.linkThemes && value) {
                        var linked = self._getLinkedId(value, 'light');
                        if (linked && linked !== self.selectedLight) {
                            self.selectedLight = linked;
                        }
                    }
                });
            },

            /**
             * _getLinkedId — 根据主题 ID 查找同名的另一模式主题
             * @param {string} id      - 当前选择的主题 ID
             * @param {'light'|'dark'} targetMode - 目标模式
             * @returns {string} 对应的主题 ID，不存在则返回空串
             */
            _getLinkedId: function(id, targetMode) {
                if (!id) return '';
                var linked;
                if (id.endsWith('-light')) {
                    linked = id.slice(0, -6) + '-dark';
                } else if (id.endsWith('-dark')) {
                    linked = id.slice(0, -5) + '-light';
                } else {
                    return '';
                }
                // 确认目标列表中存在此主题
                var targetList = targetMode === 'dark' ? this.availableDark : this.availableLight;
                return targetList.some(function(t) { return t.id === linked; }) ? linked : '';
            },

            // ---- 选中主题的描述 ----

            /** 当前亮色选中主题的描述，空串表示内置主题或未选择 */
            get selectedLightDesc() {
                if (!this.selectedLight) return '';
                var found = this.availableLight.find(function(t) { return t.id === this.selectedLight; }.bind(this));
                return found ? (found.description || '') : '';
            },

            /** 当前暗色选中主题的描述，空串表示内置主题或未选择 */
            get selectedDarkDesc() {
                if (!this.selectedDark) return '';
                var found = this.availableDark.find(function(t) { return t.id === this.selectedDark; }.bind(this));
                return found ? (found.description || '') : '';
            },

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
             * toggleLinkThemes — 切换亮暗联动开关
             * 开启时自动同步当前选择的对应主题
             */
            toggleLinkThemes: function() {
                this.linkThemes = !this.linkThemes;
                if (this.linkThemes) {
                    // 开启联动时，根据当前亮色主题自动同步暗色
                    var linked = this._getLinkedId(this.selectedLight, 'dark');
                    if (linked && linked !== this.selectedDark) {
                        this.selectedDark = linked;
                    }
                }
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
