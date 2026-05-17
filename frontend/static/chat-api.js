// ============================================================
// chat-api.js — API 调用封装
// ============================================================

import { state } from './chat-state.js';
import { updateHeaderTitle } from './chat-ui.js';
import { showStickyNote } from './components/sticky-note.js';

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
 * @param {boolean} [force=false] - 是否强制请求（忽略 titleState 守卫条件），用于用户手动点击 AI 标题按钮
 * @returns {Promise<void>}
 */
export async function fetchSessionTitle(originalTitle, force = false) {
    // 如果标题已被修改过（AI 修改或用户手动修改），不再请求
    // 状态只能从低往高变（0→1, 0→2, 1→2），>= 1 即表示已非原始状态
    // 但 force=true 时（用户手动点击按钮）忽略此守卫，强制请求 AI 重新生成
    if (!force && state.titleState >= TITLE_STATE.AI) {
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

            // ---- 显示便利贴让用户选择 ----
            showStickyNote(
                'AI 推荐标题',
                data.title,
                {
                    onApply: async (newTitle) => {
                        // 定时到点或用户点击"试试"：更新标题，标记为 AI 修改
                        updateHeaderTitle(newTitle);
                        state.titleState = TITLE_STATE.AI;
                        // 同步到后端：用户已确认接受，调用 PUT 保存，标记为机改
                        await putSessionTitle(newTitle, TITLE_STATE.AI);
                    },
                    onDismiss: (newTitle) => {
                        // 用户取消：不做任何修改，状态保持当前值
                        // 下一轮仍可继续尝试推荐
                    },
                    onReject: async (newTitle) => {
                        // 用户点击"无需推荐"：将标题状态标记为用户修改（2），
                        // 这样后续 fetchSessionTitle 检测到 state.titleState >= TITLE_STATE.AI
                        // 就不会再请求推荐标题了
                        state.titleState = TITLE_STATE.USER;
                        // 同步到后端：用原标题 PUT 保存，标记为用户修改
                        // 这样后端也会记住这个状态，刷新页面后不再推荐
                        const currentTitle = document.querySelector('.header-title')?.textContent || newTitle;
                        await putSessionTitle(currentTitle, TITLE_STATE.USER);
                    },
                }
            );
        }
        // 如果 changed === false（标题未变或出错），状态保持当前值，下一轮继续尝试
    } catch (e) {
        // 静默失败，不干扰用户；状态保持当前值，下一轮继续尝试
        console.warn('获取对话标题失败:', e);
    }
}

/**
 * putSessionTitle 向后端发送 PUT 请求更新 session 标题。
 * 成功后标记 titleState 为指定状态，并更新本地标题。
 * @param {string} title - 新标题
 * @param {number} [titleState=TITLE_STATE.USER] - 标题修改状态，默认用户修改（2）
 * @returns {Promise<boolean>} 是否成功
 */
export async function putSessionTitle(title, titleState = TITLE_STATE.USER) {
	if (!title) return false;
	try {
		const url = '/api/session/title?title=' + encodeURIComponent(title) +
			'&state=' + encodeURIComponent(titleState);
		const response = await fetch(url, {
			method: 'PUT',
		});
		if (!response.ok) {
			console.warn('更新标题失败:', response.status);
			return false;
		}
		// 成功后标记为指定状态
		state.titleState = titleState;
		return true;
	} catch (e) {
		console.warn('更新标题出错:', e);
		return false;
	}
}
