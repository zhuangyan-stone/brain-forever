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
                ? (localStorage.getItem('brainforever_theme_light') || '')
                : (localStorage.getItem('brainforever_theme_dark') || '');

            if (!themeId) {
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
         */
        clear: function() {
            var old = document.getElementById('external-theme-css');
            if (old) {
                old.parentNode.removeChild(old);
            }
            _currentId = '';
        },

        /**
         * loadManifest — 从 /api/themes 加载主题清单和用户选择
         * @returns {Promise<object|null>} manifest 数据对象
         */
        loadManifest: async function() {
            try {
                var resp = await fetch('/api/themes');
                if (!resp.ok) throw new Error('HTTP ' + resp.status);
                var data = await resp.json();
                _manifestCache = data.themes || [];
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
