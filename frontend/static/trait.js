// ============================================================
// trait.js — 个人特征的自动触发业务逻辑
// ============================================================
//
// 职责：
//   1. 维护每个对话的流完成计数器（永不归零，持续递增）
//   2. 达到阈值后调用 trait-api.js 的 fetchExtractTraits
//   3. 管理请求锁，防止重复触发
//   4. 通过 Alpine store 的 chat.inlineHint 字段显示提取进度
//
// 使用方式：
//   import { accumulateCompletion } from './trait.js';
//   accumulateCompletion(sn);  // 每次 SSE 流完成时调用
//
// 触发策略（四阶阶梯式，由 STAGES 表驱动）：
//   - 第一阶（initial） ：第  3 轮触发 1 次（相当于每 3 轮，但只落在第 3 轮）
//   - 第二阶（mid）     ：前 50 条中，每到 10 的整数倍触发（10,20,30,40,50 → 第 2 次距第 1 次仅隔 7 轮）
//   - 第三阶（late）    ：50~150 轮，每 20 轮触发（70,90,110,130,150）
//   - 第四阶（final）   ：超过 150 轮后，每 50 轮触发（200,250,300,...）
//
// 分类（标签）使用另外的触发场景，不在此文件中处理。
// ============================================================

'use strict';

import { extractTraits } from './trait-api.js';

/**
 * 四阶阶段定义表。
 *
 * 每个阶段定义：
 *   - until:   本阶段覆盖的 count 上限（含边界）
 *   - every:   本阶段的触发间隔
 *   - offset:  触发计算的偏移量，实际条件为 (count - offset) % every === 0
 *
 * 触发序列推导：
 *   阶段1: ( -∞, 3], offset=0,  every=3  → count=3
 *   阶段2: (3,  50], offset=0,  every=10 → count=10,20,30,40,50
 *   阶段3: (50, 150], offset=50, every=20 → count=70,90,110,130,150
 *   阶段4: (150,  ∞), offset=150, every=50 → count=200,250,300,...
 *
 * 注意：因遍历顺序优先，count=3 被阶段 1 截获，不会落入阶段 2。
 */
var STAGES = [
    { until: 3,   every: 3,  offset: 0   },
    { until: 50,  every: 10, offset: 0   },
    { until: 150, every: 20, offset: 50  },
    { until: Infinity, every: 50, offset: 150 },
];

/**
 * 判断给定 count 是否命中某个阶段的触发点。
 * @param {number} count
 * @returns {boolean}
 */
function isTriggerPoint(count) {
    for (var i = 0; i < STAGES.length; i++) {
        var s = STAGES[i];
        if (count <= s.until && (count - s.offset) % s.every === 0) {
            return true;
        }
    }
    return false;
}

/**
 * 每个对话的流完成计数器（模块级，不会暴露到全局）。
 * key = chat SN, value = { count: number, _extracting: boolean }
 *
 * count 永不归零，持续递增，由 isTriggerPoint() 判断触发时机。
 */
var _counters = {};

/**
 * 个人特征提取时的候选提示文字（随机取一条）。
 */
var TRAIT_HINT_TEXTS = [
    '📜 AI 意味深长地看你一眼，准备生成你的特征……',
    '📜 起居郎刷完短剧，百无聊赖，来为主人写点小报告吧……',
    '📜 皇上今日所云，颇有深度，不妨深挖一下……',
    '📜 特征提取器嗡嗡作响，正在分析你的对话……',
    '📜 AI饶有兴致地读完上述对话，嗯，来记点什么吧……',
    '📜 呀，皇上又说一大堆，起居郎要做起居注了……',
    '📜 深谙春秋笔法的起居郎正在磨墨……'
];

/**
 * 自动清除延时（秒）。done/fail 后等待此时间，然后清除 inlineHint。
 * 由业务方控制，可传不同值实现不同时长。
 */
var KEEP_SECONDS = 10;

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

    // 四阶阶梯式触发，由 STAGES 表驱动
    if (isTriggerPoint(c.count) && !c._extracting) {
        c._extracting = true;

        // 通过 Alpine store 设置 inlineHint，触发响应式渲染
        try {
            var chatsStore = window.Alpine.store('chats');
            var chat = chatsStore.getOrCreate(sn);
            var texts = TRAIT_HINT_TEXTS;
            var idx = Math.floor(Math.random() * texts.length);
            chat.inlineHint = { text: texts[idx], state: 'pending' };
        } catch(e) {
            // Alpine store 未就绪时静默跳过
        }

        extractTraits(sn).then(function(data) {
            if (!data) {
                // 失败：更新 inlineHint 为 fail 状态
                try {
                    var chatsStore = window.Alpine.store('chats');
                    var chat = chatsStore.getOrCreate(sn);
                    chat.inlineHint = { text: '提取个人特征失败', state: 'fail' };
                    setTimeout(function() {
                        if (chat.inlineHint && chat.inlineHint.state === 'fail') {
                            chat.inlineHint = null;
                        }
                    }, KEEP_SECONDS * 1000);
                } catch(e) {}
                clearExtractingLock(sn);
                return;
            }

            // 提取成功后更新侧边栏 chat 数据
            try {
                var chats = window.Alpine.store('chats');
                if (chats) {
                    // 更新 items[] 中的 chat 对象（侧边栏列表）
                    var items = chats.items;
                    if (items) {
                        for (var i = 0; i < items.length; i++) {
                            if (items[i].sn === sn) {
                                if (data.extracted_at) {
                                    items[i].extracted_at = data.extracted_at;
                                }
                                if (typeof data.extracted_count === 'number') {
                                    items[i].extracted_count = data.extracted_count;
                                }
                                break;
                            }
                        }
                    }
                    // 也更新 chats[]（部分场景使用）
                    var chatList = chats.chats;
                    if (chatList) {
                        for (var j = 0; j < chatList.length; j++) {
                            if (chatList[j].sn === sn) {
                                if (data.extracted_at) {
                                    chatList[j].extracted_at = data.extracted_at;
                                }
                                if (typeof data.extracted_count === 'number') {
                                    chatList[j].extracted_count = data.extracted_count;
                                }
                                break;
                            }
                        }
                    }
                }
            } catch(e) {
                // Alpine store 未就绪时静默跳过
            }

            var featureCount = data.extracted_count || 0;
            // 成功：更新 inlineHint 为 done 状态
            try {
                var chatsStore = window.Alpine.store('chats');
                var chat = chatsStore.getOrCreate(sn);
                var successText = '已生成' + featureCount + '条特征';
                chat.inlineHint = { text: successText, state: 'done' };
                setTimeout(function() {
                    if (chat.inlineHint && chat.inlineHint.state === 'done') {
                        chat.inlineHint = null;
                    }
                }, KEEP_SECONDS * 1000);
            } catch(e) {}

            clearExtractingLock(sn);
        });
    }
}

/**
 * 仅清除请求锁，不清零计数器（计数器持续递增用于下一次触发判断）。
 * @param {string} sn
 */
function clearExtractingLock(sn) {
    if (!sn) return;
    var c = _counters[sn];
    if (c) {
        c._extracting = false;
    }
}

