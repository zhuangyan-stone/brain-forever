// ============================================================
// chat-init.js — 页面初始化流程
// 首次打开页面时，依次：
//   1. 重置 Alpine store 状态为空白对话
//   2. GET /api/session → 创建/获取 HTTP session，得到 user_no
//   3. GET /api/chat/list?user_no=xxx → 取该用户的 chats 列表
//   4. 渲染对话列表
//   5. 显示欢迎消息（页面总是从空白状态开始）
// ============================================================

import { showWelcomeMessage } from './chat-ui.js';
import { renderChatList } from './chat-list.js';

'use strict';

/**
 * initPage — 页面初始化入口
 * 在 DOMContentLoaded 时调用，完成首次打开页面的初始化流程。
 */
export async function initPage() {
    // Step 0: 重置 Alpine store 状态为空白对话
    // 刷新页面时，用户可能之前在某个 chat 页面上，需要重置为空白状态
    var chatsStore = window.Alpine.store('chats');
    if (chatsStore) {
        chatsStore.resetToBlank();
    }

    // Step 1: GET /api/session — 创建/获取 HTTP session
    let currentUserNo = '';
    let welcomeMessage = '';
    try {
        const sessionResp = await fetch('/api/session');
        if (sessionResp.ok) {
            const sessionData = await sessionResp.json();
            currentUserNo = sessionData.user_no || '';
            welcomeMessage = sessionData.welcome || '';
        } else {
            console.warn('session init failed:', sessionResp.status);
        }
    } catch (e) {
        console.warn('session init error:', e);
    }

    // Step 2: GET /api/chat/list — 取当前 HTTP session 用户的 chats 列表
    // 用户身份由 cookie 中的 http-session-sn 识别，不传 query 参数
    let chatListData = null;
    try {
        const chatListResp = await fetch('/api/chat/list');
        if (chatListResp.ok) {
            chatListData = await chatListResp.json();
        }
    } catch (e) {
        console.warn('fetch chat list error:', e);
    }

    // Step 3: 加工对话列表并存入 Alpine store（侧边栏通过响应式模板自动渲染）
    // renderChatList → restructChatLists 会将原始列表存入 store.chats
    // 后续 updateChatEntry 直接操作 store.chats，无需外部变量。
    if (chatListData && chatListData.chats) {
        renderChatList(chatListData.chats);
    }

    // 恢复登录按钮状态（Alpine 响应式渲染）
    // 空串表示匿名用户，不显示用户号
    // 注意：必须无条件赋值，因为 currentUserNo 可能从有值变为空（匿名状态）
    chatsStore.currentUserNo = currentUserNo;

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
