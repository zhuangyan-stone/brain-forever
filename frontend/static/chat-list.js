// ============================================================
// chat-list.js — 对话列表组件
// 在左侧栏显示用户的对话列表，按时间分组展示
// ============================================================

import { chatStreamMgr } from './chat-stream-mgr.js';
import { activeTickIndex, setActiveTickIndex, tickScrollOffset, setTickScrollOffset, resetTickState } from './tick-state.js';
import { showToast, addMessage, updateHeaderTitle, showWelcomeMessage, showTokenUsage, applyStreamingState, autoScrollToBottom } from './chat-ui.js';
import { putChatTitle, TITLE_STATE, switchChat } from './chat-api.js';
import { showTitleEditDialog } from './dialogs/title-edit-dialog.js';
import { updateTickNav } from './chat-ticknav.js';
import { ICON_EDIT, ICON_DELETE, ICON_PIN } from './svg_icons_re.js';
import msgbox from './components/msgbox.js';
import { renderMarkdown } from './chat-markdown.js';

'use strict';


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

export let currentChats = [];       // 当前对话列表
let activeChatSN = null;     // 当前选中的对话 SN
let contextMenuEl = null;       // 当前打开的右键菜单
let contextTargetSN = null;     // 右键菜单目标对话 SN
let hoverMenuTimer = null;     // hover 弹出菜单的关闭定时器

/**
 * 清除当前激活的选中状态（新对话等场景使用）
 * 移除所有 .chat-item 的 .active 类，重置 activeChatSN 为 null
 */
export function clearActiveChat() {
    activeChatSN = null;
    // 同步到 Alpine store
    try {
        var chatsStore = window.Alpine.store('chats');
        if (chatsStore) {
            chatsStore.activeChatSN = null;
        }
    } catch(e) {}
}

/**
 * 在侧边栏中插入一条新对话条目。
 * 侧边栏只体现已有真实 SN 的条目（即已确认存在于后端 chats[] 中的对话）。
 * 如果 sn 为空（blankChat 尚未被后端确认），则不加入侧边栏，
 * 后续由 getCurrentChatIfNeeded → updateChatEntry 拿到真实 SN 后再添加。
 * @param {string} title - 新对话的标题（截取自首条用户消息）
 * @param {string} [sn] - 对话 SN，必须为有效值才加入侧边栏
 */
export function addDirtyChat(title, sn) {
    // 没有有效 SN 的 blankChat 不加入侧边栏
    if (!sn) {
        return;
    }

    // 如果该 SN 已存在于 currentChats 中（例如从 chat_created 和 updateChatEntry 两次添加），
    // 仅更新标题，避免重复插入。
    const existing = currentChats.find(c => c.sn === sn);
    if (existing) {
        if (title) existing.title = title;
        activeChatSN = sn;
        renderChatList(currentChats, activeChatSN);
        return;
    }

    const dirtyChat = {
        id: 0,           // 尚未在 DB 中创建，id 为 0
        sn: sn,
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

    // 已有真实 SN，设置 activeChatSN，
    // 这样后续 updateCurrentChatTitle 才能正确找到 DOM 元素并更新标题。
    activeChatSN = sn;

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

    // 关闭可能打开的右键菜单
    closeContextMenu();

    // 同步到 Alpine store — Alpine 模板会响应式更新 DOM
    try {
        var chatsStore = window.Alpine.store('chats');
        if (chatsStore) {
            chatsStore.restructChatLists(chats, activeSN);
        }
    } catch(e) {}
}

/**
 * 选中一个对话 — 加载该对话的消息并渲染到主区域
 *
 * 变更（SSEResponser 重构后）：
 *   - 通过 chatStreamMgr.activateSession() 准备会话
 *   - 旧对话的 SSE 连接继续在后台接收数据（不 abort）
 *   - 旧对话的 DOM 引用被释放，但 streamingMsg 保留
 */
async function selectChat(sn) {
    // 记录是否切换到不同对话 — 统一经过关抽屉逻辑后，决定是否跳过切换
    let hasChanged = activeChatSN !== sn;
    if (hasChanged) activeChatSN = sn;

    // 一次性获取 Alpine store 引用，避免函数内反复调用 window.Alpine.store('chats')
    // 如果 store 不可用，继续执行无意义，直接返回
    var chats = window.Alpine.store('chats');
    if (!chats) return;

    // 同步到 Alpine store（侧边栏高亮 + 关闭抽屉）
    if (hasChanged || chats.activeChatSN !== activeChatSN) {
        chats.activeChatSN = activeChatSN;
    }

    if (chats.closeDrawer) {
        chats.closeDrawer();
    }

    // 点击同一对话（如当前活动 chat 标题），关完抽屉就返回，不做切换
    if (!hasChanged) return;

    // 切换 activeIndex + 清空 groups
    chats.getOrCreate(sn);
    chats.switchTo(sn);
    if (chats.active) {
        chats.active.groups = [];
        chats.active._groupSeq = 0;
    }

    // 关闭右键菜单
    closeContextMenu();

    // 0. 通过 chatStreamMgr 准备会话（getOrCreate + flushToDOM）
    // 活跃状态由 Alpine.store('chats').switchTo() 管理
    chatStreamMgr.activateSession(sn);

    // 1. 清空当前消息状态
    resetTickState();

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
    // ★ 后端在创建新对话时 title 为空字符串（尚未同步前端标题），
    //   此时 fallback 到 Alpine store 中已设置的标题
    //   （addUserMessage 时写入的 truncateTitle），避免 header 残留旧标题。
    //   注意：此时 chats.switchTo(sn) 已在开头执行，active 指向目标对话。
    if (result.title) {
        updateHeaderTitle(result.title);
    } else if (chats.active && chats.active.title) {
        updateHeaderTitle(chats.active.title);
    }
    if (typeof result.title_state === 'number' && chats.active) {
        chats.active.titleState = result.title_state;
    }

    // 8. 渲染消息 — 通过 Alpine store 的 groups 数据驱动
    // 转换 messages → groups 并设置到 Alpine store（按 SN 查找，而非假定 active）
    chats.setChatMessageGroups(sn, result.messages);

    // 8.1 渲染 reasoning、sources、token-info（这些仍由 JS 管理，Alpine 未覆盖）
    // 需要等待 Alpine 渲染完成后操作 DOM
    requestAnimationFrame(function() {
        const chatContainer = document.getElementById('chatContainer');
        if (!chatContainer) return;
        var msgIndex = 0;
        var groupEls = chatContainer.querySelectorAll('.message-group');
        for (var gi = 0; gi < groupEls.length && msgIndex < result.messages.length; gi++) {
            var groupEl = groupEls[gi];
            var userMsg = groupEl.querySelector('.message.user');
            var assistantMsg = groupEl.querySelector('.message.assistant');
            // 找到对应的 result.messages 条目
            var userEntry = null;
            var assistantEntry = null;
            if (msgIndex < result.messages.length && result.messages[msgIndex].role === 'user') {
                userEntry = result.messages[msgIndex];
                msgIndex++;
            }
            if (msgIndex < result.messages.length && result.messages[msgIndex].role === 'assistant') {
                assistantEntry = result.messages[msgIndex];
                msgIndex++;
            }
            if (assistantEntry) {
                if (assistantEntry.usage && assistantMsg) {
                    showTokenUsage(assistantMsg, assistantEntry.usage);
                }
                // sources 已由 Alpine 响应式渲染（group.assistant.sources 在 setChatMessageGroups 中设置）
            }
        }
    });

    // 9. 检查当前 session 是否有流式输出的累积数据需要渲染
    // 场景 A：切换回一个正在后台流式输出的对话（!isDone）
    //   不依赖 lastIsAssistant 判断，因为后端可能因 assistant 未写入 DB
    //   而追加了 broken message（interrupted=2），导致 lastIsAssistant=true。
    //   此时应优先使用 streamingMsg 的数据恢复流式状态。
    // 场景 B：切换回一个流式已完成的对话（isDone），但 DOM 引用已被释放
    const stream = chatStreamMgr.get(sn);

    // 从 Alpine store 获取 streamingMsg（ChatSession 不再持有）
    var streamingMsg = null;
    var chatData = chats.getOrCreate(sn);
    if (chatData) streamingMsg = chatData.streamingMsg;

    if (stream && streamingMsg && !streamingMsg.isDone) {
        // 场景 A：流未完成
        // 如果后端返回的 messages 最后一条是后端追加的 broken message（interrupted=2），
        // 将其从 result.messages 中移除，避免 broken message 污染界面。
        // 注意：用户主动中断时（interrupted=1），broken message 是追加在 assistant
        // 消息 content 末尾的，不会作为独立消息出现，因此不受此影响。
        var lastMsg = result.messages[result.messages.length - 1];
        if (lastMsg && lastMsg.role === 'assistant' && lastMsg.interrupted === 2) {
            result.messages.pop();
            // 重新设置 groups（不含 broken message）
            chats.setChatMessageGroups(sn, result.messages);
        }

        // 将 streamingMsg 的累积数据同步到最后一个 group 的 assistant
        // ★ 不 push 新 group（会导致 user:null，触发 Alpine 模板 crash）。
        //   最后一个 group 已在 setChatMessageGroups 中创建（含 user 数据），
        //   此处只需将 streaming 累积内容填入 assistant 即可。
        var chatData = chats.getOrCreate(sn);
        if (chatData && chatData.groups.length > 0) {
            var lastGroup = chatData.groups[chatData.groups.length - 1];
            var asst = lastGroup.assistant;
            asst.content = streamingMsg.content || '';
            asst.reasoning = streamingMsg.reasoning || null;
            asst.reasoningState = streamingMsg.reasoning ? (streamingMsg.reasoningState || 'thinking') : undefined;
            asst.contentHTML = streamingMsg.content ? renderMarkdown(streamingMsg.content) : '';
            asst.reasoningHTML = streamingMsg.reasoning ? renderMarkdown(streamingMsg.reasoning) : undefined;
        }

        // 获取 Alpine 渲染后的 DOM 引用
        requestAnimationFrame(function() {
            var chatContainer = document.getElementById('chatContainer');
            if (!chatContainer) return;
            var lastGroupEl = chatContainer.querySelector('.message-group:last-child');
            if (!lastGroupEl) return;
            var streamingBubble = lastGroupEl.querySelector('.bubble.streaming');
            var assistantMsgEl = lastGroupEl.querySelector('.message.assistant');
            if (assistantMsgEl) {
                stream.assistantBubble = assistantMsgEl;
                stream.contentDiv = streamingBubble || assistantMsgEl.querySelector('.bubble');
            }

            // 将已有累积内容渲染到 DOM
            // 从 Alpine store 获取 streamingMsg（ChatSession 不再持有）
            var sm = null;
            var chatData = chats.getOrCreate(sn);
            if (chatData) sm = chatData.streamingMsg;

            // 标记流式状态
            applyStreamingState(true);
        });
    } else if (stream && streamingMsg && streamingMsg.isDone && !stream.assistantBubble) {
        // 场景 B：流已完成但 DOM 引用已释放
        // 将 streamingMsg 的完成数据同步到最后一个 group 的 assistant
        // ★ 不 push 新 group（会导致 user:null，触发 Alpine 模板 crash）。
        //   最后一个 group 已在 setChatMessageGroups 中创建（含 user 数据）。
        var chatData = chats.getOrCreate(sn);
        if (chatData && chatData.groups.length > 0) {
            var sm = streamingMsg;
            var lastGroup = chatData.groups[chatData.groups.length - 1];
            var asst = lastGroup.assistant;
            asst.content = sm.content || '';
            asst.createdAt = sm.createdAt || null;
            asst.reasoning = sm.reasoning || undefined;
            asst.reasoningState = sm.reasoning ? 'done' : undefined;
            asst.sources = sm.sources && sm.sources.length > 0 ? sm.sources.slice() : undefined;
            asst.usage = sm.usage || undefined;
            asst.contentHTML = renderMarkdown(sm.content || '');
            asst.reasoningHTML = sm.reasoning ? renderMarkdown(sm.reasoning) : undefined;
            lastGroup.msgId = sm.msgId || lastGroup.msgId;
        }

        // 获取 Alpine 渲染后的 DOM 引用
        requestAnimationFrame(function() {
            var chatContainer = document.getElementById('chatContainer');
            if (!chatContainer) return;
            var lastGroupEl = chatContainer.querySelector('.message-group:last-child');
            if (!lastGroupEl) return;
            var assistantMsgEl = lastGroupEl.querySelector('.message.assistant');
            if (assistantMsgEl) {
                stream.assistantBubble = assistantMsgEl;
                stream.contentDiv = assistantMsgEl.querySelector('.bubble');
            }

            // 渲染 reasoning/usage
            if (stream.assistantBubble) {
                // 从 Alpine store 获取 streamingMsg（ChatSession 不再持有）
                var sm = null;
                var chatData = chats.getOrCreate(sn);
                if (chatData) sm = chatData.streamingMsg;
                if (sm && sm.usage) {
                    showTokenUsage(stream.assistantBubble, sm.usage);
                }
            }

            // 流已结束，确保非 Alpine 管理的 DOM 元素重置
            applyStreamingState(false);
        });
    } else {
        // 新增：切换到无流式状态的普通 chat，确保 UI 处于非流式状态
        applyStreamingState(false);
    }

    // 10. 等待 Alpine 渲染完成后更新刻度导航并自动滚到底部
    // Alpine x-for 异步渲染 DOM，同步调用 updateTickNav 会因找不到 .message.user 元素而无效
    window.Alpine.nextTick(function() {
        updateTickNav();
        autoScrollToBottom();
    });

    // 11. 清理已完成的非活跃 stream（释放内存）
    // 切换对话后，旧对话变为非活跃，触发 cleanup 回收
    chatStreamMgr.cleanup();
}

// ============================================================
// 上下文菜单（重命名、置顶、删除）
// ============================================================

/**
 * 显示上下文菜单
 */
function showContextMenu(e, chat) {
    // 如果菜单已打开且指向同一个 chat，不重复创建
    if (contextMenuEl && contextTargetSN === chat.sn) {
        return;
    }
    closeContextMenu();

    contextTargetSN = chat.sn;

    const menu = document.createElement('div');
    menu.className = 'chat-context-menu';
    menu.style.position = 'fixed';

    // 计算菜单位置
    // 使用 e.currentTarget 而非 e.target：当点击 SVG 内 <path> 子元素时，
    // e.target 可能是路径元素而非按钮本身，其 bounding rect 可能极小或为零，
    // 导致菜单定位异常。e.currentTarget 始终指向事件绑定的按钮元素。
    const rect = e.currentTarget.getBoundingClientRect();
    const menuWidth = 160;
    const menuHeight = 36 * 3 + 4; // 3 items * 36px + padding

    const isSmallScreen = document.body.classList.contains('small-screen-mode');
    let left, top;

    if (isSmallScreen) {
        // 小屏抽屉模式下：菜单以 dropdown 形式出现在按钮下方，
        // 水平对齐按钮左侧，垂直在按钮底部，更符合移动端 UX 习惯。
        left = rect.left;
        top = rect.bottom + 4;

        // 防止菜单超出右边界
        if (left + menuWidth > window.innerWidth) {
            left = window.innerWidth - menuWidth - 8;
        }
        // 防止菜单超出下边界（超出则翻转到按钮上方）
        if (top + menuHeight > window.innerHeight) {
            top = rect.top - menuHeight - 4;
        }
    } else {
        // 宽屏模式下：菜单出现在按钮右侧（经典上下文菜单位置）
        left = rect.right + 4;
        top = rect.top;

        // 防止菜单超出右边界
        if (left + menuWidth > window.innerWidth) {
            left = rect.left - menuWidth - 4;
        }
        // 防止菜单超出下边界
        if (top + menuHeight > window.innerHeight) {
            top = window.innerHeight - menuHeight - 8;
        }
    }

    menu.style.left = Math.max(4, left) + 'px';
    menu.style.top = Math.max(4, top) + 'px';

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
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_PIN + '</svg> 取消置顶';
    } else {
        pinItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_PIN + '</svg> 置顶';
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

    // 大屏 hover 模式：菜单在鼠标离开时自动关闭（带延迟，防止移动到菜单时闪烁）
    if (!isSmallScreen) {
        // 鼠标进入菜单 → 取消关闭定时器
        menu.addEventListener('mouseenter', function onMenuEnter() {
            if (hoverMenuTimer) {
                clearTimeout(hoverMenuTimer);
                hoverMenuTimer = null;
            }
        });
        // 鼠标离开菜单 → 延迟关闭
        menu.addEventListener('mouseleave', function onMenuLeave() {
            if (hoverMenuTimer) {
                clearTimeout(hoverMenuTimer);
            }
            hoverMenuTimer = setTimeout(function() {
                closeContextMenu();
            }, 200);
        });
    }

    // 点击其他地方关闭菜单
    setTimeout(() => {
        document.addEventListener('click', closeContextMenu, { once: true });
    }, 0);
}

/**
 * 关闭上下文菜单
 */
function closeContextMenu() {
    if (hoverMenuTimer) {
        clearTimeout(hoverMenuTimer);
        hoverMenuTimer = null;
    }
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
 *
 * 注意：此处检查目标 chat 自身的 streaming 状态（而非 active chat），
 * 因为侧边栏重命名操作针对的是右键点击的特定对话，不一定是当前活跃对话。
 * header 标题编辑（chat.js）直接读取 Alpine.store('chats').active?.isStreaming，
 * 因为 header 始终对应 active chat。
 */
async function handleRename(chat) {
    // 检查目标对话自身的 streaming 状态（从 Alpine store 读取）
    var targetIsStreaming = false;
    try {
        var chats = window.Alpine.store('chats');
        if (chats) {
            var chatData = chats.getOrCreate(chat.sn);
            if (chatData) targetIsStreaming = !!chatData.isStreaming;
        }
    } catch(e) {}
    if (targetIsStreaming) {
        showToast('该对话正在生成回复，请稍后再修改标题', 'info');
        return;
    }

    // 脏对话（临时 SN，尚未被后端确认）不允许修改标题
    try {
        var chats2 = window.Alpine.store('chats');
        if (chats2 && chats2.isDirtyChat && chats2.isDirtyChat(chatData)) {
            showToast('该对话尚未完成创建，请稍后再修改标题', 'info');
            return;
        }
    } catch(e) {}

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
        return;
    }

    // 更新内存中的标题
    const chat = currentChats.find(c => c.sn === sn);
    if (chat) {
        chat.title = newTitle;
    }

    // 直接更新 Alpine 模板渲染的 DOM
    const activeItems = document.querySelectorAll(`.chat-item[data-sn="${sn}"] .chat-item-title`);
    if (activeItems.length > 0) {
        activeItems.forEach(el => {
            el.textContent = truncateTitle(newTitle);
        });
    }
}

/**
 * 根据 SN 更新指定对话的标题（仅当该 chat 仍存在于 currentChats 中时）。
 * 用于 AI 标题推荐的回调中，确保标题始终更新到正确的对话上，
 * 即使当前活跃对话已切换，或该对话已被删除。
 *
 * 如果 chat 不存在于 currentChats 中（已被删除），则静默跳过。
 * 成功更新后重新渲染侧边栏列表。
 *
 * @param {string} sn - 目标对话的 SN
 * @param {string} newTitle - 新标题
 */
export function updateChatTitleBySN(sn, newTitle) {
    if (!sn || !newTitle) return;

    // 在 currentChats 中查找目标 chat
    const chat = currentChats.find(c => c.sn === sn);
    if (!chat) {
        // chat 已被删除（不存在于列表中），静默跳过
        return;
    }

    // 更新内存中的标题
    chat.title = newTitle;

    // 直接更新 Alpine 模板渲染的 DOM
    const targetItems = document.querySelectorAll(`.chat-item[data-sn="${sn}"] .chat-item-title`);
    if (targetItems.length > 0) {
        targetItems.forEach(el => {
            el.textContent = truncateTitle(newTitle);
        });
    }
}

/**
 * 更新或添加侧边栏中的单个对话条目。
 * 由 syncSidebarChatEntry 在第一轮对话完成后调用，
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
        const response = await fetch('/api/chat?sn=' + encodeURIComponent(chat.sn), {
            method: 'DELETE',
        });
        if (!response.ok) {
            showToast('删除失败', 'error');
            return;
        }

        // 0. 通过 chatStreamMgr 移除 ChatStream（abort 正在进行的 SSE 流）
        chatStreamMgr.remove(chat.sn);

        // 从本地数据移除
        const idx = currentChats.findIndex(c => c.sn === chat.sn);
        if (idx >= 0) {
            currentChats.splice(idx, 1);
        }

        // 从 Alpine store 的 items[] 中同步移除 ChatData
        try {
            var chats = window.Alpine.store('chats');
            if (chats) {
                chats.removeChat(chat.sn);
            }
        } catch(e) {}

        // 如果删除的是当前活动对话，清空主界面（消息、标题、刻度导航等）
        if (activeChatSN === chat.sn) {
            // 清空消息状态
            resetTickState();

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

// ============================================================
// 注册方法到 Alpine store
// ============================================================
// 侧边栏聊天列表由 Alpine x-for 模板渲染（index.html），
// 模板中通过 $store.chats.selectChat(sn) 调用。
// chat-list.js 是 ES Module，执行时 Alpine store 已就绪。
// ============================================================
try {
    var chats = window.Alpine.store('chats');
    if (chats) {
        chats.selectChat = selectChat;
        chats.showContextMenu = showContextMenu;
        // 当鼠标进入某个对话项时，如果当前有打开的上下文菜单且属于其他对话，立即关闭。
        // 由 Alpine 模板 @mouseenter="$store.chats.maybeCloseContextMenu(chat)" 调用。
        chats.maybeCloseContextMenu = function(chat) {
            if (contextMenuEl && contextTargetSN !== chat.sn) {
                closeContextMenu();
            }
        };

        /**
         * setSidebarChats — 替换侧边栏对话列表（切换用户时使用）。
         * 更新模块级 currentChats 变量，并通过 restructChatLists 刷新 Alpine 响应式渲染。
         * 供 chat-api.js 等外部模块在切换用户后调用，避免循环依赖。
         * @param {Array} chatList - 新的对话数组
         * @param {string} [activeSN] - 当前选中的对话 SN
         */
        chats.setSidebarChats = function(chatList, activeSN) {
            closeContextMenu();
            currentChats = chatList || [];
            activeChatSN = activeSN || null;
            chats.restructChatLists(chatList, activeSN);
        };
    }
} catch(e) {
    console.warn('chat-list: 无法注册到 Alpine store', e);
}
