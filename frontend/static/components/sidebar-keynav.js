// ============================================================
// sidebar-keynav.js — 侧边栏键盘导航 Alpine 组件（简化版）
// ============================================================
// 简化说明：移除所有 ↑↓/Home/End/←→ 导航和 Tab 循环，
//           完全依赖浏览器原生 Tab/Shift+Tab 顺序导航。
//           仅保留 Enter/Space 激活当前项。
//
//   1. sidebarTabs     — Tab 栏左右方向键切换 Tab
//   2. sidebarTreeNav  — 树内容区 Enter/Space 激活
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

'use strict';

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 1. sidebarTabs — Tab 栏键盘导航（仅左右方向键切换）
    // ============================================================
    Alpine.data('sidebarTabs', function() {
        return {
            TAB_NAMES: ['timeline', 'category', 'favorites'],

            _currentIdx: function() {
                var store = Alpine.store('chats');
                if (!store) return 0;
                var idx = this.TAB_NAMES.indexOf(store.sidebarTab || 'timeline');
                return idx < 0 ? 0 : idx;
            },

            prevTab: function() {
                var idx = this._currentIdx();
                var prev = this.TAB_NAMES[(idx - 1 + this.TAB_NAMES.length) % this.TAB_NAMES.length];
                this._switchAndFocus(prev);
            },

            nextTab: function() {
                var idx = this._currentIdx();
                var next = this.TAB_NAMES[(idx + 1) % this.TAB_NAMES.length];
                this._switchAndFocus(next);
            },

            _switchAndFocus: function(tabName) {
                var store = Alpine.store('chats');
                if (!store) return;
                store.switchSidebarTab(tabName);
                var self = this;
                Alpine.nextTick(function() {
                    var btn = self.$el.querySelector('.sidebar-tab[data-tab="' + tabName + '"]');
                    if (btn) btn.focus();
                });
            },
        };
    });

    // ============================================================
    // 2. sidebarTreeNav — 简化版：仅处理 Enter/Space 激活
    // ============================================================
    Alpine.data('sidebarTreeNav', function(tabName) {
        return {
            /** 激活当前焦点所在的 treeitem（Enter/Space） */
            activateItem: function() {
                var activeEl = document.activeElement;
                if (!activeEl || activeEl.getAttribute('role') !== 'treeitem') return;
                activeEl.click();
            },
        };
    });

});
