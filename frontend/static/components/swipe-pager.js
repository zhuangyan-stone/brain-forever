// ============================================================
// swipe-pager.js — 通用触摸滑动翻页组件
// ============================================================
// 封装三栏布局 + 触摸事件 + 圆点导航，通过 renderPage 回调
// 解耦内容渲染，方便未来其他场景复用。
//
// 用法：
//   import { SwipePager } from './components/swipe-pager.js';
//   const pager = new SwipePager(container, {
//       totalPages: 5,
//       renderPage: (pane, pageIndex) => { pane.innerHTML = ... },
//       onPageChange: (page) => { ... },
//   });
//   pager.mount(0);
// ============================================================

'use strict';

/**
 * @typedef {object} SwipePagerOptions
 * @property {number} totalPages - 总页数
 * @property {function(HTMLElement, number|null): void} renderPage - 渲染单页内容的回调
 *           pane: 要填充的 pane 元素（左/中/右栏）
 *           pageIndex: 页码（null 表示空栏，如首页的左栏、末页的右栏）
 * @property {function(number): void} [onPageChange] - 翻页完成后的回调
 * @property {number} [swipeThreshold=0.15] - 翻页阈值（占容器宽度的比例）
 * @property {number} [animationDuration=250] - 翻页动画时长（毫秒）
 * @property {number} [bounceDuration=200] - 回弹动画时长（毫秒）
 * @property {boolean} [showDots=true] - 是否显示底部圆点导航
 * @property {string} [containerClass='swipe-container'] - 容器 CSS 类名
 * @property {string} [sliderClass='swipe-slider'] - slider CSS 类名
 * @property {string} [paneClass='swipe-pane'] - pane CSS 类名
 * @property {string} [dotsClass='swipe-pagination-dots'] - 圆点导航 CSS 类名
 * @property {string} [dotClass='swipe-dot'] - 圆点 CSS 类名
 */

export class SwipePager {
    /**
     * @param {HTMLElement} container - 放置 slider 的容器元素
     * @param {SwipePagerOptions} options
     */
    constructor(container, options) {
        if (!container) throw new Error('SwipePager: container is required');
        if (!options || typeof options.renderPage !== 'function') {
            throw new Error('SwipePager: options.renderPage is required');
        }

        /** @type {HTMLElement} */
        this._container = container;

        /** @type {SwipePagerOptions} */
        this._options = {
            totalPages: options.totalPages || 0,
            renderPage: options.renderPage,
            onPageChange: options.onPageChange || null,
            swipeThreshold: options.swipeThreshold ?? 0.15,
            animationDuration: options.animationDuration ?? 250,
            bounceDuration: options.bounceDuration ?? 200,
            showDots: options.showDots !== false,
            containerClass: options.containerClass || 'swipe-container',
            sliderClass: options.sliderClass || 'swipe-slider',
            paneClass: options.paneClass || 'swipe-pane',
            dotsClass: options.dotsClass || 'swipe-pagination-dots',
            dotClass: options.dotClass || 'swipe-dot',
        };

        // ---- 内部状态 ----
        /** @type {{ page: number, total: number, isAnimating: boolean, isSwiping: boolean, touchStartX: number }} */
        this._state = {
            page: 0,
            total: this._options.totalPages,
            isAnimating: false,
            isSwiping: false,
            touchStartX: 0,
        };

        // ---- DOM 引用（mount 时填充） ----
        /** @type {HTMLElement|null} */
        this._slider = null;
        /** @type {{ left: HTMLElement, center: HTMLElement, right: HTMLElement }} */
        this._panes = { left: null, center: null, right: null };
        /** @type {HTMLElement|null} */
        this._dotsNav = null;

        // ---- 绑定的事件处理器引用（用于 destroy 时解绑） ----
        this._boundHandlers = {
            touchstart: this._onTouchStart.bind(this),
            touchmove: this._onTouchMove.bind(this),
            touchend: this._onTouchEnd.bind(this),
            dotsClick: this._onDotsClick.bind(this),
        };
    }

    // ============================================================
    // 公共 API
    // ============================================================

    /**
     * 挂载组件：创建 DOM、绑定事件、渲染初始页
     * @param {number} [initialPage=0]
     */
    mount(initialPage = 0) {
        this._buildDOM();
        this._bindEvents();
        this.renderPage(initialPage);
    }

    /**
     * 跳转到指定页（带动画）
     * @param {number} page
     */
    slideTo(page) {
        if (page === this._state.page) return;
        if (this._state.isAnimating) return;
        if (page < 0 || page >= this._state.total) return;

        this._state.isAnimating = true;
        const goingForward = page > this._state.page;

        // 预渲染三栏：左=page-1，中=page，右=page+1
        this._fillTriple(page);

        if (goingForward) {
            // ---- 向后翻（下一页） ----
            // 先把 slider 定位到显示左栏（=currentPage）
            this._slider.style.transition = 'none';
            this._slider.style.transform = 'translateX(0)';
            void this._slider.offsetHeight; // 强制回流
            // 动画到中栏（=page）
            this._slider.style.transition = `transform ${this._options.animationDuration}ms ease-out`;
            this._slider.style.transform = 'translateX(-33.33%)';
        } else {
            // ---- 向前翻（上一页） ----
            // 先把 slider 定位到显示右栏（=currentPage）
            this._slider.style.transition = 'none';
            this._slider.style.transform = 'translateX(-66.66%)';
            void this._slider.offsetHeight; // 强制回流
            // 动画到中栏（=page）
            this._slider.style.transition = `transform ${this._options.animationDuration}ms ease-out`;
            this._slider.style.transform = 'translateX(-33.33%)';
        }

        // 动画结束后更新状态
        setTimeout(() => {
            this._state.page = page;
            this._fillTriple(page);
            this._resetSlider();
            this._updateDots();
            this._state.isAnimating = false;
            if (this._options.onPageChange) {
                this._options.onPageChange(page);
            }
        }, this._options.animationDuration + 30);
    }

    /**
     * 直接渲染指定页（无动画）
     * @param {number} page
     */
    renderPage(page) {
        if (page < 0 || page >= this._state.total) return;
        this._state.page = page;
        this._fillTriple(page);
        this._resetSlider();
        this._updateDots();
    }

    /**
     * 更新总页数（数据变化时调用）
     * @param {number} totalPages
     * @param {number} [currentPage] - 可选的当前页，默认保持当前页不变
     */
    updateTotal(totalPages, currentPage) {
        this._state.total = totalPages;
        const newPage = (currentPage !== undefined) ? currentPage : Math.min(this._state.page, totalPages - 1);
        this.renderPage(Math.max(0, newPage));
    }

    /**
     * 销毁组件：解绑事件、清理 DOM
     */
    destroy() {
        this._unbindEvents();
        this._container.innerHTML = '';
        this._slider = null;
        this._panes = { left: null, center: null, right: null };
        this._dotsNav = null;
    }

    /** 获取当前页码 */
    get currentPage() {
        return this._state.page;
    }

    /** 获取总页数 */
    get totalPages() {
        return this._state.total;
    }

    // ============================================================
    // DOM 构建
    // ============================================================

    /**
     * 构建 slider DOM 结构
     */
    _buildDOM() {
        // 清空容器
        this._container.innerHTML = '';

        // 添加容器类（如果尚未有）
        if (!this._container.classList.contains(this._options.containerClass)) {
            this._container.classList.add(this._options.containerClass);
        }

        // slider
        this._slider = document.createElement('div');
        this._slider.className = this._options.sliderClass;
        this._container.appendChild(this._slider);

        // 三栏
        const paneLeft = document.createElement('div');
        paneLeft.className = this._options.paneClass;
        this._slider.appendChild(paneLeft);

        const paneCenter = document.createElement('div');
        paneCenter.className = this._options.paneClass;
        this._slider.appendChild(paneCenter);

        const paneRight = document.createElement('div');
        paneRight.className = this._options.paneClass;
        this._slider.appendChild(paneRight);

        this._panes = { left: paneLeft, center: paneCenter, right: paneRight };

        // 圆点导航
        if (this._options.showDots) {
            this._dotsNav = document.createElement('div');
            this._dotsNav.className = this._options.dotsClass;
            this._container.appendChild(this._dotsNav);
        }
    }

    // ============================================================
    // 事件绑定 / 解绑
    // ============================================================

    _bindEvents() {
        this._container.addEventListener('touchstart', this._boundHandlers.touchstart, { passive: true });
        this._container.addEventListener('touchmove', this._boundHandlers.touchmove, { passive: true });
        this._container.addEventListener('touchend', this._boundHandlers.touchend, { passive: true });

        if (this._dotsNav) {
            this._dotsNav.addEventListener('click', this._boundHandlers.dotsClick);
        }
    }

    _unbindEvents() {
        this._container.removeEventListener('touchstart', this._boundHandlers.touchstart);
        this._container.removeEventListener('touchmove', this._boundHandlers.touchmove);
        this._container.removeEventListener('touchend', this._boundHandlers.touchend);

        if (this._dotsNav) {
            this._dotsNav.removeEventListener('click', this._boundHandlers.dotsClick);
        }
    }

    // ============================================================
    // 触摸事件处理
    // ============================================================

    /**
     * touchstart — 记录起始位置
     */
    _onTouchStart(e) {
        if (this._state.isAnimating) return;
        this._state.touchStartX = e.changedTouches[0].screenX;
        this._state.isSwiping = false;
    }

    /**
     * touchmove — 手指跟随
     *
     * 三栏布局下，translateX 百分比相对于 slider 自身宽度（300% 容器宽度）
     * translateX(p%) 移动距离 = p/100 * slider宽度 = p/100 * 容器宽度 * 3
     * 要跟随手指移动 dx 像素：dx = p/100 * containerWidth * 3 → p = dx/containerWidth * 33.33
     *
     * 基准位置：translateX(-33.33%) → 显示中栏（当前页）
     * 左滑（下一页）：从 -33.33% 向 -66.66% 移动
     * 右滑（上一页）：从 -33.33% 向 0 移动
     */
    _onTouchMove(e) {
        if (this._state.isAnimating) return;
        const dx = e.changedTouches[0].screenX - this._state.touchStartX;
        if (Math.abs(dx) > 3) {
            this._state.isSwiping = true;
        }
        if (!this._state.isSwiping) return;

        const containerWidth = this._container.offsetWidth;
        if (containerWidth === 0) return;

        if (dx < 0 && this._state.page < this._state.total - 1) {
            // ---- 左滑（下一页）：从 -33.33% 向 -66.66% 移动 ----
            // 偏移范围 -33.33% ~ -60%（留余量给 touchend 的 -66.66%）
            const offset = Math.max(-33.33 + dx / containerWidth * 33.33, -60);
            this._slider.style.transition = 'none';
            this._slider.style.transform = `translateX(${offset}%)`;
        } else if (dx > 0 && this._state.page > 0) {
            // ---- 右滑（上一页）：从 -33.33% 向 0 移动 ----
            // 偏移范围 -33.33% ~ -5%（留余量给 touchend 的 0）
            const offset = Math.min(-33.33 + dx / containerWidth * 33.33, -5);
            this._slider.style.transition = 'none';
            this._slider.style.transform = `translateX(${offset}%)`;
        }
    }

    /**
     * touchend — 翻页判定 / 回弹
     */
    _onTouchEnd(e) {
        if (!this._state.isSwiping || this._state.isAnimating) return;
        this._state.isAnimating = true;

        const dx = e.changedTouches[0].screenX - this._state.touchStartX;
        const threshold = this._container.offsetWidth * this._options.swipeThreshold;

        if (dx < -threshold && this._state.page < this._state.total - 1) {
            // ---- 左滑翻到下一页 ----
            this._slider.style.transition = `transform ${this._options.animationDuration}ms ease-out`;
            this._slider.style.transform = 'translateX(-66.66%)';
            setTimeout(() => {
                this._state.page = this._state.page + 1;
                this._fillTriple(this._state.page);
                this._resetSlider();
                this._updateDots();
                this._state.isAnimating = false;
                if (this._options.onPageChange) {
                    this._options.onPageChange(this._state.page);
                }
            }, this._options.animationDuration + 30);
        } else if (dx > threshold && this._state.page > 0) {
            // ---- 右滑翻到上一页 ----
            this._slider.style.transition = `transform ${this._options.animationDuration}ms ease-out`;
            this._slider.style.transform = 'translateX(0)';
            setTimeout(() => {
                this._state.page = this._state.page - 1;
                this._fillTriple(this._state.page);
                this._resetSlider();
                this._updateDots();
                this._state.isAnimating = false;
                if (this._options.onPageChange) {
                    this._options.onPageChange(this._state.page);
                }
            }, this._options.animationDuration + 30);
        } else {
            // 未超过阈值 → 回弹到中栏
            this._slider.style.transition = `transform ${this._options.bounceDuration}ms ease-out`;
            this._slider.style.transform = 'translateX(-33.33%)';
            setTimeout(() => {
                this._state.isAnimating = false;
            }, this._options.bounceDuration + 30);
        }
    }

    /**
     * 点击圆点导航
     */
    _onDotsClick(e) {
        const dot = e.target.closest('.' + this._options.dotClass);
        if (!dot) return;
        const page = parseInt(dot.dataset.page, 10);
        if (!isNaN(page)) {
            this.slideTo(page);
        }
    }

    // ============================================================
    // 内部工具方法
    // ============================================================

    /**
     * 填充三栏：左=page-1，中=page，右=page+1
     * 自动处理边界（null = 空栏）
     * @param {number} page
     */
    _fillTriple(page) {
        const prev = page > 0 ? page - 1 : null;
        const next = page < this._state.total - 1 ? page + 1 : null;

        this._options.renderPage(this._panes.left, prev);
        this._options.renderPage(this._panes.center, page);
        this._options.renderPage(this._panes.right, next);
    }

    /**
     * 重置 slider 到基准位置（显示中栏）
     */
    _resetSlider() {
        this._slider.style.transition = 'none';
        this._slider.style.transform = 'translateX(-33.33%)';
    }

    /**
     * 更新圆点导航状态
     */
    _updateDots() {
        if (!this._dotsNav) return;
        this._dotsNav.innerHTML = '';
        if (this._state.total > 1) {
            for (let i = 0; i < this._state.total; i++) {
                const dot = document.createElement('span');
                dot.className = this._options.dotClass + (i === this._state.page ? ' active' : '');
                dot.dataset.page = i;
                this._dotsNav.appendChild(dot);
            }
        }
    }
}
