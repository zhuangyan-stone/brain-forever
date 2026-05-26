// ============================================================
// chat-restore.js — 对话恢复 + 欢迎消息
// ============================================================

import { state } from './chat-state.js';
import { addMessage, showSources, showTokenUsage, showWelcomeMessage, updateHeaderTitle } from './chat-ui.js';
import { restoreReasoningArea } from './chat-reasoning.js';
import { updateTickNav } from './chat-ticknav.js';
import { sessionManager } from './chat-session-manager.js';

'use strict';

/**
 * restoreChat 从后端获取当前 chat 的历史消息并恢复显示。
 * 同时从返回的 user_no 恢复登录按钮状态（页面刷新后仍显示已登录）。
 * 匿名用户也会返回 user_no="anonymous"，因此无需区分登录状态。
 */
export async function restoreChat() {
	try {
		const response = await fetch('/api/session');
		if (!response.ok) return;

		const data = await response.json();

		// ============================================================
		// 恢复登录状态：后端始终返回 user_no（匿名用户为 "anonymous"）
		// ============================================================
		if (data.user_no) {
			const loginBtn = document.getElementById('loginBtn');
			if (loginBtn) {
				loginBtn.textContent = `用户: ${data.user_no}`;
			}
		}

		// 如果后端返回了对话列表，渲染到左侧栏（匿名用户也有自己的 chat 列表）
		if (data.chats) {
			const { renderChatList } = await import('./chat-list.js');
			renderChatList(data.chats, data.current_chat_sn || null);
		}

		// 保存当前对话的 SN
		state.currentChatSN = data.current_chat_sn || '';

		// ---- 初始化 Alpine store 中的 chat 数据 ----
		var chatData = null;
		try {
			var chats = window.Alpine.store('chats');
			if (chats && data.current_chat_sn) {
				chatData = chats.getOrCreate(data.current_chat_sn);
				if (chatData) {
					chatData.title = data.title || '';
					chatData.titleState = typeof data.title_state === 'number' ? data.title_state : 0;
				}
			}
		} catch(e) {}
		// ---- Alpine store 初始化结束 ----

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

		// 有历史消息，恢复显示（双写：DOM + Alpine store）
		for (const msg of history) {
			const msgDiv = addMessage(msg.role, msg.content, msg.created_at || null);
			const entry = { role: msg.role, content: msg.content, id: msg.id, usage: msg.usage || null };
			state.messages.push(entry);

			// 同步到 Alpine store
			if (chatData) {
				chatData.messages.push({
					id: msg.id,
					role: msg.role,
					content: msg.content,
					reasoning: msg.reasoning || undefined,
					sources: msg.sources && msg.sources.length > 0 ? msg.sources : undefined,
					usage: msg.usage || undefined,
					createdAt: msg.created_at || undefined,
				});
			}
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
	
		// 检查当前 chat 的 session 是否有未渲染的 streamingMsg
		// 场景：页面刷新前，该 chat 正在后台接收 SSE 流式数据但尚未完成
		const currentSN = state.currentChatSN;
		if (currentSN) {
			const session = sessionManager.sessions.get(currentSN);
			if (session && session.streamingMsg && !session.streamingMsg.isDone) {
				// 有未完成的流式数据，需要渲染到 DOM
				// 先创建 assistant 气泡，恢复 DOM 引用
				const assistantBubble = addMessage('assistant', '', null, true);
				const contentDiv = assistantBubble.querySelector('.bubble');
				session.assistantBubble = assistantBubble;
				session.contentDiv = contentDiv;

				// 使用 session 上已有的 responser 执行 flushToDOM
				// flushToDOM 现在支持 isDone=false 时渲染已有累积内容
				session.responser.flushToDOM();
			}
		}
	} catch (e) {
		// 网络错误等情况下，回退到显示欢迎消息
		console.warn('无法恢复对话历史，显示欢迎消息:', e);
		showWelcomeMessage();
	}
}
