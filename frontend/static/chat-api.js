// ============================================================
// chat-api.js — API 调用封装
// ============================================================

import { updateHeaderTitle, showToast } from './chat-ui.js';
import { resetTickState } from './tick-state.js';
import { showAiTitleSuggestion } from './components/sticky-ai-title.js';
import { visualLength, truncateByVisualLength } from './toolsets.js';

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
 * 保留原始逻辑：
 *   - 折叠空白字符（换行、制表符、空格）为单个空格
 *   - 限制最多 50 视觉长度，中文汉字算 1.5，超长加 "…"
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
    // 限制最多 50 视觉长度（中文汉字算 1.5）
    if (visualLength(result) > 50) {
        return truncateByVisualLength(result, 50);
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
    //
    // ★ 修复：通过 sn 参数定位目标对话的 titleState，而非依赖 chats.active。
    //   用户在流式过程中可能已切换到其他对话，此时 chats.active 指向错误的对象。
    var chats = window.Alpine.store('chats');
    var activeTitleState = 0;
    if (sn && chats) {
        var targetChat = chats.getOrCreate(sn);
        if (targetChat) {
            activeTitleState = targetChat.titleState || 0;
        }
    } else if (chats && chats.active) {
        activeTitleState = chats.active.titleState || 0;
    }
    if (!force && activeTitleState >= TITLE_STATE.AI) {
        return;
    }

    // 脏对话（临时 SN，尚未被后端确认）不允许 AI 推荐标题
    // ★ 使用 sn 定位目标对话检查脏状态，而非依赖 chats.active
    if (chats && chats.isDirtyChat) {
        var dirtyCheck = sn ? chats.getOrCreate(sn) : undefined;
        if (chats.isDirtyChat(dirtyCheck)) {
            return;
        }
    }

    try {
        // 构建 URL：携带 sn 参数（如果提供）
        let url = '/api/chat/title?title=' + encodeURIComponent(originalTitle);
        if (sn) {
            url += '&sn=' + encodeURIComponent(sn);
        }
        const response = await fetch(url);
        if (!response.ok) {
            const t = await response.text();
            showToast('获取标题失败：' + t, 'error');
            return;
        }
        const data = await response.json();

        // 从返回数据中提取目标 chat SN，用于后续精确定位
        const targetSN = data.sn || sn || null;

        if (data.changed === false) {
            showToast('抱歉，AI 想不出新的标题了', 'info', 4000);
            return;
        }

        if (data.title && data.changed === true) {
            // ★ 边缘情况：异步调用期间用户可能已删除该对话
            //   如果 targetSN 在 chats.items（或 chats.chats）中已不存在，
            //   说明对话已被删除，直接跳过不再显示标题建议。
            if (targetSN && chats) {
                var existsInItems = chats.items && chats.items.some(function(c) { return c.sn === targetSN; });
                var existsInChats = chats.chats && chats.chats.some(function(c) { return c.sn === targetSN; });
                if (!existsInItems && !existsInChats) {
                    return; // 对话已被删除，不再显示标题建议
                }
            }

            // 异步调用期间用户可能已手动修改标题，再次检查防止覆盖
            // 但 force=true 时（用户手动点击按钮）忽略此守卫，强制显示推荐
            //
            // ★ 使用 targetSN 定位目标对话检查 titleState，而非依赖 chats.active
            // ★ chats 已在外层获取，直接复用
            var currentTitleState = 0;
            if (targetSN && chats) {
                var targetChat2 = chats.getOrCreate(targetSN);
                if (targetChat2) {
                    currentTitleState = targetChat2.titleState || 0;
                }
            } else if (chats && chats.active) {
                currentTitleState = chats.active.titleState || 0;
            }
            if (!force && currentTitleState === TITLE_STATE.USER) {
                return;
            }

            // ★ AI 建议新标题时，同步触发自动归类（静默）
            //   仅自动触发时（force=false，场景1：第一轮 AI 建议）提前归类
            //   用户手动点击 AI 标题按钮（force=true，场景2）则等 putChatTitle 确认后才归类
            if (!force && targetSN) {
                fetchChatTagsAuto(targetSN);
            }

            // ---- 显示便利贴让用户选择 ----
            const stickyOptions = {
                sn: targetSN,  // 携带 SN 供便利贴组件在删除时清理自身
                onApply: async (newTitle) => {
                    // 严格依据返回的 sn 找到对应的 chat 来更新标题
                    // 即使 chat 已被删除（不存在于 store.chats），前端也能正确处理
                    const { updateChatTitleBySN } = await import('./chat-list.js');
                    
                    // 先调用后端 PUT 保存标题（携带 sn 确保更新到正确的 chat）
                    const success = await putChatTitle(newTitle, TITLE_STATE.AI, targetSN);
                    if (!success) return;

                    // 如果 targetSN 匹配当前活跃 chat，更新 header 标题
                    // ★ chats 已在外层获取，通过闭包直接复用
                    var activeSN = (chats && chats.active) ? chats.active.sn : null;
                    if (activeSN === targetSN) {
                        updateHeaderTitle(newTitle);
                        if (chats && chats.active) {
                            chats.active.titleState = TITLE_STATE.AI;
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
 * putChatTitle 向后端发送 PUT 请求更新指定 chat 标题。
 * @param {string} title - 新标题
 * @param {number} [titleState=TITLE_STATE.USER] - 标题修改状态，默认用户修改（2）
 * @param {string} sn - 必传，指定要更新的 chat SN
 * @returns {Promise<boolean>} 是否成功
 */
export async function putChatTitle(title, titleState = TITLE_STATE.USER, sn) {
	if (!title || !sn) return false;

	// ★ 本地先检查该 chat 是否还存在（可能已被用户删除），避免无效的 API 调用
	var chats = window.Alpine.store('chats');
	if (chats) {
		var existsInItems = chats.items && chats.items.some(function(c) { return c.sn === sn; });
		var existsInChats = chats.chats && chats.chats.some(function(c) { return c.sn === sn; });
		if (!existsInItems && !existsInChats) {
			return false; // 对话已被删除，跳过 API 调用
		}
	}

	try {
		// ★ 在调用 API 前先读取当前旧标题，用于后续判断标题是否真正变化
		var chats = window.Alpine.store('chats');
		var oldTitle = '';
		if (chats && chats.active && chats.active.sn === sn) {
			oldTitle = chats.active.title || '';
		} else if (chats && chats.chats) {
			var found = chats.chats.find(function(c) { return c.sn === sn; });
			if (found) oldTitle = found.title || '';
		}

		const url = '/api/chat/title?title=' + encodeURIComponent(title) +
			'&state=' + encodeURIComponent(titleState) +
			'&sn=' + encodeURIComponent(sn);
		const response = await fetch(url, {
			method: 'PUT',
		});
		if (!response.ok) {
			const t = await response.text();
			showToast('更新标题失败：' + t, 'error');
			return false;
		}
		// 如果更新的就是当前活跃 chat，同步更新本地 title 和 titleState
		if (chats && chats.active && chats.active.sn === sn) {
			chats.active.title = title;
			chats.active.titleState = titleState;
		}

		// ★ 标题真正发生变化时才触发自动归类（静默）
		// 除第一轮外，确保仅在前、后标题不一致时发起 re-tag
		if (oldTitle !== title) {
			fetchChatTagsAuto(sn);
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
            const t = await response.text();
            showToast('初始化对话失败：' + t, 'error');
            return null;
        }
        return await response.json();
    } catch (e) {
        console.warn('初始化对话出错:', e);
        return null;
    }
}

/**
	* onChatLogin 调用后端 POST /api/user/login 接口，切换当前会话到登录用户。
	* 登录成功后调用 switchToUser 加载用户的对话列表，
	* 并将 user_no 持久化到 localStorage 以在页面刷新后恢复。
	* @param {string} userNo - 全局唯一用户系列号
	* @returns {Promise<boolean>} 是否成功
	*/
export async function onChatLogin(userNo) {
	if (!userNo) return false;
	// API 调用委托给 alpine-api.js 中的 window.fetchLogin
	const data = typeof window.fetchLogin === 'function'
		? await window.fetchLogin(userNo)
		: null;
	if (!data) return false;
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
			// ★ 通过闭包直接复用外层 chats，无需重新获取
			if (chats) {
				chats.currentUserNo = data.user_no || '';
				chats.currentUserNickname = data.nickname || '';
				chats.currentUserAvatar = data.avatar || '';
			}
		} catch(e) {}
	}, 0);

	// 刷新侧边栏对话列表 — 使用后端返回的 chats 数据替换旧的匿名对话列表
	// 通过 Alpine store 上的 setSidebarChats 方法（由 chat-list.js 注册），
	// 避免动态导入 chat-list.js 产生循环依赖。
	// ★ 必须处理 data.chats 为 null/undefined 的情况：
	//   - 后端 Go nil slice 序列化为 JSON null
	//   - 如果只用 if (data.chats)，null 为 falsy，侧边栏不会被清除
	//   统一转为 [] 确保侧边栏被正确清空。
try {
// ★ 复用上方已获取的 chats（同一函数内），无需重新获取
if (chats && chats.setSidebarChats) {
chats.setSidebarChats(data.chats || [], null);
}
} catch(e) {
console.warn('switchToUser: 刷新侧边栏失败', e);
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
 * onChatLogout 调用后端 POST /api/user/logout 接口退出登录，
 * 清除 localStorage 中的用户信息，然后跳转到登录页。
 * @returns {Promise<boolean>} 是否成功
 */
export async function onChatLogout() {
	// API 调用委托给 alpine-api.js 中的 window.fetchLogout
	const data = typeof window.fetchLogout === 'function'
		? await window.fetchLogout()
		: null;
	if (!data) return false;
	if (data.status === 'ok') {
		// 清除持久化的 user_no 和 avatar
		localStorage.removeItem('brainforever_user_no');
		localStorage.removeItem('brainforever_user_avatar');

		// 重置前端状态
		resetTickState();
		try {
			var chats = window.Alpine.store('chats');
			if (chats) {
				chats.resetToBlank();
				chats.currentUserNo = '';
				chats.currentUserNickname = '';
				chats.currentUserAvatar = '';
			}
		} catch(e) {}

		// 移除消息 DOM
		const chatContainer = document.getElementById('chatContainer');
		if (chatContainer) {
			chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
		}

		// 清空刻度导航
		const tickNav = document.getElementById('tickNav');
		if (tickNav) {
			tickNav.innerHTML = '';
		}

		// 跳转到登录页（replace 替换首页历史，不让后退回到已过期的首页）
		window.location.replace('/signin/');
		return true;
	}
	return false;
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
			const t = await response.text();
			showToast('切换会话失败：' + t, 'error');
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

/**
 * fetchProviderInfo 获取当前用户使用的第三方提供商信息。
 * @returns {Promise<{
 *   llm: {name: string, model: string, website: string},
 *   embedder: {name: string, model: string, website: string, dimension: number},
 *   web_search: {name: string, website: string},
 *   api_key_info: {llm_provider: string, embedder_provider: string, search_provider: string, llm_private: boolean, embedder_private: boolean, search_private: boolean}
 * }|null>}
 */
export async function fetchProviderInfo() {
    try {
        const response = await fetch('/api/info/3rd/providers');
        if (response.ok) {
            return await response.json();
        }
        const t = await response.text();
        showToast('获取第三方服务信息失败：' + t, 'error');
    } catch (e) {
        console.error('获取第三方服务信息失败:', e);
    }
    return null;
}

/**
 * _autoTaggingInFlight — 防止同 SN 同时发起多个自动归类请求
 * @type {Set<string>}
 */
const _autoTaggingInFlight = new Set();

/**
 * fetchChatTags 获取指定对话的话题分类标签。
 * @param {string} sn - 目标会话的 SN
 * @returns {Promise<{sn: string, title: string, tags: Array<string>, totalMessages: number, viewedMessages: number, allMessagesViewed: boolean}|null>}
 */
export async function fetchChatTags(sn) {
    if (!sn) return null;
    try {
        const response = await fetch('/api/chat/tags?sn=' + encodeURIComponent(sn), {
            method: 'POST',
        });
        if (!response.ok) {
            const t = await response.text();
            showToast('获取话题标签失败：' + t, 'error');
            return null;
        }
        return await response.json();
    } catch (e) {
        console.warn('获取话题标签出错:', e);
        return null;
    }
}

/**
 * fetchChatTagsAuto — 自动归类（静默，标题变更时由前端自动触发）。
 * 调用 POST /api/chat/tags?sn=XXX&force=true，跳过 taged 守卫。
 * 成功后将标签同步到客户端 chatGroups，不显示 Toast。
 * @param {string} sn - 目标会话的 SN
 */
async function fetchChatTagsAuto(sn) {
    if (!sn) return;
    // 防止同 SN 并发请求
    if (_autoTaggingInFlight.has(sn)) return;
    _autoTaggingInFlight.add(sn);

    try {
        const response = await fetch('/api/chat/tags?sn=' + encodeURIComponent(sn) + '&force=true', {
            method: 'POST',
        });
        if (!response.ok) {
            // 静默失败，仅 console 日志
            console.warn('自动归类失败:', response.status, await response.text());
            return;
        }
        const data = await response.json();
        if (data && data.tags) {
            // 将归类结果同步到客户端 chatGroups
            var chatsStore = window.Alpine.store('chats');
            if (chatsStore && chatsStore.moveChatBetweenTags) {
                chatsStore.moveChatBetweenTags(sn, data.tags);
            }
            console.log('📑 [自动归类] 对话', sn, '已归类到:', data.tags);
        }
    } catch (e) {
        console.warn('自动归类出错:', e);
    } finally {
        _autoTaggingInFlight.delete(sn);
    }
}

/**
 * fetchChatGroups 获取按标签分组的对话列表。
 * @returns {Promise<Object<string, Array<{sn: string, title: string, tag: string, create_at: string, update_at: string}>>|null>}
 */
export async function fetchChatGroups() {
    try {
        const response = await fetch('/api/chat/groups');
        if (!response.ok) {
            const t = await response.text();
            showToast('获取聊天分组失败：' + t, 'error');
            return null;
        }
        return await response.json();
    } catch (e) {
        console.warn('获取聊天分组出错:', e);
        return null;
        }
    }
    
    /**
     * addFavoriteChat 添加指定对话到收藏夹。
     * @param {string} sn - 对话 SN
     * @param {string} customTag - 收藏夹目录名（空串表示根目录）
     * @returns {Promise<boolean>} 是否成功
     */
    export async function addFavoriteChat(sn, customTag) {
        if (!sn) return false;
        try {
            const url = '/api/chat/favorites?sn=' + encodeURIComponent(sn) +
                '&custom_tag=' + encodeURIComponent(customTag || '');
            const response = await fetch(url, { method: 'PUT' });
            if (!response.ok) {
                const t = await response.text();
                showToast('添加收藏失败：' + t, 'error');
                return false;
            }
            return true;
        } catch (e) {
            console.warn('添加收藏失败:', e);
            return false;
        }
    }

    /**
     * removeFavoriteChat 从收藏夹移除指定对话。
     * @param {string} sn - 对话 SN
     * @param {string} customTag - 收藏夹目录名（空串表示根目录）
     * @returns {Promise<boolean>} 是否成功
     */
    export async function removeFavoriteChat(sn, customTag) {
        if (!sn) return false;
        try {
            const url = '/api/chat/favorites?sn=' + encodeURIComponent(sn) +
                '&custom_tag=' + encodeURIComponent(customTag || '');
            const response = await fetch(url, { method: 'DELETE' });
            if (!response.ok) {
                const t = await response.text();
                showToast('取消收藏失败：' + t, 'error');
                return false;
            }
            return true;
        } catch (e) {
            console.warn('取消收藏失败:', e);
            return false;
        }
    }

    /**
     * fetchFavorites 获取已收藏的对话列表（按 custom_tag 分组）。
     * @returns {Promise<Object<string, Array<{sn: string, title: string, custom_tag: string, create_at: string, update_at: string}>>|null>}
     */
    export async function fetchFavorites() {
        try {
            const response = await fetch('/api/chat/favorites');
            if (!response.ok) {
                const t = await response.text();
                showToast('获取收藏列表失败：' + t, 'error');
                return null;
            }
            return await response.json();
        } catch (e) {
            console.warn('获取收藏列表出错:', e);
            return null;
        }
    }
    
    /**
     * fetchSession 获取/创建 HTTP session，返回 session 数据（user_no, welcome 等）。
     * @returns {Promise<{user_no?: string, welcome?: string}|null>}
     */
export async function fetchSession() {
    try {
        const response = await fetch('/api/session');
        if (response.ok) {
            return await response.json();
        }
        const t = await response.text();
        showToast('获取session失败：' + t, 'error');
    } catch (e) {
        console.warn('session 初始化失败:', e);
    }
    return null;
}

/**
 * fetchChatList 获取当前用户的对话列表。
 * @returns {Promise<{chats?: Array}|null>}
 */
export async function fetchChatList() {
    try {
        const response = await fetch('/api/chat/list');
        if (response.ok) {
            return await response.json();
        }
        const t = await response.text();
        showToast('获取对话列表失败：' + t, 'error');
    } catch (e) {
        console.warn('获取对话列表失败:', e);
    }
    return null;
}

/**
 * togglePinChat 切换指定对话的置顶状态。
 * @param {string} sn - 对话 SN
 * @param {boolean} pinned - 是否置顶
 * @returns {Promise<boolean>} 是否成功
 */
export async function togglePinChat(sn, pinned) {
    if (!sn) return false;
    try {
        const response = await fetch('/api/chat/pin?sn=' + encodeURIComponent(sn) +
            '&pinned=' + pinned, { method: 'PUT' });
        if (!response.ok) {
            const t = await response.text();
            showToast('切换对话置顶状态失败：' + t, 'error');
            return false;
        }
        return true;
    } catch (e) {
        console.warn('切换置顶失败:', e);
        return false;
    }
}

/**
 * deleteChat 逻辑删除指定对话（移入回收站）。
 * @param {string} sn - 对话 SN
 * @returns {Promise<boolean>} 是否成功
 */
export async function deleteChat(sn) {
    if (!sn) return false;
    try {
        const response = await fetch('/api/chat?sn=' + encodeURIComponent(sn), {
            method: 'DELETE',
        });
        if (!response.ok) {
            const t = await response.text();
            showToast('删除失败：' + t, 'error');
            return false;
        }
        return true;
    } catch (e) {
        console.warn('删除对话失败:', e);
        return false;
    }
}

/**
 * listDeletedChats 获取回收站中的对话列表。
 * @returns {Promise<Array|null>}
 */
export async function listDeletedChats() {
    try {
        const response = await fetch('/api/chat/deleted');
        if (response.ok) {
            const data = await response.json();
            return data.chats || [];
        }
        const t = await response.text();
        showToast('获取回收站列表失败：' + t, 'error');
    } catch (e) {
        console.warn('获取回收站列表失败:', e);
    }
    return null;
}

/**
 * restoreChat 从回收站恢复指定对话。
 * @param {string} sn - 对话 SN
 * @returns {Promise<boolean>} 是否成功
 */
export async function restoreChat(sn) {
    if (!sn) return false;
    try {
        const response = await fetch('/api/chat/restore?sn=' + encodeURIComponent(sn), {
            method: 'PUT',
        });
        if (!response.ok) {
            const t = await response.text();
            showToast('恢复失败：' + t, 'error');
            return false;
        }
        return true;
    } catch (e) {
        console.warn('恢复对话失败:', e);
        return false;
    }
}

/**
 * permanentDeleteChat 永久删除回收站中的指定对话（不可恢复）。
 * @param {string} sn - 对话 SN
 * @returns {Promise<boolean>} 是否成功
 */
export async function permanentDeleteChat(sn) {
    if (!sn) return false;
    try {
        const response = await fetch('/api/chat/permanent?sn=' + encodeURIComponent(sn), {
            method: 'DELETE',
        });
        if (!response.ok) {
            const t = await response.text();
            showToast('永久删除失败：' + t, 'error');
            return false;
        }
        return true;
    } catch (e) {
        console.warn('永久删除对话失败:', e);
        return false;
    }
}

/**
 * emptyTrash 清空回收站（永久删除所有回收站中的对话）。
 * @returns {Promise<boolean>} 是否成功
 */
export async function emptyTrash() {
    try {
        const response = await fetch('/api/chat/trash', {
            method: 'DELETE',
        });
        return response.ok;
    } catch (e) {
        console.warn('清空回收站失败:', e);
        return false;
    }
}

/**
 * deleteMessage 删除指定消息（仅删除消息，不删除对话）。
 * @param {number} msgId - 消息 ID
 * @returns {Promise<boolean>} 是否成功
 */
export async function deleteMessage(msgId) {
    if (!msgId) return false;
    try {
        const response = await fetch('/api/chat/messages', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json; charset=utf-8' },
            body: JSON.stringify({ msg_id: msgId }),
        });
        return response.ok;
    } catch (e) {
        console.warn('删除消息失败:', e);
        return false;
    }
}

/**
 * sendChatMessage 发送用户消息到后端，返回 Response 对象供 SSE 流式读取。
 * @param {object} params
 * @param {string} params.content - 消息内容
 * @param {string} params.createdAt - 消息创建时间
 * @param {boolean} params.stream - 是否启用流式响应
 * @param {boolean} params.deepThink - 是否启用深度思考
 * @param {boolean} params.traitSearch - 是否启用个人特征检索
 * @param {boolean} params.webSearch - 是否启用联网搜索
 * @param {string} params.frontSn - 前端临时 SN
 * @param {AbortSignal} [params.signal] - 可选的 AbortSignal
 * @returns {Promise<Response>} fetch Response 对象
 */
export async function sendChatMessage({ content, createdAt, stream, deepThink, traitSearch, webSearch, frontSn, signal }) {
    const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json; charset=utf-8' },
        body: JSON.stringify({
            message: { id: 0, role: 'user', content, created_at: createdAt },
            stream: !!stream,
            deep_think: !!deepThink,
            trait_search_enabled: !!traitSearch,
            web_search_enabled: !!webSearch,
            front_sn: frontSn,
        }),
        signal: signal || undefined,
    });
    if (!response.ok) {
        const errText = await response.text();
        throw new Error(`服务器错误 [${response.status}]: ${errText}`);
    }
    return response;
}

// ============================================================
// 注册方法到 window — 供 HTML 模板 @click 调用
// chat-api.js 是 ES Module，export 的函数不会进入全局作用域。
// Alpine 模板 @click="onChatLogout" 需要通过 window 解析函数名。
// ============================================================
try {
    if (typeof onChatLogout === 'function') {
        window.onChatLogout = onChatLogout;
    }
} catch(e) {
    console.warn('chat-api: 注册到 window 失败', e);
}
