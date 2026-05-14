// ============================================================
// 脑力永恒 AI 助手 — 主入口
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
// startNewSession — 开启新对话（无刷新 SPA 方式）
// 清空当前会话的所有历史消息，进入欢迎状态
// ============================================================

async function startNewSession() {
    // 如果正在流式输出，先中止
    if (state.isStreaming && state.abortController) {
        state.abortController.abort();
    }

    try {
        const response = await fetch('/api/session/new', {
            method: 'POST',
        });

        if (!response.ok) {
            console.error('创建新会话失败:', response.status);
            return;
        }

        // ---- 无刷新重置前端状态 ----

        // 1. 清空消息状态及相关计数器
        state.messages = [];
        state.userMsgCount = 0;
        state.activeTickIndex = -1;
        state.tickScrollOffset = 0;
        state.currentGroup = null;
        state.accumulatedMarkdown = '';
        if (state.renderTimer) {
            clearTimeout(state.renderTimer);
            state.renderTimer = null;
        }

        // 2. 移除所有消息 DOM 节点（.message-group）
        const chatContainer = document.getElementById('chatContainer');
        if (chatContainer) {
            chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
        }

        // 3. 移除已有的欢迎消息（如果有）
        const existingWelcome = document.querySelector('.welcome-message');
        if (existingWelcome) {
            // 将 input-area 移回原来的位置（main-body 之后）
            const inputArea = existingWelcome.querySelector('.input-area');
            if (inputArea) {
                const mainBody = document.getElementById('mainBody');
                if (mainBody && mainBody.nextElementSibling?.classList?.contains('input-area')) {
                    // input-area 已经在正确位置，不需要移动
                } else if (mainBody) {
                    // 将 input-area 插入到 mainBody 之后
                    mainBody.parentNode.insertBefore(inputArea, mainBody.nextSibling);
                }
            }
            existingWelcome.remove();
        }

        // 4. 清空刻度导航
        const tickNav = document.getElementById('tickNav');
        if (tickNav) {
            tickNav.innerHTML = '';
        }

        // 5. 移除 welcome-state 标记
        const scrollContainer = document.getElementById('scrollContainer');
        if (scrollContainer) {
            scrollContainer.classList.remove('welcome-state');
        }

        // 6. 重新显示欢迎消息（会设置标题为"欢迎开始新对话"）
        showWelcomeMessage();
    } catch (e) {
        console.error('创建新会话出错:', e);
    }
}

// ============================================================
// 新建对话按钮（主栏顶部图标）
// ============================================================

const newChatBtn = document.getElementById('newChatBtn');

newChatBtn.addEventListener('click', startNewSession);

// ============================================================
// 左栏切换 + 品牌迁移逻辑（参照 demo.html 实现）
// ============================================================

const leftSidebar = document.getElementById('leftSidebar');
const sidebarCloseBtn = document.getElementById('sidebarCloseBtn');
const leftBrandContainer = document.getElementById('leftBrandContainer');
const mainBrandContainer = document.getElementById('mainBrandContainer');

const MIN_BOTH = 920;     // 宽屏左栏必须宽度≥800px才能保持双栏显示
const SMALL_BP = 768;     // 小屏模式阈值

let isLeftVisible = false;    // 宽屏模式下左栏是否可见 (hidden class 控制)
let autoHidden = false;       // 自动隐藏标记
let isSmallMode = false;      // 当前是否小屏模式
let isDrawerOpen = false;     // 小屏模式下抽屉是否打开

// ----- 全局切换按钮 (唯一实例，避免重复绑定) -----
let globalToggleButton = null;

// 切换按钮 SVG（复用现有 .panel-toggle 风格）
const TOGGLE_BTN_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="1" y="2" width="14" height="12" rx="1"/><line x1="5" y1="2" x2="5" y2="14"/></svg>';

// ===== 品牌文本常量（方便以后修改） =====
const BRAND_TITLE = '脑力永恒';
const BRAND_SUBTITLE = '基于 RAG 知识库的智能对话';

// 创建品牌元素（Logo + 主标题 + 副标题）
function createBrandElement() {
    const container = document.createElement('div');
    container.className = 'brand-container';
    container.style.display = 'flex';
    container.style.alignItems = 'center';
    container.style.gap = '12px';

    // Logo
    const logo = document.createElement('img');
    logo.className = 'brand-logo';
    logo.src = '/static/brain-forever.svg';
    logo.alt = '脑力永恒';

    // 标题/副标题容器
    const textDiv = document.createElement('div');
    textDiv.className = 'brand-text';

    const title = document.createElement('h1');
    title.className = 'brand-title';
    title.textContent = BRAND_TITLE;

    const subtitle = document.createElement('p');
    subtitle.className = 'brand-subtitle';
    subtitle.textContent = BRAND_SUBTITLE;

    textDiv.appendChild(title);
    textDiv.appendChild(subtitle);

    container.appendChild(logo);
    container.appendChild(textDiv);

    return container;
}

// 创建/获取切换按钮 (单例，事件绑定一次)
function getToggleButton() {
    if (!globalToggleButton) {
        globalToggleButton = document.createElement('button');
        globalToggleButton.className = 'menu-toggle-btn';
        globalToggleButton.setAttribute('aria-label', '切换侧边栏');
        globalToggleButton.title = '切换侧边栏';
        globalToggleButton.innerHTML = TOGGLE_BTN_SVG;
        // 绑定统一切换逻辑
        globalToggleButton.addEventListener('click', (e) => {
            e.stopPropagation();
            toggleSidebarMaster();
        });
    }
    return globalToggleButton;
}

// 核心切换逻辑: 宽屏切换 isLeftVisible，小屏切换抽屉开关
function toggleSidebarMaster() {
    if (isSmallMode) {
        // 小屏模式: 切换抽屉状态
        if (isDrawerOpen) closeDrawer();
        else openDrawer();
    } else {
        // 宽屏模式: 切换双栏显隐
        if (isLeftVisible) hideByUser();
        else attemptShow(true);
    }
}

// 小屏打开抽屉
function openDrawer() {
    if (!isSmallMode) return;
    leftSidebar.classList.add('drawer-open');
    isDrawerOpen = true;
    updateUI();
    updateBrandLayout();
}

function closeDrawer() {
    if (!isSmallMode) return;
    leftSidebar.classList.remove('drawer-open');
    isDrawerOpen = false;
    updateUI();
    updateBrandLayout();
}

// 宽屏显隐逻辑
function hideByUser() {
    if (isSmallMode) return;
    if (isLeftVisible) {
        isLeftVisible = false;
        autoHidden = false;
        syncDual();
        updateBrandLayout();
    }
}

function attemptShow(user = true) {
    if (isSmallMode) return;
    const w = getW();
    if (w >= MIN_BOTH) {
        if (!isLeftVisible) {
            isLeftVisible = true;
            syncDual();
            updateBrandLayout();
            if (user) { autoHidden = false; }
            else { autoHidden = false; }
        }
        return true;
    }
    return false;
}

// 强制关闭（宽屏宽度不足时）
function forceClose() {
    if (isSmallMode) return;
    if (isLeftVisible && getW() < MIN_BOTH) {
        isLeftVisible = false;
        autoHidden = true;
        syncDual();
        updateBrandLayout();
    }
}

// 自动恢复检测
function enforceAuto() {
    if (isSmallMode) return;
    const w = getW();
    if (isLeftVisible && w < MIN_BOTH) forceClose();
    if (!isLeftVisible && autoHidden && w >= MIN_BOTH) attemptShow(false);
}

// 同步 leftSidebar hidden 样式
function syncDual() {
    if (isSmallMode) return;
    if (isLeftVisible) leftSidebar.classList.remove('hidden');
    else leftSidebar.classList.add('hidden');
    updateUI();
}

// 更新状态（目前仅用于调试，可扩展）
function updateUI() {
    // 预留：可在此更新状态显示
}

// ********** 关键: 动态品牌布局 **********
// 规则:
// - 宽屏模式下，isLeftVisible === true   => 品牌渲染在 leftBrandContainer
// - 宽屏模式下，isLeftVisible === false  => 品牌渲染在 mainBrandContainer
// - 小屏模式下，isDrawerOpen === true     => 品牌渲染在 leftBrandContainer
// - 小屏模式下，isDrawerOpen === false    => 主栏只显示切换按钮 + 对话标题（品牌隐藏）
function updateBrandLayout() {
    let sidebarVisible = false;
    if (isSmallMode) {
        sidebarVisible = isDrawerOpen;
    } else {
        sidebarVisible = isLeftVisible;
    }

    const toggleButton = getToggleButton();
    const mainHeader = document.querySelector('.main-header');
    const newChatBtn = document.getElementById('newChatBtn');
    const sidebarNewChatArea = document.getElementById('sidebarNewChatArea');

    if (sidebarVisible) {
        // 品牌展示在左侧栏头部
        mainBrandContainer.innerHTML = '';
        leftBrandContainer.innerHTML = '';
        const brandElem = createBrandElement();
        leftBrandContainer.appendChild(brandElem);
        if (isSmallMode) {
            // 小屏模式（抽屉打开）：左侧栏已有关闭按钮（sidebar-close-btn），
            // 不再需要切换按钮，避免重复
            if (toggleButton.parentNode) toggleButton.remove();
            toggleButton.style.display = 'none';
        } else {
            // 宽屏模式：切换按钮放在左侧栏品牌区
            if (toggleButton.parentNode) toggleButton.remove();
            leftBrandContainer.appendChild(toggleButton);
            toggleButton.style.display = 'inline-flex';
        }
        leftBrandContainer.style.display = 'flex';
        leftBrandContainer.style.alignItems = 'center';
        leftBrandContainer.style.gap = '12px';
        leftBrandContainer.style.flex = '1';
        leftBrandContainer.style.flexWrap = 'wrap';

        // 新对话按钮移到侧边栏 header（切换按钮右侧）
        if (newChatBtn && sidebarNewChatArea) {
            if (newChatBtn.parentNode) newChatBtn.remove();
            sidebarNewChatArea.appendChild(newChatBtn);
            newChatBtn.style.display = 'inline-flex';
        }
    } else {
        // 品牌展示在主栏
        leftBrandContainer.innerHTML = '';
        mainBrandContainer.innerHTML = '';
        if (isSmallMode) {
            // 小屏模式：主栏只放切换按钮（品牌由 CSS 隐藏），
            // 切换按钮直接插入 .main-header 最前面
            if (toggleButton.parentNode) toggleButton.remove();
            mainHeader.insertBefore(toggleButton, mainHeader.firstChild);
            toggleButton.style.display = 'inline-flex';
            mainBrandContainer.style.display = 'none';
        } else {
            // 宽屏模式（左栏隐藏）：品牌 + 切换按钮放在 mainBrandContainer
            const brandElem = createBrandElement();
            mainBrandContainer.appendChild(brandElem);
            if (toggleButton.parentNode) toggleButton.remove();
            mainBrandContainer.appendChild(toggleButton);
            toggleButton.style.display = 'inline-flex';
            mainBrandContainer.style.display = 'flex';
            mainBrandContainer.style.alignItems = 'center';
            mainBrandContainer.style.gap = '12px';
        }

        // 新对话按钮移回主栏 header（mainBrandContainer 之后，main-title 之前）
        if (newChatBtn && mainHeader) {
            if (newChatBtn.parentNode) newChatBtn.remove();
            const mainTitle = document.getElementById('headerTitle');
            mainHeader.insertBefore(newChatBtn, mainTitle);
            newChatBtn.style.display = 'inline-flex';
        }
    }

}

// 响应 resize 切换模式 (小屏/宽屏)
function switchMode() {
    const w = getW();
    const newSmall = w < SMALL_BP;
    if (newSmall === isSmallMode) {
        if (!isSmallMode) enforceAuto();
        updateUI();
        updateBrandLayout();
        return;
    }
    // 模式切换
    if (newSmall) {
        // 进入小屏模式
        document.body.classList.add('small-screen-mode');
        isSmallMode = true;
        isLeftVisible = false;
        autoHidden = false;
        isDrawerOpen = false;
        leftSidebar.classList.remove('hidden');
        leftSidebar.classList.remove('drawer-open');
        updateUI();
        updateBrandLayout();
    } else {
        // 退出小屏模式, 进入宽屏
        document.body.classList.remove('small-screen-mode');
        isSmallMode = false;
        isDrawerOpen = false;
        leftSidebar.classList.remove('drawer-open');
        isLeftVisible = false;
        autoHidden = false;
        syncDual();
        enforceAuto();
        updateBrandLayout();
        updateUI();
    }
}

function getW() { return window.innerWidth; }

// 监听 sidebarCloseBtn (小屏专用关闭按钮)
if (sidebarCloseBtn) {
    sidebarCloseBtn.addEventListener('click', () => {
        if (isSmallMode && isDrawerOpen) {
            closeDrawer();
        } else if (!isSmallMode) {
            if (isLeftVisible) hideByUser();
        }
    });
}

// 监听窗口resize
let resizeTimer;
window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
        switchMode();
        if (!isSmallMode) {
            enforceAuto();
        }
        updateUI();
        updateBrandLayout();
    }, 60);
});

// 初始化
(function initSidebar() {
    const w = getW();
    if (w < SMALL_BP) {
        document.body.classList.add('small-screen-mode');
        isSmallMode = true;
        isLeftVisible = false;
        isDrawerOpen = false;
        leftSidebar.classList.remove('hidden');
        leftSidebar.classList.remove('drawer-open');
        updateUI();
        updateBrandLayout();
    } else {
        document.body.classList.remove('small-screen-mode');
        isSmallMode = false;
        isLeftVisible = false;
        autoHidden = false;
        isDrawerOpen = false;
        syncDual();
        enforceAuto();
        updateBrandLayout();
        updateUI();
    }
})();

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
    alternate: "Shift+回车键发送 ⇄ 回车键"
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
    const chatContainer = document.getElementById('scrollContainer');
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
