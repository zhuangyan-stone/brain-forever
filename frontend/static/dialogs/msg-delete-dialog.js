// ============================================================
// msg-delete-dialog.js — 消息删除对话框
// ============================================================
// 显隐状态由 Alpine 组件 deleteDialog 管理（x-data + :class="{ show }"），
// 不再手动操作 classList。
// 内容填充（从 DOM 提取消息预览）仍由本模块负责，
// 因为内容来自运行时 DOM，不适合在 Alpine 模板中预先定义。
// ============================================================

import { escapeHtml } from '../toolsets.js';
import { state } from '../chat-state.js';
import { showToast } from '../chat-ui.js';
import { updateTickNav } from '../chat-ticknav.js';

'use strict';

/**
 * showDeleteModal 显示删除确认模态框
 *
 * 通过 Alpine.$data 操作 Alpine 组件的响应式状态，
 * Alpine 自动触发 :class="{ show: true }" 更新 DOM 显隐。
 */
export function showDeleteModal() {
    const deleteModal = document.getElementById('deleteModal');
    const modalBody = document.getElementById('modalBody');
    const modalNote = document.getElementById('modalDeleteNote');
    const chatContainer = document.getElementById('chatContainer');
    if (!deleteModal || !modalBody || !chatContainer) return;

    if (state.activeTickIndex < 0) return;

    // ---- 填充内容 ----
    const deleteIndex = state.activeTickIndex;

    // 获取用户消息
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${deleteIndex}"]`);
    let html = '';
    if (userMsg) {
        const rawContent = userMsg.querySelector('.bubble').textContent || '';
        const userPreview = escapeHtml(rawContent);
        html += '<div class="del-preview-msg del-preview-user">'
            + '<div class="role-label">我</div>'
            + '<div class="del-preview-bubble">' + userPreview + '</div>'
            + '</div>';
        // 在同一个 .message-group 内查找 AI 回复
        const group = userMsg.closest('.message-group');
        if (group) {
            const assistantMsg = group.querySelector('.message.assistant');
            if (assistantMsg) {
                const assistantContent = assistantMsg.querySelector('.bubble').textContent || '';
                const assistantPreview = escapeHtml(assistantContent.trim());
                if (assistantPreview) {
                    html += '<div class="del-preview-msg del-preview-assistant">'
                        + '<div class="role-label">AI</div>'
                        + '<div class="del-preview-bubble"><span class="del-preview-text">' + assistantPreview + '</span></div>'
                        + '</div>';
                }
            }
        }
    }

    modalBody.innerHTML = html || '<div class="del-preview-empty">(无内容)</div>';

    // 更新固定提醒内容
    if (modalNote) {
        modalNote.style.display = html ? 'block' : 'none';
    }

    // ---- 通过 Alpine 打开对话框 ----
    Alpine.$data(deleteModal).open(deleteIndex);
}

/**
 * confirmDelete 确认删除（由 Alpine @click 调用，注册在 window 上）
 */
window.confirmDelete = async function() {
    const deleteModal = document.getElementById('deleteModal');
    const chatContainer = document.getElementById('chatContainer');
    if (!deleteModal || !chatContainer) return;

    const deleteData = Alpine.$data(deleteModal);
    const index = deleteData.deleteIndex;
    if (index < 0) return;

    // 获取要删除的用户消息
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${index}"]`);
    if (!userMsg) {
        deleteData.close();
        return;
    }

    // 找到该消息所在的 .message-group
    const group = userMsg.closest('.message-group');
    if (!group) {
        deleteData.close();
        return;
    }

    // 从 message-group 获取 msg_id
    const msgId = parseInt(group.dataset.msgId, 10);

    // 在移除 DOM 之前，先收集该组中所有消息的 ID
    const groupMsgIds = new Set();
    if (!isNaN(msgId)) groupMsgIds.add(msgId);

    try {
        // msgId 为 0 或无效（NaN）表示提交未完成，仅删除前端 DOM
        if (!msgId || isNaN(msgId)) {
            group.remove();
        } else {
            // 有有效 ID，先调后端 API 删除
            const response = await fetch('/api/chat/messages', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ msg_id: msgId })
            });

            if (!response.ok) {
                const errText = await response.text();
                throw new Error(`删除失败 [${response.status}]: ${errText}`);
            }

            // 后端删除成功后，移除整个消息组
            group.remove();
        }

        // 从 messages 数组中删除该组中所有消息
        state.messages = state.messages.filter(msg => !groupMsgIds.has(msg.id));

        // 重新编号所有 user 消息的 data-msg-index
        const remainingUsers = chatContainer.querySelectorAll('.message.user');
        remainingUsers.forEach((msg, i) => {
            msg.dataset.msgIndex = i;
        });

        // 更新 userMsgCount
        state.userMsgCount = remainingUsers.length;

        // 更新刻度导航
        updateTickNav();

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
