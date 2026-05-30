// ============================================================
// 第2大脑 AI 助手 — 主入口
// 导入各功能模块并完成初始化
// ============================================================

import { switchHighlightTheme } from './chat-markdown.js';
import { initDom, dom, showWelcomeMessage, updateHeaderTitle, showToast, collapseInputArea, restoreInputArea, isInputCollapsed, isScrolledToBottom } from './chat-ui.js';
import { initTickNav, updateTickNav } from './chat-ticknav.js';
import { initTooltip } from './components/tooltip.js';
import { sendMessage } from './chat-sse.js';
import { initCopyHandlers } from './chat-copy.js';
import { initDeleteModal } from './dialogs/msg-delete-dialog.js';
import { initPage } from './chat-init.js';
import { clearAllStickyNotes } from './components/sticky-note.js';
import { fetchChatTitle, putChatTitle, TITLE_STATE, onChatLogin } from './chat-api.js';
import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
import { clearActiveChat, addDirtyChat } from './chat-list.js';
import {
    ICON_COPY, ICON_SEND, ICON_SPINNER, ICON_DELETE, ICON_GLOBE,
    ICON_MOON, ICON_SUN, ICON_ARROW_UP_DOWN, ICON_TOGGLE,
    ICON_AI_TITLE, ICON_EDIT, SVG_ATTRS,
    ICON_DOTS, ICON_CLOSE, ICON_PIN, ICON_STOP,
    ICON_ATTACH, ICON_NEW_CHAT, ICON_RESTORE, ICON_COPY_MSG
} from './svg_icons_re.js';
import { sessionManager } from './chat-session-manager.js';
import { activeTickIndex, setActiveTickIndex, tickScrollOffset, setTickScrollOffset, resetTickState } from './tick-state.js';

'use strict';

// 注意：图标常量已由 svg_icons.js（在 Alpine.js 之前加载的普通 <script>）
// 注册到 window 上，此处不再重复执行 Object.assign(window, ...)。

// ============================================================
// 从 cookie 加载用户设置
// ============================================================
Alpine.store('settings').load();

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
initTooltip();

// ============================================================
// 主题切换 — applyTheme 被 Alpine store toggleTheme() 通过
// 'theme-changed' 自定义事件触发
// ============================================================

/** 应用主题到页面 */
function applyTheme(themeVal) {
    const themeStr = resolveTheme(themeVal);
    document.documentElement.setAttribute('data-theme', themeStr);
    switchHighlightTheme(themeStr);
}

// 初始化主题
applyTheme(Alpine.store('settings').theme);

// 监听 Alpine store 发起的主题变更事件
document.addEventListener('theme-changed', (e) => {
    applyTheme(e.detail.theme);
});

// ============================================================
// AI 标题按钮 — 点击触发 AI 重新生成标题（防抖动：5 秒内只生效一次）
// SVG 图标已在 HTML 中通过 Alpine 渲染，此处仅处理点击逻辑
// ============================================================
const aiTitleBtn = document.getElementById('aiTitleBtn');
if (aiTitleBtn) {
    let aiTitleDebounceTimer = null;
    aiTitleBtn.addEventListener('click', () => {
        // 正在 SSE 流式输出时，AI 标题按钮不可用（Alpine :disabled 已处理，再加一层防御）
        if (sessionManager.isStreaming) {
            return;
        }
        if (aiTitleDebounceTimer) {
            return;
        }
        showToast('已向 AI 发出请求……', 'info');
        // 使用当前对话标题作为 originalTitle 传给后端
        // force=true 忽略 titleState 守卫，强制请求 AI 重新生成标题
        // 传递当前 chat SN，确保后端返回时前端能精确定位到正确的对话
        const activeChat = window.Alpine.store('chats').active;
        const originalTitle = (activeChat && activeChat.title) || '';
        fetchChatTitle(originalTitle, true, activeChat ? activeChat.sn : '');
        // 设置防抖定时器，5 秒内不再响应点击
        aiTitleDebounceTimer = setTimeout(() => {
            aiTitleDebounceTimer = null;
        }, 5000);
    });
}

// ============================================================
// startNewSession — 开启新对话（无刷新 SPA 方式）
// 清空当前会话的所有历史消息，进入欢迎状态
// ============================================================

async function startNewSession() {
    // ---- 纯前端重置状态，不再调用后端 ----

    var chatsStore = window.Alpine.store('chats');

    // 1. 重置为空白对话状态：activeIndex = -1，创建 blankItem
    //    SN 暂时为空，用户发出第一条消息时由 sendMessage() 中的 newChat() 分配
    //    Alpine 响应式模板自动隐藏消息组、显示欢迎消息
    chatsStore.resetToBlank();
    resetTickState();

    // 2. 清空刻度导航
    const tickNav = document.getElementById('tickNav');
    if (tickNav) {
        tickNav.innerHTML = '';
    }

    // 3. 清除所有便利贴（新会话不需要旧的标题推荐）
    clearAllStickyNotes();

    // 4. 清除左侧栏对话列表的选中状态
    clearActiveChat();

    // 5. 设置欢迎消息文本（Alpine 响应式模板自动显示）
    showWelcomeMessage();

    // 6. 确保输入面板展开并同步内部折叠状态
    const msgInput = document.getElementById('messageInput');
    if (msgInput) {
        msgInput.focus();
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

const MIN_BOTH = 920;     // 宽屏左栏必须宽度≥920px才能保持双栏显示
const SMALL_BP = 920;     // 小屏模式阈值（与 MIN_BOTH 一致，避免 iPad 竖屏落入灰色地带）

let isLeftVisible = false;    // 宽屏模式下左栏是否可见 (hidden class 控制)
let autoHidden = false;       // 自动隐藏标记
let isSmallMode = false;      // 当前是否小屏模式
let isDrawerOpen = false;     // 小屏模式下抽屉是否打开

// ----- 全局切换按钮 (唯一实例，避免重复绑定) -----
let globalToggleButton = null;

// ----- 全局竖向分隔线 (唯一实例) -----
let globalHeaderDivider = null;

// 切换按钮 SVG（复用现有 .panel-toggle 风格）
const TOGGLE_BTN_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' + ICON_TOGGLE + '</svg>';

// ===== 品牌文本常量（方便以后修改） =====
const BRAND_TITLE = '第2大脑';
const BRAND_SUBTITLE = '比你更懂你' // '养育我的第2大脑'

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
    logo.alt = '第2大脑';

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
        globalToggleButton.className = 'icon-btn icon-btn--small menu-toggle-btn';
        globalToggleButton.setAttribute('aria-label', '切换侧边栏');
        globalToggleButton.dataset.tooltip = '切换侧边栏';
        globalToggleButton.innerHTML = TOGGLE_BTN_SVG;
        // 绑定统一切换逻辑
        globalToggleButton.addEventListener('click', (e) => {
            e.stopPropagation();
            toggleSidebarMaster();
        });
    }
    return globalToggleButton;
}

// 创建/获取竖向分隔线 (单例)
function getHeaderDivider() {
    if (!globalHeaderDivider) {
        globalHeaderDivider = document.createElement('div');
        globalHeaderDivider.className = 'header-divider';
    }
    return globalHeaderDivider;
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

    // ----- 竖向分隔线管理 -----
    const divider = getHeaderDivider();

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

        // 侧栏可见时移除竖向分隔线
        if (divider.parentNode) divider.remove();
    } else {
        // 品牌展示在主栏
        leftBrandContainer.innerHTML = '';
        mainBrandContainer.innerHTML = '';
        if (isSmallMode) {
            // 小屏模式（抽屉关闭）：
            // - 顺序：logo -> 切换按钮 -> 分隔线 -> 新对话 -> 标题
            // - 中屏（>480px）时 mainBrandContainer 显示一个 32×32 logo
            // - 超小屏（≤480px）时由 CSS 隐藏 mainBrandContainer

            // 先把 mainBrandContainer（含 logo）放到最前面
            if (mainBrandContainer.parentNode) mainBrandContainer.remove();
            mainHeader.insertBefore(mainBrandContainer, mainHeader.firstChild);
            mainBrandContainer.innerHTML = '';
            mainBrandContainer.style.display = 'flex';
            mainBrandContainer.style.alignItems = 'center';
            mainBrandContainer.style.gap = '0';

            // 创建仅含 logo 的品牌元素（32×32，无文字）
            const logoOnly = document.createElement('img');
            logoOnly.className = 'brand-logo brand-logo-compact';
            logoOnly.src = '/static/brain-forever.svg';
            logoOnly.alt = '第2大脑';
            logoOnly.style.width = '32px';
            logoOnly.style.height = '32px';
            logoOnly.style.borderRadius = '12px';
            mainBrandContainer.appendChild(logoOnly);

            // 切换按钮放在 mainBrandContainer 之后
            if (toggleButton.parentNode) toggleButton.remove();
            mainHeader.insertBefore(toggleButton, mainBrandContainer.nextSibling);
            toggleButton.style.display = 'inline-flex';

            // 在切换按钮之后插入竖向分隔线
            if (divider.parentNode) divider.remove();
            mainHeader.insertBefore(divider, toggleButton.nextSibling);
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

            // 在 mainBrandContainer 之后插入竖向分隔线
            if (divider.parentNode) divider.remove();
            mainHeader.insertBefore(divider, mainBrandContainer.nextSibling);
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
// 滚动区域扩展 — 当鼠标在 scrollContainer 右侧/外部时，
// 将鼠标滚轮事件转发到 scrollContainer，提升滚动体验
// ============================================================
(function initWheelForwarding() {
    const mainBody = document.getElementById('mainBody');
    const scrollContainer = document.getElementById('scrollContainer');
    if (!mainBody || !scrollContainer) return;

    mainBody.addEventListener('wheel', (e) => {
        // 如果事件目标在 scrollContainer 内部，让原生滚动自行处理
        if (scrollContainer.contains(e.target)) return;

        // 如果事件目标在 tick-nav 内部（刻度导航有自己的滚轮行为），不拦截
        const tickNav = document.getElementById('tickNav');
        if (tickNav && tickNav.contains(e.target)) return;

        // 阻止默认行为（防止意外页面滚动等）
        e.preventDefault();

        // 将滚轮增量转发到 scrollContainer
        scrollContainer.scrollTop += e.deltaY;
    }, { passive: false });
})();

// ============================================================
// 初始化：自动调整 textarea 高度
// ============================================================

const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const sendModeToggle = document.getElementById('sendModeToggle');
const sendModeLabel = document.getElementById('sendModeLabel');

// ---- 从 settings store 恢复发送模式 ----
// sendMode: 0=Enter发送, 1=Enter换行
sendModeToggle.checked = Alpine.store('settings').sendMode === 1;

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
    var isAlternate = Alpine.store('settings').sendMode === 1;
    sendModeLabel.textContent = isAlternate
        ? SEND_MODE_LABELS.alternate
        : SEND_MODE_LABELS.normal;
    // 同步更新换行提示
    if (newlineHint) {
        newlineHint.textContent = isAlternate
            ? NEWLINE_HINT_LABELS.alternate
            : NEWLINE_HINT_LABELS.normal;
    }
}

// 滑块切换发送模式
sendModeToggle.addEventListener('change', () => {
    Alpine.store('settings').sendMode = sendModeToggle.checked ? 1 : 0;
    Alpine.store('settings').save();
    updateSendModeLabel();
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
        if (Alpine.store('settings').sendMode === 1) {
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

// 发送按钮：流式输出中点击停止，否则发送消息
sendBtn.addEventListener('click', () => {
    if (sessionManager.isStreaming) {
        // 正在流式输出：停止生成
        if (sessionManager.abortController) {
            sessionManager.abortController.abort();
        }
    } else {
        sendMessage();
    }
});

// 折叠状态下的中断按钮（输入框右侧红色方块）
const stopStreamingBtn = document.getElementById('stopStreamingBtn');
if (stopStreamingBtn) {
    stopStreamingBtn.addEventListener('click', () => {
        if (sessionManager.abortController) {
            sessionManager.abortController.abort();
        }
    });
}

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
    }
    // 重置以便重复选择同一文件
    fileInput.value = '';
});

// ============================================================
// 初始化各功能模块
// ============================================================

// 初始化刻度导航事件绑定
initTickNav();

// ============================================================
// 修改对话标题：点击对话标题弹出修改对话框
// ============================================================
(function initTitleEdit() {
    const headerTitle = document.getElementById('headerTitle');
    if (!headerTitle) return;

    headerTitle.addEventListener('click', () => {
        // 正在流式输出时不允许修改标题
        if (sessionManager.isStreaming) {
            showToast('正在生成回复，请稍后再修改标题', 'info');
            return;
        }

        var activeChat = window.Alpine.store('chats').active;
        const currentTitle = (activeChat && activeChat.title) || '';
        if (!currentTitle) {
            // 欢迎状态（空标题）不弹出对话框
            return;
        }

        showTitleEditDialog({
            currentTitle: currentTitle,
            onConfirm: async (newTitle) => {
                // 先调后端 API 保存新标题
                const success = await putChatTitle(newTitle, TITLE_STATE.USER);
                if (success) {
                    // 后端确认成功后，更新页面标题
                    updateHeaderTitle(newTitle);
                    showToast('标题已更新', 'success');
                    return true;
                } else {
                    showToast('修改标题失败，请重试', 'error');
                    return false;
                }
            },
        });
    });
})();

// 初始化复制按钮和消息操作按钮的事件委托
initCopyHandlers();

// 初始化删除模态框事件绑定
initDeleteModal();

// ============================================================
// 登录按钮 — 点击后调用后端登录接口，切换用户
// ============================================================
(function initLoginBtn() {
    const loginBtn = document.getElementById('loginBtn');
    if (!loginBtn) return;

    loginBtn.addEventListener('click', async () => {
        // 流式输出时，登录按钮直接短路返回，不做任何操作
        if (sessionManager.isStreaming) {
            return;
        }

        // 简单模拟登录：使用固定 userNo 或生成一个测试号
        // 后续可改为弹出对话框让用户输入
        const userNo = 'test_user_001';
        const success = await onChatLogin(userNo);
        if (success) {
        } else {
            console.error('登录失败');
        }
    });
})();

// 页面加载后初始化：创建 HTTP session、获取对话列表、显示欢迎消息
window.addEventListener('DOMContentLoaded', async () => {
	await initPage();
});

// 页面加载后获取当前使用的 AI 信息（名称、模型、官网），更新底部免责声明
window.addEventListener('DOMContentLoaded', async () => {
    try {
        const response = await fetch('/api/chat/info/llm');
        if (response.ok) {
            const data = await response.json();
            const disclaimer = document.getElementById('aiDisclaimer');
            if (disclaimer && data.name && data.website) {
                const modelTip = data.model ? `模型：${data.model}` : '';
                disclaimer.innerHTML = `内容由 AI（<a href="${data.website}" target="_blank" rel="noopener noreferrer" data-tooltip="${modelTip}">${data.name}</a>）生成，请仔细甄别`;
            } else if (disclaimer && data.name) {
                disclaimer.textContent = `内容由 AI（${data.name}）生成，请仔细甄别`;
            }
        }
    } catch (e) {
        // 静默失败，保留默认免责声明文本
        console.debug('获取 AI 信息失败:', e);
    }
});

// 输入面板自动折叠 — 滚动刻度变化时折叠，聚焦/输入时恢复
// ============================================================

(function initInputCollapse() {
    const chatContainer = document.getElementById('scrollContainer');
    const inputArea = document.querySelector('.input-area');
    const messageInput = document.getElementById('messageInput');

    if (!chatContainer || !inputArea || !messageInput) return;

    /** 上一次记录的 activeTickIndex，用于检测刻度变化 */
    let lastActiveTickIndex = activeTickIndex;

    // ---- scrollend：滚动完全停止后，折叠输入面板 + 恢复自动滚动 ----
    chatContainer.addEventListener('scrollend', () => {
        if (!sessionManager.isStreaming) return;

        // 滚动停止后才折叠输入面板，避免 scroll anchoring 干扰滚动过程中的检测
        collapseInputArea();

        // 从 Alpine store 获取/设置当前活跃 chat 的滚动状态
        try {
            var chats = window.Alpine.store('chats');
            if (chats && chats.active) {
                if (chats.active.userScrolledUp && isScrolledToBottom()) {
                    chats.active.userScrolledUp = false;
                }
            }
        } catch(e) {}
    });

    // ---- 滚动检测：当滚动刻度变化时折叠；滚动到底部时展开 ----
    //     优先级：用户滚动检测 + 非流式刻度变化折叠
    //     注意：必须节流（200ms），确保在 chat-ticknav.js 的 updateActiveTickOnScroll
    //     （150ms 节流）更新 activeTickIndex 之后再执行，否则检测不到刻度变化。
    let scrollThrottleTimer = null;
    chatContainer.addEventListener('scroll', () => {
        if (scrollThrottleTimer) return;
        scrollThrottleTimer = setTimeout(() => {
            const currentTickIndex = activeTickIndex;
            const sc = chatContainer;
            const debugInfo = `scrollTop=${sc.scrollTop} scrollHeight=${sc.scrollHeight} clientHeight=${sc.clientHeight}`;

            if (sessionManager.isStreaming) {
                // streaming 分支：只检测用户滚动状态，不操作输入面板（避免 scroll anchoring）
                // auto-scroll 由 throttleRender 每 180ms 调用 autoScrollToBottom 负责
                try {
                    var chats2 = window.Alpine.store('chats');
                    if (chats2 && chats2.active) {
                        if (!isScrolledToBottom()) {
                            chats2.active.userScrolledUp = true;
                        } else if (chats2.active.userScrolledUp) {
                            chats2.active.userScrolledUp = false;
                        }
                    }
                } catch(e) {}

                scrollThrottleTimer = null;
                return;
            }

            // 非流式状态：检测是否已滚动到底部
            if (isScrolledToBottom()) {
                // 滚动到底部时自动展开（用户手动滚到底部时恢复输入面板）
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
                    if (!isInputCollapsed()) {
                        collapseInputArea();
                    }
                }
            }

            scrollThrottleTimer = null;
        }, 200);
    });

    // ---- 恢复条件 1：输入框获得焦点 ----
    messageInput.addEventListener('focus', restoreInputArea);

    // ---- 恢复条件 2：输入框内容变化（用户开始输入） ----
    messageInput.addEventListener('input', restoreInputArea);

    // ---- 恢复条件 3：点击输入区域任意位置 ----
    inputArea.addEventListener('click', (e) => {
        if (isInputCollapsed()) {
            messageInput.focus();
        }
    });

    // ---- 当发送消息后恢复 ----
    const sendBtn = document.getElementById('sendBtn');
    if (sendBtn) {
        sendBtn.addEventListener('click', restoreInputArea);
    }

})();
