// ============================================================
// sources-panel.js — 引用来源面板 Alpine 组件
// ============================================================
// 用途：管理 sources-panel 的折叠状态和初始化。
// 数据层由 group.assistant.sources 驱动，SwipePager 由 JS 管理。
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // sourcesPanel — 引用来源面板 Alpine 组件
    // ============================================================
    // 使用方式：
    //   <div class="sources-panel"
    //        x-data="sourcesPanel(group.assistant)"
    //        x-show="group.assistant.sources?.length > 0">
    //       ...
    //   </div>
    Alpine.data('sourcesPanel', (assistant) => ({
        collapsed: true,

        init() {
            this._assistant = assistant;
            if (assistant && assistant._sourcesCollapsed !== undefined) {
                this.collapsed = assistant._sourcesCollapsed;
            }
        },

        /**
         * 切换折叠/展开。
         * 通过 event.currentTarget.closest() 定位 panel（不依赖 this.$el，
         * 因为 Alpine 嵌套 x-data 下 $el 指向错误元素）。
         */
        toggle(event) {
            this.collapsed = !this.collapsed;
            if (assistant) {
                assistant._sourcesCollapsed = this.collapsed;
            }
            if (!this.collapsed) {
                var panelEl = event && event.currentTarget
                    ? event.currentTarget.closest('.sources-panel')
                    : null;
                if (!panelEl) return;
                var container = panelEl.querySelector('.sources-items-container');
                if (!container) return;

                setTimeout(function() {
                    var _ = container.offsetHeight; // 强制重排，确保容器有宽度

                    if (typeof window._initSourcesPager === 'function') {
                        window._initSourcesPager(container, assistant);
                    }

                    if (window.scrollPanelIntoView) {
                        window.scrollPanelIntoView(panelEl);
                    }
                }, 0);
            }
        },
    }));

});
