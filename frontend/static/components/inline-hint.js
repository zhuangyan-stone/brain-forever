// ============================================================
// components/inline-hint.js — 行内提示组件
// ============================================================
//
// 用途：
//   在最后一条消息下方插入临时的进度/状态提示，含旋转进度圈和文字，
//   适用于 API 请求前后的进度展示（如特征提取）。
//
// 使用方式：
//   import { InlineHint } from './components/inline-hint.js';
//
//   const hint = new InlineHint(container, {
//       texts: ['AI 正在思考……', '起居郎正在记录……'],
//       keepSeconds: 10,
//   });
//   hint.show();
//
//   // 请求成功后
//   hint.done(10);  // → "✓ 已生成10条特征"
//
//   // 请求失败后
//   hint.fail('请求超时');  // → "✗ 请求超时"
//
// ============================================================

'use strict';

/**
 * @typedef {Object} InlineHintOptions
 * @property {string} [text] - 固定提示文字（与 texts 二选一）
 * @property {string[]} [texts] - 候选提示文字数组（随机取一条，与 text 二选一）
 * @property {number} [keepSeconds=10] - 成功/失败后保留秒数，到期自动移除
 * @property {string} [successFormat='已记录 {n} 条个人特征'] - 成功提示格式，{n} 被替换为数量
 */

export class InlineHint {
    /**
     * @param {HTMLElement} container - 父容器（通常为 chatContainer）
     * @param {InlineHintOptions} [options]
     */
    constructor(container, options) {
        if (!container) throw new Error('InlineHint: container is required');

        /** @type {HTMLElement} */
        this.container = container;

        /** @type {InlineHintOptions} */
        this.options = Object.assign({
            text: '',
            texts: [],
            keepSeconds: 10,
            successFormat: ' 已记录 {n} 条个人特征',
        }, options || {});

        /** @type {HTMLElement|null} */
        this.el = null;

        /** @type {number|null} */
        this._removeTimer = null;

        // 绑定方法，确保定时器回调中 this 正确
        this.remove = this.remove.bind(this);
    }

    /**
     * 显示 pending 状态（旋转进度圈 + 提示文字）。
     * 如果已有实例显示，先移除再重新创建。
     */
    show() {
        // 如果已有实例，先清理
        this.remove();

        this.el = document.createElement('div');
        this.el.className = 'inline-hint';

        // 旋转进度圈
        const spinner = document.createElement('span');
        spinner.className = 'inline-hint__spinner';
        this.el.appendChild(spinner);

        // 文字
        const textEl = document.createElement('span');
        textEl.className = 'inline-hint__text';
        textEl.textContent = this._pickMessage();
        this.el.appendChild(textEl);

        this._appendToContainer();
    }

    /**
     * 切换到成功状态：✓ 已生成 N 条特征
     * @param {number} count - 生成的特征数量
     */
    done(count) {
        if (!this.el) return;

        // 替换 spinner 为成功图标
        const spinner = this.el.querySelector('.inline-hint__spinner');
        if (spinner) {
            const icon = document.createElement('span');
            icon.className = 'inline-hint__icon inline-hint__icon--success';
            icon.textContent = '\u2713'; // ✓
            spinner.replaceWith(icon);
        }

        // 更新文字
        const textEl = this.el.querySelector('.inline-hint__text');
        if (textEl) {
            if (typeof count === 'number' && count >= 0) {
                textEl.textContent = this.options.successFormat.replace('{n}', String(count));
            } else {
                textEl.textContent = '暂未发现新特征';
            }
        }

        // 启动自动移除定时器
        this._scheduleRemove();
    }

    /**
     * 切换到失败状态：✗ 错误信息
     * @param {string} [message='请求失败'] - 错误描述
     */
    fail(message) {
        if (!this.el) return;

        // 替换 spinner 为失败图标
        const spinner = this.el.querySelector('.inline-hint__spinner');
        if (spinner) {
            const icon = document.createElement('span');
            icon.className = 'inline-hint__icon inline-hint__icon--error';
            icon.textContent = '\u2717'; // ✗
            spinner.replaceWith(icon);
        }

        // 更新文字
        const textEl = this.el.querySelector('.inline-hint__text');
        if (textEl) {
            textEl.textContent = message || '请求失败';
        }

        // 启动自动移除定时器
        this._scheduleRemove();
    }

    /**
     * 从 DOM 中移除 hint。
     *
     * 实现原理：
     *   1. 锁定当前元素高度（内联 style）
     *   2. 触发浏览器 reflow
     *   3. 添加 fade-out class，触发 transition 同时收缩高度和透明度
     *   4. transition 结束后从 DOM 移除
     *
     * 效果：淡出的同时高度归零，下方内容平滑上移，而非仅仅消失。
     */
    remove() {
        if (this._removeTimer) {
            clearTimeout(this._removeTimer);
            this._removeTimer = null;
        }

        var el = this.el;
        if (!el) return;

        // 1. 锁定当前高度（box-sizing 默认为 content-box，这里用 offsetHeight）
        var h = el.offsetHeight;
        if (h > 0) {
            el.style.height = h + 'px';
            el.style.boxSizing = 'content-box';
        }

        // 2. 强制 reflow，使浏览器应用锁定高度
        void el.offsetHeight;

        // 3. 添加 class 触发 transition（高度 → 0，opacity → 0）
        el.classList.add('inline-hint--fade-out');

        // 4. transition 结束后移除 DOM
        var self = this;
        var removed = false;
        function doRemove() {
            if (removed) return;
            removed = true;
            el.removeEventListener('transitionend', doRemove);
            if (el.parentNode) {
                el.parentNode.removeChild(el);
            }
            self.el = null;
        }
        el.addEventListener('transitionend', doRemove);
        // 安全兜底：0.5s 后若 transitionend 仍未触发，强制移除
        setTimeout(doRemove, 500);
    }

    // ---- 内部方法 ----

    /**
     * 从 text 或 texts 中获取提示文字。
     * @returns {string}
     * @private
     */
    _pickMessage() {
        const texts = this.options.texts;
        if (texts && texts.length > 0) {
            const idx = Math.floor(Math.random() * texts.length);
            return texts[idx];
        }
        return this.options.text || '';
    }

    /**
     * 将元素插入到容器的末尾（最后一条消息下方）。
     * @private
     */
    _appendToContainer() {
        if (!this.el) return;
        this.container.appendChild(this.el);
    }

    /**
     * 启动自动移除定时器。
     * @private
     */
    _scheduleRemove() {
        const ms = (this.options.keepSeconds || 10) * 1000;
        this._removeTimer = setTimeout(this.remove, ms);
    }
}
