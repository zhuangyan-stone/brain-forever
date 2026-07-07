// ============================================================
// chat-init.js — 页面初始化流程
// 首次打开页面时，依次：
//   1. 重置 Alpine store 状态为空白对话
//   2. GET /api/session → 创建/获取 HTTP session，得到 user_no
//   3. 未登录（user_no 为空）→ 跳转到 /signin/
//   4. GET /api/chat/list → 取当前用户的 chats 列表
//   5. 渲染对话列表
//   6. 显示欢迎消息（页面总是从空白状态开始）
// ============================================================

import { showWelcomeMessage } from './chat-ui.js';
import { renderChatList } from './chat-list.js';
import { fetchSession, fetchChatList, createBlankChat } from './chat-api.js';

'use strict';

/**
 * initPage — 页面初始化入口
 * 在 DOMContentLoaded 时调用，完成首次打开页面的初始化流程。
 */
export async function initPage() {
    // ☆ 调试日志
    console.log('[Zoo] initPage 开始, cookie:', document.cookie);
    console.log('[Zoo] localStorage user_no:', localStorage.getItem('brainforever_user_no'));

    // Step 0: 重置 Alpine store 状态为空白对话
    // 刷新页面时，用户可能之前在某个 chat 页面上，需要重置为空白状态
    var chatsStore = window.Alpine.store('chats');
    if (chatsStore) {
        chatsStore.resetToBlank();
    }

    // Step 1: GET /api/session — 创建/获取 HTTP session
    let currentUserNo = '';
    let welcomeMessage = '';
    const sessionData = await fetchSession();
    console.log('[Zoo] fetchSession 返回:', JSON.stringify(sessionData));
    if (sessionData) {
        currentUserNo = sessionData.no || '';
        welcomeMessage = sessionData.welcome || '';
    }

    console.log('[Zoo] currentUserNo:', JSON.stringify(currentUserNo));

    // ★ 登录检查：未登录用户跳转到登录页
    // 匿名设计已废弃，所有用户必须登录才能使用。
    if (!currentUserNo) {
        console.log('[Zoo] currentUserNo 为空，跳转到 /signin/');
        window.location.href = '/signin/';
        return; // 停止后续执行
    }
    console.log('[Zoo] 登录检查通过，继续加载');

    // Step 2: GET /api/chat/list — 取当前 HTTP session 用户的 chats 列表
    // 用户身份由 cookie 中的 http-session-sn 识别，不传 query 参数
    let chatListData = await fetchChatList();

    // Step 2.5: ★ 初始化时强制重置后端 currentChat 为空白状态
    // 如果不复位后端 currentChat，用户刷新页面后 backend 的 currentChat.dbChat
    // 仍指向旧会话，导致新消息被追加到旧 chat 中，侧边栏出现重复 SN。
    // createBlankChat 是幂等的：如果后端已经是 blank chat，onNewChat 是 no-op。
    await createBlankChat();

    // Step 3: 加工对话列表并存入 Alpine store（侧边栏通过响应式模板自动渲染）
    // renderChatList → restructChatLists 会将原始列表存入 store.chats
    // 后续 updateChatEntry 直接操作 store.chats，无需外部变量。
    if (chatListData && chatListData.chats) {
        renderChatList(chatListData.chats);
    }

    // 恢复登录用户信息（Alpine 响应式渲染）
    // 空串表示匿名用户，不显示
    // 注意：必须无条件赋值，因为值可能从有变为空
    chatsStore.currentUserNo = currentUserNo;
    chatsStore.currentUserNickname = sessionData.nickname || '';

    // 从 localStorage 恢复头像 URL（登录时持久化）
    if (currentUserNo) {
        var savedAvatar = localStorage.getItem('brainforever_user_avatar');
        if (savedAvatar) {
            chatsStore.currentUserAvatar = savedAvatar;
        }
    }

    // Step 4: 显示欢迎消息（页面总是从空白状态开始）
    // 将后端返回的欢迎词设置到 Alpine store，由响应式模板自动渲染
    chatsStore.welcomeMessage = welcomeMessage;

    // 切换欢迎面板无法完全使用 Alpine 的响应式实现
    showWelcomeMessage();
}
