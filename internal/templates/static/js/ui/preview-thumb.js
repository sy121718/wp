/* ui/preview-thumb.js — 详情页预览的等比缩略（后台编辑页右栏）。

 * 要解决的问题：右栏只有 380~400px 宽，而详情页是桌面布局（1200~1440px）。
 * 直接把它塞进窄 iframe，看到的是「页面左侧的一条」——不是页面难看，是视口不对。
 * 做法反过来：iframe **按真实桌面宽度渲染**，再由本控件把整块等比缩小到容器宽度，
 * 于是右栏里出现的是**完整的页面形态**（字小，但结构、留白、模块顺序一目了然）。
 *
 * 为什么不写纯 CSS：缩放比取决于容器实际宽度（栏宽是流式的、窗口还会 resize），
 * 固定值不是露白就是溢出。这里算出 scale 写进 CSS 变量，纯 CSS 负责应用。
 *
 * 视口宽度由模板给（data-preview-viewport），默认 1440 —— 与「桌面」验收视口一致
 * （docs/rules/frontend.md 的三条视口之一）。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    var DEFAULT_VIEWPORT = 1440;
    var DEFAULT_HEIGHT = 900;

    function scaleOf(root) {
        var viewport = parseFloat(root.getAttribute('data-preview-viewport')) || DEFAULT_VIEWPORT;
        var avail = root.clientWidth;
        if (!avail || viewport <= 0) {
            return 1; // 隐藏中（display:none 时 clientWidth 为 0）—— 先按 1 渲染，显示后再扫一次
        }
        return Math.min(1, avail / viewport);
    }

    function apply(root) {
        var scale = scaleOf(root);
        root.style.setProperty('--preview-scale', String(scale));
        // 容器高度必须跟着缩，否则会留出与缩放后内容等高的空白。
        var height = parseFloat(root.getAttribute('data-preview-height')) || DEFAULT_HEIGHT;
        root.style.setProperty('--preview-outer-height', (height * scale) + 'px');
    }

    WBUI.register(function (scope) {
        WBUI.$$('[data-preview-thumb]', scope).forEach(function (root) {
            if (!WBUI.markOnce(root, 'PreviewThumb')) {
                // 已经标记过：容器可能刚被显示出来（之前 clientWidth 为 0），补算一次。
                apply(root);
                return;
            }
            apply(root);
            // 栏宽随窗口变化（两栏在窄屏会落成单列），缩放比要跟着重算。
            // 用 ResizeObserver 而不是 window.resize：容器宽度的变化不等于窗口变化
            // （侧栏折叠、两栏切换都会改它而不改窗口）。
            if (typeof ResizeObserver === 'function') {
                var ro = new ResizeObserver(function () { apply(root); });
                ro.observe(root);
            }
        });
    });
})(window);
