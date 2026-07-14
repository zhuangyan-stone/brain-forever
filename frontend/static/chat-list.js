// ============================================================
// chat-list.js — 对话列表组件
// 在左侧栏显示用户的对话列表，按时间分组展示
// ============================================================

import { chatStreamMgr } from './chat-stream-mgr.js';
import { activeTickIndex, setActiveTickIndex, tickScrollOffset, setTickScrollOffset, resetTickState } from './tick-state.js';
import { showToast, showToastHTML, addMessage, updateHeaderTitle, showWelcomeMessage, showTokenUsage, applyStreamingState, autoScrollToBottom } from './chat-ui.js';
import { putChatTitle, TITLE_STATE, switchChat, togglePinChat, deleteChat, restoreChat, permanentDeleteChat, listDeletedChats, emptyTrash, createBlankChat, fetchChatTags, addFavoriteChat, removeFavoriteChat } from './chat-api.js';
import { extractTraits } from './trait-api.js';
import { updateTickNav } from './chat-ticknav.js';
import { ICON_EDIT, ICON_DELETE, ICON_PIN, ICON_TRASH, ICON_TRASH_RESTORE, ICON_STAR } from './svg_icons_re.js';
import msgbox from './components/msgbox.js';
import { renderMarkdown } from './chat-markdown.js';
import { visualLength, truncateByVisualLength, escapeHtml } from './toolsets.js';

'use strict';


/**
 * 截取标题，最多25视觉长度（中文汉字算 1.5）
 */
function truncateTitle(title, maxLen = 25) {
    const defaultTitle = '新对话';
    if (!title) return defaultTitle;
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
    if (visualLength(result) > maxLen) {
        return truncateByVisualLength(result, maxLen);
    }
    return result || defaultTitle;
}

// ============================================================
// 对话列表渲染
// ★ 迁移完成：currentChats → store.chats，activeChatSN → store.activeChatSN
// ============================================================

let contextMenuEl = null;       // 当前打开的右键菜单
let contextTargetSN = null;     // 右键菜单目标对话 SN
let hoverMenuTimer = null;     // hover 弹出菜单的关闭定时器

/**
 * 清除当前激活的选中状态（新对话等场景使用）
 * 移除所有 .chat-item 的 .active 类，重置 activeChatSN 为 null
 * ★ 迁移后：只写 Alpine store，模块变量不再需要
 */
export function clearActiveChat() {
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
 * ★ 迁移后：操作 store.chats 替代模块变量 currentChats
 */
export function addDirtyChat(title, sn) {
    // 没有有效 SN 的 blankChat 不加入侧边栏
    if (!sn) {
        return;
    }

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const chatList = chatsStore.chats;

    // 如果该 SN 已存在于 chatList 中（例如从 chat_created 和 updateChatEntry 两次添加），
    // 仅更新标题，避免重复插入。
    const existing = chatList.find(c => c.sn === sn);
    if (existing) {
        if (title) existing.title = title;
        chatsStore.activeChatSN = sn;
        renderChatList(chatList, sn);
        return;
    }

    const dirtyChat = {
        id: 0,           // 尚未在 DB 中创建，id 为 0
        sn: sn,
        title: title,    // 标题由前端基于首条消息生成
        title_state: 0,  // 原始标题
        pinned: false,
        taged: false,
        role_no: 0,
        create_at: new Date().toISOString(),
        update_at: new Date().toISOString(),
    };

    // 插入到列表头部（最新消息位置）
    chatList.unshift(dirtyChat);

    // 已有真实 SN，设置 activeChatSN，供后续 updateChatTitleBySN 等函数正确找到 DOM 元素并更新标题。
    chatsStore.activeChatSN = sn;

    // 复用 renderChatList 的完整渲染逻辑
    renderChatList(chatList, sn);
}

/**
 * 渲染对话列表到左侧栏
 * @param {Array} chats - 对话数组
 * @param {string} [activeSN] - 当前激活的对话 SN
 */
export function renderChatList(chats, activeSN) {
    // 关闭可能打开的右键菜单
    closeContextMenu();

    // 写入 Alpine store — restructChatLists 内部会同步到 store.chats
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
 *
 * ★ 迁移后：基于 Alpine store 的 activeChatSN 作为单一数据源判断，
 *   不再依赖模块变量 activeChatSN。第二个防御性条件不再需要。
 */
async function selectChat(sn, source, subSource) {
    // 一次性获取 Alpine store 引用，避免函数内反复调用 window.Alpine.store('chats')
    // 如果 store 不可用，继续执行无意义，直接返回
    var chats = window.Alpine.store('chats');
    if (!chats) return;

    // 记录点击来源区间，用于分类 tab 下多实例 active 样式区分
    chats.activeChatSource = source || null;
    chats.activeSubSource = subSource || null;

    // 基于 Alpine store 作为单一数据源判断是否切换到不同对话
    let hasChanged = chats.activeChatSN !== sn;
    if (hasChanged) chats.activeChatSN = sn;

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
        // ★ 延迟二次滚动兜底：Alpine 渲染完成后，输入面板展开或字体/图片加载
        //   可能导致页面高度变化，初次 scrollTop 可能未达真正底部。
        //   类似 _applyDoneToDOM 中 480ms 延迟二次滚动的策略。
        setTimeout(function() {
            autoScrollToBottom();
        }, 480);
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
    const menuHeight = 36 * 6 + 4 + 10; // 6 items * 36px + padding + separator

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

    // 收藏
    const favItem = document.createElement('div');
    favItem.className = 'chat-context-menu-item';
    favItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_STAR + '</svg> 收藏';
    favItem.addEventListener('click', () => {
        closeContextMenu();
        handleToggleFavorite(chat);
    });
    menu.appendChild(favItem);

    // 重命名
    const renameItem = document.createElement('div');
    renameItem.className = 'chat-context-menu-item';
    renameItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_EDIT + '</svg> 重命名';
    renameItem.addEventListener('click', () => {
        closeContextMenu();
        handleRename(chat);
    });
    menu.appendChild(renameItem);

    // 分隔线
    const sepAfterFav = document.createElement('div');
    sepAfterFav.className = 'chat-context-menu-separator';
    menu.appendChild(sepAfterFav);

    // 提取个人特征 — 已完全交由自动触发，不再允许手工点击
    // 从未提取（extracted_at == null）时不显示此菜单项
    // 已提取时仅显示状态信息（禁用态，不可点击）
    const hasExtracted = !!chat.extracted_at;
    if (hasExtracted) {
    	const traitItem = document.createElement('div');
    	traitItem.className = 'chat-context-menu-item';

    	const hasCount = chat.extracted_count > 0;
    	let traitLabel = hasCount
    		? '已提取个人特征 ' + chat.extracted_count + ' 条'
    		: '暂未发现个人特征';

    	traitItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + window.ICON_USER + '</svg> ' + traitLabel;
    	traitItem.classList.add(hasCount
    		? 'chat-context-menu-item-success'
    		: 'chat-context-menu-item-disabled');

    	menu.appendChild(traitItem);
    }

    // 话题分类
    const tagItem = document.createElement('div');
    tagItem.className = 'chat-context-menu-item';
    tagItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"></path><line x1="7" y1="7" x2="7.01" y2="7"></line></svg> 归类';
    tagItem.addEventListener('click', async () => {
        closeContextMenu();
        // 开始申请归类，显示提示（5 秒后自动消失，结果返回后会再显示结果 Toast）
        showToast('📑 正在申请归类', 'info', 5000);
        // 🔍 调试：观察 chat.sn 是否为空
        console.log('📑 [归类调试] chat:', JSON.stringify(chat, ['id','sn','title','taged','pinned']));
        console.log('📑 [归类调试] chat.sn:', JSON.stringify(chat.sn), 'typeof:', typeof chat.sn, 'length:', chat.sn ? chat.sn.length : 'N/A');
        const result = await fetchChatTags(chat.sn);
        console.log('📑 [归类调试] fetchChatTags result:', JSON.stringify(result));
        const title = (result && result.title) || chat.title || '';
        if (result && result.tags && result.tags.length > 0) {
            console.log('📑 [' + title + ']归类：', JSON.stringify(result.tags, null, 2));
            // Toast 展示分类结果（第一行标签，第二行标题，第三行消息查看统计）
            // white-space: pre-wrap 让 \n 自动换行，无需 HTML
            var tagStr = result.tags.join('、');
            var displayTitle = title || '';
            var msg = '📑 归类：' + tagStr;
            if (displayTitle) {
                msg += '\n《' + displayTitle + '》';
            }
            // 显示 LLM 查看了多少条消息的统计信息
            if (typeof result.viewedMessages !== 'undefined') {
                var viewedInfo = '🔍 查看了 ' + result.viewedMessages + ' 条消息';
                if (result.totalMessages && result.totalMessages > 0) {
                    viewedInfo += '（共 ' + result.totalMessages + ' 条）';
                }
                if (result.allMessagesViewed) {
                    viewedInfo += ' ✅ 已看完全部';
                }
                msg += '\n' + viewedInfo;
            }
            showToast(msg, 'success', 6000);

            // ★ 归类成功后刷新分类 Tab 的缓存数据，确保切换到分类 Tab 时能看到新结果
            var chatsStore = window.Alpine.store('chats');
            if (chatsStore && chatsStore.loadChatGroups) {
                chatsStore.loadChatGroups();
            }
        } else {
            // 🔍 调试：区分"未匹配到分类"的具体原因
            if (!result) {
                console.log('📑 [归类调试] 未匹配到分类 — result 为 null (chat.sn=' + JSON.stringify(chat.sn) + ')');
            } else if (!result.tags) {
                console.log('📑 [归类调试] 未匹配到分类 — result.tags 不存在', JSON.stringify(result));
            } else {
                console.log('📑 [归类调试] 未匹配到分类 — result.tags 为空数组', JSON.stringify(result));
            }
            showToast('📑 未匹配到分类', 'info', 4000);
        }
    });
    menu.appendChild(tagItem);

    // 分隔线
    const separator = document.createElement('div');
    separator.className = 'chat-context-menu-separator';
    menu.appendChild(separator);

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
 * 提取个人特征 — 调用后端 API 提取指定对话的个人特征。
 * 结果先打印到浏览器控制台。
 */
async function handleExtractTraits(chat) {
    var sn = chat.sn;
    if (!sn) {
        showToast('无法提取特征：对话 SN 为空', 'error');
        return;
    }

    showToast('正在提取个人特征……', 'info', 5000);

    try {
        const result = await extractTraits(sn);
        if (!result) {
            return;
        }

        if (result.error) {
            showToast('提取个人特征出错: ' + result.error, 'error');
            return;
        }

        // 打印结果到浏览器控制台
        console.log('===== 个人特征提取结果 =====');
        console.log('对话 SN:', sn);
        console.log('新增特征数量:', (result.features || []).length);
        console.log('新增特征:', JSON.stringify(result.features, null, 2));
        if (result.usage) {
            console.log('Token 用量:', result.usage);
        }
        console.log('=============================');

        // 从后端响应中读取最新的提取状态，更新 chat 对象
        // 使右键菜单立即反映最新状态，无需猜测或本地计算
        if (result.extracted_at) {
            chat.extracted_at = result.extracted_at;
        }
        if (typeof result.extracted_count === 'number') {
            chat.extracted_count = result.extracted_count;
        }

        var featureCount = (result.features || []).length;
        if (featureCount > 0) {
            showToast('提取完成，新增 ' + featureCount + ' 条特征', 'success');
        } else {
            showToast('未提取到新的个人特征', 'success');
        }
    } catch (e) {
        console.error('提取个人特征异常:', e);
        showToast('提取个人特征异常', 'error');
    }
}

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

    window.showTitleEditDialog({
        currentTitle: chat.title || '',
        onConfirm: async (newTitle) => {
            const ok = await putChatTitle(newTitle, TITLE_STATE.USER, chat.sn);
            if (!ok) {
                return false;
            }
            // 更新本地数据
            chat.title = newTitle;
            // 重新渲染列表 — 从 store 读取
            var chatsStore = window.Alpine.store('chats');
            renderChatList(chatsStore ? chatsStore.chats : [], chatsStore ? chatsStore.activeChatSN : null);
            showToast('已重命名', 'success');
            return true;
        },
    });
}

/**
 * 取消收藏：直接从收藏夹移除（用于收藏树内的 chat）。
 */
async function handleUnfavorite(chat, customTag) {
    var ok = await removeFavoriteChat(chat.sn, customTag);
    if (!ok) {
        return;
    }
    var chatsStore = window.Alpine.store('chats');
    if (chatsStore && chatsStore.loadFavorites) {
        await chatsStore.loadFavorites();
    }
    showToast('已取消收藏', 'success');
}

/**
 * 处理收藏操作：弹出对话框选择目录，添加收藏。
 */
async function handleToggleFavorite(chat, defaultTag) {
    var chatsStore = window.Alpine.store('chats');

    // 确保收藏数据已加载，否则先加载
    if (chatsStore && !chatsStore.favoritesLoaded && chatsStore.loadFavorites) {
        await chatsStore.loadFavorites();
    }

    // 收集已有的收藏夹目录名
    var existingTags = [];
    if (chatsStore && chatsStore.favoritesGroups) {
        existingTags = Object.keys(chatsStore.favoritesGroups).filter(function(t) { return t !== ''; });
        existingTags.sort();
    }
    window.showFavoriteEditDialog({
        existingTags: existingTags,
        defaultTag: defaultTag || '',
        onConfirm: async function(customTag) {
            // 静默去除前后空白
            var tag = (customTag || '').trim();
            var isDuplicate = false;
            if (chatsStore && chatsStore.favoritesGroups) {
                var items = chatsStore.favoritesGroups[tag];
                if (items && items.some(function(c) { return c.sn === chat.sn; })) {
                    isDuplicate = true;
                }
            }
            if (isDuplicate) {
                showToast('该收藏已经存在，请勿重复操作', 'error');
                return false;
            }
            var ok = await addFavoriteChat(chat.sn, tag);
            if (!ok) {
                return false;
            }
            if (chatsStore && chatsStore.loadFavorites) {
                await chatsStore.loadFavorites();
            }
            showToast('已收藏到' + (tag || '根目录'), 'success');
            return true;
        },
    });
}

// ============================================================
// 类别页面的上下文菜单（收藏、重命名、提取个人特征、删除）
// ============================================================

/**
 * 显示类别页面的上下文菜单
 */
function showCategoryContextMenu(e, chat, tag) {
    if (contextMenuEl && contextTargetSN === chat.sn) {
        return;
    }
    closeContextMenu();

    contextTargetSN = chat.sn;

    const menu = document.createElement('div');
    menu.className = 'chat-context-menu';
    menu.style.position = 'fixed';

    const rect = e.currentTarget.getBoundingClientRect();
    const menuWidth = 160;
    const menuHeight = 36 * 4 + 4 + 10; // 4 items + padding + separator

    const isSmallScreen = document.body.classList.contains('small-screen-mode');
    let left, top;

    if (isSmallScreen) {
        left = rect.left;
        top = rect.bottom + 4;
        if (left + menuWidth > window.innerWidth) {
            left = window.innerWidth - menuWidth - 8;
        }
        if (top + menuHeight > window.innerHeight) {
            top = rect.top - menuHeight - 4;
        }
    } else {
        left = rect.right + 4;
        top = rect.top;
        if (left + menuWidth > window.innerWidth) {
            left = rect.left - menuWidth - 4;
        }
        if (top + menuHeight > window.innerHeight) {
            top = window.innerHeight - menuHeight - 8;
        }
    }

    menu.style.left = Math.max(4, left) + 'px';
    menu.style.top = Math.max(4, top) + 'px';

    // 从 UI 结构判断当前 chat 是否在收藏夹内
    var isInFavorites = e.currentTarget.closest('.chat-group-fav-sub') !== null;

    const favItem = document.createElement('div');
    favItem.className = 'chat-context-menu-item';
    if (isInFavorites) {
        // 收藏夹内的 chat → 取消收藏（传入 customTag 即 tag 参数）
        favItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_STAR + '</svg> 取消收藏';
        var favTag = tag || '';
        favItem.addEventListener('click', function() {
            closeContextMenu();
            handleUnfavorite(chat, favTag);
        });
    } else {
        // 普通分类内的 chat → 弹出对话框添加收藏
        favItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_STAR + '</svg> 收藏';
        var defaultTag = tag ? ('我的' + tag) : '';
        favItem.addEventListener('click', function() {
            closeContextMenu();
            handleToggleFavorite(chat, defaultTag);
        });
    }
    menu.appendChild(favItem);

    // 收藏与后续操作之间的分隔线
    const sepAfterFav = document.createElement('div');
    sepAfterFav.className = 'chat-context-menu-separator';
    menu.appendChild(sepAfterFav);

    // 重命名
    const renameItem = document.createElement('div');
    renameItem.className = 'chat-context-menu-item';
    renameItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_EDIT + '</svg> 重命名';
    renameItem.addEventListener('click', function() {
        closeContextMenu();
        handleRename(chat);
    });
    menu.appendChild(renameItem);

    // 分隔线
    const separator = document.createElement('div');
    separator.className = 'chat-context-menu-separator';
    menu.appendChild(separator);

    // 删除（警告色）
    const deleteItem = document.createElement('div');
    deleteItem.className = 'chat-context-menu-item chat-context-menu-item-danger';
    deleteItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg> 删除';
    deleteItem.addEventListener('click', function() {
        closeContextMenu();
        handleDelete(chat);
    });
    menu.appendChild(deleteItem);

    document.body.appendChild(menu);
    contextMenuEl = menu;

    // 大屏 hover 模式
    if (!isSmallScreen) {
        menu.addEventListener('mouseenter', function onMenuEnter() {
            if (hoverMenuTimer) {
                clearTimeout(hoverMenuTimer);
                hoverMenuTimer = null;
            }
        });
        menu.addEventListener('mouseleave', function onMenuLeave() {
            if (hoverMenuTimer) {
                clearTimeout(hoverMenuTimer);
            }
            hoverMenuTimer = setTimeout(function() {
                closeContextMenu();
            }, 200);
        });
    }

    setTimeout(function() {
        document.addEventListener('click', closeContextMenu, { once: true });
    }, 0);
}

/**
 * 切换置顶状态
 * ★ 迁移后：从 store.chats 获取列表
 */
async function handleTogglePin(chat) {
    const newPinned = !chat.pinned;
    const ok = await togglePinChat(chat.sn, newPinned);
    if (!ok) {
        return;
    }
    // 更新本地数据
    chat.pinned = newPinned;
    // 重新渲染列表 — restructChatLists 会从 store.chats 读取
    var chatsStore = window.Alpine.store('chats');
    renderChatList(chatsStore ? chatsStore.chats : [], chatsStore ? chatsStore.activeChatSN : null);
    showToast(newPinned ? '已置顶' : '已取消置顶', 'success');
}

/**
 * 根据 SN 更新指定对话的标题（仅当该 chat 仍存在于 store.chats 中时）。
 * 用于 AI 标题推荐的回调中，确保标题始终更新到正确的对话上，
 * 即使当前活跃对话已切换，或该对话已被删除。
 *
 * 如果 chat 不存在于 store.chats 中（已被删除），则静默跳过。
 * 成功更新后重新渲染侧边栏列表。
 *
 * ★ 迁移后：操作 store.chats 替代 currentChats
 *
 * @param {string} sn - 目标对话的 SN
 * @param {string} newTitle - 新标题
 */
export function updateChatTitleBySN(sn, newTitle) {
    if (!sn || !newTitle) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    // 在 store.chats 中查找目标 chat
    const chat = chatsStore.chats.find(c => c.sn === sn);
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
 *   - 如果该 SN 已存在于 store.chats 中，更新其标题
 *   - 如果不存在（新对话的脏数据尚未被后端确认），移除脏数据 (sn=null) 并添加新条目
 *   - 然后重新渲染整个列表
 *
 * ★ 迁移后：操作 store.chats 替代 currentChats
 *
 * @param {string} sn - 对话 SN（来自后端）
 * @param {string} title - 对话标题
 * @param {number} titleState - 标题修改状态
 */
export function updateChatEntry(sn, title, titleState) {
    if (!sn) return;

    var chatsStore = window.Alpine.store('chats');
    if (!chatsStore) return;

    const chatList = chatsStore.chats;

    // 检查该 SN 是否已存在（按真实 SN 查找）
    const existing = chatList.find(c => c.sn === sn);
    if (existing) {
        // 已存在：仅更新标题
        // 注意：后端在新对话时 chat.title 为空字符串，
        // 此时前端已有正确的原始标题，因此仅当 title 有值时更新。
        if (title) {
            existing.title = title;
        }
        existing.title_state = titleState;
    } else {
        // 不存在：移除脏数据（sn=null 的占位条目），然后添加真实条目
        var filtered = chatList.filter(c => c.sn !== null);
        // 原地替换数组内容（保持引用一致，避免 Alpine 丢失响应式追踪）
        chatList.length = 0;
        chatList.push.apply(chatList, filtered);

        // 创建新条目
        const now = new Date().toISOString();
        const newChat = {
            id: 0,
            sn: sn,
            title: title || '',
            title_state: titleState,
            pinned: false,
            taged: false,
            role_no: 0,
            create_at: now,
            update_at: now,
        };
        chatList.unshift(newChat);
    }

    // 重新渲染列表
    renderChatList(chatList, chatsStore.activeChatSN);
}

/**
 * 删除对话（逻辑删除 — 移入回收站）
 */
async function handleDelete(chat) {
    const result = await msgbox.confirm(`确认将〔${truncateTitle(chat.title)}〕\n移入回收站吗？`);
    if (result !== 1) {
        return;
    }

    const ok = await deleteChat(chat.sn);
    if (!ok) {
        return;
    }

    // 0. 通过 chatStreamMgr 移除 ChatStream（abort 正在进行的 SSE 流）
    chatStreamMgr.remove(chat.sn);

    // ★ 如果有该 chat 的 AI 标题推荐便利贴，一并清理（可能有多张）
    var titleStickies = document.querySelectorAll('.sticky-note[data-sn="' + chat.sn + '"]');
    if (titleStickies.length > 0) {
        titleStickies.forEach(function(note) {
            note.classList.add('leaving');
            note.addEventListener('animationend', function() {
                note.remove();
                var ctn = document.querySelector('.sticky-note-container');
                if (ctn && ctn.children.length === 0) {
                    ctn.remove();
                }
            }, { once: true });
        });
    }

    // 从 Alpine store 统一移除数据
    var chatsStore = window.Alpine.store('chats');

    // store 为空直接退出，后续不再反复判断
    if (!chatsStore) return;

    // ★ 在删除前记住它是不是 active chat
    const isDeletingActive = chatsStore.activeChatSN === chat.sn;

    // 从 chatList 中移除
    const idx = chatsStore.chats.findIndex(c => c.sn === chat.sn);
    if (idx >= 0) {
        var deletedChat = chatsStore.chats[idx];
        chatsStore.chats.splice(idx, 1);

        // ★ 回收站从未加载过 → 不往 UI 回收站丢（展开时会从服务端拉取全量数据）
        //   回收站已加载过 → 必须往回收站 UI 丢，保证本地数据与后端一致
        if (chatsStore.trashLoaded) {
            if (!chatsStore.deletedChats) {
                chatsStore.deletedChats = [];
            }
            // 确保不重复添加
            var existsInTrash = chatsStore.deletedChats.find(function(c) { return c.sn === chat.sn; });
            if (!existsInTrash) {
                chatsStore.deletedChats.unshift(deletedChat);
            }
        }
    }
    // 从 items[] 中同步移除 ChatData
    chatsStore.removeChat(chat.sn);

    // ★ 从智能分类树（chatGroups）中同步移除该 chat
    if (chatsStore.chatGroups) {
        var catChanged = false;
        for (var tag in chatsStore.chatGroups) {
            if (chatsStore.chatGroups.hasOwnProperty(tag)) {
                var catItems = chatsStore.chatGroups[tag];
                var catFiltered = catItems.filter(function(c) { return c.sn !== chat.sn; });
                if (catFiltered.length !== catItems.length) {
                    catChanged = true;
                    if (catFiltered.length > 0) {
                        chatsStore.chatGroups[tag] = catFiltered;
                    } else {
                        delete chatsStore.chatGroups[tag];
                    }
                }
            }
        }
        if (catChanged) {
            // 触发 Alpine 响应式更新
            chatsStore.chatGroups = Object.assign({}, chatsStore.chatGroups);
        }
    }

    // ★ 从收藏树（favoritesGroups）中同步移除该 chat
    if (chatsStore.favoritesGroups) {
        var favChanged = false;
        for (var favTag in chatsStore.favoritesGroups) {
            if (chatsStore.favoritesGroups.hasOwnProperty(favTag)) {
                var favItems = chatsStore.favoritesGroups[favTag];
                var favFiltered = favItems.filter(function(c) { return c.sn !== chat.sn; });
                if (favFiltered.length !== favItems.length) {
                    favChanged = true;
                    if (favFiltered.length > 0) {
                        chatsStore.favoritesGroups[favTag] = favFiltered;
                    } else {
                        delete chatsStore.favoritesGroups[favTag];
                    }
                }
            }
        }
        if (favChanged) {
            // 触发 Alpine 响应式更新
            chatsStore.favoritesGroups = Object.assign({}, chatsStore.favoritesGroups);
        }
    }

  
    // 如果删除的是当前活动对话，重置状态进入欢迎页
    if (isDeletingActive) {
        // 重置 Alpine store 状态为空白对话
        chatsStore.activeChatSN = '';
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

        // 清空刻度状态
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

        // ★ 调用后端 reset currentChat（删除前 deleteChat 已调用，再额外重置一次）
        await createBlankChat();

        // ★ nextTick 中调用 showWelcomeMessage，确保 Alpine 已处理完所有响应式更新
        //   （如 group 清空、activeIndex 变化等），再执行 DOM 重建。
        window.Alpine.nextTick(function() {
            showWelcomeMessage();
        });
    }

    // 重新渲染列表
    renderChatList(chatsStore.chats, chatsStore.activeChatSN);
    showToast('对话已移入回收站', 'success');

    // ★ 回收站从未加载过 → 在下一个 tick 展开它（触发 toggleTrash 拉取服务端全量数据）
    //   回收站已加载过 → 不再展开（本地已有完整数据，无需重复拉取）
    if (!chatsStore.trashLoaded) {
        window.Alpine.nextTick(function() {
            if (!chatsStore.trashExpanded) {
                chatsStore.toggleTrash();
            }
        });
    }
}

/**
 * 从回收站恢复对话
 */
async function handleRestore(chat) {
    const ok = await restoreChat(chat.sn);
    if (!ok) {
        return;
    }

    var chatsStore = window.Alpine.store('chats');
    if (chatsStore) {
        // 从 deletedChats 中移除
        var trashIdx = chatsStore.deletedChats.findIndex(function(c) { return c.sn === chat.sn; });
        if (trashIdx >= 0) {
            var restoredChat = chatsStore.deletedChats[trashIdx];
            chatsStore.deletedChats.splice(trashIdx, 1);

            // 确保 restoredChat 未被标记为 deleted
            restoredChat.deleted = false;

            // 加回主列表
            if (!chatsStore.chats) {
                chatsStore.chats = [];
            }
            // 检查是否已存在（防止重复）
            var exists = chatsStore.chats.find(function(c) { return c.sn === chat.sn; });
            if (!exists) {
                chatsStore.chats.unshift(restoredChat);
            }
        }

        // 重新渲染
        renderChatList(chatsStore.chats, chatsStore.activeChatSN);
    }
    showToast('对话已恢复', 'success');
}

/**
 * 从回收站永久删除对话
 */
async function handlePermanentDelete(chat) {
    const result = await msgbox.warning(`永久删除〔${truncateTitle(chat.title)}〕吗？\n此操作不可恢复\n对应用户特征也将被永久删除`);
    if (result !== 1) {
        return;
    }

    const ok = await permanentDeleteChat(chat.sn);
    if (!ok) {
        return;
    }

    var chatsStore = window.Alpine.store('chats');
    if (chatsStore) {
        var trashIdx = chatsStore.deletedChats.findIndex(function(c) { return c.sn === chat.sn; });
        if (trashIdx >= 0) {
            chatsStore.deletedChats.splice(trashIdx, 1);
        }
        // 同时清理 items 中可能残留的数据
        chatsStore.removeChat(chat.sn);

        // ★ 从智能分类树（chatGroups）中同步移除该 chat
        if (chatsStore.chatGroups) {
            var catChanged = false;
            for (var tag in chatsStore.chatGroups) {
                if (chatsStore.chatGroups.hasOwnProperty(tag)) {
                    var catItems = chatsStore.chatGroups[tag];
                    var catFiltered = catItems.filter(function(c) { return c.sn !== chat.sn; });
                    if (catFiltered.length !== catItems.length) {
                        catChanged = true;
                        if (catFiltered.length > 0) {
                            chatsStore.chatGroups[tag] = catFiltered;
                        } else {
                            delete chatsStore.chatGroups[tag];
                        }
                    }
                }
            }
            if (catChanged) {
                chatsStore.chatGroups = Object.assign({}, chatsStore.chatGroups);
            }
        }

        // ★ 从收藏树（favoritesGroups）中同步移除该 chat
        if (chatsStore.favoritesGroups) {
            var favChanged = false;
            for (var favTag in chatsStore.favoritesGroups) {
                if (chatsStore.favoritesGroups.hasOwnProperty(favTag)) {
                    var favItems = chatsStore.favoritesGroups[favTag];
                    var favFiltered = favItems.filter(function(c) { return c.sn !== chat.sn; });
                    if (favFiltered.length !== favItems.length) {
                        favChanged = true;
                        if (favFiltered.length > 0) {
                            chatsStore.favoritesGroups[favTag] = favFiltered;
                        } else {
                            delete chatsStore.favoritesGroups[favTag];
                        }
                    }
                }
            }
            if (favChanged) {
                chatsStore.favoritesGroups = Object.assign({}, chatsStore.favoritesGroups);
            }
        }

        // 重新渲染
        renderChatList(chatsStore.chats, chatsStore.activeChatSN);
    }
    showToast('对话已永久删除', 'success');
}

/**
 * 清空回收站（永久删除所有回收站中的对话）
 */
async function handleEmptyTrash() {
    // 获取回收站中的对话数量
    var chatsStore = window.Alpine.store('chats');
    var count = chatsStore && chatsStore.deletedChats ? chatsStore.deletedChats.length : 0;
    if (count === 0) return;

    const result = await msgbox.warning(`确认清空回收站（含 ${count} 个对话）吗？\n此操作不可恢复\n对应用户特征也将被永久删除`);
    if (result !== 1) {
        return;
    }

    const ok = await emptyTrash();
    if (!ok) {
        return;
    }

    if (chatsStore) {
        // 清空本地回收站列表
        chatsStore.deletedChats = [];
        // 重新渲染列表
        renderChatList(chatsStore.chats, chatsStore.activeChatSN);
    }
    showToast('回收站已清空', 'success');
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
        chats.showCategoryContextMenu = showCategoryContextMenu;
        // 当鼠标进入某个对话项时，如果当前有打开的上下文菜单且属于其他对话，立即关闭。
        // 由 Alpine 模板 @mouseenter="$store.chats.maybeCloseContextMenu(chat)" 调用。
        chats.maybeCloseContextMenu = function(chat) {
            if (contextMenuEl && contextTargetSN !== chat.sn) {
                closeContextMenu();
            }
        };

        /**
         * closeContextMenu — 关闭当前打开的右键菜单（由分组头部 @mouseenter 调用）
         */
        chats.closeContextMenu = closeContextMenu;

        /**
         * 回收站右键菜单 — 显示恢复/永久删除选项
         */
        chats.showTrashContextMenu = function(e, chat) {
            closeContextMenu();

            contextTargetSN = chat.sn;

            const menu = document.createElement('div');
            menu.className = 'chat-context-menu';
            menu.style.position = 'fixed';

            const rect = e.currentTarget.getBoundingClientRect();
            const menuWidth = 160;
            const menuHeight = 36 * 2 + 4 + 9;

            const isSmallScreen = document.body.classList.contains('small-screen-mode');
            let left, top;

            if (isSmallScreen) {
                left = rect.left;
                top = rect.bottom + 4;
                if (left + menuWidth > window.innerWidth) {
                    left = window.innerWidth - menuWidth - 8;
                }
                if (top + menuHeight > window.innerHeight) {
                    top = rect.top - menuHeight - 4;
                }
            } else {
                left = rect.right + 4;
                top = rect.top;
                if (left + menuWidth > window.innerWidth) {
                    left = rect.left - menuWidth - 4;
                }
                if (top + menuHeight > window.innerHeight) {
                    top = window.innerHeight - menuHeight - 8;
                }
            }

            menu.style.left = Math.max(4, left) + 'px';
            menu.style.top = Math.max(4, top) + 'px';

            // 恢复
            const restoreItem = document.createElement('div');
            restoreItem.className = 'chat-context-menu-item';
            restoreItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_TRASH_RESTORE + '</svg> 恢复';
            restoreItem.addEventListener('click', function() {
                closeContextMenu();
                handleRestore(chat);
            });
            menu.appendChild(restoreItem);

            // 分割线
            const separator = document.createElement('div');
            separator.className = 'chat-context-menu-separator';
            menu.appendChild(separator);

            // 永久删除
            const deleteItem = document.createElement('div');
            deleteItem.className = 'chat-context-menu-item chat-context-menu-item-danger';
            deleteItem.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_DELETE + '</svg> 永久删除';
            deleteItem.addEventListener('click', function() {
                closeContextMenu();
                handlePermanentDelete(chat);
            });
            menu.appendChild(deleteItem);

            document.body.appendChild(menu);
            contextMenuEl = menu;

            setTimeout(function() {
                document.addEventListener('click', closeContextMenu, { once: true });
            }, 0);
        };

        /**
         * setSidebarChats — 替换侧边栏对话列表（切换用户时使用）。
         * 直接调用 restructChatLists 写入 Alpine store（内部会同步到 store.chats）。
         * 供 chat-api.js 等外部模块在切换用户后调用，避免循环依赖。
         * ★ 迁移后：不再维护模块变量 currentChats / activeChatSN
         * @param {Array} chatList - 新的对话数组
         * @param {string} [activeSN] - 当前选中的对话 SN
         */
        chats.setSidebarChats = function(chatList, activeSN) {
            closeContextMenu();
            chats.restructChatLists(chatList, activeSN);
        };

        /**
         * emptyTrash — 清空回收站
         * 由 Alpine 模板 @click="$store.chats.emptyTrash()" 调用
         */
        chats.emptyTrash = handleEmptyTrash;
    }
} catch(e) {
    console.warn('chat-list: 无法注册到 Alpine store', e);
}
