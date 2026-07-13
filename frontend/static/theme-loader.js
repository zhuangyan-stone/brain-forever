// ============================================================
// theme-loader.js — 外源主题加载器
// ============================================================
// 职责：
//   1. 提供 window.ThemeLoader 全局 API
//   2. 运行时动态加载/切换外源主题 CSS
//   3. 缓存 manifest 中的 themes 列表
//
// 加载顺序（index.html 中）：
//   1. theme.css（内置主题）
//   2. document.write 注入的外源主题 CSS（零 FOUC 策略）
//   3. theme-loader.js  → 提供运行时切换能力
//   4. alpine-store.js  → 注册 settings store
// ============================================================

'use strict';

window.ThemeLoader = (function() {
    var _currentId = '';      // 当前已加载的外源主题 ID
    var _manifestCache = null; // themes[] 缓存

    return {
        /** 当前已加载的外源主题 ID（空字符串=使用内置主题） */
        get currentId() { return _currentId; },

        /**
         * apply — 根据当前 data-theme 加载对应的外源主题 CSS
         *
         * 读取 localStorage 中的 brainforever_theme_light / brainforever_theme_dark，
         * 决定加载哪个外源主题。如果 id 为空则清除外源 CSS。
         *
         * 此方法在以下时机调用：
         *   - 页面加载完成后（由 chat.js 的 applyTheme 调用）
         *   - 用户切换明暗模式时（由 toggleTheme 触发）
         *   - 用户通过对话框选择新主题后（由 setThemeSelection 触发）
         */
        apply: function() {
            var mode = document.documentElement.getAttribute('data-theme') || 'light';
            var themeId = mode === 'light'
                ? localStorage.getItem('brainforever_theme_light') || ''
                : localStorage.getItem('brainforever_theme_dark') || '';

            // themeId 为空或为 builtin-light/builtin-dark 时，均表示使用内置主题
            if (!themeId || themeId === 'builtin-light' || themeId === 'builtin-dark') {
                this.clear();
                return;
            }

            // 已加载相同主题，跳过
            if (_currentId === themeId) return;

            // 移除旧的
            this.clear();

            // 创建新的 <link>
            var link = document.createElement('link');
            link.rel = 'stylesheet';
            link.href = '/themes/' + encodeURIComponent(themeId) + '/theme.css';
            link.id = 'external-theme-css';
            document.head.appendChild(link);
            _currentId = themeId;
        },

        /**
         * clear — 移除所有外源主题 CSS，回退到内置主题
         *
         * 清除两类 <link>：
         *   1. 运行时加载的（id="external-theme-css"）
         *   2. 页面初始化时通过 document.write 注入的（无 id，但 href 以 /themes/ 开头）
         */
        clear: function() {
            var links = document.querySelectorAll('link[href^="/themes/"]');
            for (var i = 0; i < links.length; i++) {
                var link = links[i];
                link.parentNode.removeChild(link);
            }
            _currentId = '';
        },

        /**
         * loadManifest — 从 /api/themes/mainfes 加载主题清单和用户选择
         * @returns {Promise<object|null>} manifest 数据对象
         */
        loadManifest: async function() {
            try {
                var resp = await fetch('/api/themes/mainfes');
                if (!resp.ok) {
                    var t = await resp.text();
                    throw new Error(t);
                }
                var data = await resp.json();
                // 将内置主题作为完整对象注入到列表最前面，
                // 使所有展示代码（tooltip、对话框等）无需特判内置/外源
                var builtinThemes = [
                    { id: 'builtin-light', mode: 'light', highContrast: true, name: 'Buildin - Bright Eyes · Day ', name_zh: '默认 - 明眸·昼', description: '白底黑字，清亮明快' },
                    { id: 'builtin-dark', mode: 'dark', highContrast: true, name: 'Buildin - Bright Eyes · Night ', name_zh: '默认 - 明眸·夜', description: '黑底白字，沉静专注' },
                ];
                data.themes = builtinThemes.concat(data.themes || []);
                _manifestCache = data.themes;

                // ★ 清理 localStorage 中已删除/不存在的主题 ID，避免页面加载时
                //   尝试加载已删除主题的 CSS 导致 MIME 类型错误和控制台报错。
                //   同时也防止非标准 ID 在后续打开对话框时触发 watcher 级联问题。
                (function cleanStaleThemeIds(allThemes) {
                    var validIds = {};
                    for (var i = 0; i < allThemes.length; i++) {
                        validIds[allThemes[i].id] = true;
                    }
                    var lightId = localStorage.getItem('brainforever_theme_light');
                    var darkId = localStorage.getItem('brainforever_theme_dark');
                    var changed = false;
                    if (lightId && !validIds[lightId]) {
                        localStorage.removeItem('brainforever_theme_light');
                        changed = true;
                    }
                    if (darkId && !validIds[darkId]) {
                        localStorage.removeItem('brainforever_theme_dark');
                        changed = true;
                    }
                    if (changed) {
                        console.log('ThemeLoader: 已清理 localStorage 中不存在的主题 ID');
                    }
                })(data.themes);

                return data;
            } catch(e) {
                console.warn('ThemeLoader: 加载主题清单失败', e);
                return null;
            }
        },

        /**
         * getThemes — 获取缓存的 themes 列表
         * @returns {Array} 主题列表 [{ id, name, name_zh, mode, description }, ...]
         */
        getThemes: function() {
            return _manifestCache || [];
        },
    };
})();
