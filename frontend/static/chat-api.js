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
export async function fetchChatTitle(originalTitle, force = false) {
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
            // 但 force=true 时（用户手动点击按钮）忽略此守卫，强制显示推荐
            if (!force && state.titleState === TITLE_STATE.USER) {
                return;
            }

            // ---- 显示便利贴让用户选择 ----
            const stickyOptions = {
                onApply: async (newTitle) => {
                    // 定时到点或用户点击"试试"：更新标题，标记为 AI 修改
                    updateHeaderTitle(newTitle);
                    state.titleState = TITLE_STATE.AI;
                    // 同步到后端：用户已确认接受，调用 PUT 保存，标记为机改
                    await putChatTitle(newTitle, TITLE_STATE.AI);
                },
                onDismiss: (newTitle) => {
                    // 用户取消：不做任何修改，状态保持当前值
                    // 下一轮仍可继续尝试推荐
                },
            };
            showStickyNote('AI 推荐标题', data.title, stickyOptions);
        }
        // 如果 changed === false（标题未变或出错），状态保持当前值，下一轮继续尝试
    } catch (e) {
        // 静默失败，不干扰用户；状态保持当前值，下一轮继续尝试
        console.warn('获取对话标题失败:', e);
    }
}

/**
 * putChatTitle 向后端发送 PUT 请求更新 chat 标题。
 * 成功后标记 titleState 为指定状态，并更新本地标题。
 * @param {string} title - 新标题
 * @param {number} [titleState=TITLE_STATE.USER] - 标题修改状态，默认用户修改（2）
 * @returns {Promise<boolean>} 是否成功
 */
export async function putChatTitle(title, titleState = TITLE_STATE.USER) {
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

/**
 * newChat 调用后端 POST /api/chat/new 接口，为当前新对话初始化一个 SN。
 * 仅当当前对话尚未初始化（sn 为空）时调用。后端会创建 DB 记录（登录用户）
 * 或直接返回（匿名用户），并返回当前对话的 SN。
 * @returns {Promise<{sn: string, title: string, title_state: number}|null>}
 */
export async function newChat() {
    try {
        const response = await fetch('/api/chat/new', { method: 'POST' });
        if (!response.ok) {
            console.warn('初始化对话失败:', response.status);
            return null;
        }
        return await response.json();
    } catch (e) {
        console.warn('初始化对话出错:', e);
        return null;
    }
}

/**
	* onChatLogin 调用后端 POST /api/chat/login 接口，切换当前会话到登录用户。
	* 登录成功后调用 switchToUser 加载用户的对话列表，
	* 并将 user_no 持久化到 localStorage 以在页面刷新后恢复。
	* @param {string} userNo - 全局唯一用户系列号
	* @returns {Promise<boolean>} 是否成功
	*/
export async function onChatLogin(userNo) {
	if (!userNo) return false;
	try {
		const response = await fetch('/api/chat/login', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ user_no: userNo }),
		});
		if (!response.ok) {
			console.warn('登录失败:', response.status);
			return false;
		}
		const data = await response.json();
		if (data.status === 'ok') {
			// 持久化 user_no 到 localStorage，用于页面刷新后恢复登录状态
			localStorage.setItem('brainforever_user_no', userNo);
			await switchToUser(data);
			return true;
		}
		return false;
	} catch (e) {
		console.warn('登录出错:', e);
		return false;
	}
}

/**
	* switchToUser 切换前端状态到指定用户。
	* 清空当前对话历史，重置标题，然后加载用户的对话列表。
	* 登录后自动恢复当前会话（包含合并的匿名聊天历史）。
	* @param {object} data - 后端返回的登录响应数据 { user_no, sessions }
	*/
export async function switchToUser(data) {
	// 清空消息状态
	state.messages = [];
	state.userMsgCount = 0;
	state.activeTickIndex = -1;
	state.tickScrollOffset = 0;
	state.accumulatedMarkdown = '';
	if (state.renderTimer) {
		clearTimeout(state.renderTimer);
		state.renderTimer = null;
	}

	// 移除所有消息 DOM 节点
	const chatContainer = document.getElementById('chatContainer');
	if (chatContainer) {
		chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
	}

	// 移除已有的欢迎消息
	const existingWelcome = document.querySelector('.welcome-message');
	if (existingWelcome) {
		const inputArea = existingWelcome.querySelector('.input-area');
		if (inputArea) {
			const mainBody = document.getElementById('mainBody');
			if (mainBody && mainBody.nextElementSibling?.classList?.contains('input-area')) {
				// input-area 已经在正确位置
			} else if (mainBody) {
				mainBody.parentNode.insertBefore(inputArea, mainBody.nextSibling);
			}
		}
		existingWelcome.remove();
	}

	// 清空刻度导航
	const tickNav = document.getElementById('tickNav');
	if (tickNav) {
		tickNav.innerHTML = '';
	}

	// 移除 welcome-state 标记
	const scrollContainer = document.getElementById('scrollContainer');
	if (scrollContainer) {
		scrollContainer.classList.remove('welcome-state');
	}

	// 清除所有便利贴
	const { clearAllStickyNotes } = await import('./components/sticky-note.js');
	clearAllStickyNotes();

	// 更新标题
	updateHeaderTitle('');

	// 更新登录按钮文本
	const loginBtn = document.getElementById('loginBtn');
	if (loginBtn) {
		loginBtn.textContent = `用户: ${data.user_no}`;
	}

	// 渲染对话列表
	const { renderChatList } = await import('./chat-list.js');
	renderChatList(data.chats || [], data.current_chat_sn || null);

	// 恢复合并后的对话（后端 switchToUser 已将匿名聊天持久化并设置为当前对话）
	// 调用 GET /api/session 恢复匿名聊天的历史消息，实现无缝过渡
	const { restoreChat } = await import('./chat-restore.js');
	await restoreChat();

	// 聚焦输入框
	const msgInput = document.getElementById('messageInput');
	if (msgInput) {
		msgInput.focus();
	}
}

/**
	* switchChat 调用后端切换当前对话到指定历史对话，并返回其消息列表。
	* @param {string} sn - 目标会话的 SN
	* @returns {Promise<{messages: Array, title: string, title_state: number}|null>}
	*/
export async function switchChat(sn) {
	if (!sn) return null;
	try {
		const response = await fetch('/api/chat/switch?sn=' + encodeURIComponent(sn));
		if (!response.ok) {
			console.warn('切换会话失败:', response.status);
			return null;
		}
		const data = await response.json();
		if (data.status === 'ok') {
			return {
				messages: data.messages || [],
				title: data.title || '',
				title_state: data.title_state ?? 0,
			};
		}
		return null;
	} catch (e) {
		console.warn('切换会话出错:', e);
		return null;
	}
}
