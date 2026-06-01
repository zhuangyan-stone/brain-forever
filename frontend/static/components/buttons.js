// ============================================================
// buttons.js — 按钮 Alpine 组件
// ============================================================
// 分类：
//   1. iconBtn    — 纯图标按钮，支持 normal / small 两种尺寸
//   2. textBtn    — 带文字、带边框的按钮，可选左侧图标
//   3. toggleBtn  — 开关型按钮，点击切换选中/未选中状态
//   4. sendBtn    — 发送/停止按钮，支持两种视觉状态切换
//   5. attachBtn  — 附件上传触发按钮
//
// 此文件以普通 <script> 加载（非 ES Module），
// 在 alpine:init 事件中通过 Alpine.data() 注册所有组件，
// 使 HTML 中 x-data="iconBtn(config)" 在 Alpine 处理 DOM 时立即可用。
// ============================================================

document.addEventListener('alpine:init', function() {

    // ============================================================
    // 1. iconBtn — IconButton
    // ============================================================
    // 用途：纯图标按钮，支持两种尺寸
    // 适用：themeToggle（normal 尺寸）、aiTitleBtn（small 尺寸）、
    //       sidebarCloseBtn（normal 尺寸）、menu-toggle-btn（small 尺寸）
    //
    // 使用方式：
    //   <button class="icon-btn icon-btn--normal"
    //           x-data="iconBtn({ size: 'normal' })"
    //           @click="$store.settings.toggleTheme()"
    //           :data-tooltip="$store.settings.theme === 1 ? '亮色' : '暗色'">
    //       <svg>...</svg>
    //   </button>
    Alpine.data('iconBtn', (config = {}) => ({
        /**
         * 尺寸变体：'normal' | 'small'
         * - normal: 34×34 容器，20×20 图标
         * - small:  20×20 容器，14×14 图标
         */
        size: config.size || 'normal',

        /**
         * disabled 状态由外部注入，支持函数或布尔值
         */
        get disabled() {
            if (typeof config.disabled === 'function') {
                return config.disabled();
            }
            return config.disabled === true;
        },
    }));


    // ============================================================
    // 2. textBtn — TextButton
    // ============================================================
    // 用途：带文字、带边框的按钮，可选左侧图标
    // 适用：newChatBtn（图标+文字）、loginBtn（纯文字）
    //
    // 使用方式：
    //   <button class="text-btn"
    //           x-data="textBtn({ disabled: () => $store.chats.active?.isStreaming })"
    //           :disabled="disabled">
    //       <svg><use href="#icon-new-chat"/></svg>
    //       <span>新对话</span>
    //   </button>
    Alpine.data('textBtn', (config = {}) => ({
        /**
         * disabled 状态由外部通过 config.disabled 注入，
         * 支持传入 getter 函数（保持 Alpine 响应式）或布尔值。
         */
        get disabled() {
            if (typeof config.disabled === 'function') {
                return config.disabled();
            }
            return config.disabled === true;
        },
    }));


    // ============================================================
    // 3. toggleBtn — ToggleButton
    // ============================================================
    // 用途：开关型按钮，点击切换选中/未选中状态
    // 适用：deepThinkBtn、webSearchBtn
    //
    // 使用方式：
    //   <button class="toggle-btn"
    //           x-data="toggleBtn({
    //               active: () => $store.settings.deepThink,
    //               onToggle: () => $store.settings.toggleDeepThink(),
    //           })"
    //           :data-active="active ? 'true' : 'false'"
    //           @click="onToggle">
    //       <svg>...</svg>
    //       <span>深度思考</span>
    //   </button>
    Alpine.data('toggleBtn', (config = {}) => ({
        /**
         * 是否激活，由外部注入响应式 getter
         */
        get active() {
            if (typeof config.active === 'function') {
                return config.active();
            }
            return config.active === true;
        },

        /**
         * 点击时的切换函数，由外部注入
         */
        onToggle: config.onToggle || function() {},
    }));


    // ============================================================
    // 4. sendBtn — SendButton
    // ============================================================
    // 用途：发送/停止按钮，支持两种视觉状态切换
    // - active=false：默认状态（发送态）
    // - active=true：备选状态（停止态）
    //
    // 使用方式：
    //   <button id="sendBtn" class="send-btn"
    //           x-data="sendBtn({ active: () => $store.chats.active?.isStreaming })"
    //           :class="{ 'stop-btn': active }"
    //           :data-tooltip="active ? '停止生成' : '发送'">
    //       <template x-if="!active"><svg><!-- 纸飞机 --></svg></template>
    //       <template x-if="active"><svg><!-- 停止方块 --></svg></template>
    //   </button>
    Alpine.data('sendBtn', (config = {}) => ({
        /**
         * active 状态由外部注入，控制按钮的视觉模式：
         * - false → 默认状态（如发送）
         * - true  → 备选状态（如停止）
         *
         * 支持传入 getter 函数（保持 Alpine 响应式）或布尔值
         */
        get active() {
            if (typeof config.active === 'function') {
                return config.active();
            }
            return config.active === true;
        },
    }));


    // ============================================================
    // 5. attachBtn — AttachButton
    // ============================================================
    // 用途：附件上传触发按钮，点击打开文件选择框
    //
    // 使用方式：
    //   <button id="attachBtn" class="attach-btn" x-data="attachBtn()">
    //       <svg>...</svg>
    //   </button>
    Alpine.data('attachBtn', () => ({
        /**
         * 当前无特殊状态逻辑，仅为 Alpine 组件占位，
         * 以便将来扩展（如拖拽上传状态、文件数量徽标等）。
         */
    }));

});
