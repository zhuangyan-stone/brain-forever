// ============================================================
// chat-api.js — API 调用封装
// ============================================================

import { state } from './chat-state.js';
import { updateHeaderTitle } from './chat-ui.js';

/**
 * 标题修改状态常量
 */
export const TITLE_STATE = {
    ORIGINAL: 0,  // 原始标题
    AI: 1,        // AI 修改
    USER: 2,      // 用户手动修改
};

/**
 * truncateTitle 截取字符串作为对话标题。
 * 与后端 internal/agent/on_session.go 中的 truncateTitle 逻辑一致：
 *   - 折叠空白字符（换行、制表符、空格）为单个空格
 *   - 限制最多 50 个字符，超长加 "…"
 *
 * @param {string} s - 原始字符串
 * @returns {string} 截取后的标题
 */
export function truncateTitle(s) {
    if (!s) return '';
    // 折叠空白字符
    let result = '';
    let space = false;
    for (const ch of s) {
        if (ch === '\n' || ch === '\r' || ch === '\t' || ch === ' ') {
            if (!space) {
                result += ' ';
                space = true;
            }
        } else {
            result += ch;
            space = false;
        }
    }
    // 去除首尾空格
    result = result.trim();
    // 限制最多 50 个字符
    if (result.length > 50) {
        return result.slice(0, 50) + '…';
    }
    return result;
}

/**
 * 异步调用后端 /api/session/title 接口，让 AI 为当前对话生成标题。
 *
 * 调用条件：
 *   - titleState !== 2（用户手动修改过的标题不再覆盖）
 *   - 对话未超过 3 轮（一轮指 AI 的一次成功回复）
 *
 * 调用结果：
 *   - 成功且标题改变 → titleState 改为 1（AI 修改）
 *   - 失败或标题未变 → titleState 保持当前值，下一轮继续尝试
 *
 * @param {string} originalTitle - 原标题（用户第一条消息的截取）
 * @returns {Promise<void>}
 */
export async function fetchSessionTitle(originalTitle) {
    // 如果标题已被用户手动修改过（titleState === 2），不再覆盖
    if (state.titleState === TITLE_STATE.USER) {
        return;
    }

    try {
        const response = await fetch('/api/session/title?title=' + encodeURIComponent(originalTitle));
        if (!response.ok) return;
        const data = await response.json();

        if (data.title && data.changed === true) {
            // 异步调用期间用户可能已手动修改标题，再次检查防止覆盖
            if (state.titleState === TITLE_STATE.USER) {
                return;
            }
            // AI 成功生成了新标题，更新标题和状态
            updateHeaderTitle(data.title);
            // 状态从 0 → 1（AI 修改）
            state.titleState = TITLE_STATE.AI;
        }
        // 如果 changed === false（标题未变或出错），状态保持当前值，下一轮继续尝试
    } catch (e) {
        // 静默失败，不干扰用户；状态保持当前值，下一轮继续尝试
        console.warn('获取对话标题失败:', e);
    }
}
