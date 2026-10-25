/* ui/htmx-bridge.js — 控件层与 htmx 之间唯一的适配层。
 *
 * 为什么需要它：UI 控件（drawer / modal / product-category-tree …）不该直接调 htmx API。
 * 理由不是「洁癖」，是两条实际后果：
 *
 *   1. **层次倒置**：控件是「界面行为」，htmx 是「传输层」。控件直接 depend htmx 之后，
 *      任何不用 htmx 的页面（访问面按能力裁剪脚本）都会因为 `htmx is not defined` 而崩，
 *      而它原本只是「这个交互不生效」。
 *   2. **换传输层要改所有控件**：drawer.js 曾经为了「模板克隆进 DOM 后让 hx-* 生效」
 *      直接调 `htmx.process(body)`。改成任何别的方案（morph / 手写事件委托）都要动它。
 *
 * 现在控件只广播 `wb:rescan`（detail.el = 需要认领的新 DOM），由本文件翻译成 htmx 的
 * `process`。没有 htmx 时本文件自己就是空的，控件不受影响。
 *
 * 谁用这个事件：drawer.js（抽屉打开后）、modal.js（模态内容注入后）。
 */
(function () {
    'use strict';

    if (!window.htmx || typeof window.htmx.process !== 'function') return;

    // 一次只处理一个 el；同一 tick 里的重复请求合并（抽屉 + 模态同时开的情况）。
    var pending = [];
    var scheduled = false;

    function flush() {
        scheduled = false;
        var batch = pending;
        pending = [];
        batch.forEach(function (el) {
            if (el && el.isConnected !== false) window.htmx.process(el);
        });
    }

    document.addEventListener('wb:rescan', function (ev) {
        var el = ev && ev.detail && ev.detail.el;
        if (!el) return;
        pending.push(el);
        if (!scheduled) {
            scheduled = true;
            // 微任务合并：同一轮里多个控件各广播一次时，htmx 只被调一次。
            Promise.resolve().then(flush);
        }
    });
})();
