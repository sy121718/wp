/* ui/index.js — 原始控件基座入口（后台页面直接引用；前台由构建期内联）。
 *
 * 职责：加载控件实现、DOM 就绪后扫描、htmx 局部替换后重扫。
 * 新增控件：写 ui/<control>.js，在里面 WBUI.register(init)，并加进下面的 load 列表。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    global.WBUI.ready(function () {
        if (WBUI.scan) { WBUI.scan(document); }
    });

    // htmx 局部替换后重扫（后台大量片段走 htmx）。
    document.addEventListener('htmx:afterSwap', function (e) {
        if (WBUI.scan) { WBUI.scan(e.target || document); }
    });
})(window);
