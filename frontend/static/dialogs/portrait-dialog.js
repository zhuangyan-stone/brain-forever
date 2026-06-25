// ============================================================
// dialogs/portrait-dialog.js — 用户"个人画像"生成对话框
// ============================================================
//
// 功能：
//   1. 遮罩层 + 大对话框（不点击遮罩自动关闭）
//   2. 流式 SSE 接收画像内容，实时渲染 Markdown
//   3. 复制按钮（纯文本/Markdown/HTML 三种格式）
//   4. 分享按钮（占位）
//   5. 取消（流式中）/ 关闭（完成后）按钮
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
     * 使用 chat-markdown.js 中注册的全局渲染器
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

    // ============================================================
    // userPortraitDialog — Alpine 组件
    // ============================================================
    Alpine.data('userPortraitDialog', function() {
        return {
            // ---- 状态 ----
            show: false,
            portrait: '',           // 完整画像 Markdown 原文
            portraitHTML: '',       // 渲染后的 HTML
            portraitMeta: null,     // 结构化元数据 {core_traits, key_highlights}
            isStreaming: false,     // 是否正在流式接收
            isDone: false,          // 是否已完成
            hasError: false,
            errorMessage: '',
            userName: '',           // 用户昵称
            userAvatar: '',         // 用户头像 URL

            // 节流渲染
            _renderTimer: null,
            // SSE AbortController
            _abortController: null,
            // 安全地引用 $el（通过 init 钩子设置）
            _el: null,

            // 复制下拉菜单状态
            _copyMenuEl: null,
            _copyMenuAnchor: null,

            // ---- 计算属性 ----
            get title() {
                return (this.userName || '匿名用户') + ' 个人画像';
            },

            get showCancel() {
                return this.isStreaming && !this.isDone;
            },

            get showClose() {
                return !this.isStreaming || this.isDone;
            },

            // ---- 方法 ----

            /**
             * 打开画像对话框
             */
            open: function() {
                var self = this;

                // 重置状态
                this.portrait = '';
                this.portraitHTML = '';
                this.portraitMeta = null;
                this.isStreaming = true;
                this.isDone = false;
                this.hasError = false;
                this.errorMessage = '';
                this.show = true;

                // 从 Alpine store 获取用户信息
                try {
                    var chats = Alpine.store('chats');
                    if (chats) {
                        this.userName = chats.currentUserNo || '匿名用户';
                        this.userAvatar = chats.currentUserAvatar || '/static/img/avatar/anonymous.png';
                    }
                } catch(e) {
                    this.userName = '匿名用户';
                    this.userAvatar = '/static/img/avatar/anonymous.png';
                }

                // 使用 $nextTick 确保 DOM 已更新后再发起请求
                this.$nextTick(function() {
                    self._startFetch();
                });
            },

            /**
             * 关闭对话框
             */
            close: function() {
                this._abortSSE();
                this._closeCopyMenu();
                this.show = false;
                this.portrait = '';
                this.portraitHTML = '';
                this.portraitMeta = null;
                this.isStreaming = false;
                this.isDone = false;
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
             * 显示复制下拉菜单
             * @param {Event} event
             */
            showCopyMenu: function(event) {
                var self = this;
                var anchor = event.currentTarget;

                // 关闭已有的菜单
                this._closeCopyMenu();

                this._copyMenuAnchor = anchor;
                var rect = anchor.getBoundingClientRect();
                var menu = document.createElement('div');
                menu.className = 'portrait-copy-dropdown';
                menu.style.top = (rect.bottom + 4) + 'px';
                menu.style.left = rect.left + 'px';

                var items = [
                    { label: '复制为纯文本', format: 'plain' },
                    { label: '复制为 Markdown', format: 'markdown' },
                    { label: '复制为 HTML', format: 'html' },
                ];

                items.forEach(function(item) {
                    var option = document.createElement('div');
                    option.className = 'portrait-copy-dropdown-item';
                    option.textContent = item.label;
                    option.addEventListener('click', function(e) {
                        e.stopPropagation();
                        self._copyContent(item.format);
                        self._closeCopyMenu();
                    });
                    menu.appendChild(option);
                });

                document.body.appendChild(menu);
                this._copyMenuEl = menu;

                // 点击外部关闭菜单
                setTimeout(function() {
                    var closeHandler = function(e) {
                        if (!menu.contains(e.target) && e.target !== anchor) {
                            self._closeCopyMenu();
                            document.removeEventListener('click', closeHandler, true);
                        }
                    };
                    document.addEventListener('click', closeHandler, true);
                }, 0);
            },

            /**
             * 关闭复制下拉菜单
             */
            _closeCopyMenu: function() {
                if (this._copyMenuEl) {
                    this._copyMenuEl.remove();
                    this._copyMenuEl = null;
                }
                this._copyMenuAnchor = null;
            },

            /**
             * 执行复制操作
             * @param {'plain'|'markdown'|'html'} format
             */
            _copyContent: function(format) {
                if (!this.portrait) return;

                var self = this;
                var formatName = { plain: '纯文本', markdown: 'Markdown', html: 'HTML' }[format] || 'Markdown';
                var text = this.portrait;

                if (format === 'plain') {
                    // 从 HTML 提取纯文本
                    var tempDiv = document.createElement('div');
                    tempDiv.innerHTML = this.portraitHTML;
                    text = tempDiv.textContent || tempDiv.innerText || this.portrait;
                    copyPlainText(text).then(function(ok) {
                        self._showToast(ok ? '✓ 已复制（纯文本）' : '复制失败（纯文本）', ok);
                    });
                } else if (format === 'html') {
                    this._copyHtml(this.portraitHTML).then(function(ok) {
                        self._showToast(ok ? '✓ 已复制（HTML）' : '复制失败（HTML）', ok);
                    });
                } else {
                    // markdown - 复制原文
                    copyPlainText(this.portrait).then(function(ok) {
                        self._showToast(ok ? '✓ 已复制（Markdown）' : '复制失败（Markdown）', ok);
                    });
                }
            },

            /**
             * 复制 HTML 富文本
             * @param {string} html
             * @returns {Promise<boolean>}
             */
            _copyHtml: function(html) {
                if (!html) return Promise.resolve(false);
                var plainText = '';
                var tempDiv = document.createElement('div');
                tempDiv.innerHTML = html;
                plainText = tempDiv.textContent || '';

                var wrapped = '<!DOCTYPE html>\n<html>\n<head>\n<meta charset="utf-8">\n</head>\n<body>\n' + html + '\n</body>\n</html>';

                if (!navigator.clipboard || !navigator.clipboard.write) {
                    return Promise.resolve(fallbackCopyText(plainText));
                }

                try {
                    return navigator.clipboard.write([
                        new ClipboardItem({
                            'text/plain': new Blob([plainText], { type: 'text/plain' }),
                            'text/html': new Blob([wrapped], { type: 'text/html' }),
                        })
                    ]).then(function() { return true; })
                    .catch(function() { return fallbackCopyText(plainText); });
                } catch(e) {
                    return Promise.resolve(fallbackCopyText(plainText));
                }
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
                }).then(function(response) {
                    if (!response.ok) {
                        return response.json().then(function(data) {
                            throw new Error(data.error || '请求失败 (' + response.status + ')');
                        }).catch(function(e) {
                            if (e instanceof SyntaxError) {
                                throw new Error('画像服务暂时不可用 (' + response.status + ')');
                            }
                            throw e;
                        });
                    }

                    // 读取 SSE 流
                    var reader = response.body.getReader();
                    var decoder = new TextDecoder();
                    var buffer = '';

                    function read() {
                        reader.read().then(function(result) {
                            if (result.done) {
                                self._onStreamDone();
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
                    case 'text':
                        // 累加文本内容
                        this.portrait += (typeof data === 'string' ? data : '');
                        this._throttleRender();
                        break;

                    case 'meta':
                        // 结构化元数据：核心特质 + 重点摘要
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
             * 流完成回调
             */
            _onStreamDone: function() {
                this.isStreaming = false;
                this.isDone = true;
                // 最终渲染
                this.portraitHTML = renderMarkdown(this.portrait);
                if (this._renderTimer) {
                    clearTimeout(this._renderTimer);
                    this._renderTimer = null;
                }
            },

            /**
             * 流错误回调
             */
            _onStreamError: function(message) {
                this.isStreaming = false;
                this.hasError = true;
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
