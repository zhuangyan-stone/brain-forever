// ============================================================
// dialogs/portrait-dialog.js — 用户"个人画像"生成对话框 (v2)
// ============================================================
//
// 功能：
//   1. 遮罩层 + 大对话框（不点击遮罩自动关闭）
//   2. 流式 SSE 接收画像内容，实时渲染 Markdown
//   3. 复制按钮（Markdown 格式，内容包含左侧精华区 + 文档内容）
//   4. 分享按钮（占位）
//   5. 取消（流式中）/ 关闭（完成后）按钮
//   6. 精华区：头像、信息区、核心特质书签、印象速写引文
//
// SSE 事件类型：
//   - info:  精华区元数据（生成时间、对话数、特征数、时间跨度、润色度）
//   - text:  画像内容文本块
//   - meta:  结构化元数据（core_traits, key_highlights）
//   - error: 错误信息
//   - done:  流完成
//
// 使用方式：
//   <div class="portrait-overlay" id="portraitDialog"
//        x-data="userPortraitDialog()"
//        :class="{ show: show }">
//     ...
//   </div>
//
//   在 JS 中通过 Alpine.$data('portraitDialog') 调用：
//     Alpine.$data(portraitDialog).open()
//     Alpine.$data(portraitDialog).close()
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册组件。
// ============================================================

document.addEventListener('alpine:init', function() {

    /**
     * 渲染 Markdown 为 HTML
     * @param {string} text
     * @returns {string}
     */
    function renderMarkdown(text) {
        if (!text) return '';
        if (typeof window._alpineRenderMarkdown === 'function') {
            return window._alpineRenderMarkdown(text);
        }
        // 回退：简单转义
        return String(text).replace(/&/g, '&').replace(/</g, '<').replace(/>/g, '>');
    }

    /**
     * 复制纯文本到剪贴板
     * @param {string} text
     * @returns {Promise<boolean>}
     */
    function copyPlainText(text) {
        if (!text) return Promise.resolve(false);
        if (navigator.clipboard && navigator.clipboard.writeText) {
            return navigator.clipboard.writeText(text).then(function() { return true; })
                .catch(function() { return fallbackCopyText(text); });
        }
        return Promise.resolve(fallbackCopyText(text));
    }

    /**
     * 回退复制方案
     * @param {string} text
     * @returns {boolean}
     */
    function fallbackCopyText(text) {
        try {
            var textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            textarea.style.left = '-9999px';
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            var success = document.execCommand('copy');
            document.body.removeChild(textarea);
            return success;
        } catch(e) {
            return false;
        }
    }

    /**
     * 书签颜色类数组 — 循环使用
     */
    var BOOKMARK_COLORS = [
        'portrait-bookmark-c1',  'portrait-bookmark-c2',
        'portrait-bookmark-c3',  'portrait-bookmark-c4',
        'portrait-bookmark-c5',  'portrait-bookmark-c6',
        'portrait-bookmark-c7',  'portrait-bookmark-c8',
        'portrait-bookmark-c9',  'portrait-bookmark-c10',
    ];

    /**
     * 书签统一使用中等尺寸（不再按文本长度分配不同大小）
     * @param {string} text
     * @returns {string} CSS class
     */
    function bookmarkSizeClass(text) {
        return 'portrait-bookmark-md';
    }

    // ============================================================
    // userPortraitDialog — Alpine 组件
    // ============================================================
    Alpine.data('userPortraitDialog', function() {
        return {
            // ---- 状态 ----
            show: false,
            portrait: '',           // 完整画像 Markdown 原文
            portraitHTML: '',       // 渲染后的 HTML
            portraitTitle: '',      // 文档总标题（由 _fetchDocTitle 从 /api/user/portrait/title 获取）
            portraitMeta: null,     // 结构化元数据 {core_traits, key_highlights}
            portraitInfo: null,     // 精华区元数据 {generated_at, chat_count, ...}
            isStreaming: false,     // 是否正在流式接收
            isDone: false,          // 是否已完成（流+标题）
            isWaitingTitle: false,  // 流已完成，正在等待 LLM 生成文档标题
            hasError: false,
            errorMessage: '',
            userName: '',           // 用户昵称
            userAvatar: '',         // 用户头像 URL
            wordCloudItems: [],     // 词云布局 [{tag, count, left, top, fontSize, colorClass}]

            // 节流渲染
            _renderTimer: null,
            // ResizeObserver（监听词云容器尺寸变化自动重排）
            _resizeObserver: null,
            // SSE AbortController
            _abortController: null,
            // 标题请求 AbortController
            _titleAbortController: null,
            // 安全地引用 $el（通过 init 钩子设置）
            _el: null,

            // ---- 计算属性 ----
            get title() {
                return 'AI 印象';
            },

            get showCancel() {
                // 流式未完成 或 等待标题生成 → 显示"取消"按钮
                return (this.isStreaming && !this.isDone) || this.isWaitingTitle;
            },

            get showClose() {
                // 流式已完成 且 不等待标题 且 整体已完成 → 显示"关闭"按钮
                return !this.isStreaming && !this.isWaitingTitle && this.isDone;
            },

            // 书签统一使用中等尺寸（不再按文本长度分配不同大小）
            bookmarkSizeClass: function(text) {
                return 'portrait-bookmark-md';
            },

            /**
             * 生成词云布局 — 委托给 WordCloudLayout 组件
             * 计算结果存入 this.wordCloudItems
             */
            _generateWordCloudLayout: function() {
                var info = this.portraitInfo;
                if (!info || !info.hot_tags || !info.hot_tags.length) {
                    this.wordCloudItems = [];
                    return;
                }

                // 读取实际容器尺寸
                var container = this.$el ? this.$el.querySelector('.portrait-wordcloud') : null;
                var cw = container ? container.clientWidth : 320;
                var ch = container ? container.clientHeight : 200;
                if (cw < 50 || ch < 50) {
                    this.wordCloudItems = [];
                    return;
                }

                this.wordCloudItems = window.WordCloudLayout.calculate(info.hot_tags, cw, ch);
            },

            // ---- 方法 ----

            /**
             * 打开画像对话框
             */
            open: function() {
                var self = this;

                // 重置状态（先清理旧请求）
                if (this._titleAbortController) {
                    this._titleAbortController.abort();
                    this._titleAbortController = null;
                }
                this.portrait = '';
                this.portraitHTML = '';
                this.portraitTitle = '';
                this.portraitMeta = null;
                this.portraitInfo = null;
                this.wordCloudItems = [];
                this.isStreaming = true;
                this.isDone = false;
                this.isWaitingTitle = false;
                this.hasError = false;
                this.errorMessage = '';
                this.show = true;

                // 从 Alpine store 获取用户信息
                try {
                    var chats = Alpine.store('chats');
                    if (chats) {
                        this.userName = chats.currentUserNickname || chats.currentUserNo || '匿名用户';
                        this.userAvatar = chats.currentUserAvatar || '/static/img/avatar/anonymous.png';
                    }
                } catch(e) {
                    this.userName = '匿名用户';
                    this.userAvatar = '/static/img/avatar/anonymous.png';
                }

                // 使用 $nextTick 确保 DOM 已更新后再发起请求
                this.$nextTick(function() {
                    self._startFetch();

                    // 建立 ResizeObserver 监听词云容器尺寸变化
                    var cloudContainer = self.$el ? self.$el.querySelector('.portrait-wordcloud') : null;
                    if (cloudContainer && !self._resizeObserver) {
                        self._resizeObserver = new ResizeObserver(function() {
                            if (self.portraitInfo && self.portraitInfo.hot_tags && self.portraitInfo.hot_tags.length) {
                                self._generateWordCloudLayout();
                            }
                        });
                        self._resizeObserver.observe(cloudContainer);
                    }
                });
            },

            /**
             * 关闭对话框
             */
            close: function() {
                this._abortSSE();
                // 中止标题请求（如有进行中）
                if (this._titleAbortController) {
                    this._titleAbortController.abort();
                    this._titleAbortController = null;
                }
                // 断开 ResizeObserver
                if (this._resizeObserver) {
                    this._resizeObserver.disconnect();
                    this._resizeObserver = null;
                }
                this.show = false;
                this.portrait = '';
                this.portraitHTML = '';
                this.portraitTitle = '';
                this.portraitMeta = null;
                this.portraitInfo = null;
                this.wordCloudItems = [];
                this.isStreaming = false;
                this.isDone = false;
                this.isWaitingTitle = false;
                this.hasError = false;
            },

            /**
             * 取消（流式未完成时）
             */
            cancel: function() {
                this._abortSSE();
                this.close();
            },

            // ---- 复制功能 ----

            /**
             * 执行复制操作 — 复制内容包含左侧精华区（信息、核心特质、印象速写）+ 文档内容
             * 统一输出 Markdown 格式。
             */
            copyAll: function() {
                if (!this.portrait) return;

                var self = this;
                var md = this._buildCopyMarkdown();
                if (!md) return;

                copyPlainText(md).then(function(ok) {
                    self._showToast(ok ? '✓ 已复制' : '复制失败', ok);
                });
            },

            /**
             * 构建包含左侧精华区 + 文档内容的完整 Markdown 文本
             * @returns {string}
             */
            _buildCopyMarkdown: function() {
                var info = this.portraitInfo;
                var meta = this.portraitMeta;
                var userName = this.userName || '匿名用户';
                var docText = this.portrait || '';

                var parts = [];

                // ---- 标题 ----
                parts.push('# 用户画像 - 「' + userName + '」');
                parts.push('');

                // ---- 1. 信息区 ----
                if (info) {
                    parts.push('## 基本信息');
                    parts.push('- 基于 ' + (info.chat_count || 0) + ' 个对话 '
                        + (info.trait_count || 0) + ' 条个人特征生成'
                        + '，润色度：' + (info.retouch || 0));
                    parts.push('- 跨度 ' + (info.span_days || 0) + ' 天'
                        + (info.earliest_date ? '（' + info.earliest_date.replace(/-/g, '/') : '')
                        + (info.latest_date ? ' - ' + info.latest_date.replace(/-/g, '/') + '）' : ''));
                    parts.push('- 生成于：' + (info.generated_at || ''));
                    parts.push('');
                }

                // ---- 1b. 话题热区 ----
                if (info && info.hot_tags && info.hot_tags.length) {
                    parts.push('## 🔥 话题热区');
                    info.hot_tags.forEach(function(item) {
                        parts.push('- ' + item.tag + '（' + item.count + ' 个对话）');
                    });
                    parts.push('');
                }

                // ---- 2. 核心特质 ----
                if (meta && meta.core_traits && meta.core_traits.length) {
                    parts.push('## 🏷️ 人格标签');
                    meta.core_traits.forEach(function(trait) {
                        parts.push('- ' + trait);
                    });
                    parts.push('');
                }

                // ---- 3. 印象速写 ----
                if (meta && meta.key_highlights && meta.key_highlights.length) {
                    parts.push('## ✏️ 印象速写');
                    meta.key_highlights.forEach(function(item) {
                        parts.push('> ' + item);
                    });
                    parts.push('');
                }

                // ---- 分割线 ----
                var hasEssence = (info || (meta && (meta.core_traits || []).length > 0) || (meta && (meta.key_highlights || []).length > 0));
                if (hasEssence) {
                    parts.push('---');
                    parts.push('');
                }

                // ---- 4. 文档内容 ----
                parts.push('## AI 眼中的 ' + userName + ' ……');
                parts.push('');
                parts.push(docText);

                // ---- 5. 总标题（末行） ----
                var portraitTitle = this.portraitTitle || '';
                if (portraitTitle) {
                    parts.push('');
                    parts.push(portraitTitle);
                }

                return parts.join('\n');
            },

            /**
             * 分享按钮（占位）
             */
            share: function() {
                this._showToast('分享功能即将上线', false, 'info');
            },

            // ---- 内部方法 ----

            /**
             * 发起 SSE 请求
             */
            _startFetch: function() {
                var self = this;

                this._abortController = new AbortController();
                var signal = this._abortController.signal;

                // 发起 GET 请求到 local-server
                fetch('/api/user/portrait?retouch=3', {
                    signal: signal,
                }).then(async function(response) {
                    if (!response.ok) {
                        const t = await response.text();
                        throw new Error(t || '请求失败');
                    }

                    // 读取 SSE 流
                    var reader = response.body.getReader();
                    var decoder = new TextDecoder();
                    var buffer = '';

                    function read() {
                        reader.read().then(function(result) {
                            if (result.done) {
                                // 流字节已读完，不在此处触发 _onStreamDone；
                                // SSE 'done' 事件（由 _handleSSEEvent 解析）才是正确的完成信号。
                                return;
                            }

                            buffer += decoder.decode(result.value, { stream: true });

                            // 按行解析 SSE 数据
                            var lines = buffer.split('\n');
                            buffer = lines.pop() || ''; // 保留未完成的行

                            for (var i = 0; i < lines.length; i++) {
                                var line = lines[i];
                                if (line.startsWith('data: ')) {
                                    var dataStr = line.substring(6);
                                    try {
                                        var event = JSON.parse(dataStr);
                                        self._handleSSEEvent(event);
                                    } catch(e) {
                                        // 非 JSON 行跳过
                                    }
                                }
                            }

                            read();
                        }).catch(function(err) {
                            if (err.name === 'AbortError') {
                                // 用户取消，不报错
                                return;
                            }
                            self._onStreamError(err.message || '读取流失败');
                        });
                    }

                    read();
                }).catch(function(err) {
                    if (err.name === 'AbortError') return;
                    self._onStreamError(err.message || '请求失败');
                });
            },

            /**
             * 处理 SSE 事件
             * @param {object} event - { event: string, data: any }
             */
            _handleSSEEvent: function(event) {
                var eventType = event.event;
                var data = event.data;

                switch (eventType) {
                    case 'info':
                        // 精华区元数据（由 local-server 在流开始前发送）
                        if (data && typeof data === 'object') {
                            this.portraitInfo = data;
                            // 生成词云布局（$nextTick 确保容器已渲染）
                            var self = this;
                            this.$nextTick(function() {
                                self._generateWordCloudLayout();
                            });
                        }
                        break;

                    case 'text':
                        // 累加文本内容
                        this.portrait += (typeof data === 'string' ? data : '');
                        this._throttleRender();
                        break;

                    case 'meta':
                        // 结构化元数据：核心特质 + 印象速写
                        if (data && typeof data === 'object') {
                            this.portraitMeta = data;
                        }
                        break;

                    case 'error':
                        this._onStreamError(typeof data === 'string' ? data : '生成画像时出错');
                        break;

                    case 'done':
                        this._onStreamDone();
                        break;
                }
            },

            /**
             * 节流渲染 Markdown
             */
            _throttleRender: function() {
                var self = this;
                if (this._renderTimer) return;
                this._renderTimer = setTimeout(function() {
                    self._renderTimer = null;
                    self.portraitHTML = renderMarkdown(self.portrait);
                }, 150);
            },

            /**
             * 流完成后，调用 local-server 生成文档标题
             */
            _fetchDocTitle: function() {
                var self = this;
                var text = this.portrait;
                if (!text) {
                    // 无内容，直接结束等待状态
                    self.isWaitingTitle = false;
                    self.isDone = true;
                    return;
                }

                this._titleAbortController = new AbortController();

                fetch('/api/user/portrait/title', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: text }),
                    signal: this._titleAbortController.signal,
                }).then(async function(response) {
                    if (!response.ok) {
                        const t = await response.text();
                        console.warn('获取AI印象标题失败:', t);
                        showToast('获取AI印象标题失败：' + t, 'error');
                        return null;
                    }
                    return response.json();
                }).then(function(data) {
                    if (data && data.title) {
                        self.portraitTitle = data.title;
                    }
                    // 标题已获得（或接口返回无标题），结束等待
                    self.isWaitingTitle = false;
                    self.isDone = true;
                }).catch(function(err) {
                    // 用户取消不处理
                    if (err && err.name === 'AbortError') return;
                    // 静默失败：标题生成非必需，不影响画像展示
                    self.isWaitingTitle = false;
                    self.isDone = true;
                });
            },

            /**
             * 流完成回调
             */
            _onStreamDone: function() {
                this.isStreaming = false;
                // 不立即标记 isDone=true，而是进入"等待标题"状态，
                // 让 loading 和"取消"按钮继续保持，直到标题返回
                this.isWaitingTitle = true;

                // 最终渲染
                this.portraitHTML = renderMarkdown(this.portrait);
                if (this._renderTimer) {
                    clearTimeout(this._renderTimer);
                    this._renderTimer = null;
                }

                // 异步调用 local-server 生成文档总标题
                this._fetchDocTitle();
            },

            /**
             * 流错误回调
             */
            _onStreamError: function(message) {
                this.isStreaming = false;
                this.hasError = true;
                // 检测余额不足（402 / Insufficient Balance），附加欠费提示
                if (/402|insufficient\s*balance/i.test(message)) {
                    message += '（你可能欠费了 💸）';
                }
                this.errorMessage = message;
                this.portraitHTML = renderMarkdown(this.portrait);
                if (this._renderTimer) {
                    clearTimeout(this._renderTimer);
                    this._renderTimer = null;
                }
            },

            /**
             * 中止 SSE 连接
             */
            _abortSSE: function() {
                if (this._abortController) {
                    this._abortController.abort();
                    this._abortController = null;
                }
                if (this._renderTimer) {
                    clearTimeout(this._renderTimer);
                    this._renderTimer = null;
                }
            },

            /**
             * 渲染高亮内容为 Markdown HTML
             * @param {string} text
             * @returns {string}
             */
            renderHighlightMD: function(text) {
                if (!text) return '';
                if (typeof window._alpineRenderMarkdown === 'function') {
                    return window._alpineRenderMarkdown(text);
                }
                return String(text).replace(/&/g, '&').replace(/</g, '<').replace(/>/g, '>');
            },

            /**
             * 显示 Toast 提示
             * @param {string} message
             * @param {boolean} success
             * @param {'error'|'success'|'info'} [type]
             */
            _showToast: function(message, success, type) {
                try {
                    var uiStore = Alpine.store('ui');
                    if (uiStore) {
                        if (type) {
                            uiStore.showToast(message, type, 2000);
                        } else {
                            uiStore.showToast(message, success ? 'success' : 'error', 2000);
                        }
                    }
                } catch(e) {
                    // fallback
                }
            },
        };
    });

});

// ============================================================
// onOpenUserTraits — 打开用户画像对话框（注册到 window，供 @click 调用）
// ============================================================
window.onOpenUserTraits = function() {
    try {
        var dialogEl = document.getElementById('portraitDialog');
        if (dialogEl) {
            var data = Alpine.$data(dialogEl);
            if (data && typeof data.open === 'function') {
                data.open();
                return;
            }
        }
        console.warn('用户画像对话框组件未找到或未初始化');
    } catch (e) {
        console.error('打开用户画像对话框失败:', e);
    }
};
