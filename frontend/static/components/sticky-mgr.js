// ============================================================
// sticky-mgr.js — 便利贴通用管理器
// 负责便利贴容器的创建、定位、折叠/恢复、清理等通用逻辑
// ============================================================
// 使用方式：
//   import { getContainer, clearAllStickyNotes } from './sticky-mgr.js';
//   const ctn = getContainer();
//   // ... 在容器中添加自定义便利贴 ...
// ============================================================

'use strict';

/** 便利贴容器（单例，延迟创建） */
let container = null;

/** 用于监听 .main-content 尺寸变化的 ResizeObserver（单例） */
let resizeObserver = null;

/**
 * 更新便利贴容器的位置，使其右边缘位于 main-body 右边沿左侧 64px 处，
 * 为右侧的刻度导航（tick-nav）留出刻度线和刻度值的显示空间。
 */
function updatePosition() {
    if (!container) return;
    const mainBody = document.querySelector('.main-body');
    if (!mainBody) {
        container.style.right = '16px';
        container.style.transform = '';
        return;
    }

    const mbRect = mainBody.getBoundingClientRect();
    const vw = window.innerWidth;
    const rightVal = vw - (mbRect.right - 64);
    container.style.right = rightVal + 'px';
    container.style.transform = '';
}

/**
 * 初始化位置监听：监听窗口 resize 和 .main-content 尺寸变化（侧边栏切换时）。
 */
function initPositionWatcher() {
    if (resizeObserver) return;

    window.addEventListener('resize', updatePosition);
    const mainContent = document.querySelector('.main-content');
    if (mainContent) {
        resizeObserver = new ResizeObserver(() => { updatePosition(); });
        resizeObserver.observe(mainContent);
    }
}

/**
 * 获取或创建便利贴容器 DOM 元素。
 * 容器使用 position:fixed 右对齐到对话框（scrollContainer）右边沿。
 * @returns {HTMLElement}
 */
export function getContainer() {
    if (!container) {
        container = document.createElement('div');
        container.className = 'sticky-note-container';
        document.body.appendChild(container);
        initPositionWatcher();
        updatePosition();
    }
    return container;
}

/**
 * 折叠便利贴：只显示标题，隐藏消息、推荐标题、按钮行和恢复按钮。
 * @param {HTMLElement} note - 便利贴 DOM 元素
 */
export function collapseNote(note) {
    if (note.classList.contains('collapsed') || note.classList.contains('leaving')) return;

    note.classList.add('collapsing');
    note.addEventListener('animationend', () => {
        note.classList.remove('collapsing');
        note.classList.add('collapsed');
    }, { once: true });
}

/**
 * 恢复便利贴：从折叠状态展开，显示完整内容。
 * @param {HTMLElement} note - 便利贴 DOM 元素
 */
export function restoreNote(note) {
    if (!note.classList.contains('collapsed')) return;

    note.classList.remove('collapsed');
    const collapsedTitle = note.querySelector('.sticky-note-collapsed-title');
    if (collapsedTitle) {
        collapsedTitle.remove();
    }
}

/**
 * 清除所有便利贴（用于切换会话时清理）
 */
export function clearAllStickyNotes() {
    if (container) {
        container.remove();
        container = null;
    }
    if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = null;
    }
    window.removeEventListener('resize', updatePosition);
}
