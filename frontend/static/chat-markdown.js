// ============================================================
// chat-markdown.js — Markdown 渲染 + 代码高亮
// ============================================================

import { escapeHtml } from './toolsets.js';
import { ICON_COPY } from './svg_icons_re.js';

'use strict';

// ============================================================
// remarkable KaTeX 插件 — 支持行内 $...$ 和块级 $$...$$ 公式
// ============================================================
function remarkableKatex(md) {
  // --- 块级公式 $$...$$ ---
  md.inline.ruler.after('escape', 'katex_block', function katexBlock(state, silent) {
    var start = state.pos;
    var max = state.posMax;

    if (state.src.charCodeAt(start) !== 0x24 /* $ */) return false;
    if (start + 2 >= max) return false;
    if (state.src.charCodeAt(start + 1) !== 0x24 /* $ */) return false;

    // 查找结束 $$
    var end = start + 2;
    while (end < max - 1) {
    if (state.src.charCodeAt(end) === 0x24 /* $ */ &&
        state.src.charCodeAt(end + 1) === 0x24 /* $ */) {
        // 跳过转义 \$
        if (end > start + 2 && state.src.charCodeAt(end - 1) === 0x5C /* \ */) {
          end += 2;
          continue;
        }
        break;
      }
      end++;
    }

    if (end >= max - 1) return false; // 没有闭合 $$

    if (!silent) {
      var content = state.src.slice(start + 2, end);
      // [诊断] 检测到块级公式
      if (content.trim()) {
        console.debug('[KaTeX诊断] 块级公式匹配: content=%o, context="%s"',
          content, state.src.slice(Math.max(0, start - 15), end + 15));
      }
      state.push({
        type: 'katex_block',
        content: content,
        block: true,
        level: state.level
      });
    }

    state.pos = end + 2;
    return true;
  });

  // --- 行内公式 $...$ ---
  md.inline.ruler.after('katex_block', 'katex_inline', function katexInline(state, silent) {
    var start = state.pos;
    var max = state.posMax;

    if (state.src.charCodeAt(start) !== 0x24 /* $ */) return false;
    if (start + 1 >= max) return false;
    if (state.src.charCodeAt(start + 1) === 0x24 /* $ */) return false; // 跳过 $$

    // 前一个字符是 \ 说明是转义
    if (start > 0 && state.src.charCodeAt(start - 1) === 0x5C /* \ */) return false;

    // 查找结束 $
    var end = start + 1;
    while (end < max) {
    if (state.src.charCodeAt(end) === 0x24 /* $ */) {
        // 跳过转义
        if (end > start + 1 && state.src.charCodeAt(end - 1) === 0x5C /* \ */) {
          end++;
          continue;
        }
        break;
      }
      end++;
    }

    if (end >= max) return false; // 没有闭合 $

    if (!silent) {
      var content = state.src.slice(start + 1, end);
      // [诊断] 检测到行内公式，记录上下文以排查误匹配
      var context = state.src.slice(Math.max(0, start - 10), end + 10);
      console.debug('[KaTeX诊断] 行内公式匹配: content=%o, end=%d, max=%d, context="%s"',
        content, end, max, context);
      state.push({
        type: 'katex_inline',
        content: content,
        block: false,
        level: state.level
      });
    }

    state.pos = end + 1;
    return true;
  });

  // --- 注册 renderer rules ---
  md.renderer.rules['katex_block'] = function (tokens, idx, options, env, self) {
    try {
      var result = katex.renderToString(tokens[idx].content, {
        displayMode: true,
        throwOnError: false
      });
      return result;
    } catch (e) {
      console.debug('[KaTeX诊断] 块级公式渲染失败: content=%o, error=%o', tokens[idx].content, e.message);
      return '<div class="katex-error">' + self.utils.escapeHtml(tokens[idx].content) + '</div>';
    }
  };

  md.renderer.rules['katex_inline'] = function (tokens, idx, options, env, self) {
    try {
      var result = katex.renderToString(tokens[idx].content, {
        displayMode: false,
        throwOnError: false
      });
      return result;
    } catch (e) {
      console.debug('[KaTeX诊断] 行内公式渲染失败: content=%o, error=%o', tokens[idx].content, e.message);
      return '<span class="katex-error">' + self.utils.escapeHtml(tokens[idx].content) + '</span>';
    }
  };
}


// ============================================================
// fixTableAlignment 修复 AI 模型输出的表格对齐分隔行中的多余冒号。
//
// 问题背景：某些 LLM（如 DeepSeek V4/Flash）在生成 GFM 表格时，
// 对齐分隔行可能出现多余的冒号，例如 `::----` 或 `:::---`，
// 导致 remarkable 无法正确解析表格，整个表格退化为纯文本。
//
// 标准 GFM 表格对齐分隔行格式：
//   `----`  默认（通常左对齐）
//   `:---`  左对齐
//   `:--:`  居中对齐
//   `---:`  右对齐
//
// 本函数将表格分隔行中连续 2 个以上的冒号缩减为 1 个，
// 例如：`::----` → `:----`，`:::--:` → `:--:`。
// ============================================================
function fixTableAlignment(text) {
    // 只处理表格对齐分隔行：行内容仅包含 |、:、-、空格（即 GFM 表格的第二行）
    // 将其中连续 2+ 个冒号替换为单个冒号，覆盖所有位置：
    //   - 开头：::---- → :----
    //   - 中间：-::-  → -:-
    //   - 末尾：----:: → ---:
    //   - 混合：|::----|::| → |:----|:|
    return text.replace(/^[\s|:\-]+$/gm, (line) => {
        // 进一步确认：必须包含至少一个 -（排除全是空格/冒号/| 的噪声行）
        if (!/-/.test(line)) return line;
        // 检查是否包含连续冒号（2 个以上）
        if (!/:{2,}/.test(line)) return line;
        // deepseek 当前输出的表格 markdown 中常有 :: 甚至 ::: 的错误，
        // 当发现此问题时输出调试信息，以便后续 deepseek 官方修复后可移除本函数
        console.debug("malformed table separator detected:", line);
        // 将连续 2+ 个冒号替换为单个冒号
        return line.replace(/:{2,}/g, ':');
    });
}

/**
 * renderMarkdown 将 Markdown 文本渲染为安全的 HTML，并对代码块进行语法高亮
 * @param {string} text
 * @returns {string}
 */
export function renderMarkdown(text) {
    if (!text) return '';
    // [诊断] 记录输入文本是否包含 $ 符号
    if (text.indexOf('$') !== -1) {
        console.debug('[KaTeX诊断] renderMarkdown 输入含 $: text.length=%d, $出现次数=%d',
            text.length, (text.match(/\$/g) || []).length);
    }
    try {
        // 修复 AI 模型输出的表格对齐分隔行中的多余冒号（如 ::---- → :----）
        const fixed = fixTableAlignment(text);
        // 使用 remarkable 渲染
        const md = new remarkable.Remarkable({
            html: true,
            breaks: true,      // 支持 GitHub 风格的换行
            langPrefix: 'language-',
        });
        // 注册 KaTeX 插件
        md.use(remarkableKatex);
        const html = md.render(fixed);
        // [诊断] 检查渲染结果是否包含公式
        if (html.indexOf('katex') !== -1) {
            var katexCount = (html.match(/class="katex"/g) || []).length;
            var errorCount = (html.match(/katex-error/g) || []).length;
            console.debug('[KaTeX诊断] 渲染完成: 成功公式=%d, 错误=%d', katexCount, errorCount);
        }
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

        // 获取语言类名（remarkable 会添加 class="language-xxx"）
        const langClass = Array.from(el.classList).find(cls => cls.startsWith('language-'));
        if (langClass) {
            const lang = langClass.replace('language-', '');
            // 先检查 highlight.js 是否已注册该语言，避免控制台打印
            // "Could not find the language 'xxx'" 警告
            if (hljs.getLanguage(lang)) {
                try {
                    el.innerHTML = hljs.highlight(el.textContent, { language: lang }).value;
                    pre.setAttribute('data-lang', lang);
                } catch (_) {
                    // 高亮失败，fallback 到自动检测
                    try {
                        el.innerHTML = hljs.highlightAuto(el.textContent).value;
                    } catch (_) {
                        // 回退：不做高亮
                    }
                }
            } else {
                // 语言未注册（如 powershell、dockerfile 等），使用自动检测
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
    btn.className = 'copy-btn code-copy-btn';
    btn.dataset.tooltip = '复制代码块';
    try {
        btn.disabled = !!(window.Alpine && Alpine.store('chats') && Alpine.store('chats').active && Alpine.store('chats').active.isStreaming);
    } catch(e) {
        btn.disabled = false;
    }
    btn.innerHTML = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + ICON_COPY + '</svg><span class="copy-btn-label">复制为 Markdown</span>';

    pre.appendChild(btn);
}

/**
 * enableCopyButtons 启用指定消息气泡内的所有复制按钮
 * @param {HTMLElement} bubbleElement
 */
export function enableCopyButtons(bubbleElement) {
    bubbleElement.querySelectorAll('.copy-btn.code-copy-btn').forEach((btn) => {
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

// ---- 注册 Markdown 渲染器到全局（供 alpine-store.js 预渲染使用） ----
// alpine-store.js 是普通 <script>（非 ES Module），无法直接 import，
// 但 store 方法（addGroup, finalizeStreamingToGroup, setChatMessageGroups）
// 需要在写入数据时预渲染 contentHTML，因此通过全局引用调用 renderMarkdown。
// 由于这些方法在用户交互时才执行，此时 ES Module 已加载完毕，函数一定可用。
window._alpineRenderMarkdown = renderMarkdown;
