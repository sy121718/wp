/* ui/confirm.js — 原始控件：确认框（替代原生 confirm 与内联 onclick/onsubmit）。
 *
 * 用法（声明式，不写内联 JS）：
 *   <form method="post" action="…" data-confirm="确定删除？">…</form>
 *   <button type="submit" form="f1" data-confirm="确认删除？" data-confirm-danger>删除</button>
 *   <a href="/x" data-confirm="确定离开？">…</a>
 *
 * 可选属性：
 *   data-confirm-title   标题（默认「请确认」）
 *   data-confirm-ok      确认按钮文案（默认「确定」）
 *   data-confirm-cancel  取消按钮文案（默认「取消」）
 *   data-confirm-danger  危险操作：确认按钮走危险色
 *
 * 为什么用 <dialog>：showModal() 自带原生模态语义（焦点陷阱、inert 背景、Esc 关闭），
 * 这些自己拿 div 拼要写一堆还容易出错（读屏读不到上下文、Tab 能跑出弹窗）。
 * 内联 onclick/onsubmit 在这里被彻底去掉：模板里只留一个属性。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var dlg = null, msgEl = null, titleEl = null, okBtn = null, cancelBtn = null;
    var pending = null;   // 等待确认的动作

    function build() {
        if (dlg) { return; }
        dlg = document.createElement('dialog');
        dlg.className = 'wb-confirm';
        dlg.setAttribute('role', 'alertdialog');
        dlg.innerHTML =
            '<form method="dialog" class="wb-confirm-inner">' +
            '  <h3 class="wb-confirm-title"></h3>' +
            '  <p class="wb-confirm-msg"></p>' +
            '  <div class="wb-confirm-actions">' +
            '    <button type="button" class="btn wb-confirm-cancel"></button>' +
            '    <button type="submit" class="btn btn-primary wb-confirm-ok" value="ok"></button>' +
            '  </div>' +
            '</form>';
        document.body.appendChild(dlg);
        titleEl = dlg.querySelector('.wb-confirm-title');
        msgEl = dlg.querySelector('.wb-confirm-msg');
        okBtn = dlg.querySelector('.wb-confirm-ok');
        cancelBtn = dlg.querySelector('.wb-confirm-cancel');

        cancelBtn.addEventListener('click', function () { close(false); });
        // Esc 自己处理，不依赖 <dialog> 的原生 Esc：原生 Esc 属于浏览器的 default action，
        // 合成键盘事件（自动化 / 部分嵌入环境）不产生它 —— 实测原生 <dialog> 在 CDP 下按
        // Esc 不关。依赖它就等于「能手动用、测不到、真出问题时没有兜底」。
        dlg.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') { return; }
            e.preventDefault();
            e.stopPropagation();
            close(false);
        });
        dlg.addEventListener('close', function () {
            // Esc 关闭时 returnValue 为空串，等同于取消。
            var confirmed = dlg.returnValue === 'ok';
            var act = pending;
            pending = null;
            if (confirmed && act) { act(); }
        });
    }

    // openDialog 统一入口：声明式（data-confirm）与代码调用（WBUI.confirm / WBUI.alert）
    // 两条路都走这里，视觉与键盘行为不会分叉。
    function openDialog(o) {
        build();
        pending = o.onOk || null;
        // is-alert：只留「确定」，取消按钮靠 CSS 隐去（不依赖 [hidden]，ui.css 不引别的样式表）。
        dlg.classList.toggle('is-alert', !!o.alertOnly);
        titleEl.textContent = o.title || '请确认';
        msgEl.textContent = o.message || '确定执行该操作？';
        okBtn.textContent = o.ok || '确定';
        cancelBtn.textContent = o.cancel || '取消';
        okBtn.classList.toggle('btn-danger', !!o.danger);
        dlg.returnValue = '';
        dlg.showModal();
        okBtn.focus();
    }

    function open(el, run) {
        openDialog({
            title: el.getAttribute('data-confirm-title') || '请确认',
            message: el.getAttribute('data-confirm') || '确定执行该操作？',
            ok: el.getAttribute('data-confirm-ok') || '确定',
            cancel: el.getAttribute('data-confirm-cancel') || '取消',
            danger: el.hasAttribute('data-confirm-danger'),
            onOk: run
        });
    }

    function close(confirmed) {
        if (!dlg) { return; }
        dlg.returnValue = confirmed ? 'ok' : 'cancel';
        dlg.close();
    }

    // 提交表单前的确认。
    document.addEventListener('submit', function (e) {
        var form = e.target;
        if (!form || !form.hasAttribute || !form.hasAttribute('data-confirm')) { return; }
        if (form.dataset.wbConfirmBypass === '1') { return; }   // 已确认放行
        e.preventDefault();
        open(form, function () {
            form.dataset.wbConfirmBypass = '1';
            // requestSubmit 会再次触发 submit 事件，此时被 bypass 放行。
            if (form.requestSubmit) { form.requestSubmit(); } else { form.submit(); }
        });
    }, true);

    // 点击带 data-confirm 的按钮/链接：优先走它自己声明的动作。
    //   - button[form] + type=submit：确认后提交该表单；
    //   - 其他 <a>/<button>：确认后走原生导航/默认行为。
    document.addEventListener('click', function (e) {
        var el = e.target.closest('[data-confirm]');
        if (!el || el.tagName === 'FORM') { return; }
        if (el.dataset.wbConfirmBypass === '1') { el.dataset.wbConfirmBypass = ''; return; }
        e.preventDefault();
        open(el, function () {
            el.dataset.wbConfirmBypass = '1';
            if (el.tagName === 'A' && el.href) { global.location.href = el.href; return; }
            if (el.form && el.form.requestSubmit) { el.form.requestSubmit(el); return; }
            if (el.form) { el.form.submit(); return; }
            el.click();
        });
    }, true);

    // 代码触发的确认（给工作台这类在 JS 里 confirm() 的场景用）：
    //   WBUI.confirm('确认发布？', function () { … }, { title, ok, cancel, danger })
    // 走同一个 <dialog>，视觉与键盘行为与声明式用法完全一致。
    WBUI.confirm = function (message, onOk, opts) {
        opts = opts || {};
        openDialog({
            title: opts.title || '请确认',
            message: message,
            ok: opts.ok || '确定',
            cancel: opts.cancel || '取消',
            danger: !!opts.danger,
            onOk: onOk
        });
    };

    // WBUI.alert(message, opts) —— 只有一个按钮的提示框，替代原生 alert。
    // 原生 alert 会冻结整页、样式不可控、在 iframe/自动化下直接吞掉；后台脚本里
    // （「已复制 URL」「已保存」「请输入分类名称」…）都用它。
    //   opts: { title, ok, danger, onOk }
    WBUI.alert = function (message, opts) {
        opts = opts || {};
        openDialog({
            title: opts.title || '提示',
            message: message,
            ok: opts.ok || '知道了',
            danger: !!opts.danger,
            alertOnly: true,
            onOk: opts.onOk || null
        });
    };

    WBUI.register(function () { /* 声明式控件：无需按元素增强，事件委托已覆盖 */ });
})(window);
