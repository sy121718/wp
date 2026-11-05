/* ui/busy.js — 原始控件：按钮忙碌态（防重复提交 + 明确反馈）。
 *
 * 用法：
 *   var done = WBUI.busy(btn);                    // 手动结束
 *   WBUI.busy(btn, fetch(...));                   // promise settle 时自动恢复
 *   WBUI.busy(btn, p, { label: '保存中…' });       // 顺带换文案（结束时还原）
 *
 * 为什么进基座：这段逻辑被手写了三遍（媒体库「重新生成变体」、工作台「恢复历史」、
 * 工作台「保存设置」），而且手写版总是漏掉同样两件事：
 *   ① 结束时一律 btn.disabled = false —— 把「本来就该禁用」的按钮错误地启用；
 *   ② 请求 reject 时忘了恢复，按钮从此卡在禁用态。
 * 这里两件都兜住：记住原状态、成功失败都恢复。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    function busy(btn, wait, opts) {
        if (!btn) { return function () {}; }
        // 兼容 busy(btn, { label: '…' })：第二参不是 promise 时当 opts。
        if (wait && typeof wait === 'object' && typeof wait.then !== 'function') {
            opts = wait;
            wait = null;
        }
        opts = opts || {};
        // 同一按钮重复进入：复用同一个收尾函数，避免内层先恢复、外层还以为在忙碌。
        if (btn.__wbBusyFinish) { return btn.__wbBusyFinish; }

        var prevDisabled = !!btn.disabled;
        var prevHTML = null;
        if (opts.label != null) {
            prevHTML = btn.innerHTML;
            btn.textContent = opts.label;
        }
        btn.disabled = true;
        btn.classList.add('is-busy');
        btn.setAttribute('aria-busy', 'true');

        var finished = false;
        var finish = function () {
            if (finished) { return; }
            finished = true;
            btn.__wbBusyFinish = null;
            btn.classList.remove('is-busy');
            btn.removeAttribute('aria-busy');
            // 还原成「原来是否禁用」，不是一律启用。
            btn.disabled = prevDisabled;
            if (prevHTML != null) { btn.innerHTML = prevHTML; }
        };
        btn.__wbBusyFinish = finish;

        if (wait && typeof wait.then === 'function') {
            wait.then(finish, finish);   // 失败也要恢复，否则按钮永远卡住
        }
        return finish;
    }

    WBUI.busy = busy;

    WBUI.register(function () { /* API 控件：无声明式用法 */ });
})(window);
