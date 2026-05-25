// ============================================================
// chat-restore.js — 对话恢复 + 欢迎消息
// ============================================================

import { state } from './chat-state.js';
import { addMessage, showSources, showTokenUsage, showWelcomeMessage, updateHeaderTitle } from './chat-ui.js';
import { restoreReasoningArea } from './chat-reasoning.js';
import { updateTickNav } from './chat-ticknav.js';

'use strict';

/**
 * restoreChat 从后端获取当前 chat 的历史消息并恢复显示。
 * 同时从返回的 user_no 恢复登录按钮状态（页面刷新后仍显示已登录）。
 * 如果服务端 chat 已丢失（服务重启）但 localStorage 中有 user_no，
 * 则自动重新登录以恢复对话。
 */
export async function restoreChat() {
	try {
		const response = await fetch('/api/session');
		if (!response.ok) return;

		const data = await response.json();

		// ============================================================
		// 恢复登录状态：后端返回 user_no 表示当前 chat 已登录
		// ============================================================
		if (data.user_no) {
			const loginBtn = document.getElementById('loginBtn');
			if (loginBtn) {
				loginBtn.textContent = `用户: ${data.user_no}`;
			}
			// 同步到 localStorage，确保后续刷新时也能恢复
			localStorage.setItem('brainforever_user_no', data.user_no);

			// 刷新页面后，如果后端返回了对话列表，渲染到左侧栏
			if (data.chats) {
				const { renderChatList } = await import('./chat-list.js');
				renderChatList(data.chats, data.current_chat_sn || null);
			}
		} else {
			// 后端未返回 user_no — 检查 localStorage 是否有之前保存的
			// （可能是服务器重启导致内存 chat 丢失）
			const savedUserNo = localStorage.getItem('brainforever_user_no');
			if (savedUserNo) {
				// 无论 isNew 是否为 true，只要 localStorage 中有 user_no 就尝试重新登录。
				// 修复场景：后端重启后 session 内存数据丢失，GET /api/session 返回 isNew=true，
				// 但 localStorage 中已有登录记录，此时应自动重新登录以恢复用户状态。
				const { onChatLogin } = await import('./chat-api.js');
				await onChatLogin(savedUserNo);
				// onChatLogin 内部会调用 switchToUser → restoreChat，
				// 因此这里直接返回，避免重复处理
				return;
			}
		}

		// 保存当前对话的 SN
		state.currentChatSN = data.current_chat_sn || '';

		// 全新会话（is_new）→ 显示欢迎消息
		// 非新会话：即使 messages 为空（如后端异常），也要展示标题等信息
		const history = data.messages || [];
		if (data.is_new) {
			showWelcomeMessage();
			return;
		}

		// 恢复对话标题
		if (data.title) {
			updateHeaderTitle(data.title);
		}

		// 恢复标题修改状态
		if (typeof data.title_state === 'number') {
			state.titleState = data.title_state;
		}

		// 有历史消息，恢复显示
		for (const msg of history) {
			const msgDiv = addMessage(msg.role, msg.content, msg.created_at || null);
			const entry = { role: msg.role, content: msg.content, id: msg.id, usage: msg.usage || null };
			state.messages.push(entry);
			// 将 data-msg-id 设置在 message-group 上（同一组的 user 和 assistant 共享同一 ID）
			if (msgDiv && msg.id) {
				const group = msgDiv.closest('.message-group');
				if (group) {
					group.dataset.msgId = msg.id;
				}
			}
			// 如果是 assistant 消息且有 usage 信息，显示 token-info
			if (msg.role === 'assistant' && msg.usage && msgDiv) {
				showTokenUsage(msgDiv, msg.usage);
			}
			// 如果是 assistant 消息且有 sources（联网搜索结果），恢复显示 sources-panel
			if (msg.role === 'assistant' && msg.sources && msg.sources.length > 0) {
				showSources(msg.sources, 'web');
			}
			// 如果是 assistant 消息且有 reasoning（深度思考链），恢复显示 reasoning 区域
			if (msg.role === 'assistant' && msg.reasoning && msgDiv) {
				restoreReasoningArea(msgDiv, msg.reasoning, msg.deep_think);
			}
		}

		// 恢复完成后更新刻度导航
		updateTickNav();
	} catch (e) {
		// 网络错误等情况下，回退到显示欢迎消息
		console.warn('无法恢复对话历史，显示欢迎消息:', e);
		showWelcomeMessage();
	}
}
