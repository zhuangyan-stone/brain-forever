// ============================================================
// chat-ticknav.js — 刻度导航
// ============================================================

import { state } from './chat-state.js';
import { truncate } from './toolsets.js';

'use strict';

// 获取 chatContainer 的辅助函数（避免循环依赖）
function getChatContainer() {
    return document.getElementById('chatContainer');
}

/**
 * updateTickNav 根据当前用户消息更新右侧刻度导航
 */
export function updateTickNav() {
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;

    // 收集所有用户消息元素
    const chatContainer = getChatContainer();
    if (!chatContainer) return;
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;

    // 清空并重建刻度
    tickNav.innerHTML = '';

    // 根据偏移量计算当前显示的刻度范围
    const startIdx = state.tickScrollOffset;
    const endIdx = Math.min(startIdx + state.MAX_VISIBLE_TICKS, tickCount);

    // 判断是否还有更多刻度
    const hasPrev = state.tickScrollOffset > 0;
    const hasNext = endIdx < tickCount;

    // 是否需要滚动（超过 MAX_VISIBLE_TICKS 条）
    const needsScroll = tickCount > state.MAX_VISIBLE_TICKS;

    // 顶部箭头 — 仅当需要滚动且还有上方刻度时显示
    if (needsScroll) {
        const topArrow = document.createElement('div');
        topArrow.className = 'tick-arrow' + (hasPrev ? '' : ' tick-arrow-disabled');
        topArrow.textContent = '▲';
        topArrow.title = '向上翻动';
        topArrow.addEventListener('click', (e) => {
            e.stopPropagation();
            if (!hasPrev) return;
            state.tickScrollOffset--;
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

        // 刻度线（短横线）— 始终在条目右侧（flex-direction: row-reverse）
        const dot = document.createElement('span');
        dot.className = 'tick-dot';
        tick.appendChild(dot);

        // 刻度索引值 — 在非 hover 状态下也可见，位于刻度线左侧
        // 每跳一个标一个（第1,3,5,7,9个），上下对称
        const relPos = i - startIdx; // 当前刻度在可见范围内的相对位置（0-based）
        const idxSpan = document.createElement('span');
        idxSpan.className = 'tick-index';
        if (relPos % 2 === 0) {
            idxSpan.textContent = String(i + 1).padStart(3, '0');
        }
        tick.appendChild(idxSpan);

        // 标题文本（带绝对序号，从1开始，固定三位补0）— hover 时显示在面板最左侧
        const label = document.createElement('span');
        label.className = 'tick-label';
        const seqNum = String(i + 1).padStart(3, '0');
        label.textContent = seqNum + '. ' + title;
        tick.appendChild(label);

        // 点击刻度滚动到对应消息，并设为活动条目
        tick.addEventListener('click', () => {
            const chatContainer = getChatContainer();
            if (!chatContainer) return;
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
            state.tickScrollOffset++;
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
    if (state.activeTickIndex >= 0) {
        setActiveTick(state.activeTickIndex);
    }
}

/**
 * setActiveTick 设置指定索引的刻度为活动状态
 * @param {number} index
 */
export function setActiveTick(index) {
    state.activeTickIndex = index;
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;
    const ticks = tickNav.querySelectorAll('.tick');
    ticks.forEach((t) => {
        t.classList.toggle('active', parseInt(t.dataset.tickIndex, 10) === index);
    });
}

/**
 * updateActiveTickOnScroll 根据当前视口中的用户消息更新活动刻度
 */
function updateActiveTickOnScroll() {
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;

    // 面板展开时忽略滚动
    if (tickNav.matches(':hover')) return;

    const chatContainer = getChatContainer();
    if (!chatContainer) return;
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
    if (state.activeTickIndex >= 0 && state.activeTickIndex < userMessages.length) {
        const activeRect = userMessages[state.activeTickIndex].getBoundingClientRect();
        if (activeRect.top < containerTop && state.activeTickIndex < userMessages.length - 1) {
            // 当前消息已滚出顶部，且后面还有消息 → 跳到 targetIdx
            if (targetIdx !== state.activeTickIndex) {
                state.activeTickIndex = targetIdx;
                adjustTickOffset();
                updateTickNav();
            }
        } else if (activeRect.top >= containerTop) {
            // 当前消息仍然可见，更新为更精确的 targetIdx
            if (targetIdx !== state.activeTickIndex) {
                state.activeTickIndex = targetIdx;
                adjustTickOffset();
                updateTickNav();
            }
        }
        // 否则（已滚出顶部但无下一组）：保持 activeTickIndex 不变
    } else {
        // 没有活动消息，直接用 targetIdx
        if (targetIdx !== state.activeTickIndex) {
            state.activeTickIndex = targetIdx;
            adjustTickOffset();
            updateTickNav();
        }
    }
}

/**
 * adjustTickOffset 调整 tickScrollOffset 确保活动刻度可见
 */
function adjustTickOffset() {
    const chatContainer = getChatContainer();
    if (!chatContainer) return;
    const userMessages = chatContainer.querySelectorAll('.message.user');
    const tickCount = userMessages.length;
    if (tickCount > state.MAX_VISIBLE_TICKS) {
        if (state.activeTickIndex < state.tickScrollOffset) {
            state.tickScrollOffset = state.activeTickIndex;
        } else if (state.activeTickIndex >= state.tickScrollOffset + state.MAX_VISIBLE_TICKS) {
            state.tickScrollOffset = state.activeTickIndex - state.MAX_VISIBLE_TICKS + 1;
        }
    }
}

/**
 * 初始化刻度导航的事件绑定
 */
export function initTickNav() {
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;

    // 刻度面板鼠标滚轮滚动 — 改变偏移量，翻动显示的刻度
    tickNav.addEventListener('wheel', (e) => {
        e.preventDefault();
        const chatContainer = getChatContainer();
        if (!chatContainer) return;
        const userMessages = chatContainer.querySelectorAll('.message.user');
        const tickCount = userMessages.length;
        if (tickCount <= state.MAX_VISIBLE_TICKS) return; // 不需要滚动

        const delta = e.deltaY > 0 ? 1 : -1;
        const newOffset = state.tickScrollOffset + delta;
        // 限制范围：0 到 (tickCount - MAX_VISIBLE_TICKS)
        const maxOffset = tickCount - state.MAX_VISIBLE_TICKS;
        if (newOffset < 0 || newOffset > maxOffset) return;

        state.tickScrollOffset = newOffset;
        updateTickNav();
    });

    // 面板消失时清除活动状态
    tickNav.addEventListener('mouseleave', () => {
        if (state.activeTickIndex >= 0) {
            setActiveTick(-1);
        }
    });

    // 节流包装，避免频繁触发
    let scrollThrottleTimer = null;
    const chatContainer = getChatContainer();
    if (!chatContainer) return;
    chatContainer.addEventListener('scroll', () => {
        if (scrollThrottleTimer) return;
        scrollThrottleTimer = setTimeout(() => {
            scrollThrottleTimer = null;
            updateActiveTickOnScroll();
        }, 150);
    });
}
