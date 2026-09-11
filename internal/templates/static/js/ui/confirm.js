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
        dlg.addEventListener('close', function () {
            // Esc 关闭时 returnValue 为空串，等同于取消。
            var confirmed = dlg.returnValue === 'ok';
            var act = pending;
            pending = null;
            if (confirmed && act) { act(); }
        });
    }

    function open(el, run) {
        build();
        pending = run;
        titleEl.textContent = el.getAttribute('data-confirm-title') || '请确认';
        msgEl.textContent = el.getAttribute('data-confirm') || '确定执行该操作？';
        okBtn.textContent = el.getAttribute('data-confirm-ok') || '确定';
        cancelBtn.textContent = el.getAttribute('data-confirm-cancel') || '取消';
        okBtn.classList.toggle('btn-danger', el.hasAttribute('data-confirm-danger'));
        dlg.returnValue = '';
        dlg.showModal();
        okBtn.focus();
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

    WBUI.register(function () { /* 声明式控件：无需按元素增强，事件委托已覆盖 */ });
})(window);
