// ============================================================
// components/wordcloud.js — 椭圆螺旋词云布局算法
// ============================================================
//
// 移植自 html_demo/word_cloud.html 的椭圆螺旋布局算法，
// 提取为纯函数组件，供 portrait-dialog.js、sidebar 等调用。
//
// 使用方式：
//   var items = WordCloudLayout.calculate(hot_tags, 600, 200);
//   // items → [{tag, count, left, top, fontSize, colorClass}, ...]
//
// 参数化（options 对象）：
//   classPrefix  - CSS 类名前缀，默认 'portrait-wordcloud-c'
//   aspectRatio  - 椭圆 X/Y 半径比，默认根据容器宽高比自动推算
//   minFontSize  - 最小字号 px，默认 14
//   maxFontSize  - 最大字号 px，默认 48（实际还受 ch/5 约束）
//   padding      - 词间间距 px，默认 4
//
// 事件处理由渲染层（Alpine 模板 / 调用方）负责，不在本组件中处理。
//
// 依赖：无（纯 JS，不需要 Alpine 或其他库）
// ============================================================

'use strict';

window.WordCloudLayout = (function() {

    // ============================================================
    // 默认配置
    // ============================================================
    var DEFAULTS = {
        minFontSize: 14,
        maxFontSize: 48,         // 上限，实际还会受 ch/5 约束
        padding: 4,
        classPrefix: 'portrait-wordcloud-c',
        aspectRatio: null,       // null = 自动根据容器宽高比推算
        spiralStep: 0.35,
        spiralRadiusStep: 1.0,
        maxAttempts: 6000,
        // 36 色丰富调色板
        colors: [
            '#FF6B6B', '#4ECDC4', '#45B7D1', '#96CEB4',
            '#FFD93D', '#DDA0DD', '#6BCB77', '#FF8C42',
            '#BB8FCE', '#74B9FF', '#2b6cb0', '#3182ce',
            '#38a169', '#d69e2e', '#e53e3e', '#805ad5',
            '#dd6b20', '#319795', '#d53f8c', '#2c5282',
            '#4299e1', '#48bb78', '#ecc94b', '#fc8181',
            '#9f7aea', '#ed8936', '#38b2ac', '#ed64a6',
            '#f6ad55', '#81e6d9', '#f687b3', '#c53030',
            '#6b46c1', '#b7791f', '#285e61', '#b83280'
        ]
    };

    // ============================================================
    // 工具函数
    // ============================================================

    /**
     * 精确测量文本尺寸（创建隐藏 span 获取实际宽高）
     * @param {string} text
     * @param {number} fontSize  - px
     * @returns {{width: number, height: number}}
     */
    function measureText(text, fontSize) {
        var el = document.createElement('span');
        el.textContent = text;
        el.style.cssText = [
            'position:absolute',
            'visibility:hidden',
            'white-space:nowrap',
            'font-size:' + fontSize + 'px',
            'font-weight:700',
            'font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif',
            'line-height:1.2',
            'top:0',
            'left:0'
        ].join(';');
        document.body.appendChild(el);
        var rect = el.getBoundingClientRect();
        document.body.removeChild(el);
        return { width: rect.width, height: rect.height };
    }

    /**
     * AABB 碰撞检测
     * @param {{x:number, y:number, w:number, h:number}} a
     * @param {{x:number, y:number, w:number, h:number}} b
     * @param {number} pad  - 间距
     * @returns {boolean}  true=重叠
     */
    function isOverlapping(a, b, pad) {
        return !(a.x + a.w + pad <= b.x ||
            b.x + b.w + pad <= a.x ||
            a.y + a.h + pad <= b.y ||
            b.y + b.h + pad <= a.y);
    }

    /**
     * 字号映射：smoothstep 缓动
     * @param {number} count
     * @param {number} minCount
     * @param {number} maxCount
     * @param {number} minSize
     * @param {number} maxSize
     * @returns {number}  px
     */
    function mapFontSize(count, minCount, maxCount, minSize, maxSize) {
        var range = maxCount - minCount || 1;
        var t = (count - minCount) / range;
        var eased = t * t * (3 - 2 * t); // smoothstep
        return minSize + eased * (maxSize - minSize);
    }

    // ============================================================
    // 椭圆螺旋布局
    // ============================================================

    /**
     * 计算词云布局
     * @param {Array<{tag:string, count:number}>} tags  - hot_tags 数组
     * @param {number} cw  - 容器宽度（px）
     * @param {number} ch  - 容器高度（px）
     * @param {Object} [opts]  - 可选覆盖参数
     * @param {string} [opts.classPrefix='portrait-wordcloud-c']  - CSS 类名前缀
     * @param {number} [opts.aspectRatio]  - 椭圆 X/Y 半径比，默认自动推算
     * @param {number} [opts.minFontSize=14]  - 最小字号 px
     * @param {number} [opts.maxFontSize=48]  - 最大字号 px（还受 ch/5 约束）
     * @param {number} [opts.padding=4]  - 词间间距 px
     * @returns {Array<{tag, count, left, top, fontSize, colorClass}>}
     */
    function calculate(tags, cw, ch, opts) {
        if (!Array.isArray(tags) || tags.length === 0 || cw < 50 || ch < 50) {
            return [];
        }

        // 合并选项
        opts = opts || {};
        var classPrefix = opts.classPrefix || DEFAULTS.classPrefix;
        var minSize = opts.minFontSize || DEFAULTS.minFontSize;
        var maxSizeUser = opts.maxFontSize || DEFAULTS.maxFontSize;
        var padding = opts.padding !== undefined ? opts.padding : DEFAULTS.padding;
        var colors = DEFAULTS.colors;

        // 深拷贝并排序
        var sorted = tags.slice().sort(function(a, b) { return b.count - a.count; });

        var minCount = sorted[sorted.length - 1].count;
        var maxCount = sorted[0].count;

        // 动态字号范围（受 maxSizeUser 和容器高度共同约束）
        var maxSize = Math.min(maxSizeUser, Math.max(22, Math.round(ch / 5)));

        var cx = cw / 2;
        var cy = ch / 2;
        var placedRects = [];
        var result = [];

        // 椭圆因子：优先使用传入的 aspectRatio，否则根据容器宽高比自动适配
        var aspectFactor = opts.aspectRatio !== undefined
            ? opts.aspectRatio
            : ((cw / ch) > 1.2 ? 1.8 : 1.2);

        for (var i = 0; i < sorted.length; i++) {
            var tag = sorted[i];
            var fontSize = mapFontSize(tag.count, minCount, maxCount, minSize, maxSize);
            var dim = measureText(tag.tag, fontSize);
            var w = dim.width;
            var h = dim.height;
            var colorIdx = i % colors.length;

            var px, py;

            if (i === 0) {
                // 最大词居中
                px = cx - w / 2;
                py = cy - h / 2;
            } else {
                // 椭圆螺旋搜索可用位置
                var found = false;
                var angle = Math.random() * 0.5;
                var radius = 4;

                for (var attempt = 0; attempt < DEFAULTS.maxAttempts; attempt++) {
                    var ox = radius * aspectFactor * Math.cos(angle);
                    var oy = radius * Math.sin(angle);
                    px = cx + ox - w / 2;
                    py = cy + oy - h / 2;

                    // 边界检查
                    if (px < 0 || py < 0 || px + w > cw || py + h > ch) {
                        angle += DEFAULTS.spiralStep;
                        radius += DEFAULTS.spiralRadiusStep * 0.5;
                        continue;
                    }

                    // 碰撞检查
                    var candidate = { x: px, y: py, w: w, h: h };
                    var overlap = false;
                    for (var j = 0; j < placedRects.length; j++) {
                        if (isOverlapping(candidate, placedRects[j], padding)) {
                            overlap = true;
                            break;
                        }
                    }

                    if (!overlap) {
                        found = true;
                        break;
                    }

                    angle += DEFAULTS.spiralStep;
                    radius += DEFAULTS.spiralRadiusStep;
                }

                if (!found) {
                    // 兜底：随机圆形区域尝试
                    var attempts2 = 0;
                    do {
                        var a2 = Math.random() * 2 * Math.PI;
                        var r2 = Math.min(cw, ch) * 0.3 + Math.random() * 0.4 * Math.min(cw, ch);
                        px = cx + r2 * aspectFactor * Math.cos(a2) - w / 2;
                        py = cy + r2 * Math.sin(a2) - h / 2;
                        attempts2++;
                    } while ((px < 0 || py < 0 || px + w > cw || py + h > ch) && attempts2 < 50);

                    px = Math.max(0, Math.min(px, cw - w));
                    py = Math.max(0, Math.min(py, ch - h));
                }
            }

            placedRects.push({ x: px, y: py, w: w, h: h });

            result.push({
                tag: tag.tag,
                count: tag.count,
                left: Math.round(px),
                top: Math.round(py),
                fontSize: fontSize.toFixed(1) + 'px',
                colorClass: classPrefix + ((i % 10) + 1)
            });
        }

        return result;
    }

    // ============================================================
    // 公开接口
    // ============================================================

    return {
        calculate: calculate
    };

})();
