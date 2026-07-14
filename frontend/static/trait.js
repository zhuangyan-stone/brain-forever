// ============================================================
// trait.js — 个人特征的自动触发业务逻辑
// ============================================================
//
// 职责：
//   1. 维护每个对话的流完成计数器
//   2. 达到阈值后调用 trait-api.js 的 fetchExtractTraits
//   3. 管理请求锁，防止重复触发
//
// 使用方式：
//   import { accumulateCompletion } from './trait.js';
//   accumulateCompletion(sn);  // 每次 SSE 流完成时调用
//
// 触发条件（可随时调整）：
//   - 特征提取：每累积 3 次流完成（约 3 轮对话）
//
// 归类（标签）使用另外的触发场景，不在此文件中处理。
// ============================================================

'use strict';

import { extractTraits } from './trait-api.js';

/**
 * 每个对话的流完成计数器（模块级，不会暴露到全局）。
 * key = chat SN, value = { count: number, _extracting: boolean }
 */
var _counters = {};

/**
 * 累加一次流完成计数。每次 SSE 流完成（onDone）时调用。
 * 达到阈值后自动触发特征提取。
 * @param {string} sn - 对话 SN
 */
export function accumulateCompletion(sn) {
    if (!sn) return;

    if (!_counters[sn]) {
        _counters[sn] = { count: 0, _extracting: false };
    }

    var c = _counters[sn];
    c.count++;

    // 累积 10 轮后触发特征提取
    if (c.count >= 10 && !c._extracting) {
        c._extracting = true;
        extractTraits(sn).then(function() {
            resetCounter(sn);
        });
    }
}

/**
 * 重置指定对话的计数器。
 * @param {string} sn
 */
export function resetCounter(sn) {
    if (!sn) return;
    _counters[sn] = { count: 0, _extracting: false };
}
