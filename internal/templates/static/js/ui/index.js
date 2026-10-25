/* ui/index.js — 原始控件基座入口（后台页面直接引用；前台由构建期内联）。
 *
 * 职责：加载控件实现、DOM 就绪后扫描、**DOM 变化后重扫**。
 * 新增控件：写 ui/<control>.js，在里面 WBUI.register(init)，并加进加载列表。
 *
 * 重扫机制：MutationObserver 而不是 htmx 事件。
 *
 * 为什么换掉：`document.addEventListener('htmx:after:swap', …)` 让「控件增强」这件事
 * 依赖 htmx 存在 —— 而 htmx 只是**换入内容的一种方式**（还有 drawer 克隆模板、
 * modal 注入 HTML、控件自己 replaceChildren）。那些路径不派发 htmx 事件，于是新内容
 * 里的控件全部失效，且不报错。
 *
 * MutationObserver 覆盖所有路径：只要节点进了文档，就重扫那棵子树。
 * htmx 事件仍保留为一条**可选**的立即触发路径 —— 它比 MutationObserver 早一个微任务，
 * 能让 htmx 换入的控件在首帧就是增强态（避免「先看到原生 select，再跳成自绘」）。
 * 没有 htmx 时那条监听不会注册，其余逻辑不受影响。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    function scan(el) {
        if (WBUI.scan) { WBUI.scan(el || document); }
    }

    global.WBUI.ready(function () { scan(document); });

    // ── 主路径：MutationObserver ────────────────────────────────────────────
    //
    // 只看「新增元素节点」，并把同一 tick 内的多个新增合并成一次扫描 ——
    // 一段片段换入会产生几十条 mutation，逐条扫会让每个控件被增强几十次。
    var queue = [];
    var scheduled = false;

    function flush() {
        scheduled = false;
        var batch = queue;
        queue = [];
        // 去重：被祖先覆盖的节点不再单独扫。
        var roots = [];
        batch.forEach(function (el) {
            if (!el.isConnected) { return; }
            for (var i = 0; i < roots.length; i++) {
                if (roots[i].contains(el)) { return; }
            }
            roots.push(el);
        });
        roots.forEach(scan);
    }

    function observe() {
        var MO = global.MutationObserver || global.WebKitMutationObserver;
        if (!MO) { return; }
        var mo = new MO(function (mutations) {
            for (var i = 0; i < mutations.length; i++) {
                var added = mutations[i].addedNodes;
                for (var j = 0; j < added.length; j++) {
                    if (added[j].nodeType === 1) { queue.push(added[j]); }
                }
            }
            if (queue.length && !scheduled) {
                scheduled = true;
                Promise.resolve().then(flush);
            }
        });
        mo.observe(document.documentElement, { childList: true, subtree: true });
    }

    global.WBUI.ready(observe);

    // ── 可选加速：htmx 换入时立即扫（比上面早一个微任务）────────────────────
    //
    // 不存在也不影响：没有 htmx 时这段不注册，MutationObserver 仍然会扫到。
    // htmx 4 的事件名是冒号分段（afterSwap → after:swap）。
    document.addEventListener('htmx:after:swap', function (e) {
        scan(e.target || document);
    });
})(window);
