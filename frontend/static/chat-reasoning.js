// ============================================================
// chat-reasoning.js — Reasoning（深度思考）状态管理
// ============================================================

import { state } from './chat-state.js';
import { renderMarkdown } from './chat-markdown.js';
import { scrollToBottom } from './chat-ui.js';

'use strict';

/**
 * 获取或创建 assistant 气泡中的 reasoning 状态区域
 * @param {HTMLElement} assistantBubble
 * @returns {HTMLElement}
 */
function getOrCreateReasoningArea(assistantBubble) {
    let area = assistantBubble.querySelector('.reasoning-area');
    if (!area) {
        area = document.createElement('div');
        area.className = 'reasoning-area';
        // 插入到气泡之前
        const bubble = assistantBubble.querySelector('.bubble');
        if (bubble) {
            bubble.insertAdjacentElement('beforebegin', area);
        } else {
            assistantBubble.appendChild(area);
        }
    }
    return area;
}

/**
 * 创建 reasoning 区域（含标题栏和内容区）
 * @param {HTMLElement} assistantBubble
 * @returns {HTMLElement} reasoning-area 元素
 */
export function createReasoningArea(assistantBubble) {
    let reasoningArea = assistantBubble.querySelector('.reasoning-area');
    if (reasoningArea) return reasoningArea;

    reasoningArea = getOrCreateReasoningArea(assistantBubble);
    reasoningArea.className = 'reasoning-area active';

    // 隐藏独立的 AI 角色标签，将其合并到 reasoning-header 中
    const roleLabel = assistantBubble.querySelector('.role-label-ai');
    if (roleLabel) {
        roleLabel.style.display = 'none';
    }

    const titleText = '正在思考……';
    reasoningArea.innerHTML = `
        <div class="reasoning-header">
            <span class="reasoning-toggle" data-tooltip="折叠思考过程">▶</span>
            <span class="reasoning-icon">🤖</span>
            <span class="reasoning-role-badge">AI</span>
            <span class="reasoning-title">${titleText}</span>
        </div>
        <div class="reasoning-content"></div>
    `;
    // 点击 header 切换折叠/展开
    const header = reasoningArea.querySelector('.reasoning-header');
    header.addEventListener('click', (e) => {
        toggleReasoningCollapse(header);
    });

    return reasoningArea;
}

/**
 * 根据工具名称返回对应的图标 emoji
 * @param {string} toolName - 工具函数名
 * @returns {string} 图标字符串
 */
function getToolIcon(toolName) {
    switch (toolName) {
        case 'web_search':
            return '🔍';
        case 'get_current_local_time':
            return '🕐';
        case 'personal_trait_search':
            return '🧑';
        default:
            return '⚙';
    }
}

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
 * 确保 reasoning 区域存在，返回其 content 元素
 * @param {HTMLElement} assistantBubble
 * @returns {HTMLElement|null}
 */
export function ensureReasoningContent(assistantBubble) {
    let reasoningArea = assistantBubble.querySelector('.reasoning-area');
    if (!reasoningArea) {
        reasoningArea = createReasoningArea(assistantBubble);
    }
    return reasoningArea.querySelector('.reasoning-content');
}

/**
 * 对 reasoning-content 元素执行节流渲染
 * @param {HTMLElement} contentEl
 */
export function scheduleReasoningRender(contentEl) {
    if (!contentEl.renderTimer) {
        contentEl.renderTimer = setTimeout(() => {
            contentEl.renderTimer = null;
            contentEl.innerHTML = renderMarkdown(contentEl.rawText);
            scrollToBottom();
        }, state.RENDER_INTERVAL);
    }
}

/**
 * 将 reasoning 区域标记为"思考完成"：标题改为"思考完成"、移除 active、添加 done、立即最终渲染
 * @param {HTMLElement} assistantBubble
 */
export function finalizeReasoningArea(assistantBubble) {
    const area = assistantBubble.querySelector('.reasoning-area.active');
    if (!area) return;

    const titleEl = area.querySelector('.reasoning-title');
    if (titleEl) {
        titleEl.textContent = '思考完成';
    }
    area.classList.remove('active');
    area.classList.add('done');

    const contentEl = area.querySelector('.reasoning-content');
    if (contentEl) {
        if (contentEl.renderTimer) {
            clearTimeout(contentEl.renderTimer);
            contentEl.renderTimer = null;
        }
        if (contentEl.rawText) {
            contentEl.innerHTML = renderMarkdown(contentEl.rawText);
        }
    }
}

/**
 * 处理 reasoning 事件
 * @param {object} event
 * @param {HTMLElement} assistantBubble
 */
export function handleReasoningEvent(event, assistantBubble) {
    const contentEl = ensureReasoningContent(assistantBubble);
    if (!contentEl || !event.content) return;

    if (!contentEl.rawText) contentEl.rawText = '';

    if (event.subject === 'tool-pending') {
        // tool-pending：显示工具调用提示（带图标）
        const icon = getToolIcon(event.tool);
        contentEl.rawText += `\n> ${icon} ${event.content}\n`;
    } else {
        // 真正的 LLM 思考内容
        contentEl.rawText += event.content;
    }

    scheduleReasoningRender(contentEl);
}

/**
 * 在 assistant 消息气泡中恢复 reasoning（深度思考链）区域
 * @param {HTMLElement} assistantBubble - .message.assistant 元素
 * @param {string} reasoningText - 思考链的原始 Markdown 文本
 */
export function restoreReasoningArea(assistantBubble, reasoningText) {
    if (!assistantBubble || !reasoningText) return;

    // 隐藏独立的 AI 角色标签，将其合并到 reasoning-header 中
    const roleLabel = assistantBubble.querySelector('.role-label-ai');
    if (roleLabel) {
        roleLabel.style.display = 'none';
    }

    // 创建 reasoning 区域（默认折叠）
    const reasoningArea = document.createElement('div');
    reasoningArea.className = 'reasoning-area done collapsed';
    const titleText = '思考完成';
    reasoningArea.innerHTML = `
        <div class="reasoning-header">
            <span class="reasoning-toggle" data-tooltip="折叠思考过程">▶</span>
            <span class="reasoning-icon">🤖</span>
            <span class="reasoning-role-badge">AI</span>
            <span class="reasoning-title">${titleText}</span>
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
