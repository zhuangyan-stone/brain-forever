// ============================================================
// sticky-ai-title.js — AI 推荐标题便利贴组件
// 在页面右侧显示 AI 推荐的对话标题，提供"接受/拒绝"操作
// ============================================================
// 使用方式：
//   import { showAiTitleSuggestion } from './sticky-ai-title.js';
//   showAiTitleSuggestion('AI 为本次对话推荐了一个新标题', '新标题文本', {
//       onApply: (title) => { /* 接受回调 */ },
//       onDismiss: (title) => { /* 拒绝回调 */ },
//   });
// ============================================================

'use strict';

import { ICON_CLOSE } from '../svg_icons_re.js';
import { getContainer } from './sticky-mgr.js';

/** 定时时长（毫秒）— 15 秒后自动应用标题 */
const TIMER_DURATION = 15000;

/**
 * 显示一张标题推荐便利贴。
 *
 * @param {string} message   - 提示消息，如 "AI 推荐标题"
 * @param {string} title     - AI 推荐的新标题
 * @param {object} [options] - 可选配置
 * @param {function} [options.onApply] - 应用标题的回调（定时到点或用户点击"采纳"时调用），参数为 title
 * @param {function} [options.onDismiss] - 用户取消时的回调，参数为 title
 * @returns {HTMLElement} 创建的便利贴 DOM 元素
 */
export function showAiTitleSuggestion(message, title, options = {}) {
    const { onApply, onDismiss } = options;

    const ctn = getContainer();

    // ---- 创建便利贴 DOM ----
    const note = document.createElement('div');
    note.className = 'sticky-note';

    // ---- 右上角关闭按钮 ----
    const closeBtn = document.createElement('button');
    closeBtn.className = 'sticky-note-close';
    closeBtn.innerHTML = '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + ICON_CLOSE + '</svg>';
    closeBtn.setAttribute('aria-label', '关闭');
    closeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
            }
        }, { once: true });
        if (typeof onDismiss === 'function') {
            onDismiss(title);
        }
    });
    note.appendChild(closeBtn);

    // 消息文本
    const msgEl = document.createElement('div');
    msgEl.className = 'sticky-note-message';
    msgEl.textContent = message;
    note.appendChild(msgEl);

    // 推荐标题（突出显示）
    const titleEl = document.createElement('div');
    titleEl.className = 'sticky-note-title';
    titleEl.textContent = title;
    note.appendChild(titleEl);

    // ---- 定时进度条（在 actions 区之上） ----
    const progressBar = document.createElement('div');
    progressBar.className = 'sticky-note-progress';
    const progressFill = document.createElement('div');
    progressFill.className = 'sticky-note-progress-fill';
    progressBar.appendChild(progressFill);
    note.appendChild(progressBar);

    // 按钮行
    const actionsEl = document.createElement('div');
    actionsEl.className = 'sticky-note-actions';

    // "✗ 不要" 按钮
    const dismissBtn = document.createElement('button');
    dismissBtn.className = 'sticky-note-btn sticky-note-btn-dismiss';
    dismissBtn.textContent = '✗ 不要';
    dismissBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
            }
        }, { once: true });
        if (typeof onDismiss === 'function') {
            onDismiss(title);
        }
    });
    actionsEl.appendChild(dismissBtn);

    // "✓ 采纳" 按钮（含定时剩余秒数）
    const acceptBtn = document.createElement('button');
    acceptBtn.className = 'sticky-note-btn sticky-note-btn-accept';
    acceptBtn.innerHTML = '✓ 采纳 <span class="sticky-note-countdown"></span>';
    acceptBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
            }
        }, { once: true });
        if (typeof onApply === 'function') {
            onApply(title);
        }
    });
    actionsEl.appendChild(acceptBtn);

    note.appendChild(actionsEl);

    // ---- 定时进度条 + 倒计时秒数 + 自动应用标题 ----
    const startTime = Date.now();
    const countdownSpan = acceptBtn.querySelector('.sticky-note-countdown');

    const progressInterval = setInterval(() => {
        const elapsed = Date.now() - startTime;
        const pct = Math.min(elapsed / TIMER_DURATION, 1);
        progressFill.style.width = (pct * 100) + '%';

        const remaining = Math.max(0, Math.ceil((TIMER_DURATION - elapsed) / 1000));
        if (countdownSpan) {
            const formatted = String(remaining).padStart(2, '0');
            countdownSpan.textContent = remaining > 0 ? `(${formatted})` : '';
        }
    }, 100);

    const autoAcceptTimer = setTimeout(() => {
        clearInterval(progressInterval);
        progressFill.style.width = '100%';
        if (countdownSpan) countdownSpan.textContent = '';
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
            }
        }, { once: true });
        if (typeof onApply === 'function') {
            onApply(title);
        }
    }, TIMER_DURATION);

    ctn.appendChild(note);

    return note;
}
