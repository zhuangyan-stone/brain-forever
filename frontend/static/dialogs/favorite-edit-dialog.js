// ============================================================
// favorite-edit-dialog.js — 收藏夹编辑对话框调用入口
// ============================================================
// UI 层由 Alpine 组件 favoriteEditDialog 渲染（见 dialogs.js + index.html），
// 使用 x-show 控制显隐、x-model 双向绑定输入。
// 本模块仅提供调用入口，Alpine 组件处理所有 DOM 和事件管理。
//
// 使用方式：
//   import { showFavoriteEditDialog } from './dialogs/favorite-edit-dialog.js';
//   showFavoriteEditDialog({
//       existingTags: ['目录1', '目录2'],
//       onConfirm: async (customTag) => { /* 确认回调 */ },
//   });
// ============================================================

'use strict';

/**
 * 显示收藏夹编辑对话框。
 *
 * @param {object} options
 * @param {string[]} options.existingTags - 已有的收藏夹目录名列表
 * @param {string} [options.defaultTag] - 默认收藏夹目录名
 * @param {(customTag: string) => Promise<boolean>} options.onConfirm - 确认回调，返回是否成功
 */
export function showFavoriteEditDialog({ existingTags, defaultTag, onConfirm } = {}) {
    if (!Array.isArray(existingTags)) existingTags = [];

    const dialogEl = document.getElementById('favoriteEditDialog');
    if (!dialogEl) return;

    Alpine.$data(dialogEl).open({
        existingTags: existingTags,
        defaultTag: defaultTag || '',
        onConfirm: onConfirm,
    });
}
