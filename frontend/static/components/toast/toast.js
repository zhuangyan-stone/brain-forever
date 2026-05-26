// ============================================================
// components/toast/toast.js — Alpine.js Toast 组件（参考实现）
// ============================================================
//
// ⚠️ 重要说明（Alpine 初始化时序）：
//
//   最终架构：Toast 数据由 Alpine.store('ui') 管理，
//   该 store 在 index.html 中通过 alpine:init 事件注册（位于 Alpine.js
//   脚本之前），确保 Alpine 处理 DOM 时 store 已就绪。
//
//   本文件保留 toastManager() 工厂函数作为参考实现和文档，
//   实际运行时由 Alpine.store('ui').showToast() 提供相同功能。
//
//   触发方式：
//     showToast(message, type, duration)  // 定义在 chat-ui.js
//     → Alpine.store('ui').showToast(...)  // 操作响应式数据
//     → x-for / x-show 自动更新 DOM
//
// ============================================================

'use strict';

/**
 * Alpine.js 组件数据工厂：Toast 管理器
 *
 * 此函数是 Alpine.store('ui') 中 showToast 方法的参考实现。
 * 实际运行时由 index.html 中通过 alpine:init 注册的 store 提供。
 *
 * @returns {{ toasts: Array, _nextId: number, addToast: Function }}
 */
export function toastManager() {
    return {
        toasts: [],
        _nextId: 0,

        addToast({ message, type = 'error', duration = 4000 }) {
            const id = ++this._nextId;
            const toast = { id, message, type, visible: false };
            this.toasts.push(toast);
            this.$nextTick(() => { toast.visible = true; });
            setTimeout(() => {
                toast.visible = false;
                setTimeout(() => {
                    this.toasts = this.toasts.filter(t => t.id !== id);
                }, 300);
            }, duration);
        },
    };
}
