// ============================================================
// chat-session.js — 会话恢复 + 欢迎消息
// ============================================================

import { state } from './chat-state.js';
import { addMessage, showSources, showTokenUsage, showWelcomeMessage, updateHeaderTitle, updateTitleHistoryStyle } from './chat-ui.js';
import { restoreReasoningArea } from './chat-reasoning.js';
import { updateTickNav } from './chat-ticknav.js';

'use strict';

/**
 * restoreSession 从后端获取当前 session 的历史消息并恢复显示
 */
export async function restoreSession() {
    try {
        const response = await fetch('/api/session');
        if (!response.ok) return;

        const data = await response.json();

        // isNew 或 history 为空 → 显示欢迎消息
        const history = data.history || [];
        if (data.is_new || history.length === 0) {
            showWelcomeMessage();
            return;
        }

        // 恢复对话标题（后端从第一条用户消息截取）
        if (data.title) {
            updateHeaderTitle(data.title);
        }

        // 恢复标题修改状态
        if (typeof data.title_state === 'number') {
            state.titleState = data.title_state;
        }

        // 恢复完成后更新标题历史样式
        updateTitleHistoryStyle();

        // 有历史消息，恢复显示
        for (const msg of history) {
            const msgDiv = addMessage(msg.role, msg.content);
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
        console.warn('无法恢复会话历史，显示欢迎消息:', e);
        showWelcomeMessage();
    }
}
