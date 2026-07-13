// ============================================================
// dialogs/msg-delete-dialog.js — 删除确认对话框 Alpine 组件
// ============================================================
// 包含：
//   1. msgDeleteDialog — 删除确认对话框的 Alpine 状态管理
//   2. confirmDelete   — 确认删除的回调（注册到 window）
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // msgDeleteDialog — 删除确认对话框
    // ============================================================
    // 用途：消息删除确认对话框的状态管理
    //
    // 使用方式：
    //   <div class="dialog-overlay" id="msgDeleteModal"
    //        x-data="msgDeleteDialog()"
    //        :class="{ show: show }">
    //   </div>
    //
    //   在 JS 中通过 Alpine.$data(msgDeleteModal) 操作：
    //     Alpine.$data(msgDeleteModal).open(deleteIndex)
    //     Alpine.$data(msgDeleteModal).close()
    Alpine.data('msgDeleteDialog', () => ({
        show: false,
        deleteIndex: -1,

        /**
         * 打开删除对话框
         * @param {number} index - 要删除的消息索引
         */
        open: function(index) {
            this.deleteIndex = index;
            this.show = true;
            // 内容填充由 chatContainer Alpine 组件中的 showDeleteModal() 完成
            // （内容从 Alpine store 动态提取，不适合 Alpine 模板）
        },

        /**
         * 关闭删除对话框
         */
        close: function() {
            this.show = false;
            this.deleteIndex = -1;
        },
    }));

});

// ============================================================
// confirmDelete — 确认删除（由 Alpine @click 调用，注册在 window 上）
// ============================================================
window.confirmDelete = async function() {
    const deleteModal = document.getElementById('msgDeleteModal');
    if (!deleteModal) return;

    const deleteData = Alpine.$data(deleteModal);
    const index = deleteData.deleteIndex;
    if (index < 0) return;

    var chats = window.Alpine.store('chats');
    var group = chats && chats.active ? chats.active.groups[index] : null;
    if (!group) {
        deleteData.close();
        return;
    }

    const msgId = group.msgId || 0;

    try {
        // msgId 为 0 表示提交未完成，仅删除前端数据
        if (msgId) {
            const { deleteMessage } = await import('./chat-api.js');
            const ok = await deleteMessage(msgId);
            if (!ok) {
                throw new Error('删除失败，服务器返回错误');
            }
        }

        // 从 Alpine store 的 groups 数组中移除（Alpine x-for 自动更新 DOM）
        chats.active.groups.splice(index, 1);

        // 更新刻度导航
        const { updateTickNav } = await import('./chat-ticknav.js');
        updateTickNav();

        // 显示删除成功提示
        window.showToast('消息已删除', 'success');

    } catch (e) {
        console.error('删除失败:', e);
        window.showToast('删除失败: ' + e.message, 'error');
    } finally {
        deleteData.close();
    }
};
