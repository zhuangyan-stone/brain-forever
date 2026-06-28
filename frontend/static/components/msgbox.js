// ============================================================
// msgbox.js — 统一消息框组件
// 提供三种风格的消息框：警告框、信息框、提问框
// 复用 dialog.css 中的基础对话框样式（dialog-overlay / dialog-box）
// ============================================================
// 使用方式：
//   import msgbox from './components/msgbox.js';
//
//   // 警告框 — 返回 Promise<number>：-1（取消）或 1（确认）
//   const result = await msgbox.warning('〔xxx〕删除后不可恢复，请确认是否删除？');
//   if (result === 1) { /* 执行删除 */ }
//
//   // 信息框 — 返回 Promise<void>
//   await msgbox.alert('操作已成功完成。');
//
//   // 提问框 — 返回 Promise<number>：-1（关闭）、0（否）、1（是）
//   const answer = await msgbox.confirm('是否保存当前更改？');
// ============================================================

'use strict';

/**
 * 通用对话框关闭与清理
 */
function closeDialog(overlay) {
    overlay.classList.remove('show');
    overlay.classList.add('msgbox-overlay-closing');
    // 动画结束后移除 DOM
    overlay.addEventListener('animationend', () => {
        if (overlay.parentNode) {
            overlay.parentNode.removeChild(overlay);
        }
    }, { once: true });
    // 兜底：如果 animationend 未触发，直接移除
    setTimeout(() => {
        if (overlay.parentNode) {
            overlay.parentNode.removeChild(overlay);
        }
    }, 300);
}

/**
 * 图标 Unicode 映射
 */
const MSGBOX_ICON_CHAR = {
    info: '\u1F6C8',     // 🛈
    warning: '\u26A0',  // ⚠
    question: '\u2BD1'  // ⯑
};

/**
 * 创建基础对话框 DOM 结构
 * @param {string} title - 对话框标题
 * @param {string} [iconType] - 图标类型：'info' | 'warning' | 'question'
 * @returns {{ overlay: HTMLElement, body: HTMLElement, footer: HTMLElement, closeBtn: HTMLElement }}
 */
function createDialog(title, iconType) {
    const overlay = document.createElement('div');
    overlay.className = 'dialog-overlay show';

    const box = document.createElement('div');
    box.className = 'dialog-box';

    // ---- 头部（仅标题 + 关闭按钮） ----
    const header = document.createElement('div');
    header.className = 'dialog-header';

    const titleEl = document.createElement('h3');
    titleEl.textContent = title;
    header.appendChild(titleEl);

    const closeBtn = document.createElement('button');
    closeBtn.className = 'dialog-close-btn';
    closeBtn.innerHTML = '&times;';
    closeBtn.setAttribute('aria-label', '关闭');
    header.appendChild(closeBtn);
    box.appendChild(header);

    // ---- 内容区：左右布局（图标 | 消息） ----
    const body = document.createElement('div');
    body.className = 'msgbox-body';
    box.appendChild(body);

    // 左侧：图标（64×64）
    const iconArea = document.createElement('div');
    iconArea.className = 'msgbox-icon-left';
    if (iconType && MSGBOX_ICON_CHAR[iconType]) {
        const iconSpan = document.createElement('span');
        iconSpan.className = 'msgbox-icon-char';
        iconSpan.textContent = MSGBOX_ICON_CHAR[iconType];
        iconArea.appendChild(iconSpan);
    }
    body.appendChild(iconArea);

    // 右侧：消息内容区（由具体方法填充）
    const contentArea = document.createElement('div');
    contentArea.className = 'msgbox-content-right';
    body.appendChild(contentArea);

    // ---- 底部按钮区（占位，由具体方法填充） ----
    const footer = document.createElement('div');
    footer.className = 'dialog-footer';
    box.appendChild(footer);

    overlay.appendChild(box);

    return { overlay, body, footer, closeBtn, contentArea };
}

/**
 * 通用按钮创建
 * @param {string} text - 按钮文字
 * @param {string} className - 样式类名
 * @param {Function} onClick - 点击回调
 * @returns {HTMLButtonElement}
 */
function createBtn(text, className, onClick) {
    const btn = document.createElement('button');
    btn.className = 'dialog-btn ' + className;
    btn.textContent = text;
    btn.addEventListener('click', onClick);
    return btn;
}

/**
 * 将对话框添加到 document.body
 */
function showDialog(overlay) {
    document.body.appendChild(overlay);
}

// ============================================================
// 公开 API
// ============================================================

const msgbox = {

    /**
     * 信息框
     * 标题："信息"，按钮："知道了"
     * @param {string} message - 信息内容
     * @returns {Promise<void>}
     */
    alert(message) {
        return new Promise((resolve) => {
            const { overlay, footer, closeBtn, contentArea } = createDialog('信息', 'info');

            // 消息内容
            const msgEl = document.createElement('p');
            msgEl.className = 'msgbox-message';
            msgEl.textContent = message;
            contentArea.appendChild(msgEl);

            // "知道了" 按钮
            const okBtn = createBtn('知道了', 'dialog-btn-confirm', () => {
                closeDialog(overlay);
                resolve();
            });
            footer.appendChild(okBtn);

            // 关闭按钮
            closeBtn.addEventListener('click', () => {
                closeDialog(overlay);
                resolve();
            });

            // 点击遮罩层外部可以关闭
            overlay.addEventListener('click', (e) => {
                if (e.target === overlay) {
                    closeDialog(overlay);
                    resolve();
                }
            });

            showDialog(overlay);
        });
    },

    /**
     * 警告框
     * 标题："警告"，按钮："取消"(-1)、"确认"(1)，确认按钮为警告色
     * @param {string} message - 警告内容
     * @returns {Promise<number>} -1 取消 / 1 确认
     */
    warning(message) {
        return new Promise((resolve) => {
            const { overlay, footer, closeBtn, contentArea } = createDialog('警告', 'warning');

            // 消息内容
            const msgEl = document.createElement('p');
            msgEl.className = 'msgbox-message';
            msgEl.textContent = message;
            contentArea.appendChild(msgEl);

            // 通用退出：取消 + 清理 Escape 监听
            function exit(value) {
                document.removeEventListener('keydown', onKeyDown);
                closeDialog(overlay);
                resolve(value);
            }

            // Escape → 取消
            function onKeyDown(e) {
                if (e.key === 'Escape') {
                    exit(-1);
                }
            }

            // "取消" 按钮
            const cancelBtn = createBtn('取消', 'dialog-btn-cancel', () => exit(-1));
            footer.appendChild(cancelBtn);

            // "确认" 按钮（使用 dialog-btn-delete 样式，统一危险色）
            const confirmBtn = createBtn('确认', 'dialog-btn-delete', () => exit(1));
            footer.appendChild(confirmBtn);

            // 关闭按钮
            closeBtn.addEventListener('click', () => exit(-1));

            // 点击遮罩层外部 → 取消
            overlay.addEventListener('click', (e) => {
                if (e.target === overlay) {
                    exit(-1);
                }
            });

            // Escape → 取消
            document.addEventListener('keydown', onKeyDown);

            showDialog(overlay);
        });
    },

    /**
     * 提问框
     * 标题："提问"，按钮："取消"(-1)、"否"(0)、"是"(1)
     * @param {string} question - 问题内容
     * @returns {Promise<number>} -1 取消 / 0 否 / 1 是
     */
    confirm(question) {
        return new Promise((resolve) => {
            const { overlay, footer, closeBtn, contentArea } = createDialog('提问', 'question');

            // 问题内容
            const msgEl = document.createElement('p');
            msgEl.className = 'msgbox-message';
            msgEl.textContent = question;
            contentArea.appendChild(msgEl);

            // 通用退出：取消 + 清理 Escape 监听
            function exit(value) {
                document.removeEventListener('keydown', onKeyDown);
                closeDialog(overlay);
                resolve(value);
            }

            // Escape → 关闭（返回 -1）
            function onKeyDown(e) {
                if (e.key === 'Escape') {
                    exit(-1);
                }
            }

            // "否" 按钮
            const noBtn = createBtn('否', 'dialog-btn-cancel', () => exit(0));
            footer.appendChild(noBtn);

            // "是" 按钮（强调色）
            const yesBtn = createBtn('是', 'msgbox-btn-primary', () => exit(1));
            footer.appendChild(yesBtn);

            // 关闭按钮
            closeBtn.addEventListener('click', () => exit(-1));

            // 点击遮罩层外部 → 关闭
            overlay.addEventListener('click', (e) => {
                if (e.target === overlay) {
                    exit(-1);
                }
            });

            // Escape → 关闭
            document.addEventListener('keydown', onKeyDown);

            showDialog(overlay);
        });
    }
};

export default msgbox;
