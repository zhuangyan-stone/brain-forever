// ============================================================
// trait-api.js — 个人特征提取的 API 调用
// ============================================================
//
// 唯一的 extractTraits 函数，被 trait.js（自动触发）和
// chat-list.js（右键菜单）共同调用。
// ============================================================

'use strict';

/**
 * extractTraits 调用后端 API 提取指定对话的个人特征。
 * 后端会从本地数据库读取消息，然后调用 remote-server 的 LLM 进行特征提取。
 * @param {string} sn - 对话 SN
 * @returns {Promise<{features: Array, usage?: object}|null>}
 */
export async function extractTraits(sn) {
    if (!sn) return null;
    try {
        const response = await fetch('/api/chat/traits', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json; charset=utf-8' },
            body: JSON.stringify({ sn: sn }),
        });
        if (!response.ok) {
            const errText = await response.text();
            console.warn('提取特征失败:', response.status, errText);
            return null;
        }
        return await response.json();
    } catch (e) {
        console.warn('提取特征出错:', e);
        return null;
    }
}
