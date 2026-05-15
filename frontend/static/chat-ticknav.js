// ============================================================
// chat-ticknav.js — 刻度导航
// ============================================================

import { state } from './chat-state.js';
import { truncate } from './toolsets.js';

'use strict';

// 获取实际滚动容器的辅助函数（避免循环依赖）
// 注意：布局重构后，实际滚动的容器是 #scrollContainer（.scroll-container），
// 而非 #chatContainer（.chat-container 的 overflow-y: visible，不触发 scroll 事件）。
function getScrollContainer() {
    return document.getElementById('scrollContainer');
}

/**
 * updateTickNav 根据当前用户消息更新右侧刻度导航
 */
export function updateTickNav() {
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;

    // 面板锁定期间（用户点击刻度后的平滑滚动期间）：
    // 允许 DOM 重建以支持面板翻页（滚轮、箭头），
    // 因为锁定展开态下刻度值被 CSS 隐藏（display: none），不会闪烁。
    // 但被动路径（SSE 完成、删除消息等）触发的重建也不会导致可见闪烁，
    // 因为刻度值同样被 CSS 隐藏。

    // 收集所有用户消息元素（用户消息在 #chatContainer 内）
    const chatContainer = document.getElementById('chatContainer');
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
        topArrow.className = 'tick-arrow tick-arrow-up' + (hasPrev ? '' : ' tick-arrow-disabled');
        topArrow.title = '向上翻动';
        topArrow.addEventListener('click', (e) => {
            e.stopPropagation();
            if (state.tickScrollOffset <= 0) return;
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
        // 以当前活动刻度为中心，按距当前刻度的距离跳格显示：
        // dist=0（当前刻度）总是显示，dist=1 隐藏，dist=2 显示，dist=3 隐藏……
        // 注意：此处不设置 data-dist 和刻度值文本，由后续 setActiveTick 统一处理
        const idxSpan = document.createElement('span');
        idxSpan.className = 'tick-index';
        tick.appendChild(idxSpan);

        // 标题文本（带绝对序号，从1开始，固定三位补0）— hover 时显示在面板最左侧
        const label = document.createElement('span');
        label.className = 'tick-label';
        const seqNum = String(i + 1).padStart(2, '0');
        label.textContent = seqNum + '. ' + title;
        tick.appendChild(label);

        // 点击刻度滚动到对应消息，并设为活动条目
        tick.addEventListener('click', () => {
            const scrollContainer = getScrollContainer();
            if (!scrollContainer) return;
            const targetMsg = scrollContainer.querySelector(`.message.user[data-msg-index="${i}"]`);
            if (targetMsg) {
                // 立即更新面板活动状态（高亮、dist、刻度值），让用户获得即时视觉反馈
                setActiveTick(i);
                // 记录目标索引，锁定面板保持展开态，利用展开态 CSS 隐藏刻度值，
                // 避免滚动过程中刻度值频繁显/隐闪烁
                state.targetTickIndex = i;
                tickNav.classList.add('tick-nav-locked');
                targetMsg.scrollIntoView({ behavior: 'smooth', block: 'start' });
                // 面板解锁由 updateActiveTickOnScroll 在检测到目标消息进入视口时自动完成，
                // 不再使用固定延时，以支持任意距离的平滑滚动
                const bubble = targetMsg.querySelector('.bubble');
                if (bubble) {
                    bubble.classList.remove('highlight');
                }
            }
        });

        tickNav.appendChild(tick);
    }

    // 底部箭头 — 仅当需要滚动且还有下方刻度时显示
    if (needsScroll) {
        const bottomArrow = document.createElement('div');
        bottomArrow.className = 'tick-arrow tick-arrow-down' + (hasNext ? '' : ' tick-arrow-disabled');
        bottomArrow.title = '向下翻动';
        bottomArrow.addEventListener('click', (e) => {
            e.stopPropagation();
            const chatContainer = document.getElementById('chatContainer');
            if (!chatContainer) return;
            const maxOffset = chatContainer.querySelectorAll('.message.user').length - state.MAX_VISIBLE_TICKS;
            if (state.tickScrollOffset >= maxOffset) return;
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
        const tickIdx = parseInt(t.dataset.tickIndex, 10);
        const isActive = tickIdx === index;
        t.classList.toggle('active', isActive);
        const dist = Math.abs(tickIdx - index);
        // 更新距当前活动刻度的距离，用于渐进式透明度
        t.dataset.dist = dist;

        // 同步更新刻度序号：以当前活动刻度为中心，按距当前刻度的距离跳格显示
        const idxSpan = t.querySelector('.tick-index');
        if (idxSpan) {
            if (dist % 2 === 0) {
                idxSpan.textContent = String(tickIdx + 1).padStart(2, '0');
            } else {
                idxSpan.textContent = '';
            }
        }
    });
}

/**
 * updateActiveTickOnScroll 根据当前视口中的用户消息更新活动刻度
 *
 * 注意：使用 getScrollContainer() 获取实际滚动的容器（#scrollContainer），
 * 而非 #chatContainer（其 overflow-y: visible，不触发 scroll 事件）。
 */
function updateActiveTickOnScroll() {
    const tickNav = document.getElementById('tickNav');
    if (!tickNav) return;

    const scrollContainer = getScrollContainer();
    if (!scrollContainer) return;
    const userMessages = scrollContainer.querySelectorAll('.message.user');
    if (userMessages.length === 0) return;

    // 面板被锁定（用户点击刻度后的平滑滚动期间）
    // 注意：此检测必须在 :hover 检查之前，因为锁定状态下鼠标可能停留在面板上，
    // 如果先检查 :hover 会直接 return，导致锁定检测永远不会执行，面板无法解锁。
    if (tickNav.classList.contains('tick-nav-locked')) {
        // 检测目标消息（targetTickIndex）是否已进入视口
        const targetIdx = state.targetTickIndex;
        if (targetIdx >= 0 && targetIdx < userMessages.length) {
            const targetRect = userMessages[targetIdx].getBoundingClientRect();
            const containerRect = scrollContainer.getBoundingClientRect();
            // 双向检测：向下滚动时目标顶部进入视口底部，向上滚动时目标底部进入视口顶部
            const arrived = targetRect.top < containerRect.bottom
                        && targetRect.bottom > containerRect.top;
            if (arrived) {
                // 更新活动刻度为目标索引
                state.activeTickIndex = targetIdx;
                state.targetTickIndex = -1;
                tickNav.classList.remove('tick-nav-locked');
                // 不立即触发高亮动画，而是标记为待高亮，
                // 等滚动完全停止后再触发（由 scrollDebounceTimer 处理）
                state.pendingHighlightIndex = targetIdx;
                // 重建刻度导航以反映新的活动刻度
                updateTickNav();
            }
        }
        return;
    }

    const containerRect = scrollContainer.getBoundingClientRect();
    const containerTop = containerRect.top;

    // 找到第一个底部在容器顶部以下的用户消息（即第一个可见或部分可见的消息）。
    // 使用 rect.bottom > containerTop 而非 rect.top >= containerTop，因为消息之间
    // 有间距（gap/padding），rect.top 可能远大于 containerTop，导致跳过中间消息。
    // 如果所有消息都在容器顶部以上（全部滚出），则定位到最后一个。
    let targetIdx = 0;
    for (let i = 0; i < userMessages.length; i++) {
        const rect = userMessages[i].getBoundingClientRect();
        if (rect.bottom > containerTop) {
            targetIdx = i;
            break;
        }
        // 遍历到最后一个都没找到（全部已滚出顶部），targetIdx 设为最后一个
        targetIdx = i;
    }

    // 决定是否需要更新活动刻度：
    // - 无活动消息 → 更新
    // - 活动消息已滚出顶部且后面还有消息 → 更新到下一个可见消息
    // - 活动消息仍然可见 → 更新为更精确的 targetIdx
    // - 活动消息已滚出顶部但已是最后一条 → 保持不动（targetIdx 会等于 activeTickIndex）
    let shouldUpdate = false;
    if (state.activeTickIndex < 0 || state.activeTickIndex >= userMessages.length) {
        shouldUpdate = true;
    } else {
        const activeRect = userMessages[state.activeTickIndex].getBoundingClientRect();
        if (activeRect.bottom <= containerTop && state.activeTickIndex < userMessages.length - 1) {
            shouldUpdate = true;
        } else if (activeRect.bottom > containerTop) {
            shouldUpdate = true;
        }
    }

    if (shouldUpdate && targetIdx !== state.activeTickIndex) {
        state.activeTickIndex = targetIdx;
        adjustTickOffset();
        updateTickNav();
    }
}

/**
 * adjustTickOffset 调整 tickScrollOffset 确保活动刻度可见
 */
function adjustTickOffset() {
    const chatContainer = document.getElementById('chatContainer');
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
        const chatContainer = document.getElementById('chatContainer');
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

    // 面板消失时：根据当前滚动位置重新计算活动刻度，并确保 tickScrollOffset 包含活动刻度
    tickNav.addEventListener('mouseleave', () => {
        // 先根据滚动位置重新计算活动刻度
        updateActiveTickOnScroll();
        // 强制调整 tickScrollOffset，确保活动刻度在面板可见范围内。
        // 当用户仅滚动面板（未滚动主内容）时，updateActiveTickOnScroll 中
        // targetIdx === activeTickIndex 会导致 adjustTickOffset 不被执行，
        // 导致 tickScrollOffset 保持偏移后的值，下次打开面板时活动刻度不可见。
        adjustTickOffset();
        updateTickNav();
    });

    // 滚动事件处理：节流执行 updateActiveTickOnScroll + debounce 触发待高亮动画
    // 注意：滚动事件必须绑定在 #scrollContainer（实际滚动的容器）上，
    // 而非 #chatContainer（其 overflow-y: visible，不触发 scroll 事件）。
    let scrollThrottleTimer = null;
    let scrollDebounceTimer = null;
    const HIGHLIGHT_DEBOUNCE_MS = 300; // 滚动停止后等待 300ms 再触发高亮
    const scrollContainer = getScrollContainer();
    if (!scrollContainer) return;
    scrollContainer.addEventListener('scroll', () => {
        // 节流：每 150ms 最多执行一次 updateActiveTickOnScroll
        if (!scrollThrottleTimer) {
            scrollThrottleTimer = setTimeout(() => {
                scrollThrottleTimer = null;
                updateActiveTickOnScroll();
            }, 150);
        }
        // debounce：每次滚动都重置计时器，滚动停止 HIGHLIGHT_DEBOUNCE_MS 后触发待高亮
        if (scrollDebounceTimer) {
            clearTimeout(scrollDebounceTimer);
        }
        scrollDebounceTimer = setTimeout(() => {
            scrollDebounceTimer = null;
            // 滚动已停止，检查是否有待高亮的目标消息
            if (state.pendingHighlightIndex >= 0) {
                const scrollContainer = getScrollContainer();
                if (scrollContainer) {
                    const userMessages = scrollContainer.querySelectorAll('.message.user');
                    const idx = state.pendingHighlightIndex;
                    state.pendingHighlightIndex = -1;
                    if (idx >= 0 && idx < userMessages.length) {
                        const bubble = userMessages[idx].querySelector('.bubble');
                        if (bubble) {
                            bubble.classList.remove('highlight');
                            // 用 requestAnimationFrame 确保 DOM 更新后再添加 class
                            requestAnimationFrame(() => {
                                bubble.classList.add('highlight');
                            });
                        }
                    }
                }
            }
        }, HIGHLIGHT_DEBOUNCE_MS);
    });
}
