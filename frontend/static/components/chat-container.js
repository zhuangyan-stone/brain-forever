// ============================================================
// chat-container.js — 聊天容器 Alpine 组件
// ============================================================
// 用途：管理 #chatContainer 的 Alpine 状态，提供 x-for 模板中
//       使用的辅助方法（showDeleteModal、confirmDeleteGroup 等）。
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // chatContainer — 聊天容器 Alpine 组件
    // ============================================================
    // 使用方式：
    //   <main class="chat-container" id="chatContainer"
    //         x-data="chatContainer()"
    //         x-init="init($el)">
    //       <template x-for="(group, idx) in $store.chats.active?.groups ?? []">
    //           ...
    //       </template>
    //   </main>
    Alpine.data('chatContainer', () => ({
        /**
         * 初始化：保存容器 DOM 引用
         * @param {HTMLElement} el
         */
        init: function(el) {
            // 保存容器引用供外部 JS 使用
            window._chatContainerEl = el;
        },

        /**
         * 显示删除确认对话框
         * 由 Alpine x-for 模板中的 @click 调用
         * @param {number} idx - groups 数组中的索引
         */
        showDeleteModal: function(idx) {
            var chats = window.Alpine.store('chats');
            if (!chats || !chats.active) return;
            var group = chats.active.groups[idx];
            if (!group) return;

            // ---- 填充对话框内容：显示用户消息和助手消息预览 ----
            var modalBody = document.getElementById('modalBody');
            var modalNote = document.getElementById('modalDeleteNote');
            if (modalBody) {
                var html = '';

                // 用户消息预览
                if (group.user) {
                    var userPreview = this._escapeHtml(group.user.content || '');
                    html += '<div class="del-preview-msg del-preview-user">'
                        + '<div class="role-label">我</div>'
                        + '<div class="del-preview-bubble">' + userPreview + '</div>'
                        + '</div>';

                    // AI 回复预览
                    var assistantContent = group.assistant ? group.assistant.content : '';
                    if (assistantContent) {
                        var assistantPreview = this._escapeHtml(assistantContent.trim());
                        if (assistantPreview) {
                            html += '<div class="del-preview-msg del-preview-assistant">'
                                + '<div class="role-label">AI</div>'
                                + '<div class="del-preview-bubble"><span class="del-preview-text">' + assistantPreview + '</span></div>'
                                + '</div>';
                        }
                    }
                }

                modalBody.innerHTML = html || '<div class="del-preview-empty">(无内容)</div>';

                // 更新固定提醒内容
                if (modalNote) {
                    modalNote.style.display = html ? 'block' : 'none';
                }
            }

            // 设置活动刻度索引（用于刻度导航高亮）
            try {
                var tickNav = Alpine.store('tickNav');
                if (tickNav) tickNav.activeTickIndex = idx;
            } catch(e) {}

            // 通过 Alpine 打开删除确认对话框
            var deleteModal = document.getElementById('msgDeleteModal');
            if (!deleteModal) return;
            Alpine.$data(deleteModal).open(idx);
        },

        /**
         * HTML 转义（内联实现，因为此文件以普通 <script> 加载，无法 import）
         * @param {string} str
         * @returns {string}
         */
        _escapeHtml: function(str) {
            var div = document.createElement('div');
            div.textContent = str;
            return div.innerHTML;
        },

        /**
         * 确认删除指定索引的消息组（由 confirmDelete 调用）
         * @param {number} idx - groups 数组中的索引
         */
        confirmDeleteGroup: function(idx) {
            var chats = window.Alpine.store('chats');
            if (chats) {
                chats.deleteGroup(idx);
            }
        },
    }));

});
