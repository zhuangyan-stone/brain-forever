// ============================================================
// chat-api.js — API 调用封装
// ============================================================

import { updateHeaderTitle } from './chat-ui.js';
import { resetTickState } from './tick-state.js';
import { showAiTitleSuggestion } from './components/sticky-ai-title.js';

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
 * 异步调用后端 /api/session/title 接口，让 AI 为指定对话生成标题。
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
 * @param {string} [sn] - 当前对话的 SN，传递给后端以便返回时确认目标 chat
 * @returns {Promise<void>}
 */
export async function fetchChatTitle(originalTitle, force = false, sn) {
    // 如果标题已被修改过（AI 修改或用户手动修改），不再请求
    // 状态只能从低往高变（0→1, 0→2, 1→2），>= 1 即表示已非原始状态
    // 但 force=true 时（用户手动点击按钮）忽略此守卫，强制请求 AI 重新生成
    var chats = window.Alpine.store('chats');
    var activeTitleState = (chats && chats.active) ? chats.active.titleState : 0;
    if (!force && activeTitleState >= TITLE_STATE.AI) {
        return;
    }

    // 脏对话（临时 SN，尚未被后端确认）不允许 AI 推荐标题
    if (chats && chats.isDirtyChat && chats.isDirtyChat()) {
        return;
    }

    try {
        // 构建 URL：携带 sn 参数（如果提供）
        let url = '/api/session/title?title=' + encodeURIComponent(originalTitle);
        if (sn) {
            url += '&sn=' + encodeURIComponent(sn);
        }
        const response = await fetch(url);
        if (!response.ok) return;
        const data = await response.json();

        // 从返回数据中提取目标 chat SN，用于后续精确定位
        const targetSN = data.sn || sn || null;

        if (data.title && data.changed === true) {
            // 异步调用期间用户可能已手动修改标题，再次检查防止覆盖
            // 但 force=true 时（用户手动点击按钮）忽略此守卫，强制显示推荐
            var chats2 = window.Alpine.store('chats');
            var currentTitleState = (chats2 && chats2.active) ? chats2.active.titleState : 0;
            if (!force && currentTitleState === TITLE_STATE.USER) {
                return;
            }

            // ---- 显示便利贴让用户选择 ----
            const stickyOptions = {
                onApply: async (newTitle) => {
                    // 严格依据返回的 sn 找到对应的 chat 来更新标题
                    // 即使 chat 已被删除（不存在于 currentChats），前端也能正确处理
                    const { updateChatTitleBySN } = await import('./chat-list.js');
                    
                    // 先调用后端 PUT 保存标题（携带 sn 确保更新到正确的 chat）
                    const success = await putChatTitle(newTitle, TITLE_STATE.AI, targetSN);
                    if (!success) return;

                    // 如果 targetSN 匹配当前活跃 chat，更新 header 标题
                    var chats3 = window.Alpine.store('chats');
                    var activeSN = (chats3 && chats3.active) ? chats3.active.sn : null;
                    if (activeSN === targetSN) {
                        updateHeaderTitle(newTitle);
                        if (chats3 && chats3.active) {
                            chats3.active.titleState = TITLE_STATE.AI;
                        }
                    }

                    // 尝试在侧边栏中找到该 chat 并更新标题（如果 chat 已被删除则跳过）
                    updateChatTitleBySN(targetSN, newTitle);
                },
                onDismiss: () => {
                    // 用户取消：不做任何修改，状态保持当前值
                    // 下一轮仍可继续尝试推荐
                },
            };
            showAiTitleSuggestion('AI 推荐标题', data.title, stickyOptions);
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
 * @param {string} [sn] - 可选，指定要更新的 chat SN（不传则更新当前活跃 chat）
 * @returns {Promise<boolean>} 是否成功
 */
export async function putChatTitle(title, titleState = TITLE_STATE.USER, sn) {
	if (!title) return false;
	try {
		let url = '/api/session/title?title=' + encodeURIComponent(title) +
			'&state=' + encodeURIComponent(titleState);
		if (sn) {
			url += '&sn=' + encodeURIComponent(sn);
		}
		const response = await fetch(url, {
			method: 'PUT',
		});
		if (!response.ok) {
			console.warn('更新标题失败:', response.status);
			return false;
		}
		// 不携带 sn 时更新的是当前活跃 chat，更新 Alpine store
		// 携带 sn 时由调用方自行处理本地状态
		if (!sn) {
			var chats4 = window.Alpine.store('chats');
			if (chats4 && chats4.active) {
				chats4.active.titleState = titleState;
			}
		}
		return true;
	} catch (e) {
		console.warn('更新标题出错:', e);
		return false;
	}
}

/**
 * createBlankChat 调用后端 PUT /api/chat/new 接口，将后端 currentChat 重置为 blank chat。
 * blank chat 无 SN、无 DB 记录、不在 session.chats[] 中。
 * SN 将在第一条消息发送时由后端的 ensureDBSession 生成。
 * @returns {Promise<{sn: string, title: string, title_state: number}|null>}
 */
export async function createBlankChat() {
    try {
        const response = await fetch('/api/chat/new', { method: 'PUT' });
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
			// 持久化 user_no 和 avatar 到 localStorage，用于页面刷新后恢复登录状态
			localStorage.setItem('brainforever_user_no', userNo);
			if (data.avatar) {
				localStorage.setItem('brainforever_user_avatar', data.avatar);
			}
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
	* 清空当前对话历史，重置标题，然后用后端返回的 chats 数据刷新侧边栏。
	* 登录后自动恢复当前会话（包含合并的匿名聊天历史）。
	* @param {object} data - 后端返回的登录响应数据 { user_no, avatar?, chats? }
	*/
export async function switchToUser(data) {
	// 清空消息状态
	resetTickState();
	try {
		var chats = window.Alpine.store('chats');
		if (chats) {
			// ★ 必须使用 resetToBlank() 而非 reset()：
			//   reset() 会将 blankItem 设为 null，
			//   导致 prepareChat() 中的 promoteBlankItem() 不执行，
			//   进而 chats.active 为 null，消息添加和流式操作全部静默失败。
			//   详见 alpine-store.js resetToBlank 注释。
			chats.resetToBlank();
		}
	} catch(e) {}

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
	const { clearAllStickyNotes } = await import('./components/sticky-mgr.js');
	clearAllStickyNotes();

	// 更新标题
	updateHeaderTitle('');

	// ★ 修复：延迟设置 currentUserNo/currentUserAvatar，避免浏览器将 mousedown→mouseup
	//   的 click 事件重定向到新出现的头像按钮上。
	//   场景还原：
	//     1. 鼠标按下（mousedown）在登录按钮上
	//     2. 登录异步完成，switchToUser 执行到这里
	//     3. 若在此处同步设置 currentUserNo，Alpine 立即移除登录按钮、显示头像
	//     4. 此时鼠标仍在同一位置，但登录按钮已消失，头像按钮取而代之
	//     5. 鼠标抬起（mouseup）时，浏览器将合成 click 重定向到头像按钮
	//     6. @click="open = !open" 触发，open 变为 true，下拉菜单自动展开
	//   使用 setTimeout(0) 推迟到当前宏任务完成、所有 mouse 事件处理完毕后再更新 DOM。
	setTimeout(function() {
		try {
			var chats = window.Alpine.store('chats');
			if (chats) {
				chats.currentUserNo = data.user_no || '';
				chats.currentUserAvatar = data.avatar || '';
			}
		} catch(e) {}
	}, 0);

	// 刷新侧边栏对话列表 — 使用后端返回的 chats 数据替换旧的匿名对话列表
	// 通过 Alpine store 上的 setSidebarChats 方法（由 chat-list.js 注册），
	// 避免动态导入 chat-list.js 产生循环依赖。
	if (data.chats) {
		try {
			var chatsStore = window.Alpine.store('chats');
			if (chatsStore && chatsStore.setSidebarChats) {
				chatsStore.setSidebarChats(data.chats, null);
			}
		} catch(e) {
			console.warn('switchToUser: 刷新侧边栏失败', e);
		}
	}

	// ★ 恢复欢迎状态：switchToUser 执行过程中，chats.resetToBlank() 将 activeIndex 设为 -1，
	//   但 welcome-message DOM 元素已被移除，Alpine 的 x-show 无法重新创建已销毁的元素。
	//   同时 input-area 也被移回了原位（mainBody 下方），导致输入面板掉到底部。
	//   需要调用 showWelcomeMessage() 重新创建 welcome-message 并将 input-area 移入其中，
	//   使欢迎页面恢复垂直居中的正确状态。
	const { showWelcomeMessage } = await import('./chat-ui.js');
	showWelcomeMessage();

	// 聚焦输入框
	const msgInput = document.getElementById('messageInput');
	if (msgInput) {
		msgInput.focus();
	}
}

/**
 * onChatLogout 调用后端 POST /api/chat/logout 接口，退出登录回到匿名状态。
 * 成功后清除 localStorage 中的 user_no，然后重新获取匿名用户的对话列表
 * 并刷新侧边栏，不刷新页面。
 * @returns {Promise<boolean>} 是否成功
 */
export async function onChatLogout() {
	try {
		const response = await fetch('/api/chat/logout', {
			method: 'POST',
		});
		if (!response.ok) {
			console.warn('退出登录失败:', response.status);
			return false;
		}
		const data = await response.json();
		if (data.status === 'ok') {
			// 清除持久化的 user_no 和 avatar
			localStorage.removeItem('brainforever_user_no');
			localStorage.removeItem('brainforever_user_avatar');

			// 重置前端状态（清空消息、侧边栏等）
			resetTickState();
			try {
				var chatsStore = window.Alpine.store('chats');
				if (chatsStore) {
					// ★ 必须使用 resetToBlank() 而非 reset()，
					//    原因同上 switchToUser() 注释。
					chatsStore.resetToBlank();
					chatsStore.currentUserNo = '';
					chatsStore.currentUserAvatar = '';
				}
			} catch(e) {}

			// 移除消息 DOM
			const chatContainer = document.getElementById('chatContainer');
			if (chatContainer) {
				chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
			}

			// 获取匿名用户的对话列表
			const listResp = await fetch('/api/chat/list');
			if (listResp.ok) {
				const listData = await listResp.json();
				if (listData.chats) {
					try {
						var chatsStore2 = window.Alpine.store('chats');
						if (chatsStore2 && chatsStore2.setSidebarChats) {
							chatsStore2.setSidebarChats(listData.chats, null);
						}
					} catch(e) {
						console.warn('onChatLogout: 刷新侧边栏失败', e);
					}
				}
			}

			// 更新标题、输入框聚焦
			const { updateHeaderTitle, showWelcomeMessage } = await import('./chat-ui.js');
			updateHeaderTitle('');
			showWelcomeMessage();

			const msgInput = document.getElementById('messageInput');
			if (msgInput) msgInput.focus();

			return true;
		}
		return false;
	} catch (e) {
		console.warn('退出登录出错:', e);
		return false;
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

// ============================================================
// 注册方法到 Alpine store + window — 供 HTML 模板中 @click 表达式调用
// chat-api.js 是 ES Module，export 的函数不会进入全局作用域。
// 但 Alpine 模板 @click="onChatLogout" 需要在 Alpine 的表达式作用域
// （Alpine store 或 window）中找到该函数。
// 因此同时注册到 Alpine store（供 $store.chats.onChatLogout 调用）
// 和 window（供裸名 onChatLogout 调用）。
// ============================================================
try {
    if (typeof onChatLogout === 'function') {
        window.onChatLogout = onChatLogout;
    }
    if (typeof onChatLogin === 'function') {
        window.onChatLogin = onChatLogin;
    }
    var chatsApi = window.Alpine.store('chats');
    if (chatsApi) {
        chatsApi.onChatLogin = onChatLogin;
        chatsApi.onChatLogout = onChatLogout;
    }
} catch(e) {
    console.warn('chat-api: 注册到 window/Alpine store 失败', e);
}
