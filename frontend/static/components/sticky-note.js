// ============================================================
// sticky-note.js — 便利贴式标题推荐组件
// 在页面右侧显示 AI 推荐的对话标题，提供"接受/拒绝"操作
// ============================================================
// 使用方式：
//   import { showStickyNote } from './components/sticky-note.js';
//   showStickyNote('AI 为本次对话推荐了一个新标题', '新标题文本', {
//       onAccept: (title) => { /* 接受回调 */ },
//       onDismiss: (title) => { /* 拒绝回调 */ },
//   });
// ============================================================

'use strict';

/** 对称小叉 SVG */
const ICON_CLOSE = '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><line x1="3" y1="3" x2="13" y2="13"/><line x1="13" y1="3" x2="3" y2="13"/></svg>';

/** 方框图标 SVG — 表示恢复窗口最大化（单个方框） */
const ICON_RESTORE = '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="2.5" width="11" height="11" rx="1.5"/></svg>';

/** 定时时长（毫秒）— 20 秒后自动应用标题 */
const TIMER_DURATION = 20000;

/**
 * 便利贴容器（单例，延迟创建）
 * @type {HTMLElement|null}
 */
let container = null;

/**
 * 用于监听 .main-content 尺寸变化的 ResizeObserver（单例）
 * @type {ResizeObserver|null}
 */
let resizeObserver = null;

/**
 * 获取或创建便利贴容器 DOM 元素。
 * 容器使用 position:fixed 右对齐到对话框（scrollContainer）右边沿。
 * @returns {HTMLElement}
 */
function getContainer() {
    if (!container) {
        container = document.createElement('div');
        container.className = 'sticky-note-container';
        document.body.appendChild(container);
        // 初始化位置监听
        initPositionWatcher();
        updatePosition();
    }
    return container;
}

/**
 * 初始化位置监听：监听窗口 resize 和 .main-content 尺寸变化（侧边栏切换时）。
 */
function initPositionWatcher() {
    if (resizeObserver) return; // 已初始化

    // 窗口 resize 时更新
    window.addEventListener('resize', updatePosition);

    // 用 ResizeObserver 监听 .main-content 尺寸变化（侧边栏显隐会改变其宽度）
    const mainContent = document.querySelector('.main-content');
    if (mainContent) {
        resizeObserver = new ResizeObserver(() => {
            updatePosition();
        });
        resizeObserver.observe(mainContent);
    }
}

/**
 * 更新便利贴容器的位置，使其右边缘位于 main-body 右边沿左侧 64px 处，
 * 为右侧的刻度导航（tick-nav）留出刻度线和刻度值的显示空间。
 */
function updatePosition() {
    if (!container) return;
    const mainBody = document.querySelector('.main-body');
    if (!mainBody) {
        // 兜底：固定在右侧
        container.style.right = '16px';
        container.style.transform = '';
        return;
    }

    const mbRect = mainBody.getBoundingClientRect();
    const vw = window.innerWidth;

    // right 值 = 视口右边缘到 (main-body 右边沿 - 64px) 的距离
    // 这样便利贴的右边缘就位于 main-body 右边沿左侧 64px 处
    const rightVal = vw - (mbRect.right - 64);

    container.style.right = rightVal + 'px';
    // 移除旧的居中 transform
    container.style.transform = '';
}

/**
 * 折叠便利贴：只显示标题，隐藏消息、推荐标题、按钮行和恢复按钮。
 * @param {HTMLElement} note - 便利贴 DOM 元素
 */
function collapseNote(note) {
    if (note.classList.contains('collapsed') || note.classList.contains('leaving')) return;

    // 先执行折叠动画
    note.classList.add('collapsing');
    // 动画结束后切换为 collapsed 状态
    note.addEventListener('animationend', () => {
        note.classList.remove('collapsing');
        note.classList.add('collapsed');
    }, { once: true });
}

/**
 * 恢复便利贴：从折叠状态展开，显示完整内容。
 * @param {HTMLElement} note - 便利贴 DOM 元素
 */
function restoreNote(note) {
    if (!note.classList.contains('collapsed')) return;

    note.classList.remove('collapsed');
    // 移除折叠标题
    const collapsedTitle = note.querySelector('.sticky-note-collapsed-title');
    if (collapsedTitle) {
        collapsedTitle.remove();
    }
    // 恢复按钮由 CSS 控制显隐（.collapsed 移除后自动隐藏）
}

/**
 * 显示一张标题推荐便利贴。
 *
 * @param {string} message   - 提示消息，如 "AI 推荐标题"
 * @param {string} title     - AI 推荐的新标题
 * @param {object} [options] - 可选配置
 * @param {function} [options.onApply] - 应用标题的回调（定时到点或用户点击"试试"时调用），参数为 title
 * @param {function} [options.onDismiss] - 用户取消时的回调，参数为 title
 * @param {function} [options.onReject] - 用户点击"无需推荐"时的回调，参数为 title
 * @returns {HTMLElement} 创建的便利贴 DOM 元素
 */
export function showStickyNote(message, title, options = {}) {
    const { onApply, onDismiss, onReject } = options;

    const ctn = getContainer();

    // 每次显示时重新计算位置（侧边栏可能已切换）
    updatePosition();

    // ---- 创建便利贴 DOM ----
    const note = document.createElement('div');
    note.className = 'sticky-note';

    // ---- 右上角按钮组 ----
    // 恢复最大化按钮（方框图标）— 放在关闭按钮左边
    const restoreBtn = document.createElement('button');
    restoreBtn.className = 'sticky-note-restore';
    restoreBtn.innerHTML = ICON_RESTORE;
    restoreBtn.setAttribute('aria-label', '恢复窗口大小');
    restoreBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        restoreNote(note);
    });
    note.appendChild(restoreBtn);

    // 右上角关闭按钮（对称小叉 SVG）
    const closeBtn = document.createElement('button');
    closeBtn.className = 'sticky-note-close';
    closeBtn.innerHTML = ICON_CLOSE;
    closeBtn.setAttribute('aria-label', '关闭');
    closeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // 取消定时器
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        // 触发 dismiss 行为
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
                container = null;
            }
        }, { once: true });
        if (typeof onDismiss === 'function') {
            onDismiss(title);
        }
    });
    note.appendChild(closeBtn);

    // 折叠后只显示的标题行
    const collapsedTitleEl = document.createElement('div');
    collapsedTitleEl.className = 'sticky-note-collapsed-title';
    collapsedTitleEl.textContent = title;
    note.appendChild(collapsedTitleEl);

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

    // "无需推荐" 按钮（红色警告色，放在最左边）
    const rejectBtn = document.createElement('button');
    rejectBtn.className = 'sticky-note-btn sticky-note-btn-reject';
    rejectBtn.textContent = '无需推荐';
    rejectBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // 取消定时器
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        // 离场动画后移除
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
                container = null;
            }
        }, { once: true });
        if (typeof onReject === 'function') {
            onReject(title);
        }
    });
    actionsEl.appendChild(rejectBtn);

    // "✗ 不要" 按钮
    const dismissBtn = document.createElement('button');
    dismissBtn.className = 'sticky-note-btn sticky-note-btn-dismiss';
    dismissBtn.textContent = '✗ 不要';
    dismissBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // 取消定时器
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        // 离场动画后移除
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            // 容器为空时也移除容器
            if (ctn.children.length === 0) {
                ctn.remove();
                container = null;
            }
        }, { once: true });
        if (typeof onDismiss === 'function') {
            onDismiss(title);
        }
    });
    actionsEl.appendChild(dismissBtn);

    // "✓ 甚好" 按钮
    const acceptBtn = document.createElement('button');
    acceptBtn.className = 'sticky-note-btn sticky-note-btn-accept';
    acceptBtn.textContent = '✓ 甚好';
    acceptBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // 取消定时器
        clearInterval(progressInterval);
        clearTimeout(autoAcceptTimer);
        // 离场动画后移除
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
                container = null;
            }
        }, { once: true });
        if (typeof onApply === 'function') {
            onApply(title);
        }
    });
    actionsEl.appendChild(acceptBtn);

    note.appendChild(actionsEl);

    // ---- 30 秒定时进度条 + 自动应用标题 ----
    const startTime = Date.now();

    // 每 100ms 更新一次进度条
    const progressInterval = setInterval(() => {
        const elapsed = Date.now() - startTime;
        const pct = Math.min(elapsed / TIMER_DURATION, 1);
        progressFill.style.width = (pct * 100) + '%';
    }, 100);

    // 30 秒后自动应用标题
    const autoAcceptTimer = setTimeout(() => {
        clearInterval(progressInterval);
        // 进度条填满
        progressFill.style.width = '100%';
        // 离场动画后移除
        note.classList.add('leaving');
        note.addEventListener('animationend', () => {
            note.remove();
            if (ctn.children.length === 0) {
                ctn.remove();
                container = null;
            }
        }, { once: true });
        if (typeof onApply === 'function') {
            onApply(title);
        }
    }, TIMER_DURATION);

    // 添加到容器
    ctn.appendChild(note);

    return note;
}

/**
 * 清除所有便利贴（用于切换会话时清理）
 */
export function clearAllStickyNotes() {
    if (container) {
        container.remove();
        container = null;
    }
    // 清理监听器
    if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = null;
    }
    window.removeEventListener('resize', updatePosition);
}
