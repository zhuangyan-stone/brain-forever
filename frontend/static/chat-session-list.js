// ============================================================
// chat-session-list.js — 会话列表组件
// 在左侧栏显示用户的会话列表，按时间分组展示
// ============================================================

import { state } from './chat-state.js';
import { showToast } from './chat-ui.js';
import { putSessionTitle, TITLE_STATE } from './chat-api.js';
import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
import { ICON_EDIT, ICON_DELETE } from './svg_icons.js';

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
 * 将会话列表按时间分组
 * @param {Array} sessions - 会话数组
 * @returns {Object} 分组结果
 */
function groupSessions(sessions) {
    const pinned = [];      // 置顶会话
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

    for (const sess of sessions) {
        // 如果会话已分类（category > 0），归入分类分组
        if (sess.category && sess.category > 0) {
            const catKey = String(sess.category);
            if (!categorized[catKey]) {
                categorized[catKey] = [];
            }
            categorized[catKey].push(sess);
            continue;
        }

        if (sess.pinned) {
            pinned.push(sess);
            continue;
        }

        const updateDate = new Date(sess.update_at);
        if (updateDate >= todayStart) {
            today.push(sess);
        } else if (updateDate >= yesterdayStart) {
            yesterday.push(sess);
        } else if (updateDate >= weekAgoStart) {
            within7Days.push(sess);
        } else if (updateDate >= monthAgoStart) {
            within30Days.push(sess);
        } else {
            // 更早：按具体日期分组
            const dateKey = getDateStr(sess.update_at);
            if (!earlier[dateKey]) {
                earlier[dateKey] = [];
            }
            earlier[dateKey].push(sess);
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
// 会话列表渲染
// ============================================================

let currentSessions = [];       // 当前会话列表
let activeSessionSN = null;     // 当前选中的会话 SN
let contextMenuEl = null;       // 当前打开的右键菜单
let contextTargetSN = null;     // 右键菜单目标会话 SN

/**
 * 渲染会话列表到左侧栏
 * @param {Array} sessions - 会话数组
 * @param {string} [activeSN] - 当前激活的会话 SN
 */
export function renderSessionList(sessions, activeSN) {
    currentSessions = sessions || [];
    activeSessionSN = activeSN || null;

    const sidebarContent = document.getElementById('sidebarContent');
    if (!sidebarContent) return;

    // 关闭可能打开的右键菜单
    closeContextMenu();

    // 清空并重新渲染
    sidebarContent.innerHTML = '';
    const listEl = document.createElement('div');
    listEl.className = 'session-list';
    sidebarContent.appendChild(listEl);

    if (!sessions || sessions.length === 0) {
        const emptyEl = document.createElement('div');
        emptyEl.className = 'session-list-empty';
        emptyEl.textContent = '暂无对话记录';
        listEl.appendChild(emptyEl);
        return;
    }

    const groups = groupSessions(sessions);

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
        earlierGroup.className = 'session-group';

        const header = document.createElement('div');
        header.className = 'session-group-header';
        header.textContent = '更早';
        earlierGroup.appendChild(header);

        const body = document.createElement('div');
        body.className = 'session-group-body';

        for (const dateKey of earlierDates) {
            // 日期子分组
            const dateGroup = document.createElement('div');
            dateGroup.className = 'session-date-group';

            const dateHeader = document.createElement('div');
            dateHeader.className = 'session-date-header';
            dateHeader.textContent = dateKey;
            dateGroup.appendChild(dateHeader);

            for (const sess of groups.earlier[dateKey]) {
                const item = createSessionItem(sess);
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
        categoryGroup.className = 'session-group';
        const categoryHeader = document.createElement('div');
        categoryHeader.className = 'session-group-header';
        categoryHeader.textContent = '分类';
        categoryGroup.appendChild(categoryHeader);
        const categoryBody = document.createElement('div');
        categoryBody.className = 'session-group-body';
        for (const catKey of catKeys) {
            appendGroup(categoryBody, `分类 ${catKey}`, groups.categorized[catKey]);
        }
        categoryGroup.appendChild(categoryBody);
        listEl.appendChild(categoryGroup);
    } else {
        // 无已分类会话，显示空的分组（留空占位）
        const categoryGroup = document.createElement('div');
        categoryGroup.className = 'session-group';
        const categoryHeader = document.createElement('div');
        categoryHeader.className = 'session-group-header';
        categoryHeader.textContent = '分类';
        categoryGroup.appendChild(categoryHeader);
        const categoryBody = document.createElement('div');
        categoryBody.className = 'session-group-body';
        categoryBody.style.display = 'none';
        categoryGroup.appendChild(categoryBody);
        listEl.appendChild(categoryGroup);
    }
}

/**
 * 添加一个分组到列表
 */
function appendGroup(parentEl, label, sessions) {
    const group = document.createElement('div');
    group.className = 'session-group';

    const header = document.createElement('div');
    header.className = 'session-group-header';
    header.textContent = label;
    group.appendChild(header);

    const body = document.createElement('div');
    body.className = 'session-group-body';

    for (const sess of sessions) {
        const item = createSessionItem(sess);
        body.appendChild(item);
    }

    group.appendChild(body);
    parentEl.appendChild(group);
}

/**
 * 创建单个会话项
 */
function createSessionItem(sess) {
    const item = document.createElement('div');
    item.className = 'session-item';
    item.dataset.sn = sess.sn;

    if (sess.sn === activeSessionSN) {
        item.classList.add('active');
    }

    // 标题
    const titleEl = document.createElement('div');
    titleEl.className = 'session-item-title';
    titleEl.textContent = truncateTitle(sess.title);
    item.appendChild(titleEl);

    // 更多按钮（hover 或选中时显示）
    const moreBtn = document.createElement('button');
    moreBtn.className = 'session-item-more-btn';
    moreBtn.innerHTML = '<svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor"><circle cx="8" cy="3" r="1.5"/><circle cx="8" cy="8" r="1.5"/><circle cx="8" cy="13" r="1.5"/></svg>';
    moreBtn.dataset.tooltip = '更多操作';
    item.appendChild(moreBtn);

    // 点击会话项 — 切换到该会话
    item.addEventListener('click', (e) => {
        // 如果点击的是 moreBtn，不触发切换
        if (e.target.closest('.session-item-more-btn')) return;
        selectSession(sess.sn);
    });

    // 点击更多按钮 — 显示上下文菜单
    moreBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        showContextMenu(e, sess);
    });

    // 右键菜单
    item.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        showContextMenu(e, sess);
    });

    return item;
}

/**
 * 选中一个会话
 */
function selectSession(sn) {
    // 更新高亮
    document.querySelectorAll('.session-item').forEach(el => {
        el.classList.toggle('active', el.dataset.sn === sn);
    });
    activeSessionSN = sn;

    // 关闭右键菜单
    closeContextMenu();

    // TODO: 后续实现切换到该会话的逻辑
    // 目前先显示 toast 提示
    const sess = currentSessions.find(s => s.sn === sn);
    if (sess) {
        showToast(`已选择: ${truncateTitle(sess.title)}`, 'info');
    }
}

// ============================================================
// 上下文菜单（重命名、置顶、删除）
// ============================================================

/**
 * 显示上下文菜单
 */
function showContextMenu(e, sess) {
    closeContextMenu();

    contextTargetSN = sess.sn;

    const menu = document.createElement('div');
    menu.className = 'session-context-menu';
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
    renameItem.className = 'session-context-menu-item';
    renameItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_EDIT + '</svg> 重命名';
    renameItem.addEventListener('click', () => {
        closeContextMenu();
        handleRename(sess);
    });
    menu.appendChild(renameItem);

    // 置顶/取消置顶
    const pinItem = document.createElement('div');
    pinItem.className = 'session-context-menu-item';
    if (sess.pinned) {
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2z"/></svg> 取消置顶';
    } else {
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2z"/></svg> 置顶';
    }
    pinItem.addEventListener('click', () => {
        closeContextMenu();
        handleTogglePin(sess);
    });
    menu.appendChild(pinItem);

    // 删除（警告色）
    const deleteItem = document.createElement('div');
    deleteItem.className = 'session-context-menu-item session-context-menu-item-danger';
    deleteItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg> 删除';
    deleteItem.addEventListener('click', () => {
        closeContextMenu();
        handleDelete(sess);
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
 * 重命名会话
 */
async function handleRename(sess) {
    showTitleEditDialog({
        currentTitle: sess.title || '',
        onConfirm: async (newTitle) => {
            try {
                const response = await fetch('/api/session/title?title=' + encodeURIComponent(newTitle) +
                    '&state=' + TITLE_STATE.USER + '&sn=' + encodeURIComponent(sess.sn), {
                    method: 'PUT',
                });
                if (!response.ok) {
                    showToast('重命名失败', 'error');
                    return false;
                }
                // 更新本地数据
                sess.title = newTitle;
                // 重新渲染列表
                renderSessionList(currentSessions, activeSessionSN);
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
async function handleTogglePin(sess) {
    const newPinned = !sess.pinned;
    try {
        const response = await fetch('/api/session/pin?sn=' + encodeURIComponent(sess.sn) +
            '&pinned=' + newPinned, {
            method: 'PUT',
        });
        if (!response.ok) {
            showToast('操作失败', 'error');
            return;
        }
        // 更新本地数据
        sess.pinned = newPinned;
        // 重新渲染列表
        renderSessionList(currentSessions, activeSessionSN);
        showToast(newPinned ? '已置顶' : '已取消置顶', 'success');
    } catch (e) {
        showToast('操作出错', 'error');
    }
}

/**
 * 删除会话
 */
async function handleDelete(sess) {
    if (!confirm(`确定要删除对话「${truncateTitle(sess.title)}」吗？\n此操作不可撤销。`)) {
        return;
    }

    try {
        const response = await fetch('/api/session?sn=' + encodeURIComponent(sess.sn), {
            method: 'DELETE',
        });
        if (!response.ok) {
            showToast('删除失败', 'error');
            return;
        }
        // 从本地数据移除
        const idx = currentSessions.findIndex(s => s.sn === sess.sn);
        if (idx >= 0) {
            currentSessions.splice(idx, 1);
        }
        // 重新渲染列表
        renderSessionList(currentSessions, activeSessionSN);
        showToast('已删除', 'success');
    } catch (e) {
        showToast('删除出错', 'error');
    }
}
