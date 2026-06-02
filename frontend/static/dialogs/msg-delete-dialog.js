// ============================================================
// msg-delete-dialog.js — 消息删除对话框
// ============================================================
// 显隐状态由 Alpine 组件 deleteDialog 管理（x-data + :class="{ show }"），
// 不再手动操作 classList。
// 内容填充从 Alpine store 的 groups 数组获取，不再从 DOM 提取。
// 确认删除时操作 Alpine store 的 groups 数组，Alpine x-for 自动更新 DOM。
// ============================================================

import { escapeHtml } from '../toolsets.js';
import { showToast } from '../chat-ui.js';
import { updateTickNav } from '../chat-ticknav.js';
import { deleteMessage } from '../chat-api.js';

'use strict';

/**
 * showDeleteModal 显示删除确认模态框
 *
 * 从 Alpine store 的 groups 数组中获取消息内容预览，
 * 不再从 DOM 中查找（因为 DOM 由 Alpine 管理，无 data-msg-index 属性）。
 *
 * @param {number} index - groups 数组中的索引
 */
export function showDeleteModal(index) {
    const deleteModal = document.getElementById('deleteModal');
    const modalBody = document.getElementById('modalBody');
    const modalNote = document.getElementById('modalDeleteNote');
    if (!deleteModal || !modalBody) return;

    if (index < 0) return;

    // ---- 从 Alpine store 获取消息内容预览 ----
    var chats = window.Alpine.store('chats');
    var group = chats && chats.active ? chats.active.groups[index] : null;
    if (!group) return;

    let html = '';

    // 用户消息预览
    if (group.user) {
        const userPreview = escapeHtml(group.user.content || '');
        html += '<div class="del-preview-msg del-preview-user">'
            + '<div class="role-label">我</div>'
            + '<div class="del-preview-bubble">' + userPreview + '</div>'
            + '</div>';

        // AI 回复预览（从 group.assistant.content 获取）
        // ★ 方案B：group.assistant 始终存在，直接取其 content
        const assistantContent = group.assistant ? group.assistant.content : '';
        if (assistantContent) {
            const assistantPreview = escapeHtml(assistantContent.trim());
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

    // ---- 通过 Alpine 打开对话框 ----
    Alpine.$data(deleteModal).open(index);
}

/**
 * confirmDelete 确认删除（由 Alpine @click 调用，注册在 window 上）
 *
 * 操作 Alpine store 的 groups 数组（splice），Alpine x-for 自动移除 DOM。
 * 同时清理 Alpine store 中对应的 messages 条目。
 */
window.confirmDelete = async function() {
    const deleteModal = document.getElementById('deleteModal');
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

    // 获取 msgId 用于后端删除和 Alpine store messages 清理
    const msgId = group.msgId || 0;

    try {
        // msgId 为 0 表示提交未完成，仅删除前端数据
        if (msgId) {
            // 有有效 ID，先调后端 API 删除
            const ok = await deleteMessage(msgId);
            if (!ok) {
                throw new Error('删除失败，服务器返回错误');
            }
        }

        // 从 Alpine store 的 groups 数组中移除（Alpine x-for 自动更新 DOM）
        chats.active.groups.splice(index, 1);

        // 更新刻度导航
        updateTickNav();

        // 显示删除成功提示
        showToast('消息已删除', 'success');

    } catch (e) {
        console.error('删除失败:', e);
        showToast('删除失败: ' + e.message, 'error');
    } finally {
        deleteData.close();
    }
};

/**
 * 初始化删除模态框
 *
 * ⚠️ 注意：Alpine 已通过 @click 处理关闭按钮和遮罩层点击，
 *    不再需要手动绑定事件监听器。
 *    此函数保留为空，确保调用方（chat.js）不会因缺少导出而报错。
 *
 * 若将来需要与其他非 Alpine 逻辑集成，在此添加。
 */
export function initDeleteModal() {
    // 事件绑定已由 Alpine 的 @click 处理，无需额外操作
}
