// ============================================================
// BrainOnline AI 助手 — 主入口
// 导入各功能模块并完成初始化
// ============================================================

import { state } from './chat-state.js';
import { switchHighlightTheme } from './chat-markdown.js';
import { initDom, dom, showWelcomeMessage } from './chat-ui.js';
import { initTickNav, updateTickNav } from './chat-ticknav.js';
import { sendMessage } from './chat-sse.js';
import { initCopyHandlers } from './chat-copy.js';
import { initDeleteModal } from './chat-delete.js';
import { restoreSession } from './chat-session.js';

'use strict';

// ============================================================
// 初始化 DOM 引用
// ============================================================
initDom();

// ============================================================
// 切换按钮状态（深度思考 / 智能搜索）
// ============================================================

const deepThinkBtn = document.getElementById('deepThinkBtn');
const webSearchBtn = document.getElementById('webSearchBtn');

/**
 * toggleButton 切换按钮的选中/未选中状态
 * @param {HTMLElement} btn - 按钮元素
 * @param {boolean} active - 是否选中
 */
function toggleButton(btn, active) {
    btn.dataset.active = active ? 'true' : 'false';
}

// 深度思考按钮点击
deepThinkBtn.addEventListener('click', () => {
    state.deepThinkActive = !state.deepThinkActive;
    toggleButton(deepThinkBtn, state.deepThinkActive);
});

// 智能搜索按钮点击
webSearchBtn.addEventListener('click', () => {
    state.webSearchActive = !state.webSearchActive;
    toggleButton(webSearchBtn, state.webSearchActive);
});

// ============================================================
// 主题切换
// ============================================================

const themeToggle = document.getElementById('themeToggle');

// 从 localStorage 读取已保存的主题，首次使用默认为 'light'（亮色）
const savedTheme = localStorage.getItem('brainonline-theme') || 'light';
document.documentElement.setAttribute('data-theme', savedTheme);
updateThemeButton(savedTheme);
switchHighlightTheme(savedTheme);

themeToggle.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme') || 'dark';
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('brainonline-theme', next);
    updateThemeButton(next);
    switchHighlightTheme(next);
});

function updateThemeButton(theme) {
    themeToggle.innerHTML = theme === 'dark'
        ? `<svg class="theme-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
        </svg>`
        : `<svg class="theme-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="5"/>
            <line x1="12" y1="1" x2="12" y2="3"/>
            <line x1="12" y1="21" x2="12" y2="23"/>
            <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/>
            <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/>
            <line x1="1" y1="12" x2="3" y2="12"/>
            <line x1="21" y1="12" x2="23" y2="12"/>
            <line x1="4.22" y1="19.78" x2="5.64" y2="18.32"/>
            <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>
        </svg>`;
    themeToggle.title = theme === 'dark' ? '切换到亮色主题' : '切换到暗色主题';
}

// ============================================================
// 初始化：自动调整 textarea 高度
// ============================================================

const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const sendModeToggle = document.getElementById('sendModeToggle');
const sendModeLabel = document.getElementById('sendModeLabel');

messageInput.addEventListener('input', () => {
    messageInput.style.height = 'auto';
    messageInput.style.height = Math.min(messageInput.scrollHeight, 120) + 'px';
});

// 发送模式标签文本
const SEND_MODE_LABELS = {
    normal: '回车键发送，Shift+回车键换行',
    alternate: '回车键换行，Shift+回车键发送'
};

// 更新发送模式标签
function updateSendModeLabel() {
    sendModeLabel.textContent = state.sendModeAlternate
        ? SEND_MODE_LABELS.alternate
        : SEND_MODE_LABELS.normal;
}

// 滑块切换发送模式
sendModeToggle.addEventListener('change', () => {
    state.sendModeAlternate = sendModeToggle.checked;
    updateSendModeLabel();
});

// 键盘发送/换行逻辑
messageInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        if (state.sendModeAlternate) {
            // 模式二: Enter换行, Shift+Enter发送
            if (e.shiftKey) {
                e.preventDefault();
                sendMessage();
            }
            // Enter 不阻止默认行为，即换行
        } else {
            // 模式一: Enter发送, Shift+Enter换行
            if (!e.shiftKey) {
                e.preventDefault();
                sendMessage();
            }
        }
    }
});

sendBtn.addEventListener('click', sendMessage);

// 附件按钮 — 点击弹出文件选择框
const attachBtn = document.getElementById('attachBtn');
const fileInput = document.getElementById('fileInput');

attachBtn.addEventListener('click', () => {
    fileInput.click();
});

// 文件选择后的处理
fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
        // 目前仅做选择演示，后续可扩展上传逻辑
        const names = Array.from(fileInput.files).map(f => f.name).join(', ');
        console.log('已选择文件:', names);
    }
    // 重置以便重复选择同一文件
    fileInput.value = '';
});

// ============================================================
// 初始化各功能模块
// ============================================================

// 初始化刻度导航事件绑定
initTickNav();

// 初始化复制按钮和消息操作按钮的事件委托
initCopyHandlers();

// 初始化删除模态框事件绑定
initDeleteModal();

// 页面加载后先恢复会话
window.addEventListener('DOMContentLoaded', restoreSession);
