// ============================================================
// tick-state.js — 刻度导航模块级状态变量
// ============================================================
// 同时同步到 Alpine.store('tickNav')，供 buttons.js 等
// 非 ES Module 代码读取当前刻度状态。
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
 * 将当前状态同步到 Alpine store
 */
function syncToAlpine() {
    try {
        var store = window.Alpine.store('tickNav');
        if (store) {
            store.activeTickIndex = activeTickIndex;
            store.tickScrollOffset = tickScrollOffset;
            store.targetTickIndex = targetTickIndex;
            store.pendingHighlightIndex = pendingHighlightIndex;
        }
    } catch(e) {
        // Alpine 尚未初始化，忽略
    }
}

/**
 * 设置 activeTickIndex
 * @param {number} val
 */
export function setActiveTickIndex(val) {
    activeTickIndex = val;
    syncToAlpine();
}

/**
 * 设置 tickScrollOffset
 * @param {number} val
 */
export function setTickScrollOffset(val) {
    tickScrollOffset = val;
    syncToAlpine();
}

/**
 * 设置 targetTickIndex
 * @param {number} val
 */
export function setTargetTickIndex(val) {
    targetTickIndex = val;
    syncToAlpine();
}

/**
 * 设置 pendingHighlightIndex
 * @param {number} val
 */
export function setPendingHighlightIndex(val) {
    pendingHighlightIndex = val;
    syncToAlpine();
}

/**
 * 重置所有刻度导航状态
 */
export function resetTickState() {
    activeTickIndex = -1;
    tickScrollOffset = 0;
    targetTickIndex = -1;
    pendingHighlightIndex = -1;
    syncToAlpine();
}
