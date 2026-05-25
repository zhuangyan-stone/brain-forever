// ============================================================
// chat-list.js — 对话列表组件
// 在左侧栏显示用户的对话列表，按时间分组展示
// ============================================================

import { state } from './chat-state.js';
import { sessionManager } from './chat-session-manager.js';
import { showToast, addMessage, updateHeaderTitle, showWelcomeMessage, showSources, showTokenUsage, applyStreamingState } from './chat-ui.js';
import { putChatTitle, TITLE_STATE, switchChat } from './chat-api.js';
import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
import { restoreReasoningArea } from './chat-reasoning.js';
import { updateTickNav } from './chat-ticknav.js';
import { ICON_EDIT, ICON_DELETE } from './svg_icons.js';
import msgbox from './components/msgbox.js';

'use strict';

// ============================================================
// 时间分组工具函数
// ============================================================

/**
 * 获取日期字符串（YYYY/MM/DD 格式）
 * @param {string} isoStr - ISO 格式时间字符串
 * @returns {string}
 */
function getDateStr(isoStr) {
    if (!isoStr) return '';
    const d = new Date(isoStr);
    return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}

/**
 * 判断两个日期是否是同一天
 */
function isSameDay(d1, d2) {
    return d1.getFullYear() === d2.getFullYear() &&
        d1.getMonth() === d2.getMonth() &&
        d1.getDate() === d2.getDate();
}

/**
 * 根据日期获取时间分组标签
 * @param {Date} date
 * @returns {string} 分组标签
 */
function getTimeGroupLabel(date) {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);
    const weekAgo = new Date(today);
    weekAgo.setDate(weekAgo.getDate() - 7);
    const monthAgo = new Date(today);
    monthAgo.setDate(monthAgo.getDate() - 30);

    if (date >= today) return '今天';
    if (date >= yesterday) return '昨天';
    if (date >= weekAgo) return '7天内';
    if (date >= monthAgo) return '30天内';
    return '更早';
}

/**
 * 将对话列表按时间分组
 * @param {Array} chats - 对话数组
 * @returns {Object} 分组结果
 */
function groupChats(chats) {
    const pinned = [];      // 置顶对话
    const today = [];
    const yesterday = [];
    const within7Days = [];
    const within30Days = [];
    const earlier = {};     // 更早：按日期分组 { '2026/3/25': [...] }
    const categorized = {}; // 已分类：按 category 分组 { categoryId: [...] }

    const now = new Date();
    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterdayStart = new Date(todayStart);
    yesterdayStart.setDate(yesterdayStart.getDate() - 1);
    const weekAgoStart = new Date(todayStart);
    weekAgoStart.setDate(weekAgoStart.getDate() - 7);
    const monthAgoStart = new Date(todayStart);
    monthAgoStart.setDate(monthAgoStart.getDate() - 30);

    for (const chat of chats) {
        // 如果对话已分类（category > 0），归入分类分组
        if (chat.category && chat.category > 0) {
            const catKey = String(chat.category);
            if (!categorized[catKey]) {
                categorized[catKey] = [];
            }
            categorized[catKey].push(chat);
            continue;
        }

        if (chat.pinned) {
            pinned.push(chat);
            continue;
        }

        const updateDate = new Date(chat.update_at);
        if (updateDate >= todayStart) {
            today.push(chat);
        } else if (updateDate >= yesterdayStart) {
            yesterday.push(chat);
        } else if (updateDate >= weekAgoStart) {
            within7Days.push(chat);
        } else if (updateDate >= monthAgoStart) {
            within30Days.push(chat);
        } else {
            // 更早：按具体日期分组
            const dateKey = getDateStr(chat.update_at);
            if (!earlier[dateKey]) {
                earlier[dateKey] = [];
            }
            earlier[dateKey].push(chat);
        }
    }

    return { pinned, today, yesterday, within7Days, within30Days, earlier, categorized };
}

/**
 * 截取标题，最多25字
 */
function truncateTitle(title, maxLen = 25) {
    if (!title) return '新对话';
    // 折叠空白
    let result = '';
    let space = false;
    for (const ch of title) {
        if (ch === '\n' || ch === '\r' || ch === '\t' || ch === ' ') {
            if (!space) {
                result += ' ';
                space = true;
            }
        } else {
            result += ch;
            space = false;
        }
    }
    result = result.trim();
    if (result.length > maxLen) {
        return result.slice(0, maxLen) + '…';
    }
    return result || '新对话';
}

// ============================================================
// 对话列表渲染
// ============================================================

let currentChats = [];       // 当前对话列表
let activeChatSN = null;     // 当前选中的对话 SN
let contextMenuEl = null;       // 当前打开的右键菜单
let contextTargetSN = null;     // 右键菜单目标对话 SN

/**
 * 清除当前激活的选中状态（新对话等场景使用）
 * 移除所有 .chat-item 的 .active 类，重置 activeChatSN 为 null
 */
export function clearActiveChat() {
    activeChatSN = null;
    document.querySelectorAll('.chat-item').forEach(el => {
        el.classList.remove('active');
    });
}

/**
 * 重置对话列表（新对话时使用）
 * 清空内存中的聊天列表和选中状态，避免旧数据残留。
 * 后续由 refreshChatListIfNeeded 从后端重新加载。
 */
export function resetChatList() {
    currentChats = [];
    activeChatSN = null;
    const sidebarContent = document.getElementById('sidebarContent');
    if (sidebarContent) {
        sidebarContent.innerHTML = '';
    }
    // 移除可能存在的上下文菜单
    closeContextMenu();
}

/**
 * 在侧边栏中插入一条新对话条目。
 * 如果已有 SN（通过 newChat 预先获取），直接使用真实 SN；
 * 否则 sn 为 null，后续由 getCurrentChatIfNeeded 替换。
 * @param {string} title - 新对话的标题（截取自首条用户消息）
 * @param {string} [sn] - 可选的 SN，预先从后端获取
 */
export function addDirtyChat(title, sn) {
    const dirtyChat = {
        id: 0,           // 尚未在 DB 中创建，id 为 0
        sn: sn || null,  // 优先使用预先获取的 SN
        title: title,    // 标题由前端基于首条消息生成
        title_state: 0,  // 原始标题
        pinned: false,
        category: 0,
        role_no: 0,
        create_at: new Date().toISOString(),
        update_at: new Date().toISOString(),
    };

    // 插入到列表头部（最新消息位置）
    currentChats.unshift(dirtyChat);

    // 如果已有 SN（通过 newChat 预先获取），设置 activeChatSN，
    // 这样后续 updateCurrentChatTitle 才能正确找到 DOM 元素并更新标题。
    // 之前设为 null 导致 AI 推荐标题后侧边栏仍显示旧标题，
    // 必须用户手动点击条目后才更新。
    if (sn) {
        activeChatSN = sn;
    } else {
        activeChatSN = null;
    }

    // 复用 renderChatList 的完整渲染逻辑
    renderChatList(currentChats, activeChatSN);
}

/**
 * 渲染对话列表到左侧栏
 * @param {Array} chats - 对话数组
 * @param {string} [activeSN] - 当前激活的对话 SN
 */
export function renderChatList(chats, activeSN) {
    currentChats = chats || [];
    activeChatSN = activeSN || null;

    const sidebarContent = document.getElementById('sidebarContent');
    if (!sidebarContent) return;

    // 关闭可能打开的右键菜单
    closeContextMenu();

    // 清空并重新渲染
    sidebarContent.innerHTML = '';
    const listEl = document.createElement('div');
    listEl.className = 'chat-list';
    sidebarContent.appendChild(listEl);

    if (!chats || chats.length === 0) {
        const emptyEl = document.createElement('div');
        emptyEl.className = 'chat-list-empty';
        emptyEl.textContent = '暂无对话记录';
        listEl.appendChild(emptyEl);
        return;
    }

    const groups = groupChats(chats);

    // 1. 置顶分组
    if (groups.pinned.length > 0) {
        appendGroup(listEl, '📌 置顶', groups.pinned);
    }

    // 2. 今天
    if (groups.today.length > 0) {
        appendGroup(listEl, '今天', groups.today);
    }

    // 3. 昨天
    if (groups.yesterday.length > 0) {
        appendGroup(listEl, '昨天', groups.yesterday);
    }

    // 4. 7天内
    if (groups.within7Days.length > 0) {
        appendGroup(listEl, '7天内', groups.within7Days);
    }

    // 5. 30天内
    if (groups.within30Days.length > 0) {
        appendGroup(listEl, '30天内', groups.within30Days);
    }

    // 6. 更早 — 先按日期分组，再按具体日期展示
    const earlierDates = Object.keys(groups.earlier).sort((a, b) => {
        // 按日期降序（最新的在前）
        const da = new Date(a);
        const db = new Date(b);
        return db - da;
    });

    if (earlierDates.length > 0) {
        const earlierGroup = document.createElement('div');
        earlierGroup.className = 'chat-group';

        const header = document.createElement('div');
        header.className = 'chat-group-header';
        header.textContent = '更早';
        earlierGroup.appendChild(header);

        const body = document.createElement('div');
        body.className = 'chat-group-body';

        for (const dateKey of earlierDates) {
            // 日期子分组
            const dateGroup = document.createElement('div');
            dateGroup.className = 'chat-date-group';

            const dateHeader = document.createElement('div');
            dateHeader.className = 'chat-date-header';
            dateHeader.textContent = dateKey;
            dateGroup.appendChild(dateHeader);

            for (const chat of groups.earlier[dateKey]) {
                const item = createChatItem(chat);
                dateGroup.appendChild(item);
            }

            body.appendChild(dateGroup);
        }

        earlierGroup.appendChild(body);
        listEl.appendChild(earlierGroup);
    }

    // 7. 分类分组
    const catKeys = Object.keys(groups.categorized);
    if (catKeys.length > 0) {
        const categoryGroup = document.createElement('div');
        categoryGroup.className = 'chat-group';
        const categoryHeader = document.createElement('div');
        categoryHeader.className = 'chat-group-header';
        categoryHeader.textContent = '分类';
        categoryGroup.appendChild(categoryHeader);
        const categoryBody = document.createElement('div');
        categoryBody.className = 'chat-group-body';
        for (const catKey of catKeys) {
            appendGroup(categoryBody, `分类 ${catKey}`, groups.categorized[catKey]);
        }
        categoryGroup.appendChild(categoryBody);
        listEl.appendChild(categoryGroup);
    } else {
        // 无已分类对话，显示空的分组（留空占位）
        const categoryGroup = document.createElement('div');
        categoryGroup.className = 'chat-group';
        const categoryHeader = document.createElement('div');
        categoryHeader.className = 'chat-group-header';
        categoryHeader.textContent = '分类';
        categoryGroup.appendChild(categoryHeader);
        const categoryBody = document.createElement('div');
        categoryBody.className = 'chat-group-body';
        categoryBody.style.display = 'none';
        categoryGroup.appendChild(categoryBody);
        listEl.appendChild(categoryGroup);
    }
}

/**
 * 添加一个分组到列表
 */
function appendGroup(parentEl, label, chats) {
    const group = document.createElement('div');
    group.className = 'chat-group';

    const header = document.createElement('div');
    header.className = 'chat-group-header';
    header.textContent = label;
    group.appendChild(header);

    const body = document.createElement('div');
    body.className = 'chat-group-body';

    for (const chat of chats) {
        const item = createChatItem(chat);
        body.appendChild(item);
    }

    group.appendChild(body);
    parentEl.appendChild(group);
}

/**
 * 创建单个对话项
 */
function createChatItem(chat) {
    const item = document.createElement('div');
    item.className = 'chat-item';
    item.dataset.sn = chat.sn;

    if (chat.sn === activeChatSN) {
        item.classList.add('active');
    }

    // 标题
    const titleEl = document.createElement('div');
    titleEl.className = 'chat-item-title';
    titleEl.textContent = truncateTitle(chat.title);
    item.appendChild(titleEl);

    // 更多按钮（hover 或选中时显示）
    const moreBtn = document.createElement('button');
    moreBtn.className = 'chat-item-more-btn';
    moreBtn.innerHTML = '<svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor"><circle cx="8" cy="3" r="1.5"/><circle cx="8" cy="8" r="1.5"/><circle cx="8" cy="13" r="1.5"/></svg>';
    moreBtn.dataset.tooltip = '更多操作';
    item.appendChild(moreBtn);

    // 点击对话项 — 切换到该对话
    item.addEventListener('click', (e) => {
        // 如果点击的是 moreBtn，不触发切换
        if (e.target.closest('.chat-item-more-btn')) return;
        selectChat(chat.sn);
    });

    // 点击更多按钮 — 显示上下文菜单
    moreBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        showContextMenu(e, chat);
    });

    // 右键菜单
    item.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        showContextMenu(e, chat);
    });

    return item;
}

/**
 * 选中一个对话 — 加载该对话的消息并渲染到主区域
 *
 * 变更（SSEResponser 重构后）：
 *   - 通过 sessionManager.switchTo() 切换活跃会话
 *   - 旧对话的 SSE 连接继续在后台接收数据（不 abort）
 *   - 旧对话的 DOM 引用被释放，但 streamingMsg 保留
 */
async function selectChat(sn) {
    // 更新高亮
    document.querySelectorAll('.chat-item').forEach(el => {
        el.classList.toggle('active', el.dataset.sn === sn);
    });
    activeChatSN = sn;
    state.currentChatSN = sn; // 同步到全局状态

    // 关闭右键菜单
    closeContextMenu();

    // 0. 通过 sessionManager 切换活跃会话
    // 旧 session 标记为非活跃（_isActive = false），其 SSE 连接继续后台接收
    // 新 session 标记为活跃（_isActive = true）
    sessionManager.switchTo(sn);

    // 1. 清空当前消息状态
    state.messages = [];
    state.userMsgCount = 0;
    state.activeTickIndex = -1;
    state.tickScrollOffset = 0;

    // 2. 移除所有消息 DOM 节点
    const chatContainer = document.getElementById('chatContainer');
    if (chatContainer) {
        chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
    }

    // 3. 移除 welcome-message（如果有），把 input-area 移回原位
    const existingWelcome = document.querySelector('.welcome-message');
    if (existingWelcome) {
        const inputArea = existingWelcome.querySelector('.input-area');
        if (inputArea) {
            const mainBody = document.getElementById('mainBody');
            if (mainBody && mainBody.nextElementSibling?.classList?.contains('input-area')) {
                // 已在正确位置
            } else if (mainBody) {
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

    // 6. 调用后端 API 加载目标对话的消息
    const result = await switchChat(sn);
    if (!result) {
        showToast('加载对话失败', 'error');
        return;
    }

    // 7. 更新标题
    if (result.title) {
        updateHeaderTitle(result.title);
    }
    if (typeof result.title_state === 'number') {
        state.titleState = result.title_state;
    }

    // 8. 渲染消息
    for (const msg of result.messages) {
        const msgDiv = addMessage(msg.role, msg.content, msg.created_at || null);
        const entry = { role: msg.role, content: msg.content, id: msg.id, usage: msg.usage || null };
        state.messages.push(entry);
        // 将 data-msg-id 设置在 message-group 上
        if (msgDiv && msg.id) {
            const group = msgDiv.closest('.message-group');
            if (group) {
                group.dataset.msgId = msg.id;
            }
        }
        // assistant 消息 token-info
        if (msg.role === 'assistant' && msg.usage && msgDiv) {
            showTokenUsage(msgDiv, msg.usage);
        }
        // assistant 消息 sources
        if (msg.role === 'assistant' && msg.sources && msg.sources.length > 0) {
            showSources(msg.sources, 'web');
        }
        // assistant 消息 reasoning
        if (msg.role === 'assistant' && msg.reasoning && msgDiv) {
            restoreReasoningArea(msgDiv, msg.reasoning, msg.deep_think);
        }
    }

    // 9. 检查当前 session 是否有流式输出的累积数据需要渲染
    // 场景 A：切换回一个正在后台流式输出的对话（!isDone）
    //   仅当历史消息最后一条不是 assistant 时才创建气泡（否则 AI 已回复完成）
    // 场景 B：切换回一个流式已完成的对话（isDone），但 DOM 引用已被释放
    const session = sessionManager.sessions.get(sn);
    const lastMsg = result.messages[result.messages.length - 1];
    const lastIsAssistant = lastMsg && lastMsg.role === 'assistant';

    if (session && session.streamingMsg && !session.streamingMsg.isDone && !lastIsAssistant) {
        // 场景 A：流未完成且最后一条不是 assistant，重新创建气泡恢复 DOM 引用
        const assistantBubble = addMessage('assistant', '', null, true);
        const contentDiv = assistantBubble.querySelector('.bubble');
        session.assistantBubble = assistantBubble;
        session.contentDiv = contentDiv;

        // 将已有累积内容渲染到 DOM
        const msg = session.streamingMsg;
        if (msg.reasoning) {
            restoreReasoningArea(assistantBubble, msg.reasoning);
        }
        if (msg.content) {
            contentDiv.innerHTML = msg.content;
            contentDiv.classList.add('streaming');
        }
        if (msg.webSources.length > 0) {
            showSources(msg.webSources, 'web');
        }

        // 标记流式状态
        applyStreamingState(true);
    } else if (session && session.streamingMsg && session.streamingMsg.isDone && !session.assistantBubble) {
        // 场景 B：流已完成但 DOM 引用已释放，通过 flushToDOM 渲染
        // 注意：switchTo() 中已调用 flushToDOM()，但当时 assistantBubble 为 null 所以被跳过
        // 这里需要先创建 assistantBubble，再调用 flushToDOM
        const assistantBubble = addMessage('assistant', '', null, true);
        const contentDiv = assistantBubble.querySelector('.bubble');
        session.assistantBubble = assistantBubble;
        session.contentDiv = contentDiv;

        // 现在 flushToDOM 可以正常工作了
        session.responser.flushToDOM();
    }

    // 10. 更新刻度导航
    updateTickNav();
}

// ============================================================
// 上下文菜单（重命名、置顶、删除）
// ============================================================

/**
 * 显示上下文菜单
 */
function showContextMenu(e, chat) {
    closeContextMenu();

    contextTargetSN = chat.sn;

    const menu = document.createElement('div');
    menu.className = 'chat-context-menu';
    menu.style.position = 'fixed';

    // 计算菜单位置
    const rect = e.target.getBoundingClientRect();
    const menuWidth = 160;
    const menuHeight = 36 * 3 + 4; // 3 items * 36px + padding

    let left = rect.right + 4;
    let top = rect.top;

    // 防止菜单超出右边界
    if (left + menuWidth > window.innerWidth) {
        left = rect.left - menuWidth - 4;
    }
    // 防止菜单超出下边界
    if (top + menuHeight > window.innerHeight) {
        top = window.innerHeight - menuHeight - 8;
    }

    menu.style.left = left + 'px';
    menu.style.top = top + 'px';

    // 重命名
    const renameItem = document.createElement('div');
    renameItem.className = 'chat-context-menu-item';
    renameItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_EDIT + '</svg> 重命名';
    renameItem.addEventListener('click', () => {
        closeContextMenu();
        handleRename(chat);
    });
    menu.appendChild(renameItem);

    // 置顶/取消置顶
    const pinItem = document.createElement('div');
    pinItem.className = 'chat-context-menu-item';
    if (chat.pinned) {
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2z"/></svg> 取消置顶';
    } else {
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2z"/></svg> 置顶';
    }
    pinItem.addEventListener('click', () => {
        closeContextMenu();
        handleTogglePin(chat);
    });
    menu.appendChild(pinItem);

    // 删除（警告色）
    const deleteItem = document.createElement('div');
    deleteItem.className = 'chat-context-menu-item chat-context-menu-item-danger';
    deleteItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg> 删除';
    deleteItem.addEventListener('click', () => {
        closeContextMenu();
        handleDelete(chat);
    });
    menu.appendChild(deleteItem);

    document.body.appendChild(menu);
    contextMenuEl = menu;

    // 点击其他地方关闭菜单
    setTimeout(() => {
        document.addEventListener('click', closeContextMenu, { once: true });
    }, 0);
}

/**
 * 关闭上下文菜单
 */
function closeContextMenu() {
    if (contextMenuEl) {
        contextMenuEl.remove();
        contextMenuEl = null;
    }
    contextTargetSN = null;
}

// ============================================================
// 操作处理
// ============================================================

/**
 * 重命名对话
 */
async function handleRename(chat) {
    // 正在流式输出时不允许修改标题（同 HEADER 点击标题的逻辑一致）
    if (state.isStreaming) {
        showToast('正在生成回复，请稍后再修改标题', 'info');
        return;
    }

    showTitleEditDialog({
        currentTitle: chat.title || '',
        onConfirm: async (newTitle) => {
            try {
                const response = await fetch('/api/session/title?title=' + encodeURIComponent(newTitle) +
                    '&state=' + TITLE_STATE.USER + '&sn=' + encodeURIComponent(chat.sn), {
                    method: 'PUT',
                });
                if (!response.ok) {
                    showToast('重命名失败', 'error');
                    return false;
                }
                // 更新本地数据
                chat.title = newTitle;
                // 重新渲染列表
                renderChatList(currentChats, activeChatSN);
                showToast('已重命名', 'success');
                return true;
            } catch (e) {
                showToast('重命名出错', 'error');
                return false;
            }
        },
    });
}

/**
 * 切换置顶状态
 */
async function handleTogglePin(chat) {
    const newPinned = !chat.pinned;
    try {
        const response = await fetch('/api/chat/pin?sn=' + encodeURIComponent(chat.sn) +
            '&pinned=' + newPinned, {
            method: 'PUT',
        });
        if (!response.ok) {
            showToast('操作失败', 'error');
            return;
        }
        // 更新本地数据
        chat.pinned = newPinned;
        // 重新渲染列表
        renderChatList(currentChats, activeChatSN);
        showToast(newPinned ? '已置顶' : '已取消置顶', 'success');
    } catch (e) {
        showToast('操作出错', 'error');
    }
}

/**
 * 更新当前活动对话的标题并同步到侧边栏
 * 直接操作 DOM，不重新渲染整个列表
 * 当 activeChatSN 为 null（新对话刚创建后，列表尚未从后端刷新）时，
 * 尝试通过 currentChats 中标题为空的项来匹配新对话。
 * @param {string} newTitle - 新标题
 */
export function updateCurrentChatTitle(newTitle) {
    if (!newTitle) return;

    let sn = activeChatSN;

    // 新对话刚创建后 activeChatSN 为 null（clearActiveChat 清除了选中状态），
    // 此时 currentChats 中最后一个（最旧的）对话可能有一个空标题或默认标题的占位。
    // 但我们无法精确匹配到新对话，因此直接跳过 DOM 更新，
    // 等待后续 refreshChatListIfNeeded 从后端拉取完整列表后再渲染。
    if (!sn) {
        // activeChatSN 为 null 时不更新 DOM（因为无法确定要更新的目标），
        // 但可以更新 currentChats 中对应的条目——不过也无法确定哪个条目是新对话，
        // 所以此处跳过，由后续的 refreshChatListIfNeeded 负责。
        return;
    }

    // 更新内存中的标题
    const chat = currentChats.find(c => c.sn === sn);
    if (chat) {
        chat.title = newTitle;
    }

    // 直接更新 DOM
    // 注意: querySelectorAll + forEach 以确保如果存在多个相同 data-sn 的项（Bug 3），
    // 所有项都更新而非只更新第一个
    const activeItems = document.querySelectorAll(`.chat-item[data-sn="${sn}"] .chat-item-title`);
    if (activeItems.length > 0) {
        activeItems.forEach(el => {
            el.textContent = truncateTitle(newTitle);
        });
    }
}

/**
 * 更新或添加侧边栏中的单个对话条目。
 * 由 getCurrentChatIfNeeded 在第一轮对话完成后调用，
 * 替换旧的 refreshChatListIfNeeded 全量刷新方式。
 *
 * 功能：
 *   - 如果该 SN 已存在于 currentChats 中，更新其标题
 *   - 如果不存在（新对话的脏数据尚未被后端确认），移除脏数据 (sn=null) 并添加新条目
 *   - 然后重新渲染整个列表
 *
 * @param {string} sn - 对话 SN（来自后端）
 * @param {string} title - 对话标题
 * @param {number} titleState - 标题修改状态
 */
export function updateChatEntry(sn, title, titleState) {
    if (!sn) return;

    // 检查该 SN 是否已存在
    const existing = currentChats.find(c => c.sn === sn);
    if (existing) {
        // 已存在：仅更新标题
        // 注意：后端在新对话时 currentChat.title 为空字符串，
        // 此时前端已有正确的原始标题，因此仅当 title 有值时更新。
        if (title) {
            existing.title = title;
        }
        existing.title_state = titleState;
    } else {
        // 不存在：移除脏数据（sn=null 的占位条目），然后添加真实条目
        currentChats = currentChats.filter(c => c.sn !== null);

        // 创建新条目
        const now = new Date().toISOString();
        const newChat = {
            id: 0,
            sn: sn,
            title: title || '',
            title_state: titleState,
            pinned: false,
            category: 0,
            role_no: 0,
            create_at: now,
            update_at: now,
        };
        currentChats.unshift(newChat);
    }

    // 重新渲染列表
    renderChatList(currentChats, activeChatSN);
}

/**
 * 删除对话
 */
async function handleDelete(chat) {
    const result = await msgbox.warning(`「${truncateTitle(chat.title)}」删除后不可恢复，请确认是否删除？`);
    if (result !== 1) {
        return;
    }

    try {
        const response = await fetch('/api/session?sn=' + encodeURIComponent(chat.sn), {
            method: 'DELETE',
        });
        if (!response.ok) {
            showToast('删除失败', 'error');
            return;
        }

        // 0. 通过 sessionManager 移除 ChatSession（abort 正在进行的 SSE 流）
        sessionManager.remove(chat.sn);

        // 从本地数据移除
        const idx = currentChats.findIndex(c => c.sn === chat.sn);
        if (idx >= 0) {
            currentChats.splice(idx, 1);
        }

        // 如果删除的是当前活动对话，清空主界面（消息、标题、刻度导航等）
        if (activeChatSN === chat.sn) {
            // 清空消息状态
            state.messages = [];
            state.userMsgCount = 0;
            state.activeTickIndex = -1;
            state.tickScrollOffset = 0;

            // 移除所有消息 DOM
            const chatContainer = document.getElementById('chatContainer');
            if (chatContainer) {
                chatContainer.querySelectorAll('.message-group').forEach(el => el.remove());
            }

            // 清空刻度导航
            const tickNav = document.getElementById('tickNav');
            if (tickNav) {
                tickNav.innerHTML = '';
            }

            // 重置当前选中状态
            activeChatSN = null;

            // 显示欢迎消息（会同时清空 header 标题）
            showWelcomeMessage();
        }

        // 重新渲染列表（activeChatSN 已置 null，侧边栏无选中项）
        renderChatList(currentChats, activeChatSN);
        showToast('已删除', 'success');
    } catch (e) {
        showToast('删除出错', 'error');
    }
}
