// ============================================================
// title-edit-dialog.js — 修改对话标题对话框
// ============================================================
// UI 层已迁移到 Alpine 组件 titleEditDialog（见 buttons.js + index.html），
// 使用 x-show 控制显隐、x-model 双向绑定输入、x-text 显示原标题。
// 本模块仅提供调用入口，Alpine 组件处理所有 DOM 和事件管理。
//
// 使用方式（不变）：
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
 * UI 由 Alpine 组件 titleEditDialog 渲染，本函数只负责传递数据。
 *
 * @param {object} options
 * @param {string} options.currentTitle - 当前对话标题
 * @param {(newTitle: string) => Promise<boolean>} options.onConfirm - 确认回调，返回是否成功
 * @param {() => void} [options.onCancel] - 取消回调（可选）
 */
export function showTitleEditDialog({ currentTitle, onConfirm, onCancel } = {}) {
    if (!currentTitle) return;

    const titleEditDialog = document.getElementById('titleEditDialog');
    if (!titleEditDialog) return;

    Alpine.$data(titleEditDialog).open({
        currentTitle: currentTitle,
        onConfirm: onConfirm,
        onCancel: onCancel,
    });
}
