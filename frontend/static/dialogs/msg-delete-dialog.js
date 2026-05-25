// ============================================================
// msg-delete-dialog.js — 消息删除对话框
// ============================================================

import { escapeHtml } from '../toolsets.js';
import { state } from '../chat-state.js';
import { showToast } from '../chat-ui.js';
import { updateTickNav } from '../chat-ticknav.js';

'use strict';

/**
 * showDeleteModal 显示删除确认模态框
 */
export function showDeleteModal() {
    const deleteModal = document.getElementById('deleteModal');
    const modalBody = document.getElementById('modalBody');
    const modalNote = document.getElementById('modalDeleteNote');
    const chatContainer = document.getElementById('chatContainer');
    if (!deleteModal || !modalBody || !chatContainer) return;

    if (state.activeTickIndex < 0) return;

    // 将当前活动索引保存到模态框，避免 mouseleave 清除 activeTickIndex 后丢失
    deleteModal.dataset.deleteIndex = state.activeTickIndex;

    // 获取用户消息
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${state.activeTickIndex}"]`);
    let html = '';
    if (userMsg) {
        const rawContent = userMsg.querySelector('.bubble').textContent || '';
        // 用户消息完整保留在 DOM 中（CSS 控制单行截断）
        const userPreview = escapeHtml(rawContent);
        html += '<div class="del-preview-msg del-preview-user">'
            + '<div class="role-label">我</div>'
            + '<div class="del-preview-bubble">' + userPreview + '</div>'
            + '</div>';
        // 在同一个 .message-group 内查找 AI 回复（不依赖 data-msg-id 配对）
        const group = userMsg.closest('.message-group');
        if (group) {
            const assistantMsg = group.querySelector('.message.assistant');
            if (assistantMsg) {
                const assistantContent = assistantMsg.querySelector('.bubble').textContent || '';
                // 助手消息完整保留在 DOM 中（CSS 控制单行截断）
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

    deleteModal.classList.add('show');
}

/**
 * hideDeleteModal 隐藏删除模态框
 */
function hideDeleteModal() {
    const deleteModal = document.getElementById('deleteModal');
    if (!deleteModal) return;
    deleteModal.classList.remove('show');
    delete deleteModal.dataset.deleteIndex;
}

/**
 * confirmDelete 确认删除
 */
async function confirmDelete() {
    const deleteModal = document.getElementById('deleteModal');
    const chatContainer = document.getElementById('chatContainer');
    if (!deleteModal || !chatContainer) return;

    const index = parseInt(deleteModal.dataset.deleteIndex, 10);
    if (isNaN(index) || index < 0) return;

    // 获取要删除的用户消息
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${index}"]`);
    if (!userMsg) {
        hideDeleteModal();
        return;
    }

    // 找到该消息所在的 .message-group
    const group = userMsg.closest('.message-group');
    if (!group) {
        hideDeleteModal();
        return;
    }

    // 从 message-group 获取 msg_id（同一组的 user 和 assistant 共享同一 ID）
    const msgId = parseInt(group.dataset.msgId, 10);

    // 在移除 DOM 之前，先收集该组中所有消息的 ID（用于后续清理 messages 数组）
    const groupMsgIds = new Set();
    // 同一组的 user 和 assistant 共享同一 ID，只需添加一次
    if (!isNaN(msgId)) groupMsgIds.add(msgId);

    try {
        // msgId 为 0 或无效（NaN）表示提交未完成（失败或尚未分配），仅删除前端 DOM
        if (!msgId || isNaN(msgId)) {
            group.remove();
        } else {
            // 有有效 ID，先调后端 API 删除
            const response = await fetch('/api/history', {
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

        // 从 messages 数组中删除该组中所有消息（包括 id=0 的条目）
        state.messages = state.messages.filter(msg => !groupMsgIds.has(msg.id));

        // 重新编号所有 user 消息的 data-msg-index
        const remainingUsers = chatContainer.querySelectorAll('.message.user');
        remainingUsers.forEach((msg, i) => {
            msg.dataset.msgIndex = i;
        });

        // 更新 userMsgCount
        state.userMsgCount = remainingUsers.length;

        // currentGroup 已取消，不再需要维护。
        // addMessage('assistant') 会通过 dom.chatContainer.querySelector('.message-group:last-child')
        // 自动找到当前组，因此无需设置 state.currentGroup。

        // 更新刻度导航
        updateTickNav();

    } catch (e) {
        console.error('删除失败:', e);
        showToast('删除失败: ' + e.message, 'error');
    } finally {
        hideDeleteModal();
    }
}

/**
 * 初始化删除模态框的事件绑定
 */
export function initDeleteModal() {
    const deleteModal = document.getElementById('deleteModal');
    const modalCloseBtn = document.getElementById('modalCloseBtn');
    const modalCancelBtn = document.getElementById('modalCancelBtn');
    const modalConfirmBtn = document.getElementById('modalConfirmBtn');

    if (!deleteModal || !modalCloseBtn || !modalCancelBtn || !modalConfirmBtn) return;

    modalCloseBtn.addEventListener('click', hideDeleteModal);
    modalCancelBtn.addEventListener('click', hideDeleteModal);
    modalConfirmBtn.addEventListener('click', confirmDelete);

    // 点击模态框外部关闭
    deleteModal.addEventListener('click', (e) => {
        if (e.target === deleteModal) {
            hideDeleteModal();
        }
    });
}
