// ============================================================
// format.js — 消息时间格式化 & AI 角色标签文本生成函数
// ============================================================
// 包含：
//   1. formatMessageTime  — ISO 时间戳 → HH:mm:ss
//   2. assistantLabel     — AI 角色标签文本
//
// 这两个函数作为 window 全局变量暴露，供 Alpine 模板中的
// x-text 表达式直接引用（如 formatMessageTime(group.user.createdAt)）。
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中注册到 window 上，
// 确保 Alpine 处理 DOM 时这些函数立即可用。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 1. formatMessageTime — 消息时间格式化函数
    // ============================================================
    // 在 Alpine x-text 表达式中使用：formatMessageTime(group.user.createdAt)
    window.formatMessageTime = function(isoStr) {
        if (!isoStr) return '';
        try {
            var d = new Date(isoStr);
            var hh = String(d.getHours()).padStart(2, '0');
            var mm = String(d.getMinutes()).padStart(2, '0');
            var ss = String(d.getSeconds()).padStart(2, '0');
            var timeStr = hh + ':' + mm + ':' + ss;

            // 判断是否当天：比较年月日
            var now = new Date();
            var isToday = d.getFullYear() === now.getFullYear()
                       && d.getMonth() === now.getMonth()
                       && d.getDate() === now.getDate();

            if (isToday) {
                return timeStr;
            } else {
                // 非当天，显示日期 + 时间：xxxx/xx/xx xx:xx:xx
                var yyyy = String(d.getFullYear());
                var mo   = String(d.getMonth() + 1).padStart(2, '0');
                var dd   = String(d.getDate()).padStart(2, '0');
                return yyyy + '/' + mo + '/' + dd + ' ' + timeStr;
            }
        } catch(e) {
            return '';
        }
    };

    // ============================================================
    // 2. assistantLabel — AI 角色标签文本生成函数
    // ============================================================
    // 在 Alpine x-text 表达式中使用：assistantLabel(group.assistant)
    //
    // 将嵌套三元表达式的复杂逻辑封装为函数，与 formatMessageTime 保持一致的风格。
    window.assistantLabel = function(assistant) {
        var prefix = '🤖 AI';
        if (assistant.reasoningHTML) {
            return assistant.reasoningState === 'done'
                ? prefix + ' 思考完成 (' + formatMessageTime(assistant.createdAt) + ')'
                : prefix + ' 正在思考……';
        }
        return prefix + (assistant.createdAt ? ' (' + formatMessageTime(assistant.createdAt) + ')' : '');
    };

});
