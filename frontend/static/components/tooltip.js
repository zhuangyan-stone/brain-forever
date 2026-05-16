// ============================================================
// tooltip.js — 自定义 Tooltip 组件
// 替代浏览器原生 title 属性，绕过 Edge on Linux 渲染 bug
// ============================================================

'use strict';

let tooltipEl = null;
let showTimer = null;
let currentTarget = null;

/**
 * 初始化 Tooltip 组件。
 * 使用事件委托监听所有带 [data-tooltip] 属性的元素。
 * 在页面加载后调用一次即可。
 */
export function initTooltip() {
    document.addEventListener('mouseover', onMouseOver);
    document.addEventListener('mouseout', onMouseOut);
    document.addEventListener('click', onAnyClick);
    // 捕获阶段监听滚动，确保能捕获到任意容器的滚动
    window.addEventListener('scroll', hideTooltip, true);
}

/**
 * 销毁 Tooltip 组件，移除事件监听。
 */
export function destroyTooltip() {
    document.removeEventListener('mouseover', onMouseOver);
    document.removeEventListener('mouseout', onMouseOut);
    document.removeEventListener('click', onAnyClick);
    window.removeEventListener('scroll', hideTooltip, true);
    clearTimeout(showTimer);
    hideTooltip();
    currentTarget = null;
}

function onMouseOver(e) {
    const target = e.target.closest('[data-tooltip]');
    if (!target) return;
    if (target === currentTarget) return;

    currentTarget = target;
    clearTimeout(showTimer);
    showTimer = setTimeout(() => showTooltip(target), 300);
}

function onMouseOut(e) {
    const target = e.target.closest('[data-tooltip]');
    if (!target) return;

    clearTimeout(showTimer);
    hideTooltip();
    currentTarget = null;
}

function onAnyClick() {
    hideTooltip();
    currentTarget = null;
}

function showTooltip(target) {
    hideTooltip(); // 移除旧 tooltip

    const text = target.getAttribute('data-tooltip');
    if (!text) return;

    tooltipEl = document.createElement('div');
    tooltipEl.className = 'tooltip';
    tooltipEl.textContent = text;
    document.body.appendChild(tooltipEl);

    // 定位
    positionTooltip(target);

    // 触发显示动画
    requestAnimationFrame(() => {
        if (tooltipEl) {
            tooltipEl.classList.add('visible');
        }
    });
}

function positionTooltip(target) {
    if (!tooltipEl) return;

    const rect = target.getBoundingClientRect();
    const tipRect = tooltipEl.getBoundingClientRect();
    const gap = 6;

    // 默认在上方
    let top = rect.top - tipRect.height - gap;
    let left = rect.left + (rect.width - tipRect.width) / 2;

    // 上方空间不足，放到下方
    if (top < 4) {
        top = rect.bottom + gap;
    }

    // 水平不超出视口
    if (left < 4) left = 4;
    if (left + tipRect.width > window.innerWidth - 4) {
        left = window.innerWidth - tipRect.width - 4;
    }

    tooltipEl.style.top = top + 'px';
    tooltipEl.style.left = left + 'px';
}

export function hideTooltip() {
    if (tooltipEl) {
        tooltipEl.remove();
        tooltipEl = null;
    }
}
