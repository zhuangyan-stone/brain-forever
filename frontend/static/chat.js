// ============================================================
// 第2大脑 AI 助手 — 主入口
// 导入各功能模块并完成初始化
// ============================================================

import { switchHighlightTheme } from './chat-markdown.js';
import { initDom, dom, showWelcomeMessage, updateHeaderTitle, showToast, collapseInputArea, restoreInputArea, isInputCollapsed, isScrolledToBottom, _autoScrolling } from './chat-ui.js';
import { initTickNav, updateTickNav } from './chat-ticknav.js';
import { initTooltip } from './components/tooltip.js';
import { sendMessage } from './chat-sse.js';
import { initCopyHandlers } from './chat-copy.js';
import { initDeleteModal } from './dialogs/msg-delete-dialog.js';
import { initPage } from './chat-init.js';
import { clearAllStickyNotes } from './components/sticky-mgr.js';
import { fetchChatTitle, putChatTitle, TITLE_STATE, onChatLogin, onChatLogout, createBlankChat, fetchLlmInfo } from './chat-api.js';
import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
import { clearActiveChat, updateChatTitleBySN } from './chat-list.js';
import { ICON_TOGGLE_OPEN, ICON_TOGGLE_CLOSE } from './svg_icons_re.js';
import { chatStreamMgr } from './chat-stream-mgr.js';
import { activeTickIndex, setActiveTickIndex, tickScrollOffset, setTickScrollOffset, resetTickState } from './tick-state.js';

'use strict';

// 注意：图标常量已由 svg_icons.js（在 Alpine.js 之前加载的普通 <script>）
// 注册到 window 上，此处不再重复执行 Object.assign(window, ...)。

// ============================================================
// 从 cookie 加载用户设置
// ============================================================
Alpine.store('settings').load();

// 预加载主题清单，使切换按钮 tooltip 能正确显示主题中文名
if (window.ThemeLoader) {
    window.ThemeLoader.loadManifest().then(function(data) {
        if (data && data.themes) {
            Alpine.store('settings').themeManifest = data.themes;
        }
    });
}

function resolveTheme(theme) {
    if (theme >= 2) {
        // 跟随系统（theme=2 或 3）
        return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    // 0=手动亮, 1=手动暗
    return theme === 1 ? 'dark' : 'light';
}

// ============================================================
// 初始化 DOM 引用
// ============================================================
initDom();
initTooltip();

// ============================================================
// 主题切换 — applyTheme846 被 Alpine store toggleTheme() 通过
// 'theme-changed' 自定义事件触发
// ============================================================

/** 应用主题到页面 */
function applyTheme(themeVal) {
    const themeStr = resolveTheme(themeVal);
    document.documentElement.setAttribute('data-theme', themeStr);
    switchHighlightTheme(themeStr);
    console.log('[theme] theme:', themeVal);
    // 外源主题联动：切换明暗时自动加载对应的外源主题 CSS
    if (window.ThemeLoader) {
        window.ThemeLoader.apply();
    }
}

// 初始化主题
applyTheme(Alpine.store('settings').theme);

// 监听 Alpine store 发起的主题变更事件
document.addEventListener('theme-changed', (e) => {
    applyTheme(e.detail.theme);
});

// 监听系统主题变化（跟随系统模式下自动切换）
(function() {
    const darkMq = window.matchMedia('(prefers-color-scheme: dark)');
    darkMq.addEventListener('change', (e) => {
        const settings = Alpine.store('settings');
        const mode = e.matches ? 'dark' : 'light';
        // 只在跟随系统模式（theme >= 2）时自动切换
        if (settings.theme >= 2) {
            applyTheme(settings.theme); // resolveTheme 会根据 prefers-color-scheme 重新计算
            console.log('[theme] system change — follow system, theme:', settings.theme, 'mode:', mode);
        } else {
            console.log('[theme] system change — ignored (manual mode', settings.theme, ')');
        }
    });
})();

// ============================================================
// AI 标题按钮 — 点击触发 AI 重新生成标题（防抖动：5 秒内只生效一次）
// SVG 图标已在 HTML 中通过 Alpine 渲染，此处仅处理点击逻辑
// ============================================================
const aiTitleBtn = document.getElementById('aiTitleBtn');
if (aiTitleBtn) {
    let aiTitleDebounceTimer = null;
    aiTitleBtn.addEventListener('click', () => {
        // 正在 SSE 流式输出时，AI 标题按钮不可用（Alpine :disabled 已处理，再加一层防御）
        if (Alpine.store('chats').active?.isStreaming) {
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
// startNewChat — 开启新对话（无刷新 SPA 方式）
// 清空当前会话的所有历史消息，进入欢迎状态
// 同时调用后端 PUT /api/chat/new 将后端 currentChat 重置为 blank chat
// ============================================================

async function startNewChat() {
    var chatsStore = window.Alpine.store('chats');

    // ★ 防重复：如果当前已经是空白对话（脏对话），不再反复重置前端状态。
    //   用户连续点击"新对话"按钮时，第一次已重置为 blankItem，
    //   后续点击直接跳过，避免不必要的 DOM 操作。
    // ★ 但必须仍调用 createBlankChat() 复位后端 currentChat，
    //   否则刷新页面后后端 currentChat 未复位，新消息会复用旧 chat 的 SN。
    if (chatsStore && chatsStore.isDirtyChat && chatsStore.isDirtyChat()) {
        // 后端 PUT /api/chat/new 是幂等的：如果后端已经是空白状态，onNewChat 是 no-op。
        await createBlankChat();
        // 但确保输入框聚焦（用户可能期望点击后直接输入）
        const msgInput = document.getElementById('messageInput');
        if (msgInput) {
            msgInput.focus();
        }
        // 小屏抽屉模式下，点击"新对话"后自动关闭左侧边栏（抽屉）
        if (isSmallMode && isDrawerOpen) {
            closeDrawer();
        }
        return;
    }

    // 1. 重置为空白对话状态：activeIndex = -1，创建 blankItem
    //    Alpine 响应式模板自动隐藏消息组、显示欢迎消息
    // ★ 注意：不使用 resetToBlank()，因为那会清空 items[]（已有对话的消息数据）
    //   和 chatsTimeline（侧边栏的对话列表），导致侧边栏消失。
    //   这里只重置活跃状态和 blankItem，保留已有对话的数据。
    chatsStore.activeIndex = -1;
    chatsStore.blankItem = {
        sn: '',
        title: '',
        titleState: 0,
        isStreaming: false,
        userScrolledUp: false,
        streamingMsg: null,
        groups: [],
        _groupSeq: 0,
    };
    chatsStore.inputCollapsed = false;
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

    // 5. 调用后端 PUT /api/chat/new 将后端 currentChat 重置为 blank chat
    //    blank chat 无 SN、无 DB 记录、不在 session.chats[] 中
    //    SN 将在第一条消息发送时由 ensureDBSession 生成
    await createBlankChat();

    // 6. 设置欢迎消息文本（Alpine 响应式模板自动显示）
    showWelcomeMessage();

    // 7. 确保输入面板展开并同步内部折叠状态
    const msgInput2 = document.getElementById('messageInput');
    if (msgInput2) {
        msgInput2.focus();
    }

    // 8. 小屏抽屉模式下，点击"新对话"后自动关闭左侧边栏（抽屉）
    if (isSmallMode && isDrawerOpen) {
        closeDrawer();
    }
}

// ============================================================
// 新建对话按钮（主栏顶部图标）
// ============================================================

const newChatBtn = document.getElementById('newChatBtn');

newChatBtn.addEventListener('click', startNewChat);

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

// 侧栏收起时显示的"展开"按钮 SVG（矩形 + 竖线）
const TOGGLE_BTN_OPEN_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' + ICON_TOGGLE_OPEN + '</svg>';
// 侧栏展开后显示的"关闭"按钮 SVG（矩形 + 左指向箭头 ←）
const TOGGLE_BTN_CLOSE_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' + ICON_TOGGLE_CLOSE + '</svg>';

/**
 * setToggleButtonIcon — 根据侧边栏状态更新切换按钮的 SVG 图标。
 * @param {boolean} open - true 表示侧边栏已展开，使用关闭箭头图标；false 使用展开图标。
 */
function setToggleButtonIcon(open) {
    const btn = getToggleButton();
    btn.innerHTML = open ? TOGGLE_BTN_CLOSE_SVG : TOGGLE_BTN_OPEN_SVG;
}

// ===== 品牌文本常量（方便以后修改） =====
const BRAND_TITLE = '第2大脑';
const BRAND_SUBTITLE = '尽量懂你' 

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
        globalToggleButton.dataset.tooltip = '切换侧边栏 (Ctrl+B)';
        globalToggleButton.innerHTML = TOGGLE_BTN_OPEN_SVG;
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

// 将 closeDrawer 注册到 Alpine store，供 chat-list.js 的 selectChat 等外部模块调用
try {
    var _chatsStore = window.Alpine.store('chats');
    if (_chatsStore) {
        _chatsStore.closeDrawer = function() {
            if (isSmallMode && isDrawerOpen) {
                closeDrawer();
            }
        };
    }
} catch(e) {}

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

    // 根据侧边栏展开/收起状态更新切换按钮图标
    setToggleButtonIcon(sidebarVisible);

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

// ---- 从 settings store 恢复发送模式 ----
// sendMode: 0=Enter发送, 1=Enter换行
sendModeToggle.checked = Alpine.store('settings').sendMode === 1;

messageInput.addEventListener('input', () => {
    messageInput.style.height = 'auto';
    messageInput.style.height = Math.min(messageInput.scrollHeight, 120) + 'px';
});

const sendModeTextLeft = document.getElementById('sendModeTextLeft');
const sendModeTextRight = document.getElementById('sendModeTextRight');

// 更新文字高亮状态：滑块滑向哪边，哪边的文字高亮
function updateSendModeLabels() {
    var isAlternate = Alpine.store('settings').sendMode === 1;
    // 切换左右文字的高亮状态
    sendModeTextLeft.classList.toggle('active', !isAlternate);
    sendModeTextRight.classList.toggle('active', isAlternate);
}

// 滑块切换发送模式
sendModeToggle.addEventListener('change', () => {
    Alpine.store('settings').sendMode = sendModeToggle.checked ? 1 : 0;
    Alpine.store('settings').save();
    updateSendModeLabels();
});

// 点击左侧/右侧文字都可切换到另一模式
sendModeTextLeft.addEventListener('click', () => {
    sendModeToggle.checked = !sendModeToggle.checked;
    sendModeToggle.dispatchEvent(new Event('change'));
});
sendModeTextRight.addEventListener('click', () => {
    sendModeToggle.checked = !sendModeToggle.checked;
    sendModeToggle.dispatchEvent(new Event('change'));
});

// 初始化文字高亮状态
updateSendModeLabels();

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
    const activeChat = Alpine.store('chats').active;
    if (activeChat?.isStreaming) {
        // 正在流式输出：停止生成
        const activeSn = activeChat.sn;
        const stream = activeSn ? chatStreamMgr.get(activeSn) : null;
        if (stream?.abortController) {
            stream.abortController.abort();
        }
    } else {
        sendMessage();
    }
});

// 折叠状态下的中断按钮（输入框右侧红色方块）
const stopStreamingBtn = document.getElementById('stopStreamingBtn');
if (stopStreamingBtn) {
    stopStreamingBtn.addEventListener('click', () => {
        const activeChat = Alpine.store('chats').active;
        const activeSn = activeChat?.sn;
        const stream = activeSn ? chatStreamMgr.get(activeSn) : null;
        if (stream?.abortController) {
            stream.abortController.abort();
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
        if (Alpine.store('chats').active?.isStreaming) {
            showToast('正在生成回复，请稍后再修改标题', 'info');
            return;
        }

        var chats = window.Alpine.store('chats');
        var activeChat = chats.active;
        const currentTitle = (activeChat && activeChat.title) || '';
        if (!currentTitle) {
            // 欢迎状态（空标题）不弹出对话框
            return;
        }

        // blankChat 没有有效 SN，不允许修改标题
        if (!activeChat || !activeChat.sn) {
            return;
        }

        // 脏对话（临时 SN，尚未被后端确认）不允许修改标题
        if (chats.isDirtyChat(activeChat)) {
            showToast('该对话尚未完成创建，请稍后再修改标题', 'info');
            return;
        }

        showTitleEditDialog({
            currentTitle: currentTitle,
            onConfirm: async (newTitle) => {
                // 先调后端 API 保存新标题
                const success = await putChatTitle(newTitle, TITLE_STATE.USER, activeChat.sn);
                if (success) {
                    // 后端确认成功后，更新页面标题和侧边栏
                    updateHeaderTitle(newTitle);
                    updateChatTitleBySN(activeChat.sn, newTitle);
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
// 登录按钮 — 供 Alpine 模板 @click 调用
// 跳转到独立登录页
// ============================================================
window.onChatLoginClick = async function() {
    // 流式输出时，登录按钮直接短路返回，不做任何操作
    if (Alpine.store('chats').active?.isStreaming) {
        return;
    }
    window.location.href = '/signin/';
};

// 页面加载后初始化：创建 HTTP session、获取对话列表、显示欢迎消息
window.addEventListener('DOMContentLoaded', async () => {
	await initPage();
});

// 页面加载后获取当前使用的 AI 信息（名称、模型、官网），更新底部免责声明
window.addEventListener('DOMContentLoaded', async () => {
    const data = await fetchLlmInfo();
    if (!data) return;
    const disclaimer = document.getElementById('aiDisclaimer');
    if (disclaimer && data.name && data.website) {
        const modelTip = data.model ? `模型：${data.model}` : '';
        disclaimer.innerHTML = `内容由 AI（<a href="${data.website}" target="_blank" rel="noopener noreferrer" data-tooltip="${modelTip}">${data.name}</a>）生成，请仔细甄别`;
    } else if (disclaimer && data.name) {
        disclaimer.textContent = `内容由 AI（${data.name}）生成，请仔细甄别`;
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
        if (!Alpine.store('chats').active?.isStreaming) return;

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
    //     注意：非流式分支必须节流（150ms），确保在 chat-ticknav.js 的 updateActiveTickOnScroll
    //     （100ms 节流）更新 activeTickIndex 之后再执行，否则检测不到刻度变化。
    //     流式分支不做节流——scroll 事件即时用 _autoScrolling 判断，
    //     避免 200ms 延迟后 _autoScrolling 已被 setTimeout(0) 清除导致误判。
    let scrollThrottleTimer = null;
    chatContainer.addEventListener('scroll', () => {
        if (Alpine.store('chats').active?.isStreaming) {
            // ★ 流式分支：无节流，即时处理
            //   auto-scroll 由 throttleRender 每 180ms 调用 autoScrollToBottom 负责
            //   _autoScrolling 在同一事件循环中仍为 true，可准确拦截自己触发的 scroll 事件
            const sc = chatContainer;
            if (_autoScrolling) {
                return;
            }
            try {
                var chats2 = window.Alpine.store('chats');
                if (chats2 && chats2.active) {
                    var atBottom = isScrolledToBottom();
                    if (!atBottom) {
                        if (!chats2.active.userScrolledUp) {
                            chats2.active.userScrolledUp = true;
                        }
                    } else if (chats2.active.userScrolledUp) {
                        chats2.active.userScrolledUp = false;
                    }
                }
            } catch(e) {}
            return;
        }

        // 非流式分支：200ms 节流，避免高频 scroll 事件
        if (scrollThrottleTimer) return;
        scrollThrottleTimer = setTimeout(() => {
            const currentTickIndex = activeTickIndex;
            const sc = chatContainer;

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
        }, 150);
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

// ============================================================
// 全局热键 — F2：聚焦输入框
// ============================================================
document.addEventListener('keydown', (e) => {
    if (e.key === 'F2') {
        e.preventDefault();
        const msgInput = document.getElementById('messageInput');
        if (msgInput) {
            // 如果输入面板已折叠，先恢复
            if (isInputCollapsed()) {
                restoreInputArea();
            }
            msgInput.focus();
        }
    }
});

// ============================================================
// 全局热键 — Ctrl+B：切换左边栏（仅在没有弹出对话框时生效）
// 调用 toggleSidebarMaster() 走统一切换逻辑（宽屏双栏/小屏抽屉）
// ============================================================
document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'b') {
        // 检查是否有对话框打开（Alpine 对话框 + msgbox 动态对话框均有 .show class）
        const hasDialog = document.querySelector('.dialog-overlay.show, .portrait-overlay.show');
        if (hasDialog) return;

        e.preventDefault();
        toggleSidebarMaster();
    }
});

// ============================================================
// 全局热键 — Ctrl+Alt+N：开启新对话
// Ctrl+N / Ctrl+Shift+N 已被浏览器自身占用（新窗口/无痕窗口），
// 因此改为 Ctrl+Alt+N（三键组合，N 取自 New，语义直观）。
// 仅在无对话框打开时生效。
// ============================================================
document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.altKey && e.key === 'n') {
        // 检查是否有对话框打开
        const hasDialog = document.querySelector('.dialog-overlay.show, .portrait-overlay.show');
        if (hasDialog) return;

        e.preventDefault();
        startNewChat();
    }
});

// ============================================================
// Placeholder 动态提示 — 无焦点时显示 F2 快捷键提示
// 小屏模式（无物理键盘）下始终显示 "说点什么？"，不显示 F2 提示
// ============================================================
(function initPlaceholderHint() {
    const PLACEHOLDER_FOCUS = '说点什么？';
    const PLACEHOLDER_BLUR =  '按 F2 键开始输入';
    const msgInput = document.getElementById('messageInput');
    if (!msgInput) return;

    // 判断是否为小屏模式
    function isSmallScreen() {
        return document.body.classList.contains('small-screen-mode');
    }

    // 获取当前应显示的 placeholder
    function getPlaceholder() {
        return isSmallScreen() ? PLACEHOLDER_FOCUS : PLACEHOLDER_BLUR;
    }

    // 初始状态：无焦点，根据屏幕模式显示对应提示
    msgInput.placeholder = getPlaceholder();

    msgInput.addEventListener('focus', () => {
        msgInput.placeholder = PLACEHOLDER_FOCUS;
    });

    msgInput.addEventListener('blur', () => {
        msgInput.placeholder = getPlaceholder();
    });
})();
