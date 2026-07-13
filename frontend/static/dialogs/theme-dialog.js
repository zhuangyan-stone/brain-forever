// ============================================================
// dialogs/theme-dialog.js — 主题选择对话框 Alpine 组件
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
            selectedMode: 'light',// 立即启用：'light' | 'dark'
            linkThemes: true,     // 亮暗联动开关（默认开启）

            /**
             * init — Alpine 初始化时注册 watcher，监听亮/暗主题选择变化
             * 选择改变时立即生效（不再需要点击"确认"按钮）
             */
            init: function() {
                var self = this;
                // 标记：open() 完成初始值设定之前不触发立即生效
                this._initComplete = false;

                this.$watch('selectedLight', function(value) {
                    // 亮暗联动：同步暗色主题
                    if (self.linkThemes) {
                        var linked = value ? self._getLinkedId(value, 'dark') : '';
                        if (linked !== self.selectedDark) {
                            self.selectedDark = linked;
                        }
                    }
                    // 初始化完成后，主题选择立即生效
                    if (self._initComplete) {
                        self._applyImmediately();
                    }
                });
                this.$watch('selectedDark', function(value) {
                    // 亮暗联动：同步亮色主题
                    if (self.linkThemes) {
                        var linked = value ? self._getLinkedId(value, 'light') : '';
                        if (linked !== self.selectedLight) {
                            self.selectedLight = linked;
                        }
                    }
                    // 初始化完成后，主题选择立即生效
                    if (self._initComplete) {
                        self._applyImmediately();
                    }
                });
                this.$watch('selectedMode', function(value) {
                    // 初始化完成后，切换亮暗模式立即生效
                    if (self._initComplete) {
                        self._applyMode(value);
                    }
                });
            },

            /**
             * _applyImmediately — 将当前选择的亮/暗主题立即应用并持久化
             */
            _applyImmediately: function() {
                Alpine.store('settings').setThemeSelection(this.selectedLight, this.selectedDark);
                // 手动模式下同步主题选择到服务端
                var settings = Alpine.store('settings');
                if (settings.theme < 2 && settings.themeSync) {
                    if (typeof window.fetchApplyThemeSelection === 'function') {
                        window.fetchApplyThemeSelection(settings.theme, this.selectedLight, this.selectedDark);
                    }
                }
            },

            /**
             * _applyMode — 将页面切换为指定亮/暗模式
             * @param {'light'|'dark'} mode
             */
            _applyMode: function(mode) {
                var themeVal = mode === 'dark' ? 1 : 0;
                Alpine.store('settings').theme = themeVal;
                Alpine.store('settings')._save();
                document.dispatchEvent(new CustomEvent('theme-changed', {
                    detail: { theme: themeVal }
                }));
            },

            /**
             * _getLinkedId — 根据主题 ID 查找同名的另一模式主题
             * @param {string} id      - 当前选择的主题 ID
             * @param {'light'|'dark'} targetMode - 目标模式
             * @returns {string} 对应的主题 ID，不存在则返回空串
             *
             * ★ 安全约束：返回的 linked ID 必须与原始 id 属于同一前缀（即 A-light → A-dark 且
             *   A-dark → A-light），且必须存在于目标列表中，否则返回空串。
             *   空串意味着"内置主题"，不会进一步触发 watcher 联动。
             */
            _getLinkedId: function(id, targetMode) {
                if (!id) return '';
                var linked;
                if (id.endsWith('-light')) {
                    linked = id.slice(0, -6) + '-dark';
                } else if (id.endsWith('-dark')) {
                    linked = id.slice(0, -5) + '-light';
                } else {
                    // 不以 -light/-dark 结尾的 ID（如已删除的 'coffee-mocha'），
                    // 无法推断对应模式的主题，直接返回空串。
                    return '';
                }
                // 自检：linked 不能等于原 id（防止 malformed ID 导致死循环）
                if (linked === id) return '';
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
             * 4. 保存原始值，供取消时恢复
             */
            open: async function() {
                var data = await window.ThemeLoader.loadManifest();
                var allThemes = (data && data.themes) || [];

                // data.themes 已由 ThemeLoader 注入内置主题（含完整 id/name/name_zh），
                // 且内置主题排在列表最前面，直接按 mode 过滤即可
                this.availableLight = allThemes.filter(function(t) { return t.mode === 'light'; });
                this.availableDark = allThemes.filter(function(t) { return t.mode === 'dark'; });

                // ★ 修复：从 localStorage 读取已保存的选择时，验证其是否存在于可用主题列表中。
                //   如果保存的主题 ID 已被删除/不存在于 manifest（如旧版遗留的 'coffee-mocha'），
                //   降级到内置主题，避免非-existent ID 触发 watcher 级联循环导致页面卡死。
                var savedLight = localStorage.getItem('brainforever_theme_light');
                var savedDark  = localStorage.getItem('brainforever_theme_dark');

                // 验证亮色主题
                if (savedLight && !this.availableLight.some(function(t) { return t.id === savedLight; })) {
                    savedLight = 'builtin-light';
                }
                // 验证暗色主题
                if (savedDark && !this.availableDark.some(function(t) { return t.id === savedDark; })) {
                    savedDark = 'builtin-dark';
                }

                // ★ 保存原始值，供取消时恢复
                this._originalLight = savedLight || 'builtin-light';
                this._originalDark  = savedDark || 'builtin-dark';
                // ★ 保存当前亮暗模式，供取消时恢复（bit 0=1 为暗色）
                this._originalMode = (Alpine.store('settings').theme & 1) ? 'dark' : 'light';

                // ★ 在设置初始值之前禁用联动 watcher，防止 selectedLight/selectedDark
                //   被重复赋值时触发 watcher 之间的级联反应（Alpine $watch 同步执行，
                //   可能因响应式系统内部机制导致无限更新循环）。
                //   设置完初始值后再恢复联动。
                var prevLink = this.linkThemes;
                this.linkThemes = false;

                this.selectedLight = this._originalLight;
                this.selectedDark  = this._originalDark;
                this.selectedMode  = this._originalMode;

                this.linkThemes = prevLink;

                // ★ 标记初始化完成，之后的选择变化将立即生效
                this._initComplete = true;

                this.show = true;
            },

            /**
             * toggleLinkThemes — 切换亮暗联动开关
             * 开启时自动同步当前选择的对应主题
             */
            toggleLinkThemes: function() {
                this.linkThemes = !this.linkThemes;
                if (this.linkThemes) {
                    // 开启联动时，根据当前"立即启用"选中的模式决定同步方向
                    // 这样联动方向总是跟随用户最后一次操作的主题
                    if (this.selectedMode === 'dark') {
                        // 暗色→亮色：取 selectedDark 的联动亮色，覆盖 selectedLight
                        var linked = this.selectedDark
                            ? this._getLinkedId(this.selectedDark, 'light')
                            : '';
                        if (linked !== this.selectedLight) {
                            this.selectedLight = linked;
                        }
                    } else {
                        // 亮色→暗色：取 selectedLight 的联动暗色，覆盖 selectedDark
                        var linked = this.selectedLight
                            ? this._getLinkedId(this.selectedLight, 'dark')
                            : '';
                        if (linked !== this.selectedDark) {
                            this.selectedDark = linked;
                        }
                    }
                }
            },

            /**
             * close — 关闭对话框（取消操作），恢复原始主题选择
             */
            close: function() {
                var needRestore = false;

                // 1. 恢复主题选择（若变更）
                if (this._originalLight !== undefined && this._originalDark !== undefined) {
                    var themeChanged = this.selectedLight !== this._originalLight
                        || this.selectedDark !== this._originalDark;
                    if (themeChanged) {
                        this._initComplete = false;
                        this.selectedLight = this._originalLight;
                        this.selectedDark  = this._originalDark;
                        this._initComplete = true;
                        // 先将原始主题 ID 持久化（不触发 ThemeLoader.apply，避免与模式恢复冲突）
                        Alpine.store('settings').setThemeSelection(this.selectedLight, this.selectedDark);
                        // 同步恢复后的主题到服务端
                        var settings = Alpine.store('settings');
                        if (settings.theme < 2 && settings.themeSync) {
                            if (typeof window.fetchApplyThemeSelection === 'function') {
                                window.fetchApplyThemeSelection(settings.theme, this.selectedLight, this.selectedDark);
                            }
                        }
                        needRestore = true;
                    }
                }

                // 2. 恢复亮暗模式（若变更）
                if (this._originalMode !== undefined && this.selectedMode !== this._originalMode) {
                    this.selectedMode = this._originalMode;
                    // _applyMode 会触发 ThemeLoader.apply，重新加载正确模式的主题 CSS
                    this._applyMode(this._originalMode);
                    needRestore = true;
                } else if (needRestore) {
                    // 主题变了但模式没变，手动触发 ThemeLoader 加载已恢复的主题 CSS
                    if (window.ThemeLoader) {
                        window.ThemeLoader.apply();
                    }
                }

                this.show = false;
            },

            /**
             * confirm — 确认选择，关闭对话框
             * 主题选择已在 watcher 中立即生效，此处仅关闭对话框（不恢复原始值）
             */
            confirm: function() {
                this.show = false;
            },
        };
    });

});

// ============================================================
// onOpenThemeDialog — 打开主题选择对话框（注册到 window，供 @click 调用）
// ============================================================
window.onOpenThemeDialog = function() {
    try {
        var dialogEl = document.getElementById('themeDialog');
        if (dialogEl) {
            var data = Alpine.$data(dialogEl);
            if (data && typeof data.open === 'function') {
                data.open();
                return;
            }
        }
        console.warn('主题选择对话框组件未找到或未初始化');
    } catch (e) {
        console.error('打开主题选择对话框失败:', e);
    }
};
