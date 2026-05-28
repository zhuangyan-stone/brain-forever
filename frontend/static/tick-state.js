// ============================================================
// tick-state.js — 刻度导航模块级状态变量
// ============================================================
// 从原 chat-state.js 的 state 对象中迁移出来的模块级变量。
// 这些变量不适合放在 Alpine store 中，因为它们属于 UI 状态
// 或临时标志，不需要响应式绑定。
//
// 各模块通过 import { ... } from './tick-state.js' 引用。
// ============================================================

'use strict';

// ============================================================
// 常量
// ============================================================

/** 最多同时显示的刻度数 */
export const MAX_VISIBLE_TICKS = 9;

// ============================================================
// 刻度导航状态
// ============================================================

/** 当前活动刻度的索引，-1 表示无活动刻度 */
export let activeTickIndex = -1;

/** 刻度滚动偏移量（0 表示从第一条开始显示） */
export let tickScrollOffset = 0;

/** 用户点击刻度后的目标索引，用于平滑滚动到位后精确解锁面板 */
export let targetTickIndex = -1;

/** 待高亮消息索引 */
export let pendingHighlightIndex = -1;

/**
 * 设置 activeTickIndex
 * @param {number} val
 */
export function setActiveTickIndex(val) {
    activeTickIndex = val;
}

/**
 * 设置 tickScrollOffset
 * @param {number} val
 */
export function setTickScrollOffset(val) {
    tickScrollOffset = val;
}

/**
 * 设置 targetTickIndex
 * @param {number} val
 */
export function setTargetTickIndex(val) {
    targetTickIndex = val;
}

/**
 * 设置 pendingHighlightIndex
 * @param {number} val
 */
export function setPendingHighlightIndex(val) {
    pendingHighlightIndex = val;
}

/**
 * 重置所有刻度导航状态
 */
export function resetTickState() {
    activeTickIndex = -1;
    tickScrollOffset = 0;
    targetTickIndex = -1;
    pendingHighlightIndex = -1;
}

