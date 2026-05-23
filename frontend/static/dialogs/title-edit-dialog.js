// ============================================================
// title-edit-dialog.js — 修改对话标题对话框
// 使用 dialog.css 中的公共对话框样式，复用 dialog-overlay / dialog-box 结构。
// 风格与现有删除确认对话框一致（但不是 sticky）。
// ============================================================
// 使用方式：
//   import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
//   showTitleEditDialog({
//       currentTitle: '原标题',
//       onConfirm: async (newTitle) => { /* 确认回调 */ },
//   });
// ============================================================

'use strict';

/**
 * 显示修改对话标题对话框。
 *
 * 对话框结构：
 *   - 标题："修改对话标题"
 *   - 内容区：
 *       - 原标题（较大字体显示）
 *       - label："新标题"
 *       - 编辑框（单行，默认值为原标题，自动全选）
 *   - UI 联动：编辑框输入时，原标题内容同步变更
 *   - 底部按钮：取消、确认
 *
 * 确认后调用 onConfirm(newTitle)，需返回 Promise<boolean> 表示后端是否修改成功。
 * 成功后对话框自动关闭，调用方自行更新页面标题。
 *
 * @param {object} options
 * @param {string} options.currentTitle - 当前对话标题
 * @param {(newTitle: string) => Promise<boolean>} options.onConfirm - 确认回调，返回是否成功
 * @param {() => void} [options.onCancel] - 取消回调（可选）
 */
export function showTitleEditDialog({ currentTitle, onConfirm, onCancel } = {}) {
    if (!currentTitle) return;

    // ---- 创建遮罩层 ----
    const overlay = document.createElement('div');
    overlay.className = 'dialog-overlay show';

    // ---- 对话框容器 ----
    const box = document.createElement('div');
    box.className = 'dialog-box';

    // ---- 头部 ----
    const header = document.createElement('div');
    header.className = 'dialog-header';
    const titleEl = document.createElement('h3');
    titleEl.textContent = '修改对话标题';
    const closeBtn = document.createElement('button');
    closeBtn.className = 'dialog-close-btn';
    closeBtn.innerHTML = '&times;';
    closeBtn.setAttribute('aria-label', '关闭');
    header.appendChild(titleEl);
    header.appendChild(closeBtn);
    box.appendChild(header);

    // ---- 内容区 ----
    const body = document.createElement('div');
    body.className = 'dialog-body';

    // 原标题（较大字体显示）
    const originalEl = document.createElement('div');
    originalEl.className = 'title-edit-original';
    originalEl.textContent = currentTitle;
    body.appendChild(originalEl);

    // label：新标题
    const labelEl = document.createElement('label');
    labelEl.className = 'title-edit-label';
    labelEl.textContent = '新标题';
    body.appendChild(labelEl);

    // 编辑框（单行，不换行），限长 50 字符（与后端 truncateTitle 一致）
    const inputEl = document.createElement('input');
    inputEl.type = 'text';
    inputEl.className = 'title-edit-input';
    inputEl.value = currentTitle;
    inputEl.maxLength = 50;
    // 默认全部选中
    inputEl.select();
    body.appendChild(inputEl);

    box.appendChild(body);

    // ---- UI 联动：编辑框输入时，原标题内容同步变更 ----
    // 清空时恢复为原始原标题（不显示"(空)"）
    inputEl.addEventListener('input', () => {
        const val = inputEl.value;
        originalEl.textContent = val || currentTitle;
    });

    // ---- 底部按钮区 ----
    const footer = document.createElement('div');
    footer.className = 'dialog-footer';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'dialog-btn dialog-btn-cancel';
    cancelBtn.textContent = '取消';
    footer.appendChild(cancelBtn);

    const confirmBtn = document.createElement('button');
    confirmBtn.className = 'dialog-btn dialog-btn-confirm';
    confirmBtn.textContent = '确认';
    footer.appendChild(confirmBtn);

    box.appendChild(footer);

    overlay.appendChild(box);
    document.body.appendChild(overlay);

    // ---- 自动聚焦编辑框（select() 后仍需 focus 以确保光标可见） ----
    // 延迟确保 DOM 已渲染
    requestAnimationFrame(() => {
        inputEl.focus();
        inputEl.select();
    });

    // ---- 关闭对话框 ----
    function closeDialog() {
        overlay.classList.remove('show');
        // 等过渡动画结束后移除 DOM
        overlay.addEventListener('transitionend', () => {
            if (overlay.parentNode) {
                overlay.parentNode.removeChild(overlay);
            }
        }, { once: true });
        // 兜底：如果 transitionend 未触发（如 display:none 未过渡），直接移除
        setTimeout(() => {
            if (overlay.parentNode) {
                overlay.parentNode.removeChild(overlay);
            }
        }, 300);
    }

    // ---- 事件绑定 ----

    // 关闭按钮
    closeBtn.addEventListener('click', () => {
        if (typeof onCancel === 'function') onCancel();
        closeDialog();
    });

    // 取消按钮
    cancelBtn.addEventListener('click', () => {
        if (typeof onCancel === 'function') onCancel();
        closeDialog();
    });

    // 确认按钮
    confirmBtn.addEventListener('click', async () => {
        const newTitle = inputEl.value.trim();
        if (!newTitle) {
            // 空标题不允许提交
            inputEl.focus();
            return;
        }
        // 禁用按钮防止重复提交
        confirmBtn.disabled = true;
        cancelBtn.disabled = true;
        confirmBtn.textContent = '提交中…';

        try {
            const success = await onConfirm(newTitle);
            if (success) {
                closeDialog();
            } else {
                // 后端失败，恢复按钮
                confirmBtn.disabled = false;
                cancelBtn.disabled = false;
                confirmBtn.textContent = '确认';
            }
        } catch (e) {
            console.error('修改标题出错:', e);
            confirmBtn.disabled = false;
            cancelBtn.disabled = false;
            confirmBtn.textContent = '确认';
        }
    });

    // 点击遮罩层外部关闭
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) {
            if (typeof onCancel === 'function') onCancel();
            closeDialog();
        }
    });

    // 键盘支持：Enter 确认，Escape 取消
    inputEl.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            confirmBtn.click();
        } else if (e.key === 'Escape') {
            if (typeof onCancel === 'function') onCancel();
            closeDialog();
        }
    });
}
