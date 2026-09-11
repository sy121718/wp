/* ui/toast.js — 原始控件：轻提示（非模态操作反馈）。
 *
 * 与 confirm.js 的 WBUI.alert 分工（两者是同一个问题的两种强度）：
 *   alert  = 模态框，用户必须点一下才继续 —— 「必须被看到、且需要做决定」的信息；
 *   toast  = 非模态，几秒后自己消失   —— 「操作做完了」这类顺带反馈。
 * 「已复制 URL」「已保存」用模态框是过重的：它打断用户，却什么都没改变。
 *
 * 用法：
 *   WBUI.toast('已复制 URL')
 *   WBUI.toast('已保存')
 *   WBUI.toast('请先勾选要下载的图片', { type: 'error' })
 *   WBUI.toast('保存失败：名称重复', { type: 'error', duration: 6000 })
 *   WBUI.toast.dismiss(el)          // 手动关掉某一条
 *
 * opts.type      'info'（默认）| 'success' | 'error'
 * opts.duration  毫秒；默认 3000，error 默认 5000；传 0 表示不自动消失
 * opts.dismissible 是否带关闭按钮；默认只有 error 带
 *
 * 无障碍：容器 role="status" + aria-live="polite"；error 单条用 role="alert"，读屏会立刻播报。
 *
 * 为什么移除 DOM 不依赖 transitionend：无头 / 自动化环境可能不产生渲染帧
 * （AGENTS.md 记过这个坑），靠事件收尾会让 toast 永远留在页面上。用固定时长兜底，
 * 动画只是锦上添花 —— 行为在任何环境都可断言。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var HOST_CLASS = 'wb-toasts';
    var MAX = 4;          // 同屏最多几条，超了先挤掉最早的
    var LEAVE_MS = 220;   // 与 ui.css 的退场过渡时长对齐

    function host() {
        var el = document.querySelector('.' + HOST_CLASS);
        if (el) { return el; }
        el = document.createElement('div');
        el.className = HOST_CLASS;
        el.setAttribute('role', 'status');
        el.setAttribute('aria-live', 'polite');
        document.body.appendChild(el);
        return el;
    }

    function dismiss(el) {
        if (!el || el.__wbToastLeaving) { return; }
        el.__wbToastLeaving = true;
        el.classList.add('is-leaving');
        global.setTimeout(function () {
            if (el.parentNode) { el.parentNode.removeChild(el); }
        }, LEAVE_MS);
    }

    function toast(message, opts) {
        opts = opts || {};
        var type = opts.type || 'info';
        var box = host();

        var el = document.createElement('div');
        el.className = 'wb-toast is-' + type;
        el.setAttribute('role', type === 'error' ? 'alert' : 'status');

        var text = document.createElement('span');
        text.className = 'wb-toast-text';
        text.textContent = message == null ? '' : String(message);
        el.appendChild(text);

        var withClose = opts.dismissible != null ? !!opts.dismissible : type === 'error';
        if (withClose) {
            var x = document.createElement('button');
            x.type = 'button';
            x.className = 'wb-toast-x';
            x.setAttribute('aria-label', '关闭提示');
            x.textContent = '×';
            x.addEventListener('click', function () { dismiss(el); });
            el.appendChild(x);
        }

        var live = box.querySelectorAll('.wb-toast');
        if (live.length >= MAX) { dismiss(live[0]); }
        box.appendChild(el);

        var ms = opts.duration != null ? opts.duration : (type === 'error' ? 5000 : 3000);
        if (ms > 0) {
            global.setTimeout(function () { dismiss(el); }, ms);
        }
        return el;
    }

    WBUI.toast = toast;
    WBUI.toast.dismiss = dismiss;

    WBUI.register(function () { /* API 控件：无声明式用法，不需要按元素增强 */ });
})(window);
