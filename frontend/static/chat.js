// ============================================================
// 脑力永生 AI 助手 — 主入口
// 导入各功能模块并完成初始化
// ============================================================

import { state, UserSettings } from './chat-state.js';
import { switchHighlightTheme } from './chat-markdown.js';
import { initDom, dom, showWelcomeMessage } from './chat-ui.js';
import { initTickNav, updateTickNav } from './chat-ticknav.js';
import { sendMessage } from './chat-sse.js';
import { initCopyHandlers } from './chat-copy.js';
import { initDeleteModal } from './chat-delete.js';
import { restoreSession } from './chat-session.js';

'use strict';

// ============================================================
// 从 cookie 加载用户设置
// ============================================================
UserSettings.load();

// ============================================================
// 主题映射工具
// ============================================================
// UserSettings.theme: 0=明亮, 1=暗色, 2=跟随系统（保留值，未来系统设置中使用）
const THEME_VALUES = ['light', 'dark', 'system'];

function resolveTheme(theme) {
    if (theme === 2) {
        // 跟随系统
        return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    return THEME_VALUES[theme] || 'light';
}

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

// ---- 从 UserSettings 恢复按钮状态 ----
state.deepThinkActive = UserSettings.deepThink;
toggleButton(deepThinkBtn, state.deepThinkActive);

state.webSearchActive = UserSettings.webSearch;
toggleButton(webSearchBtn, state.webSearchActive);

// 深度思考按钮点击
deepThinkBtn.addEventListener('click', () => {
    state.deepThinkActive = !state.deepThinkActive;
    toggleButton(deepThinkBtn, state.deepThinkActive);
    UserSettings.deepThink = state.deepThinkActive;
    UserSettings.save();
});

// 智能搜索按钮点击
webSearchBtn.addEventListener('click', () => {
    state.webSearchActive = !state.webSearchActive;
    toggleButton(webSearchBtn, state.webSearchActive);
    UserSettings.webSearch = state.webSearchActive;
    UserSettings.save();
});

// ============================================================
// 主题切换
// ============================================================

const themeToggle = document.getElementById('themeToggle');

/** 应用主题到页面 */
function applyTheme(themeVal) {
    const themeStr = resolveTheme(themeVal);
    document.documentElement.setAttribute('data-theme', themeStr);
    updateThemeButton(themeStr);
    switchHighlightTheme(themeStr);
}

// 初始化主题
applyTheme(UserSettings.theme);

themeToggle.addEventListener('click', () => {
    // 主页切换仅在 亮(0) 和 暗(1) 之间切换，跳过 跟随系统(2)
    UserSettings.theme = UserSettings.theme === 0 ? 1 : 0;
    applyTheme(UserSettings.theme);
    UserSettings.save();
});

function updateThemeButton(themeStr) {
    themeToggle.innerHTML = themeStr === 'dark'
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
    themeToggle.title = themeStr === 'dark' ? '切换到亮色主题' : '切换到暗色主题';
}

// ============================================================
// 左侧面板切换
// ============================================================

const panelToggle = document.getElementById('panelToggle');
const panelToggleInner = document.getElementById('panelToggleInner');
const headerMenuBtn = document.getElementById('headerMenuBtn');

function toggleLeftPanel() {
    document.body.classList.toggle('left-panel-visible');
}

if (panelToggle) {
    panelToggle.addEventListener('click', toggleLeftPanel);
}
if (panelToggleInner) {
    panelToggleInner.addEventListener('click', toggleLeftPanel);
}
if (headerMenuBtn) {
    headerMenuBtn.addEventListener('click', toggleLeftPanel);
}

// ============================================================
// 初始化：自动调整 textarea 高度
// ============================================================

const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const sendModeToggle = document.getElementById('sendModeToggle');
const sendModeLabel = document.getElementById('sendModeLabel');

// ---- 从 UserSettings 恢复发送模式 ----
// sendMode: 0=Enter发送, 1=Enter换行
state.sendModeAlternate = UserSettings.sendMode === 1;
sendModeToggle.checked = state.sendModeAlternate;

messageInput.addEventListener('input', () => {
    messageInput.style.height = 'auto';
    messageInput.style.height = Math.min(messageInput.scrollHeight, 120) + 'px';
});

// 发送模式标签文本
const SEND_MODE_LABELS = {
    normal: "回车键发送 ⇄ Shift+回车键",
    alternate: "Shift+回车键'发送 ⇄ 回车键"
};

// 换行提示文本
const NEWLINE_HINT_LABELS = {
    normal: '换行：Shift+回车键',
    alternate: '换行：回车键'
};

const newlineHint = document.getElementById('newlineHint');

// 更新发送模式标签
function updateSendModeLabel() {
    sendModeLabel.textContent = state.sendModeAlternate
        ? SEND_MODE_LABELS.alternate
        : SEND_MODE_LABELS.normal;
    // 同步更新换行提示
    if (newlineHint) {
        newlineHint.textContent = state.sendModeAlternate
            ? NEWLINE_HINT_LABELS.alternate
            : NEWLINE_HINT_LABELS.normal;
    }
}

// 滑块切换发送模式
sendModeToggle.addEventListener('change', () => {
    state.sendModeAlternate = sendModeToggle.checked;
    updateSendModeLabel();
    UserSettings.sendMode = state.sendModeAlternate ? 1 : 0;
    UserSettings.save();
});

// 点击标签文本也可切换发送模式
sendModeLabel.addEventListener('click', () => {
    sendModeToggle.checked = !sendModeToggle.checked;
    sendModeToggle.dispatchEvent(new Event('change'));
});

// 初始化发送模式标签（从 JS 常量设置初始文本，避免 HTML 中重复定义）
updateSendModeLabel();

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
// 开启新对话按钮
// ============================================================

const newSessionBtn = document.getElementById('newSessionBtn');

newSessionBtn.addEventListener('click', async () => {
    if (state.isStreaming) {
        // 如果正在流式输出，先中止
        if (state.abortController) {
            state.abortController.abort();
        }
    }

    try {
        const response = await fetch('/api/session/new', {
            method: 'POST',
        });

        if (!response.ok) {
            console.error('创建新会话失败:', response.status);
            return;
        }

        // 重新加载页面以重置所有状态
        window.location.reload();
    } catch (e) {
        console.error('创建新会话出错:', e);
    }
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

// ============================================================
// 输入面板自动折叠 — 滚动刻度变化时折叠，聚焦/输入时恢复
// ============================================================

(function initInputCollapse() {
    const chatContainer = document.getElementById('chatContainer');
    const inputArea = document.querySelector('.input-area');
    const messageInput = document.getElementById('messageInput');

    if (!chatContainer || !inputArea || !messageInput) return;

    /** 是否已折叠 */
    let isCollapsed = false;

    /** 上一次记录的 activeTickIndex，用于检测刻度变化 */
    let lastActiveTickIndex = state.activeTickIndex;

    /**
     * 折叠输入面板（隐藏 send-mode-corner 和 input-footer）
     */
    function collapseInputArea() {
        if (isCollapsed) return;
        isCollapsed = true;
        inputArea.classList.add('collapsed');
    }

    /**
     * 恢复输入面板（显示所有内容）
     */
    function restoreInputArea() {
        if (!isCollapsed) return;
        isCollapsed = false;
        inputArea.classList.remove('collapsed');
    }

    // ---- 滚动检测：当滚动刻度变化时折叠；滚动到底部时展开 ----
    //     优先级：AI 正在回答时始终折叠 > 滚动到底部展开 > 刻度变化折叠
    //     注意：必须节流（200ms），确保在 chat-ticknav.js 的 updateActiveTickOnScroll
    //     （150ms 节流）更新 activeTickIndex 之后再执行，否则检测不到刻度变化。
    let scrollThrottleTimer = null;
    chatContainer.addEventListener('scroll', () => {
        if (scrollThrottleTimer) return;
        scrollThrottleTimer = setTimeout(() => {
            scrollThrottleTimer = null;

            const currentTickIndex = state.activeTickIndex;

            // 最高优先级：AI 正在回答时始终折叠输入面板
            if (state.isStreaming) {
                collapseInputArea();
                return;
            }

            // 检测是否已滚动到底部（向下无可再滚）
            const isAtBottom = chatContainer.scrollHeight - chatContainer.scrollTop - chatContainer.clientHeight < 8;

            if (isAtBottom) {
                // 滚动到底部时自动展开
                restoreInputArea();

                // 展开后页面高度可能变化（输入面板展开），延迟再滚一次到底部
                setTimeout(() => {
                    chatContainer.scrollTop = chatContainer.scrollHeight;
                }, 500);
            } else {
                // 刻度变化时折叠，并记住新刻度
                if (currentTickIndex !== lastActiveTickIndex) {
                    lastActiveTickIndex = currentTickIndex;
                    // 已折叠时不再重复触发
                    if (!isCollapsed) {
                        collapseInputArea();
                    }
                }
            }
        }, 200);
    });

    // ---- 恢复条件 1：输入框获得焦点 ----
    messageInput.addEventListener('focus', restoreInputArea);

    // ---- 恢复条件 2：输入框内容变化（用户开始输入） ----
    messageInput.addEventListener('input', restoreInputArea);

    // ---- 恢复条件 3：点击输入区域任意位置 ----
    inputArea.addEventListener('click', (e) => {
        if (isCollapsed) {
            messageInput.focus();
        }
    });

    // ---- 当发送消息后恢复 ----
    const sendBtn = document.getElementById('sendBtn');
    if (sendBtn) {
        sendBtn.addEventListener('click', restoreInputArea);
    }

    console.log('[input-collapse] 输入面板自动折叠功能已初始化（基于滚动刻度变化）');
})();
