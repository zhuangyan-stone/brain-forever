// ============================================================
// chat-reasoning.js — Reasoning（深度思考）状态管理
// ============================================================

import { renderMarkdown } from './chat-markdown.js';

'use strict';

/**
 * Reasoning 状态 → 标题文本映射
 *
 * reasoningState 取值：
 *   'thinking'    — 流式进行中，显示"正在思考……"
 *   'done'        — 正常完成，显示"思考完成"
 *   'interrupted' — 被中断，显示"AI 思路已被掐断"
 */
const REASONING_TITLES = {
    thinking: '正在思考……',
    done: '思考完成',
    interrupted: 'AI 思路已被掐断',
};

/**
 * 切换 reasoning 区域的折叠/展开状态
 * @param {HTMLElement} headerEl
 */
export function toggleReasoningCollapse(headerEl) {
    const area = headerEl.closest('.reasoning-area');
    if (!area) return;
    const isCollapsed = area.classList.toggle('collapsed');
    const toggleBtn = headerEl.querySelector('.reasoning-toggle');
    if (toggleBtn) {
        // 使用 ▶ 作为基础字符，展开时通过 CSS transform: rotate(90deg) 变为 ▼
        // 折叠时 transform: rotate(0deg) 保持 ▶ — 与 sources-panel 完全一致
        toggleBtn.textContent = '▶';
        toggleBtn.dataset.tooltip = isCollapsed ? '展开思考内容' : '折叠思考内容';
    }
}

/**
 * 在 assistant 消息气泡中恢复 reasoning（深度思考链）区域
 * @param {HTMLElement} assistantBubble - .message.assistant 元素
 * @param {string} reasoningText - 思考链的原始 Markdown 文本
 * @param {string} [reasoningState='done'] - 推理状态（'thinking' | 'done' | 'interrupted'）
 */
export function restoreReasoningArea(assistantBubble, reasoningText, reasoningState) {
    if (!assistantBubble || !reasoningText) return;

    // 🛡 防重复守卫：如果 assistant 气泡中已有 .reasoning-area（已由 Alpine 模板渲染），
    //    则不重复创建，避免 reasoning 区域显示两次。
    //    场景：Alpine 方案B 下，group.assistant.reasoningHTML 被设置后 Alpine 会自动渲染；
    //    此处 JS 调用是历史遗留，需跳过以免重复。
    if (assistantBubble.querySelector('.reasoning-area')) {
        return;
    }

    reasoningState = reasoningState || 'done';
    const titleText = REASONING_TITLES[reasoningState] || REASONING_TITLES.done;

    // 隐藏独立的 AI 角色标签，将其合并到 reasoning-header 中
    const roleLabel = assistantBubble.querySelector('.role-label-ai');
    let roleTimeText = '';
    if (roleLabel) {
        const labelText = roleLabel.textContent || '';
        const timeMatch = labelText.match(/\((\d{2}:\d{2}:\d{2})\)/);
        if (timeMatch) {
            roleTimeText = ` (${timeMatch[1]})`;
        }
        roleLabel.style.display = 'none';
    }

    // 创建 reasoning 区域（默认折叠）
    const reasoningArea = document.createElement('div');
    reasoningArea.className = 'reasoning-area done collapsed';
    // 将时间文本存储在 reasoning-area 上
    reasoningArea.dataset.roleTimeText = roleTimeText || '';
    reasoningArea.innerHTML = `
        <div class="reasoning-header">
            <span class="reasoning-toggle" data-tooltip="折叠思考过程">▶</span>
            <span class="reasoning-icon">🤖</span>
            <span class="reasoning-role-badge">AI</span>
            <span class="reasoning-title">${titleText}${roleTimeText}</span>
        </div>
        <div class="reasoning-content">${renderMarkdown(reasoningText)}</div>
    `;

    // 点击 header 切换折叠/展开
    const header = reasoningArea.querySelector('.reasoning-header');
    header.addEventListener('click', (e) => {
        toggleReasoningCollapse(header);
    });

    // 插入到气泡之前
    const bubble = assistantBubble.querySelector('.bubble');
    if (bubble) {
        bubble.insertAdjacentElement('beforebegin', reasoningArea);
    } else {
        assistantBubble.appendChild(reasoningArea);
    }
}
