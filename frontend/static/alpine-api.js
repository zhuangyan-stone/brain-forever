// ============================================================
// alpine-api.js — 供 Alpine Store（普通 <script>）调用的 API 函数
// ============================================================
// 此文件以普通 <script> 加载（非 ES Module），
// 因为调用方 alpine-store.js 也是普通 <script>，无法 import。
// 函数通过 window 暴露，showToast 由 chat-ui.js 注册到 window。
// ============================================================

/**
 * fetchSyncThemeModeToServer — 同步亮暗模式到服务端
 * 调用 PUT /api/user/theme/mode
 * @param {number} mode - 0=亮, 1=暗
 */
window.fetchSyncThemeModeToServer = async function (mode) {
    try {
        var resp = await fetch('/api/user/theme/mode', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mode: mode }),
        });
        if (!resp.ok) {
            var t = await resp.text();
            window.showToast('同步亮暗模式失败：' + t, 'error');
        }
    } catch (e) {
        console.warn('同步亮暗模式到服务端失败:', e);
    }
};

/**
 * fetchApiKeySettings — 从后端获取 API-Key 设置（已脱敏）
 * 调用 GET /api/user/settings/apikey
 * @returns {Promise<object|null>} 设置对象，失败返回 null
 */
window.fetchApiKeySettings = async function() {
    try {
        var resp = await fetch('/api/user/settings/apikey');
        if (resp.ok) {
            return await resp.json();
        } else {
            const t = await resp.text();
            window.showToast('获取API-Key设置失败：' + t, 'error');
        }
    } catch (e) {
        console.warn('获取API-Key设置失败，使用默认值:', e);
    }
    return null;
};

/**
 * fetchLogin — 调用后端 POST /api/user/login 登录
 * @param {string} userNo - 全局唯一用户系列号
 * @returns {Promise<object|null>} 登录响应数据（含 status、chats 等），失败返回 null
 */
window.fetchLogin = async function(userNo) {
    try {
        const resp = await fetch('/api/user/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json; charset=utf-8' },
            body: JSON.stringify({ user_no: userNo }),
        });
        if (!resp.ok) {
            const t = await resp.text();
            window.showToast('登录失败：' + t, 'error');
            return null;
        }
        return await resp.json();
    } catch (e) {
        console.warn('登录API请求失败:', e);
        return null;
    }
};

/**
 * fetchLogout — 调用后端 POST /api/user/logout 退出登录
 * @returns {Promise<object|null>} 退出响应数据（含 status），失败返回 null
 */
window.fetchLogout = async function() {
    try {
        const resp = await fetch('/api/user/logout', {
            method: 'POST',
        });
        if (!resp.ok) {
            const t = await resp.text();
            window.showToast('退出登录失败：' + t, 'error');
            return null;
        }
        return await resp.json();
    } catch (e) {
        console.warn('退出登录API请求失败:', e);
        return null;
    }
};

/**
 * fetchApplyThemeSelection — 同步外源主题选择到服务端
 * 调用 POST /api/user/theme/apply
 * @param {number|string} theme - 主题模式值
 * @param {string} lightId - 亮色主题 ID
 * @param {string} darkId - 暗色主题 ID
 */
window.fetchApplyThemeSelection = async function(theme, lightId, darkId) {
    try {
        const resp = await fetch('/api/user/theme/apply', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                actived: String(theme),
                'actived-light': lightId,
                'actived-dark': darkId,
            }),
        });
        if (!resp.ok) {
            const t = await resp.text();
            window.showToast('同步主题选择失败：' + t, 'error');
        }
    } catch (e) {
        console.warn('同步主题选择到服务端失败:', e);
    }
};

