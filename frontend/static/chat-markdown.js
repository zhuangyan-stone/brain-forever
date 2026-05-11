// ============================================================
// chat-markdown.js — Markdown 渲染 + 代码高亮
// ============================================================

import { escapeHtml } from './toolsets.js';
import { state } from './chat-state.js';

'use strict';

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
    return text
        // 引号 + 强调标记 → 引号 + 零宽空格 + 强调标记
        .replace(/(["\u201c\u201d])(?=[*_]{1,2})/g, '$1\u200B')
        // 强调标记 + 引号 → 强调标记 + 零宽空格 + 引号
        .replace(/(?<=[*_]{1,2})(["\u201c\u201d])/g, '\u200B$1');
}

/**
 * renderMarkdown 将 Markdown 文本渲染为安全的 HTML，并对代码块进行语法高亮
 * @param {string} text
 * @returns {string}
 */
export function renderMarkdown(text) {
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

/**
 * highlightCodeBlocks 对 HTML 中的 <pre><code> 代码块进行语法高亮，并添加复制按钮
 * @param {string} html
 * @returns {string}
 */
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

/**
 * addCopyButton 为 <pre> 代码块添加复制按钮（仅创建 DOM，事件由委托处理）
 * @param {HTMLElement} pre
 */
function addCopyButton(pre) {
    // 避免重复添加
    if (pre.querySelector('.copy-btn')) return;

    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.textContent = '复制 ▾';
    btn.disabled = state.isStreaming; // 流式输出时禁用

    pre.appendChild(btn);
}

/**
 * enableCopyButtons 启用指定消息气泡内的所有复制按钮
 * @param {HTMLElement} bubbleElement
 */
export function enableCopyButtons(bubbleElement) {
    bubbleElement.querySelectorAll('.copy-btn').forEach((btn) => {
        btn.disabled = false;
    });
}

/**
 * switchHighlightTheme 切换 highlight.js 的主题样式表
 * @param {string} theme - 'dark' 或 'light'
 */
export function switchHighlightTheme(theme) {
    const darkTheme = document.getElementById('hljs-theme-dark');
    const lightTheme = document.getElementById('hljs-theme-light');
    if (darkTheme && lightTheme) {
        darkTheme.disabled = theme !== 'dark';
        lightTheme.disabled = theme !== 'light';
    }
}
