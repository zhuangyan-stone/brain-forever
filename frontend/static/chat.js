// ============================================================
// BrainOnline AI 助手 — 聊天逻辑 + SSE 流式消费
// ============================================================

import { copyPlainText, copyMarkdown, copyHtml, htmlToMarkdown } from './clipboard.js';
import { escapeHtml, truncate } from './toolsets.js';

'use strict';

// DOM 元素
const chatContainer = document.getElementById('chatContainer');
const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const themeToggle = document.getElementById('themeToggle');
const tickNav = document.getElementById('tickNav');
const deleteModal = document.getElementById('deleteModal');
const modalBody = document.getElementById('modalBody');
const modalCloseBtn = document.getElementById('modalCloseBtn');
const modalCancelBtn = document.getElementById('modalCancelBtn');
const modalConfirmBtn = document.getElementById('modalConfirmBtn');
const toastContainer = document.getElementById('toastContainer');
const deepThinkBtn = document.getElementById('deepThinkBtn');
const webSearchBtn = document.getElementById('webSearchBtn');
const attachBtn = document.getElementById('attachBtn');
const fileInput = document.getElementById('fileInput');
const sendModeToggle = document.getElementById('sendModeToggle');
const sendModeLabel = document.getElementById('sendModeLabel');

// ============================================================
// 切换按钮状态（深度思考 / 智能搜索）
// ============================================================

// 状态变量
let deepThinkActive = true;
let webSearchActive = true;

// 当前会话的快照：发送消息时锁定，SSE 处理期间基于此判断
let sessionDeepThinkingEnabled = false;

/**
 * toggleButton 切换按钮的选中/未选中状态
 * @param {HTMLElement} btn - 按钮元素
 * @param {boolean} active - 是否选中
 */
function toggleButton(btn, active) {
    btn.dataset.active = active ? 'true' : 'false';
}

// 深度思考按钮点击
deepThinkBtn.addEventListener('click', () => {
    deepThinkActive = !deepThinkActive;
    toggleButton(deepThinkBtn, deepThinkActive);
});

// 智能搜索按钮点击
webSearchBtn.addEventListener('click', () => {
    webSearchActive = !webSearchActive;
    toggleButton(webSearchBtn, webSearchActive);
});

// ============================================================
// 主题切换
// ============================================================

// 从 localStorage 读取已保存的主题，首次使用默认为 'light'（亮色）
const savedTheme = localStorage.getItem('brainonline-theme') || 'light';
document.documentElement.setAttribute('data-theme', savedTheme);
updateThemeButton(savedTheme);
switchHighlightTheme(savedTheme);

themeToggle.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme') || 'dark';
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('brainonline-theme', next);
    updateThemeButton(next);
    switchHighlightTheme(next);
});

function updateThemeButton(theme) {
    themeToggle.innerHTML = theme === 'dark'
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
            <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/>
            <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>
        </svg>`;
    themeToggle.title = theme === 'dark' ? '切换到亮色主题' : '切换到暗色主题';
}

// switchHighlightTheme 切换 highlight.js 的主题样式表
function switchHighlightTheme(theme) {
    const darkTheme = document.getElementById('hljs-theme-dark');
    const lightTheme = document.getElementById('hljs-theme-light');
    if (darkTheme && lightTheme) {
        darkTheme.disabled = theme !== 'dark';
        lightTheme.disabled = theme !== 'light';
    }
}

// 状态
let messages = [];          // 完整的消息历史 [{role, content}]
let isStreaming = false;    // 是否正在流式接收
let abortController = null; // 用于取消请求

// Reasoning 计时
let reasoningStartTime = null;  // Date.now() 时间戳，思考开始时刻

// 刻度导航状态
let userMsgCount = 0;       // 用户消息计数（用于生成 data-msg-index）
let activeTickIndex = -1;   // 当前活动刻度的索引，-1 表示无活动刻度
let currentGroup = null;    // 当前消息组，用于将同一问答对的 user + assistant 包裹在 .message-group 内

// 流式渲染相关
let accumulatedMarkdown = ''; // 当前流式消息累积的原始 Markdown
let renderTimer = null;       // 节流渲染定时器
const RENDER_INTERVAL = 120;   // 渲染节流间隔（毫秒）

// ============================================================
// 初始化：自动调整 textarea 高度
// ============================================================

messageInput.addEventListener('input', () => {
    messageInput.style.height = 'auto';
    messageInput.style.height = Math.min(messageInput.scrollHeight, 120) + 'px';
});

// 发送模式状态: false = Enter发送/Shift+Enter换行, true = Enter换行/Shift+Enter发送
let sendModeAlternate = false;

// 发送模式标签文本
const SEND_MODE_LABELS = {
    normal: '回车键发送，Shift+回车键换行',
    alternate: '回车键换行，Shift+回车键发送'
};

// 更新发送模式标签
function updateSendModeLabel() {
    sendModeLabel.textContent = sendModeAlternate
        ? SEND_MODE_LABELS.alternate
        : SEND_MODE_LABELS.normal;
}

// 滑块切换发送模式
sendModeToggle.addEventListener('change', () => {
    sendModeAlternate = sendModeToggle.checked;
    updateSendModeLabel();
});

// 键盘发送/换行逻辑
messageInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        if (sendModeAlternate) {
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
// 发送消息
// ============================================================

async function sendMessage() {
    const content = messageInput.value.trim();
    if (!content || isStreaming) return;

    // 清空输入框
    messageInput.value = '';
    messageInput.style.height = 'auto';

    // 删除欢迎消息（用户发出第一条消息时）
    const welcomeEl = chatContainer.querySelector('.welcome-message');
    if (welcomeEl) {
        // 将输入区域移回 #app 底部
        const inputArea = welcomeEl.querySelector('.input-area');
        if (inputArea) {
            document.getElementById('app').appendChild(inputArea);
        }
        welcomeEl.remove();
        document.getElementById('app').classList.remove('welcome-state');
    }

    // 生成 UTC 时间
    const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'); // UTC, e.g. "2026-05-02T16:30:00Z"

    // 添加用户消息到界面（ID 由后端分配，先传 0）
    const userDiv = addMessage('user', content);
    const userEntry = { role: 'user', content, id: 0, created_at: createdAt };
    messages.push(userEntry);

    // 禁用输入
    setInputEnabled(false);

    // 创建空的 assistant 消息占位
    const assistantBubble = addMessage('assistant', '', true);
    isStreaming = true;
    // 禁用所有删除按钮（流式请求进行中禁止删除）
    updateDeleteButtons();

    // 创建 AbortController
    abortController = new AbortController();

    try {
        // 锁定本轮会话的深度思考状态（防止流式过程中用户乱点按钮导致状态漂移）
        sessionDeepThinkingEnabled = deepThinkActive;

        // 发送请求 — 只传用户最新的一句话，历史由后端维护
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                message: { id: 0, role: 'user', content: content, created_at: createdAt },
                stream: true,
                deep_think: deepThinkActive,
                web_search_enabled: webSearchActive
            }),
            signal: abortController.signal
        });

        if (!response.ok) {
            const errText = await response.text();
            throw new Error(`服务器错误 [${response.status}]: ${errText}`);
        }

        // 读取 SSE 流
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });

            // 按行分割
            const lines = buffer.split('\n');
            buffer = lines.pop() || ''; // 保留未完成的行

            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed || !trimmed.startsWith('data: ')) continue;

                const jsonStr = trimmed.slice(6);
                try {
                    const event = JSON.parse(jsonStr);
                    handleSSEEvent(event, assistantBubble);
                } catch (e) {
                    console.warn('解析 SSE 事件失败:', jsonStr);
                }
            }
        }

        // 处理 buffer 中剩余的数据
        if (buffer.trim().startsWith('data: ')) {
            const jsonStr = buffer.trim().slice(6);
            try {
                const event = JSON.parse(jsonStr);
                handleSSEEvent(event, assistantBubble);
            } catch (e) {
                console.warn('解析剩余 SSE 事件失败:', jsonStr);
            }
        }

    } catch (err) {
        if (err.name === 'AbortError') {
            console.log('请求已取消');
        } else {
            console.error('请求失败:', err);
            showError(assistantBubble, err.message);
        }
    } finally {
        isStreaming = false;
        abortController = null;
        setInputEnabled(true);
        updateDeleteButtons();
        messageInput.focus();

        // 移除 streaming 类
        const contentDiv = assistantBubble.querySelector('.bubble');
        if (contentDiv) {
            contentDiv.classList.remove('streaming');
        }

        // 清理渲染状态（防止取消请求后定时器残留）
        if (renderTimer) {
            clearTimeout(renderTimer);
            renderTimer = null;
        }
        accumulatedMarkdown = '';

        // 清理 reasoning 区域的节流渲染定时器（防止取消请求后定时器残留）
        const reasoningContentEl = assistantBubble.querySelector('.reasoning-content');
        if (reasoningContentEl && reasoningContentEl.renderTimer) {
            clearTimeout(reasoningContentEl.renderTimer);
            reasoningContentEl.renderTimer = null;
        }

        reasoningStartTime = null;
    }
}

// ============================================================
// Reasoning（深度思考）状态管理
// ============================================================

/**
 * 格式化思考用时，返回如 "12.3s" 的字符串
 * @param {number} elapsedMs - 经过的毫秒数
 * @returns {string}
 */
function formatReasoningTime(elapsedMs) {
    const seconds = elapsedMs / 1000;
    if (seconds < 60) {
        return seconds.toFixed(1) + 's';
    }
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return `${mins}分${secs}秒`;
}

/**
 * 记录 reasoning 开始时间
 * 思考过程中标题保持"正在思考…"，完成时通过 stopReasoningTimer 显示最终用时
 * @param {HTMLElement} titleEl - .reasoning-title 元素（保留参数以兼容调用方）
 */
function startReasoningTimer(titleEl) {
    reasoningStartTime = Date.now();
}

/**
 * 停止 reasoning 计时器，在标题中显示最终用时
 * @param {HTMLElement} titleEl - .reasoning-title 元素
 */
function stopReasoningTimer(titleEl) {
    if (reasoningStartTime && titleEl) {
        const elapsed = Date.now() - reasoningStartTime;
        const prefix = '思考完成';
        titleEl.textContent = `${prefix} (${formatReasoningTime(elapsed)})`;
    }
    reasoningStartTime = null;
}

/**
 * 获取或创建 assistant 气泡中的 reasoning 状态区域
 */
function getOrCreateReasoningArea(assistantBubble) {
    let area = assistantBubble.querySelector('.reasoning-area');
    if (!area) {
        area = document.createElement('div');
        area.className = 'reasoning-area';
        // 插入到气泡之前
        const bubble = assistantBubble.querySelector('.bubble');
        if (bubble) {
            bubble.insertAdjacentElement('beforebegin', area);
        } else {
            assistantBubble.appendChild(area);
        }
    }
    return area;
}

/**
 * 创建 reasoning 区域（含标题栏和内容区）
 * @param {HTMLElement} assistantBubble
 * @returns {HTMLElement} reasoning-area 元素
 */
function createReasoningArea(assistantBubble) {
    let reasoningArea = assistantBubble.querySelector('.reasoning-area');
    if (reasoningArea) return reasoningArea;

    reasoningArea = getOrCreateReasoningArea(assistantBubble);
    reasoningArea.className = 'reasoning-area active';

    // 隐藏独立的 AI 角色标签，将其合并到 reasoning-header 中
    const roleLabel = assistantBubble.querySelector('.role-label-ai');
    if (roleLabel) {
        roleLabel.style.display = 'none';
    }

    const titleText = '正在思考……';
    reasoningArea.innerHTML = `
        <div class="reasoning-header">
            <span class="reasoning-toggle" title="折叠思考过程">▶</span>
            <span class="reasoning-icon">🤖</span>
            <span class="reasoning-role-badge">AI</span>
            <span class="reasoning-title">${titleText}</span>
        </div>
        <div class="reasoning-content"></div>
    `;
    // 点击 header 切换折叠/展开
    const header = reasoningArea.querySelector('.reasoning-header');
    header.addEventListener('click', (e) => {
        toggleReasoningCollapse(header);
    });
    // 启动思考用时计时器
    const titleEl = reasoningArea.querySelector('.reasoning-title');
    startReasoningTimer(titleEl);

    return reasoningArea;
}

/**
 * 根据工具名称返回对应的图标 emoji
 * @param {string} toolName - 工具函数名
 * @returns {string} 图标字符串
 */
function getToolIcon(toolName) {
    switch (toolName) {
        case 'web_search':
            return '🔍';
        case 'get_current_local_time':
            return '🕐';
        case 'personal_trait_search':
            return '🧑';
        default:
            return '⚙';
    }
}

/**
 * 切换 reasoning 区域的折叠/展开状态
 */
function toggleReasoningCollapse(headerEl) {
    const area = headerEl.closest('.reasoning-area');
    if (!area) return;
    const isCollapsed = area.classList.toggle('collapsed');
    const toggleBtn = headerEl.querySelector('.reasoning-toggle');
    if (toggleBtn) {
        // 使用 ▶ 作为基础字符，展开时通过 CSS transform: rotate(90deg) 变为 ▼
        // 折叠时 transform: rotate(0deg) 保持 ▶ — 与 sources-panel 完全一致
        toggleBtn.textContent = '▶';
        toggleBtn.title = isCollapsed ? '展开思考内容' : '折叠思考内容';
    }
}

// ============================================================
// SSE 事件处理
// ============================================================

function handleSSEEvent(event, assistantBubble) {
    const contentDiv = assistantBubble.querySelector('.bubble');

    switch (event.type) {
        case 'reasoning':
            // reasoning 事件有两种 subject：
            //   1. subject=""（空串）：真正的 LLM 思考内容
            //   2. subject="tool-pending"：LLM 决定调用某个工具
            if (event.subject === 'tool-pending') {
                // ---- tool-pending：显示工具调用提示 ----
                let reasoningArea = assistantBubble.querySelector('.reasoning-area');
                if (!reasoningArea) {
                    reasoningArea = createReasoningArea(assistantBubble);
                }
                const contentEl = reasoningArea.querySelector('.reasoning-content');
                if (contentEl && event.content) {
                    if (!contentEl.rawText) contentEl.rawText = '';
                    // 根据工具名称选择图标
                    const icon = getToolIcon(event.tool);
                    // 添加为独立一行（使用 blockquote 风格，但用自定义类以便样式控制）
                    contentEl.rawText += `\n> ${icon} ${event.content}\n`;
                    // 节流渲染
                    if (!contentEl.renderTimer) {
                        contentEl.renderTimer = setTimeout(() => {
                            contentEl.renderTimer = null;
                            contentEl.innerHTML = renderMarkdown(contentEl.rawText);
                            scrollToBottom();
                        }, RENDER_INTERVAL);
                    }
                }
            } else {
                // ---- 真正的 LLM 思考内容（subject=""） ----
                let reasoningArea = assistantBubble.querySelector('.reasoning-area');
                if (!reasoningArea) {
                    reasoningArea = createReasoningArea(assistantBubble);
                }
                if (event.content) {
                    const contentEl = reasoningArea.querySelector('.reasoning-content');
                    if (contentEl) {
                        if (!contentEl.rawText) contentEl.rawText = '';
                        contentEl.rawText += event.content;
                        // 节流渲染
                        if (!contentEl.renderTimer) {
                            contentEl.renderTimer = setTimeout(() => {
                                contentEl.renderTimer = null;
                                contentEl.innerHTML = renderMarkdown(contentEl.rawText);
                                scrollToBottom();
                            }, RENDER_INTERVAL);
                        }
                    }
                }
            }
            break;

        case 'text':
            // AI 开始正式回复 → 如果存在 reasoning 区域，标记为"思考完成"
            const textReasoningArea = assistantBubble.querySelector('.reasoning-area.active');
            if (textReasoningArea) {
                const titleEl = textReasoningArea.querySelector('.reasoning-title');
                if (titleEl) {
                    stopReasoningTimer(titleEl);
                }
                textReasoningArea.classList.remove('active');
                textReasoningArea.classList.add('done');
            }

            // 停止搜索提示的闪烁动画
            const hints = assistantBubble.querySelectorAll('.web-search-hint');
            hints.forEach(h => h.style.animation = 'none');

            // 累积原始 Markdown，实时渲染为 HTML（带节流）
            if (contentDiv) {
                accumulatedMarkdown += event.content;
                contentDiv.classList.add('streaming');

                // 节流渲染：RENDER_INTERVAL 毫秒内最多渲染一次
                if (!renderTimer) {
                    renderTimer = setTimeout(() => {
                        renderTimer = null;
                        contentDiv.innerHTML = renderMarkdown(accumulatedMarkdown);
                        scrollToBottom();
                    }, RENDER_INTERVAL);
                }
            }
            break;

        case 'sources':
            // 显示引用来源（知识库引用或联网搜索结果）
            if (event.sources) {
                showSources(event.sources, 'rag');
            }
            if (event.web_sources) {
                showSources(event.web_sources, 'web');
            }
            break;

case 'done':
    // 流结束：如果 reasoning 区域仍处于 active 状态（没有 text 事件），标记为完成
    const doneReasoningArea = assistantBubble.querySelector('.reasoning-area.active');
    if (doneReasoningArea) {
        const titleEl = doneReasoningArea.querySelector('.reasoning-title');
        if (titleEl) {
            stopReasoningTimer(titleEl);
        }
        doneReasoningArea.classList.remove('active');
        doneReasoningArea.classList.add('done');
        // 清除 reasoning 节流定时器，立即做最终渲染
        const doneContentEl = doneReasoningArea.querySelector('.reasoning-content');
        if (doneContentEl) {
            if (doneContentEl.renderTimer) {
                clearTimeout(doneContentEl.renderTimer);
                doneContentEl.renderTimer = null;
            }
            if (doneContentEl.rawText) {
                doneContentEl.innerHTML = renderMarkdown(doneContentEl.rawText);
            }
        }
    }

    // 停止搜索提示的闪烁动画（安全兜底）
    const doneHints = assistantBubble.querySelectorAll('.web-search-hint');
    doneHints.forEach(h => h.style.animation = 'none');

    // 流结束：确保最后一次渲染完成，保存纯文本到 messages
    if (contentDiv) {
        contentDiv.classList.remove('streaming');
        // 清除未执行的节流定时器，立即做最终渲染
        if (renderTimer) {
            clearTimeout(renderTimer);
            renderTimer = null;
        }
        // 最终渲染为 HTML
        contentDiv.innerHTML = renderMarkdown(accumulatedMarkdown);
        // 启用所有复制按钮（流已结束）
        enableCopyButtons(assistantBubble);
        // 后端返回的用户消息 ID（前端之前传 0，由后端分配）
        const userMsgId = event.msg_id || 0;
        if (userMsgId) {
            // 更新本地 messages 数组中最新一条 id===0 的用户消息的 ID
            for (let i = messages.length - 1; i >= 0; i--) {
                if (messages[i].role === 'user' && messages[i].id === 0) {
                    messages[i].id = userMsgId;
                    break;
                }
            }
            // 更新用户消息 DOM 上的 data-msg-id（最新一条 user 消息）
            const userMsgEl = chatContainer.querySelector('.message.user:last-child');
            if (userMsgEl) {
                userMsgEl.dataset.msgId = userMsgId;
            }
        }
        // AI 回复复用用户消息的 ID（source ID）
        const usage = event.usage || null;
        messages.push({ role: 'assistant', content: accumulatedMarkdown, id: userMsgId, usage });
        // 保存 msg_id 到 AI 回复的 DOM
        if (userMsgId) {
            assistantBubble.dataset.msgId = userMsgId;
        }
        // 显示 token 用量信息
        if (event.usage) {
            showTokenUsage(assistantBubble, event.usage);
        }
        // 重置累积变量，为下一次流式做准备
        accumulatedMarkdown = '';
        scrollToBottom();
    }
    break;

        case 'error':
            // 错误
            showError(assistantBubble, event.message);
            break;

        default:
            console.log('未知事件类型:', event.type);
    }
}

// ============================================================
// Token 用量显示
// ============================================================

/**
 * showTokenUsage 在 assistant 消息气泡下方显示 token 用量信息。
 * 如果 is_estimated 为 true，则附加提示说明为估算值。
 * @param {HTMLElement} assistantBubble - assistant 消息的 .message 元素
 * @param {object} usage - { prompt_tokens, completion_tokens, total_tokens, is_estimated }
 */
function showTokenUsage(assistantBubble, usage) {
    // 移除已有的 token-info（防止重复添加）
    const existing = assistantBubble.querySelector('.token-info');
    if (existing) existing.remove();

    const info = document.createElement('div');
    info.className = 'token-info';

    const text = `提示 ${usage.prompt_tokens} + 生成 ${usage.completion_tokens} = ${usage.total_tokens}`;

    if (usage.is_estimated) {
        info.title = '当前大模型未返回 token 消耗数据，此处为估算值，供参考';
        info.innerHTML = `词元消耗：<span class="token-estimated-icon">⚠</span> ${text} <span class="token-estimated-label">(估算)</span>`;
    } else {
        info.textContent = `词元消耗：${text}`;
    }

    // 插入到 message-actions 内部（与操作按钮同行显示）
    const actions = assistantBubble.querySelector('.message-actions');
    if (actions) {
        actions.prepend(info);
    }
}

// ============================================================
// Markdown 渲染
// ============================================================

// 注册 marked-katex-extension，支持数学公式（行内 $...$ 和块级 $$...$$）
try {
    if (typeof markedKatex !== 'undefined') {
        marked.use(markedKatex());
    }
} catch (e) {
    console.warn('marked-katex-extension 加载失败，数学公式将不可用:', e);
}

/**
 * 在引号与强调标记（**、*、__、_）之间插入零宽空格（\u200B），
 * 解决 marked 将引号后的 ** 错误识别为左定界符而非右定界符的问题。
 *
 * 问题背景：marked 遵循 CommonMark 规范，使用 Unicode 属性 \p{P}（标点符号）
 * 来判断 * 和 _ 的定界符类型。引号字符（如 " U+0022、" U+201C、" U+201D）
 * 属于标点符号，导致紧随其后的 ** 被错误分类为左定界符，从而使前面的 **
 * 找不到配对的右定界符，strong/em 解析失败。
 *
 * 零宽空格（\u200B）不属于 \p{P}，插入后 marked 能正确识别定界符，
 * 且在最终渲染结果中不可见。
 */
function fixQuotesAroundEmphasis(text) {
    // 在引号与强调标记（**、*、__、_）之间插入零宽空格（\u200B），
    // 解决 marked 将引号后的 ** 错误识别为左定界符的问题。
    // 匹配的引号：英文引号 "（U+0022）、中文左引号 "（U+201C）、中文右引号 "（U+201D）
    return text
        // 引号 + 强调标记 → 引号 + 零宽空格 + 强调标记
        .replace(/(["\u201c\u201d])(?=[*_]{1,2})/g, '$1\u200B')
        // 强调标记 + 引号 → 强调标记 + 零宽空格 + 引号
        .replace(/(?<=[*_]{1,2})(["\u201c\u201d])/g, '\u200B$1');
}

// renderMarkdown 将 Markdown 文本渲染为安全的 HTML，并对代码块进行语法高亮
function renderMarkdown(text) {
    if (!text) return '';
    try {
        // 修复引号与强调标记相邻时的定界符识别问题
        const fixed = fixQuotesAroundEmphasis(text);
        // 使用 marked 渲染
        const html = marked.parse(fixed, {
            breaks: true,      // 支持 GitHub 风格的换行
            gfm: true,         // 启用 GitHub Flavored Markdown
        });
        // 将 HTML 插入临时容器，对代码块执行语法高亮
        return highlightCodeBlocks(html);
    } catch (e) {
        console.warn('Markdown 渲染失败，回退到纯文本:', e);
        return escapeHtml(text);
    }
}

// highlightCodeBlocks 对 HTML 中的 <pre><code> 代码块进行语法高亮，并添加复制按钮
function highlightCodeBlocks(html) {
    const temp = document.createElement('div');
    temp.innerHTML = html;

    // 查找所有 <pre><code> 代码块
    temp.querySelectorAll('pre code').forEach((el) => {
        const pre = el.parentElement;

        // 获取语言类名（marked 会添加 class="language-xxx"）
        const langClass = Array.from(el.classList).find(cls => cls.startsWith('language-'));
        if (langClass) {
            const lang = langClass.replace('language-', '');
            try {
                // 使用 highlight.js 进行语法高亮
                el.innerHTML = hljs.highlight(el.textContent, { language: lang }).value;
                // 添加语言标签属性
                pre.setAttribute('data-lang', lang);
            } catch (e) {
                // 如果 highlight.js 不支持该语言，使用自动检测
                try {
                    el.innerHTML = hljs.highlightAuto(el.textContent).value;
                } catch (_) {
                    // 回退：不做高亮
                }
            }
        } else {
            // 没有指定语言，尝试自动检测
            try {
                el.innerHTML = hljs.highlightAuto(el.textContent).value;
            } catch (_) {
                // 回退：不做高亮
            }
        }

        // 添加复制按钮
        addCopyButton(pre);
    });

    return temp.innerHTML;
}

// addCopyButton 为 <pre> 代码块添加复制按钮（仅创建 DOM，事件由委托处理）
function addCopyButton(pre) {
    // 避免重复添加
    if (pre.querySelector('.copy-btn')) return;

    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.textContent = '复制 ▾';
    btn.disabled = isStreaming; // 流式输出时禁用

    pre.appendChild(btn);
}

// enableCopyButtons 启用指定消息气泡内的所有复制按钮
function enableCopyButtons(bubbleElement) {
    bubbleElement.querySelectorAll('.copy-btn').forEach((btn) => {
        btn.disabled = false;
    });
}

// ============================================================
// 通用下拉菜单 — 创建并显示包含多个选项的下拉菜单
// ============================================================

/**
 * showDropdownMenu 在目标按钮下方显示一个下拉菜单
 * @param {HTMLElement} anchor - 触发菜单的按钮元素
 * @param {Array<{label:string, action:()=>void}>} items - 菜单项列表
 * @param {object} [opts]
 * @param {string} [opts.position='bottom'] - 菜单弹出位置
 */
function showDropdownMenu(anchor, items, opts) {
    // 移除已有的下拉菜单
    const existing = document.querySelector('.copy-dropdown-menu');
    if (existing) existing.remove();

    const menu = document.createElement('div');
    menu.className = 'copy-dropdown-menu';

    items.forEach((item) => {
        const option = document.createElement('div');
        option.className = 'copy-dropdown-item';
        option.textContent = item.label;
        option.addEventListener('click', (e) => {
            e.stopPropagation();
            menu.remove();
            item.action();
        });
        menu.appendChild(option);
    });

    // 定位
    const rect = anchor.getBoundingClientRect();
    const position = (opts && opts.position) || 'bottom';
    if (position === 'bottom') {
        menu.style.top = (rect.bottom + 4) + 'px';
        menu.style.left = rect.left + 'px';
    } else {
        menu.style.bottom = (window.innerHeight - rect.top + 4) + 'px';
        menu.style.left = rect.left + 'px';
    }
    menu.style.position = 'fixed';
    menu.style.zIndex = '9999';

    document.body.appendChild(menu);

    // 点击外部关闭
    const closeHandler = (ev) => {
        if (!menu.contains(ev.target) && ev.target !== anchor) {
            menu.remove();
            document.removeEventListener('click', closeHandler, true);
        }
    };
    // 延迟绑定，避免立即触发
    setTimeout(() => {
        document.addEventListener('click', closeHandler, true);
    }, 0);
}

// ============================================================
// 工具函数：生成复制菜单项
// ============================================================

/**
 * 生成一个下拉菜单项，用于复制指定格式的内容
 * @param {() => Promise<boolean>} copyFn - 执行复制操作的异步函数，返回是否成功
 * @param {string} formatName - 格式名称（如 "纯文本"、"Markdown"、"HTML"）
 * @returns {{label:string, action:()=>void}}
 */
function makeCopyMenuItem(copyFn, formatName) {
    return {
        label: `复制为 ${formatName}`,
        action: () => {
            copyFn().then(ok => {
                showToast(ok ? `✓ 已复制（${formatName}）` : `复制失败（${formatName}）`, ok ? 'success' : 'error', 2000);
            });
        },
    };
}

// ============================================================
// 事件委托：复制按钮点击处理
// ============================================================

// 在 chatContainer 上监听 click 事件，通过事件委托处理所有 .copy-btn 的点击
// 这样即使 innerHTML 被替换，事件也不会丢失
chatContainer.addEventListener('click', (e) => {
    const btn = e.target.closest('.copy-btn');
    if (!btn) return;

    // 找到所属的 <pre> 代码块
    const pre = btn.closest('pre');
    if (!pre) return;

    // 获取代码块的纯文本内容（忽略高亮标签）
    const code = pre.querySelector('code');
    const text = code ? code.textContent : '';
    if (!text) return;

    // 获取语言
    const lang = pre.getAttribute('data-lang') || '';

    // 构建 Markdown 格式（代码围栏）
    const markdown = lang
        ? '```' + lang + '\n' + text + '\n```'
        : '```\n' + text + '\n```';

    // 构建干净的 HTML 格式（只包含代码块本身，不含复制按钮等 UI 元素）
    const codeEl = pre.querySelector('code');
    const highlightedHtml = codeEl ? codeEl.innerHTML : '';
    const html = highlightedHtml
        ? `<pre><code${lang ? ` class="language-${lang}"` : ''}>${highlightedHtml}</code></pre>`
        : '';

    // 显示下拉菜单
    showDropdownMenu(btn, [
        makeCopyMenuItem(() => copyPlainText(text), '纯文本'),
        makeCopyMenuItem(() => copyMarkdown(markdown), 'Markdown'),
        makeCopyMenuItem(() => copyHtml(html), 'HTML'),
    ], { position: 'top' });
});

// ============================================================
// 事件委托：消息操作按钮（复制消息 / 删除消息）
// ============================================================

chatContainer.addEventListener('click', (e) => {
    // 复制消息按钮（带格式选择的下拉菜单）
    const copyMsgBtn = e.target.closest('.copy-msg-btn');
    if (copyMsgBtn) {
        e.stopPropagation();
        const messageEl = copyMsgBtn.closest('.message');
        if (!messageEl) return;
        const bubble = messageEl.querySelector('.bubble');
        const text = bubble ? bubble.textContent : '';
        if (!text) return;

        // 获取渲染后的 HTML
        const html = bubble ? bubble.innerHTML : '';

        // 获取原始 Markdown 源
        // 根据消息角色（user/assistant）从 messages[] 数组中匹配对应条目
        const msgId = messageEl.dataset.msgId;
        const isUser = messageEl.classList.contains('user');
        const role = isUser ? 'user' : 'assistant';
        let markdown = null;
        if (msgId) {
            const msg = messages.find(m => String(m.id) === msgId && m.role === role);
            if (msg && msg.content) {
                markdown = msg.content;
            }
        }
        // 如果 messages 中找不到，回退策略
        if (!markdown) {
            if (isUser) {
                // 用户消息：原始输入即纯文本/简单 Markdown，直接用 textContent
                markdown = text;
            } else {
                // 助手消息：用 Turndown 从 HTML 反向转换
                markdown = html ? htmlToMarkdown(html) : text;
            }
        }

        showDropdownMenu(copyMsgBtn, [
            makeCopyMenuItem(() => copyPlainText(text), '纯文本'),
            makeCopyMenuItem(() => copyMarkdown(markdown), 'Markdown'),
            makeCopyMenuItem(() => copyHtml(html), 'HTML'),
        ]);
        return;
    }

    // 删除消息按钮（组级删除按钮，直接挂在 .message-group 下）
    const deleteMsgBtn = e.target.closest('.delete-msg-btn');
    if (deleteMsgBtn) {
        e.stopPropagation();

        const group = deleteMsgBtn.closest('.message-group');
        if (!group) return;

        // 找到该组中的用户消息，获取 data-msg-index
        const userMsg = group.querySelector('.message.user');
        if (!userMsg) return;

        const msgIndex = parseInt(userMsg.dataset.msgIndex, 10);
        if (isNaN(msgIndex) || msgIndex < 0) return;

        // 设置活动刻度索引并显示删除确认框
        activeTickIndex = msgIndex;
        setActiveTick(msgIndex);
        showDeleteModal();
        return;
    }
});

// ============================================================
// DOM 操作
// ============================================================

// addMessage 添加消息气泡到聊天区域
// 用户消息（role='user'）会创建新的 .message-group 包裹层；
// 助手消息（role='assistant'）追加到当前 .message-group 内。
// 返回创建的 .message 元素。
function addMessage(role, content, isStreaming = false) {
    const div = document.createElement('div');
    div.className = `message ${role}`;

    // 为用户消息添加 data-msg-index 属性，用于刻度导航定位
    if (role === 'user') {
        div.dataset.msgIndex = userMsgCount;
        userMsgCount++;

        // 创建新的消息组包裹层，添加到聊天区域
        const group = document.createElement('div');
        group.className = 'message-group';
        group.appendChild(div);

        // 为消息组添加左上角删除按钮
        const groupDeleteBtn = document.createElement('button');
        groupDeleteBtn.className = 'msg-action-btn delete-msg-btn group-delete-btn';
        groupDeleteBtn.title = '删除本轮对话';
        groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
        groupDeleteBtn.disabled = isStreaming;
        group.appendChild(groupDeleteBtn);

        chatContainer.appendChild(group);

        // 记录为当前组，后续 assistant 消息会追加到此组内
        currentGroup = group;
    } else {
        // assistant 消息：追加到当前消息组
        if (currentGroup) {
            currentGroup.appendChild(div);
        } else {
            // 兜底：没有当前组时（如欢迎消息），创建一个独立的消息组
            const group = document.createElement('div');
            group.className = 'message-group';
            group.appendChild(div);

            // 为消息组添加左上角删除按钮
            const groupDeleteBtn = document.createElement('button');
            groupDeleteBtn.className = 'msg-action-btn delete-msg-btn group-delete-btn';
            groupDeleteBtn.title = '删除本轮对话';
            groupDeleteBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
            groupDeleteBtn.disabled = isStreaming;
            group.appendChild(groupDeleteBtn);

            chatContainer.appendChild(group);
            currentGroup = group;
        }
    }

    const inner = document.createElement('div');
    inner.className = 'message-inner';

    // 角色标签
    const label = document.createElement('div');
    label.className = 'role-label';
    label.textContent = role === 'user' ? '我' : '🤖 AI';
    if (role === 'assistant') {
        label.classList.add('role-label-ai');
    }
    inner.appendChild(label);

    // 气泡
    const bubble = document.createElement('div');
    bubble.className = 'bubble';
    if (isStreaming) {
        // 流式输出时用 textContent 保留原始 Markdown
        bubble.textContent = content;
        bubble.classList.add('streaming');
    } else {
        // 非流式（如欢迎消息）直接渲染 Markdown
        bubble.innerHTML = renderMarkdown(content);
    }
    inner.appendChild(bubble);

    // 添加操作按钮（仅复制按钮），放在气泡下方
    const actions = document.createElement('div');
    actions.className = 'message-actions';

    // 复制按钮
    const copyBtn = document.createElement('button');
    copyBtn.className = 'msg-action-btn copy-msg-btn';
    copyBtn.title = '复制当前消息内容';
    copyBtn.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    actions.appendChild(copyBtn);

    inner.appendChild(actions);

    div.appendChild(inner);

    // 注意：user 消息的 div 已在上面追加到 group 中，assistant 消息也追加到 group 中。
    // 这里不再重复追加。user 消息由前面的 group.appendChild(div) 处理；
    // assistant 消息由前面的 group.appendChild(div) 或 chatContainer.appendChild(div) 处理。
    // 但由于代码流程的执行顺序，role === 'user' 时 chatContainer.appendChild(div) 被跳过，
    // 仅保留 group.appendChild(div)；role === 'assistant' 时同样由前面的逻辑处理。
    // 所以这里不再执行 chatContainer.appendChild(div)。
    // 注意：group 已在上面的 role === 'user' 分支中被追加到 chatContainer。

    // 新增消息后，重置刻度偏移量到最新位置
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;
    if (tickCount > MAX_VISIBLE_TICKS) {
        tickScrollOffset = tickCount - MAX_VISIBLE_TICKS;
    } else {
        tickScrollOffset = 0;
    }
    // 更新刻度导航
    updateTickNav();

    scrollToBottom();
    return div;
}

// showSources 显示引用来源面板
// type: 'rag' 表示知识库引用，'web' 表示联网搜索结果
function showSources(sources, type) {
    if (!sources || sources.length === 0) return;

    // 知识库引用只显示相似度超过 60% 的
    if (type === 'rag') {
        sources = sources.filter(src => src.score > 0.6);
        if (sources.length === 0) return;
    }

    // 获取当前消息组（最后一个 .message-group）
    const lastGroup = chatContainer.querySelector('.message-group:last-child');
    if (!lastGroup) return; // 没有消息组时不做任何事

    // 在当前消息组内查找或创建 sources 面板（每个组独立）
    let panel = lastGroup.querySelector('.sources-panel');
    if (!panel) {
        panel = document.createElement('div');
        panel.className = 'sources-panel';
        // 将面板插入到组内 assistant 消息之后（组内最后一个 .message 之后）
        const lastMsg = lastGroup.querySelector('.message:last-child');
        if (lastMsg) {
            lastMsg.insertAdjacentElement('afterend', panel);
        } else {
            lastGroup.appendChild(panel);
        }
    }

    // 强制搜索按钮的 globe 图标（缩小版）
    const globeIconSvg = '<svg class="sources-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>';

    const section = document.createElement('div');
    section.className = 'sources-section';

    if (type === 'rag') {
        // ---- 知识库引用 ----
        const title = document.createElement('div');
        title.className = 'sources-title sources-collapsible';
        title.innerHTML = `${globeIconSvg} 参考了以下知识库内容`;
        title.setAttribute('role', 'button');
        title.tabIndex = 0;
        section.appendChild(title);

        const body = document.createElement('div');
        body.className = 'sources-body';
        body.style.display = 'none'; // 默认折叠

        sources.forEach((src) => {
            const item = document.createElement('div');
            item.className = 'source-item';
            item.innerHTML = `
                <span class="source-title">${escapeHtml(src.title)}</span>
                <span class="source-score">相似度: ${(src.score * 100).toFixed(1)}%</span>
                ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)">${escapeHtml(truncate(src.content, 100))}</div>` : ''}
            `;
            body.appendChild(item);
        });

        section.appendChild(body);

        // 点击标题切换折叠
        title.addEventListener('click', () => toggleSourcesSection(title, body));
        title.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                toggleSourcesSection(title, body);
            }
        });
    } else if (type === 'web') {
        // ---- 联网搜索结果（分页显示） ----
        // 分组：URL 非空的排在前面，URL 为空的排在后面
        const withUrl = sources.filter(s => s.url);
        const withoutUrl = sources.filter(s => !s.url);
        sources = withUrl.concat(withoutUrl);
        const PAGE_SIZE = 5;
        const totalPages = Math.ceil(sources.length / PAGE_SIZE);
        let currentPage = 0;

        const title = document.createElement('div');
        title.className = 'sources-title sources-collapsible';
        title.innerHTML = `${globeIconSvg} 参考了 ${sources.length} 个联网搜索结果`;
        title.setAttribute('role', 'button');
        title.tabIndex = 0;
        section.appendChild(title);

        const body = document.createElement('div');
        body.className = 'sources-body';
        body.style.display = 'none'; // 默认折叠

        // 容器：用于存放当前页的条目，切换页时清空重建
        const itemsContainer = document.createElement('div');
        itemsContainer.className = 'sources-items-container';
        body.appendChild(itemsContainer);

        // 分页圆点导航（底部居中）
        const dotsNav = document.createElement('div');
        dotsNav.className = 'sources-pagination-dots';
        body.appendChild(dotsNav);

        /**
         * 渲染指定页码的条目和圆点状态
         */
        function renderPage(page) {
            currentPage = page;

            // 清空条目容器
            itemsContainer.innerHTML = '';

            const start = page * PAGE_SIZE;
            const end = Math.min(start + PAGE_SIZE, sources.length);
            const pageSources = sources.slice(start, end);

            pageSources.forEach((src) => {
                console.log('[sources-panel] source item:', { title: src.title, content: src.content, publish_date: src.publish_date, url: src.url, site_name: src.site_name });
                const item = document.createElement('div');
                item.className = 'source-item';

                // 清理标题：去除标题中重复的网站名称以及搜索引擎附加的 "（发布时间：XXXX）" 后缀
                let cleanTitle = src.title || '';
                // 先去除 "（发布时间：XXXX）" 或 "(发布时间：XXXX)" 后缀
                cleanTitle = cleanTitle.replace(/[（(]发布时间：.*?[）)]/g, '').trim();
                const siteName = src.site_name || '';
                if (siteName) {
                    if (cleanTitle.startsWith(siteName)) {
                        cleanTitle = cleanTitle.slice(siteName.length);
                    }
                    if (cleanTitle.endsWith(siteName)) {
                        cleanTitle = cleanTitle.slice(0, -siteName.length);
                    }
                    cleanTitle = cleanTitle.replace(/^[\s\-_:—：]+|[\s\-_:—：]+$/g, '');
                }
                if (!cleanTitle) {
                    cleanTitle = src.title ? src.title.replace(/[（(]发布时间：.*?[）)]/g, '').trim() : '';
                }

                let siteBadgeHtml = '';
                if (src.site_icon || src.site_name) {
                    const iconHtml = src.site_icon
                        ? `<img class="source-site-icon" src="${escapeHtml(src.site_icon)}" alt="" loading="lazy" onerror="this.style.display='none'">`
                        : '';
                    const nameHtml = src.site_name
                        ? `<span class="source-site-name">${escapeHtml(src.site_name)}</span>`
                        : '';
                    if (iconHtml || nameHtml) {
                        siteBadgeHtml = `<span class="source-site-badge">${iconHtml}${nameHtml}</span>`;
                    }
                }

                // URL 为空时，标题不加链接效果（纯文本显示）
                const titleHtml = src.url
                    ? `<a class="source-title source-link" href="${escapeHtml(src.url)}" target="_blank" rel="noopener">${escapeHtml(cleanTitle)}</a>`
                    : `<span class="source-title">${escapeHtml(cleanTitle)}</span>`;

                // 发布时间格式化为 [发布于：XXXX]
                const publishHtml = src.publish_date
                    ? `<span style="color:var(--text-muted);font-size:0.75rem;display:block;margin-top:4px">[发布于：${escapeHtml(src.publish_date)}]</span>`
                    : '';

                item.innerHTML = `
                    <div class="source-title-row">
                        ${titleHtml}
                        ${siteBadgeHtml}
                    </div>
                    ${publishHtml}
                    ${src.content ? `<div style="margin-top:4px;font-size:0.78rem;color:var(--text-muted)">${escapeHtml(truncate(src.content, 100))}</div>` : ''}
                `;
                itemsContainer.appendChild(item);
            });

            // 重建圆点导航（仅当有多页时显示）
            dotsNav.innerHTML = '';
            if (totalPages > 1) {
                for (let i = 0; i < totalPages; i++) {
                    const dot = document.createElement('span');
                    dot.className = 'sources-pagination-dot' + (i === currentPage ? ' active' : '');
                    dot.dataset.page = i;
                    dot.addEventListener('click', () => {
                        renderPage(i);
                    });
                    dotsNav.appendChild(dot);
                }
            }
        }

        // 初始渲染第 0 页
        renderPage(0);

        section.appendChild(body);

        // 点击标题切换折叠
        title.addEventListener('click', () => toggleSourcesSection(title, body));
        title.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                toggleSourcesSection(title, body);
            }
        });
    }

    panel.appendChild(section);
    scrollToBottom();
}

// toggleSourcesSection 切换引用来源区域的折叠/展开
function toggleSourcesSection(titleEl, bodyEl) {
    const isCollapsed = bodyEl.style.display === 'none';
    bodyEl.style.display = isCollapsed ? '' : 'none';
    titleEl.classList.toggle('collapsed', !isCollapsed);
    titleEl.classList.toggle('expanded', isCollapsed);
}

// ============================================================
// Toast 消息提示
// ============================================================

// showToast 显示一个自动消失的 Toast 消息
// type: 'error' | 'success' | 'info'
// duration: 显示时长（毫秒），默认 4000
function showToast(message, type, duration) {
    if (!toastContainer) return;
    type = type || 'error';
    duration = duration || 4000;

    const toast = document.createElement('div');
    toast.className = 'toast toast-' + type;
    toast.textContent = message;
    toastContainer.appendChild(toast);

    // 触发动画
    requestAnimationFrame(() => {
        toast.classList.add('show');
    });

    // 自动移除
    setTimeout(() => {
        toast.classList.remove('show');
        // 等过渡动画结束后移除 DOM
        setTimeout(() => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        }, 300);
    }, duration);
}

// ============================================================
// 错误显示
// ============================================================

// showError 显示错误信息
function showError(assistantBubble, message) {
    // 如果 assistant 气泡是空的，直接显示错误
    const contentDiv = assistantBubble.querySelector('.bubble');
    if (contentDiv && !contentDiv.textContent.trim()) {
        contentDiv.innerHTML = `❌ ${escapeHtml(message)}`;
        contentDiv.classList.remove('streaming');
        assistantBubble.classList.add('error');
    } else {
        // 如果 assistant 气泡已有内容（如流式已开始），改用 toast 显示错误，
        // 避免创建无法删除的独立错误消息气泡
        showToast(message, 'error', 6000);
    }
    scrollToBottom();
}

// updateDeleteButtons 更新所有删除按钮的禁用状态
function updateDeleteButtons() {
    const deleteBtns = chatContainer.querySelectorAll('.delete-msg-btn');
    deleteBtns.forEach(btn => {
        btn.disabled = isStreaming;
    });
}

// setInputEnabled 启用/禁用输入
function setInputEnabled(enabled) {
    messageInput.disabled = !enabled;
    sendBtn.disabled = !enabled;
    if (enabled) {
        sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" fill="currentColor"/></svg>`;
    } else {
        sendBtn.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" style="animation:spin 1s linear infinite"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8z" fill="currentColor"/><path d="M12 6c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6z" fill="var(--bg-input)"/></svg>`;
    }
}

// scrollToBottom 滚动到底部
function scrollToBottom() {
    requestAnimationFrame(() => {
        chatContainer.scrollTop = chatContainer.scrollHeight;
    });
}

// ============================================================
// 初始化：从后端恢复会话历史
// ============================================================

// restoreSession 从后端获取当前 session 的历史消息并恢复显示
async function restoreSession() {
    try {
        const response = await fetch('/api/session');
        if (!response.ok) return;

        const data = await response.json();

        // isNew 或 history 为空 → 显示欢迎消息
        const history = data.history || [];
        if (data.is_new || history.length === 0) {
            showWelcomeMessage();
            return;
        }

        // 有历史消息，恢复显示
        for (const msg of history) {
            const msgDiv = addMessage(msg.role, msg.content);
            const entry = { role: msg.role, content: msg.content, id: msg.id, usage: msg.usage || null };
            messages.push(entry);
            // 保存 ID 到 DOM（addMessage 返回的即是 .message 元素）
            if (msgDiv && msg.id) {
                msgDiv.dataset.msgId = msg.id;
            }
            // 如果是 assistant 消息且有 usage 信息，显示 token-info
            if (msg.role === 'assistant' && msg.usage && msgDiv) {
                showTokenUsage(msgDiv, msg.usage);
            }
            // 如果是 assistant 消息且有 sources（联网搜索结果），恢复显示 sources-panel
            if (msg.role === 'assistant' && msg.sources && msg.sources.length > 0) {
                showSources(msg.sources, 'web');
            }
            // 如果是 assistant 消息且有 reasoning（深度思考链），恢复显示 reasoning 区域
            if (msg.role === 'assistant' && msg.reasoning && msgDiv) {
                restoreReasoningArea(msgDiv, msg.reasoning, msg.deep_think);
            }
        }
    } catch (e) {
        // 网络错误等情况下，回退到显示欢迎消息
        console.warn('无法恢复会话历史，显示欢迎消息:', e);
        showWelcomeMessage();
    }
}

/**
 * restoreReasoningArea 在 assistant 消息气泡中恢复 reasoning（深度思考链）区域
 * @param {HTMLElement} assistantBubble - .message.assistant 元素
 * @param {string} reasoningText - 思考链的原始 Markdown 文本
 */
function restoreReasoningArea(assistantBubble, reasoningText, wasDeepThink) {
    if (!assistantBubble || !reasoningText) return;

    // 隐藏独立的 AI 角色标签，将其合并到 reasoning-header 中
    const roleLabel = assistantBubble.querySelector('.role-label-ai');
    if (roleLabel) {
        roleLabel.style.display = 'none';
    }

    // 创建 reasoning 区域（默认折叠）
    const reasoningArea = document.createElement('div');
    reasoningArea.className = 'reasoning-area done collapsed';
    const titleText = '思考完成';
    reasoningArea.innerHTML = `
        <div class="reasoning-header">
            <span class="reasoning-toggle" title="折叠思考过程">▶</span>
            <span class="reasoning-icon">🤖</span>
            <span class="reasoning-role-badge">AI</span>
            <span class="reasoning-title">${titleText}</span>
        </div>
        <div class="reasoning-content">${renderMarkdown(reasoningText)}</div>
    `;

    // 点击 header 切换折叠/展开
    const header = reasoningArea.querySelector('.reasoning-header');
    header.addEventListener('click', (e) => {
        toggleReasoningCollapse(header);
    });

    // 插入到气泡之前
    const bubble = assistantBubble.querySelector('.bubble');
    if (bubble) {
        bubble.insertAdjacentElement('beforebegin', reasoningArea);
    } else {
        assistantBubble.appendChild(reasoningArea);
    }
}

// showWelcomeMessage 显示独立于消息系统的欢迎信息
function showWelcomeMessage() {
    // 避免重复添加
    if (chatContainer.querySelector('.welcome-message')) return;

    const el = document.createElement('div');
    el.className = 'welcome-message';
    el.textContent = '你好！我是 BrainOnline AI 助手，基于知识库的智能对话系统。请问有什么可以帮助你的？';
    chatContainer.appendChild(el);

    // 将输入区域移动到欢迎消息内部，使二者作为一个整体居中
    const inputArea = document.querySelector('.input-area');
    if (inputArea) {
        el.appendChild(inputArea);
    }

    // 标记欢迎状态
    document.getElementById('app').classList.add('welcome-state');
}

// 最多同时显示的刻度数
const MAX_VISIBLE_TICKS = 10;
// 刻度滚动偏移量（0 表示从第一条开始显示）
let tickScrollOffset = 0;

// updateTickNav 根据当前用户消息更新右侧刻度导航
function updateTickNav() {
    // 收集所有用户消息元素
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;

    // 清空并重建刻度
    tickNav.innerHTML = '';

    // 根据偏移量计算当前显示的刻度范围
    const startIdx = tickScrollOffset;
    const endIdx = Math.min(startIdx + MAX_VISIBLE_TICKS, tickCount);

    // 判断是否还有更多刻度
    const hasPrev = tickScrollOffset > 0;
    const hasNext = endIdx < tickCount;

    // 是否需要滚动（超过 MAX_VISIBLE_TICKS 条）
    const needsScroll = tickCount > MAX_VISIBLE_TICKS;

    // 顶部箭头 — 仅当需要滚动且还有上方刻度时显示
    if (needsScroll) {
        const topArrow = document.createElement('div');
        topArrow.className = 'tick-arrow' + (hasPrev ? '' : ' tick-arrow-disabled');
        topArrow.textContent = '▲';
        topArrow.title = '向上翻动';
        topArrow.addEventListener('click', (e) => {
            e.stopPropagation();
            if (!hasPrev) return;
            tickScrollOffset--;
            updateTickNav();
        });
        tickNav.appendChild(topArrow);
    }

    for (let i = startIdx; i < endIdx; i++) {
        const userMsg = userMessages[i];
        const content = userMsg.querySelector('.bubble').textContent || '';
        const title = truncate(content.replace(/\n/g, ' ').trim(), 30);

        const tick = document.createElement('div');
        tick.className = 'tick';
        tick.dataset.tickIndex = i;

        // 刻度线（短横线）
        const dot = document.createElement('span');
        dot.className = 'tick-dot';
        tick.appendChild(dot);

        // 标题文本（带绝对序号，从1开始，固定三位补0）
        const label = document.createElement('span');
        label.className = 'tick-label';
        const seqNum = String(i + 1).padStart(3, '0');
        label.textContent = seqNum + '. ' + title;
        tick.appendChild(label);

        // 点击刻度滚动到对应消息，并设为活动条目
        tick.addEventListener('click', () => {
            const targetMsg = chatContainer.querySelector(`.message.user[data-msg-index="${i}"]`);
            if (targetMsg) {
                setActiveTick(i);
                targetMsg.scrollIntoView({ behavior: 'smooth', block: 'start' });
                // 等待平滑滚动完成后给用户气泡添加高亮动画
                const bubble = targetMsg.querySelector('.bubble');
                if (bubble) {
                    bubble.classList.remove('highlight');
                    // 平滑滚动约需 400ms，延迟触发确保用户能看到动画
                    setTimeout(() => {
                        bubble.classList.add('highlight');
                    }, 420);
                }
            }
        });

        tickNav.appendChild(tick);
    }

    // 底部箭头 — 仅当需要滚动且还有下方刻度时显示
    if (needsScroll) {
        const bottomArrow = document.createElement('div');
        bottomArrow.className = 'tick-arrow' + (hasNext ? '' : ' tick-arrow-disabled');
        bottomArrow.textContent = '▼';
        bottomArrow.title = '向下翻动';
        bottomArrow.addEventListener('click', (e) => {
            e.stopPropagation();
            if (!hasNext) return;
            tickScrollOffset++;
            updateTickNav();
        });
        tickNav.appendChild(bottomArrow);
    }

    // 首尾刻度指示 — 用刻度线透明度提示还有更多内容
    const ticks = tickNav.querySelectorAll('.tick');
    if (ticks.length > 0) {
        if (hasPrev) {
            ticks[0].classList.add('tick-edge-prev');
        }
        if (hasNext) {
            ticks[ticks.length - 1].classList.add('tick-edge-next');
        }
    }

    // 重建后重新应用 active 状态（如果有）
    if (activeTickIndex >= 0) {
        setActiveTick(activeTickIndex);
    }
}

// 刻度面板鼠标滚轮滚动 — 改变偏移量，翻动显示的刻度
tickNav.addEventListener('wheel', (e) => {
    e.preventDefault();
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;
    if (tickCount <= MAX_VISIBLE_TICKS) return; // 不需要滚动

    const delta = e.deltaY > 0 ? 1 : -1;
    const newOffset = tickScrollOffset + delta;
    // 限制范围：0 到 (tickCount - MAX_VISIBLE_TICKS)
    const maxOffset = tickCount - MAX_VISIBLE_TICKS;
    if (newOffset < 0 || newOffset > maxOffset) return;

    tickScrollOffset = newOffset;
    updateTickNav();
});

// ============================================================
// 页面滚动时自动更新活动刻度（仅面板折叠时生效）
// ============================================================

// updateActiveTickOnScroll 根据当前视口中的用户消息更新活动刻度
function updateActiveTickOnScroll() {
    // 面板展开时忽略滚动
    if (tickNav.matches(':hover')) return;

    const userMessages = chatContainer.querySelectorAll('.message.user');
    if (userMessages.length === 0) return;

    const containerRect = chatContainer.getBoundingClientRect();
    const containerTop = containerRect.top;

    // 找到第一个顶部在容器顶部或以下的用户消息（即第一个完全/部分可见的消息）
    let targetIdx = 0;
    for (let i = 0; i < userMessages.length; i++) {
        const rect = userMessages[i].getBoundingClientRect();
        if (rect.top >= containerTop) {
            targetIdx = i;
            break;
        }
        targetIdx = i; // 遍历到最后一个都没找到，说明全部已滚出顶部
    }

    // 如果当前活动消息已滚出顶部，且后面还有消息，则跳到下一个可见消息；
    // 但如果后面已经没有消息了（即当前是最后一个），则保持不动。
    if (activeTickIndex >= 0 && activeTickIndex < userMessages.length) {
        const activeRect = userMessages[activeTickIndex].getBoundingClientRect();
        if (activeRect.top < containerTop && activeTickIndex < userMessages.length - 1) {
            // 当前消息已滚出顶部，且后面还有消息 → 跳到 targetIdx
            if (targetIdx !== activeTickIndex) {
                activeTickIndex = targetIdx;
                adjustTickOffset();
                updateTickNav();
            }
        } else if (activeRect.top >= containerTop) {
            // 当前消息仍然可见，更新为更精确的 targetIdx
            if (targetIdx !== activeTickIndex) {
                activeTickIndex = targetIdx;
                adjustTickOffset();
                updateTickNav();
            }
        }
        // 否则（已滚出顶部但无下一组）：保持 activeTickIndex 不变
    } else {
        // 没有活动消息，直接用 targetIdx
        if (targetIdx !== activeTickIndex) {
            activeTickIndex = targetIdx;
            adjustTickOffset();
            updateTickNav();
        }
    }
}

// adjustTickOffset 调整 tickScrollOffset 确保活动刻度可见
function adjustTickOffset() {
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;
    if (tickCount > MAX_VISIBLE_TICKS) {
        if (activeTickIndex < tickScrollOffset) {
            tickScrollOffset = activeTickIndex;
        } else if (activeTickIndex >= tickScrollOffset + MAX_VISIBLE_TICKS) {
            tickScrollOffset = activeTickIndex - MAX_VISIBLE_TICKS + 1;
        }
    }
}

// 节流包装，避免频繁触发
let scrollThrottleTimer = null;
chatContainer.addEventListener('scroll', () => {
    if (scrollThrottleTimer) return;
    scrollThrottleTimer = setTimeout(() => {
        scrollThrottleTimer = null;
        updateActiveTickOnScroll();
    }, 150);
});

// setActiveTick 设置指定索引的刻度为活动状态
function setActiveTick(index) {
    activeTickIndex = index;
    const ticks = tickNav.querySelectorAll('.tick');
    ticks.forEach((t) => {
        t.classList.toggle('active', parseInt(t.dataset.tickIndex, 10) === index);
    });
}

// 面板消失时清除活动状态
tickNav.addEventListener('mouseleave', () => {
    if (activeTickIndex >= 0) {
        setActiveTick(-1);
    }
});

// ============================================================
// 删除模态框
// ============================================================

// showDeleteModal 显示删除确认模态框
function showDeleteModal() {
    if (activeTickIndex < 0) return;

    // 将当前活动索引保存到模态框，避免 mouseleave 清除 activeTickIndex 后丢失
    deleteModal.dataset.deleteIndex = activeTickIndex;

    // 获取用户消息
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${activeTickIndex}"]`);
    let html = '';
    if (userMsg) {
        const rawContent = userMsg.querySelector('.bubble').textContent || '';
        // 用户问题最多显示 28 字
        const userPreview = escapeHtml(truncate(rawContent, 28));
        html += `<div style="margin-bottom:8px; border-bottom: black 1px solid;"><strong>我：</strong>${userPreview}</div>`;
        // 在同一个 .message-group 内查找 AI 回复（不依赖 data-msg-id 配对）
        const group = userMsg.closest('.message-group');
        if (group) {
            const assistantMsg = group.querySelector('.message.assistant');
            if (assistantMsg) {
                const assistantContent = assistantMsg.querySelector('.bubble').textContent || '';
                // 去掉首尾空白，最多显示 62 字
                const assistantPreview = escapeHtml(truncate(assistantContent.trim(), 62));
                if (assistantPreview) {
                    html += `<div style="margin-bottom:4px; border-bottom: black 1px solid;"><strong>AI：</strong>${assistantPreview}</div>`;
                    html += `<div style="margin-bottom:4px; color:red">（注意：双方对话将一起删除）</div>`;
                }
            }
        }
    }

    modalBody.innerHTML = html || '(无内容)';
    deleteModal.classList.add('show');
}

// hideDeleteModal 隐藏删除模态框
function hideDeleteModal() {
    deleteModal.classList.remove('show');
    delete deleteModal.dataset.deleteIndex;
}

// confirmDelete 确认删除
async function confirmDelete() {
    const index = parseInt(deleteModal.dataset.deleteIndex, 10);
    if (isNaN(index) || index < 0) return;

    // 获取要删除的用户消息的 msg_id
    const userMsg = chatContainer.querySelector(`.message.user[data-msg-index="${index}"]`);
    if (!userMsg) {
        hideDeleteModal();
        return;
    }
    const msgId = parseInt(userMsg.dataset.msgId, 10);

    // 找到该消息所在的 .message-group
    const group = userMsg.closest('.message-group');
    if (!group) {
        hideDeleteModal();
        return;
    }

    // 在移除 DOM 之前，先收集该组中所有消息的 ID（用于后续清理 messages 数组）
    const groupMsgIds = new Set();
    group.querySelectorAll('.message').forEach(el => {
        const id = parseInt(el.dataset.msgId, 10);
        if (!isNaN(id)) groupMsgIds.add(id);
    });

    try {
        // msgId 为 0 或无效（NaN）表示提交未完成（失败或尚未分配），仅删除前端 DOM
        if (!msgId || isNaN(msgId)) {
            group.remove();
        } else {
            // 有有效 ID，先调后端 API 删除
            const response = await fetch('/api/history', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ msg_id: msgId })
            });

            if (!response.ok) {
                const errText = await response.text();
                throw new Error(`删除失败 [${response.status}]: ${errText}`);
            }

            // 后端删除成功后，移除整个消息组
            group.remove();
        }

        // 从 messages 数组中删除该组中所有消息（包括 id=0 的条目）
        messages = messages.filter(msg => !groupMsgIds.has(msg.id));

        // 重新编号所有 user 消息的 data-msg-index
        const remainingUsers = chatContainer.querySelectorAll('.message.user');
        remainingUsers.forEach((msg, i) => {
            msg.dataset.msgIndex = i;
        });

        // 更新 userMsgCount
        userMsgCount = remainingUsers.length;

        // 如果所有消息都被删除（没有用户消息了），重置 currentGroup
        if (remainingUsers.length === 0) {
            currentGroup = null;
        } else {
            // 更新 currentGroup 为最后一个 group
            const lastGroup = chatContainer.querySelector('.message-group:last-child');
            currentGroup = lastGroup;
        }

        // 更新刻度导航
        updateTickNav();

    } catch (e) {
        console.error('删除失败:', e);
        showToast('删除失败: ' + e.message, 'error');
    } finally {
        hideDeleteModal();
    }
}

// 模态框事件绑定
modalCloseBtn.addEventListener('click', hideDeleteModal);
modalCancelBtn.addEventListener('click', hideDeleteModal);
modalConfirmBtn.addEventListener('click', confirmDelete);

// 点击模态框外部关闭
deleteModal.addEventListener('click', (e) => {
    if (e.target === deleteModal) {
        hideDeleteModal();
    }
});

// 页面加载后先恢复会话
window.addEventListener('DOMContentLoaded', restoreSession);
