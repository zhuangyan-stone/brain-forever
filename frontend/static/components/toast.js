// ============================================================
// components/toast.js — Alpine.js Toast 组件（参考实现）
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

