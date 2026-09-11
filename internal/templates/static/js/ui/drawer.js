/* ui/drawer.js — 原始控件：右侧抽屉（后台通用）。
 *
 * 用法（与旧 admin.js 的契约完全一致，页面模板不用改）：
 *   <button data-drawer-open="#tpl-id" data-drawer-title="新建XX">…</button>
 *   <template id="tpl-id"><form>…</form></template>
 *   <aside data-drawer hidden><header>…<button data-drawer-close>✕</button></header>
 *     <div data-drawer-body></div></aside>
 *   <div data-drawer-mask hidden></div>
 *
 * 迁进基座带来的实质改进：**打开时对新插入的内容做一次扫描**。
 * 旧实现在抽屉里塞进新表单就完事，表单里的原生控件（下拉等）得不到增强 ——
 * 用户看到的是抽屉里一套原生控件、页面上另一套。现在统一走 WBUI.scan。
 *
 * 广播 wbui:drawer-open（detail: { body }）：业务侧（如上级菜单候选过滤）可挂载自己的逻辑，
 * 不必把业务规则塞进控件里。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var drawer = null, mask = null, body = null, titleEl = null;

    function openDrawer(tplSel, titleText) {
        var tpl = document.querySelector(tplSel);
        if (!tpl || !drawer) { return; }
        body.innerHTML = '';
        body.appendChild(tpl.content.cloneNode(true));
        if (titleEl) { titleEl.textContent = titleText || ''; }
        drawer.hidden = false;
        mask.hidden = false;
        // 同步加 class：rAF 在后台标签页会无限期挂起，导致抽屉停在屏幕外
        //（hidden 已移除但过渡起始帧不执行）。同步切换牺牲后台页的滑入动画，换 100% 可靠。
        document.body.classList.add('drawer-open');
        // 先扫描再聚焦：扫描会把原生 select 换成自绘触发器，
        // 若先聚焦原生 select，焦点会落在一个视觉隐藏的元素上。
        if (WBUI.scan) { WBUI.scan(body); }
        document.dispatchEvent(new CustomEvent('wbui:drawer-open', { detail: { body: body, template: tplSel } }));
        var first = body.querySelector('input:not([type=hidden]), select, textarea, button');
        if (first) { first.focus(); }
    }

    function closeDrawer() {
        if (!drawer || drawer.hidden) { return; }
        drawer.hidden = true;
        mask.hidden = true;
        document.body.classList.remove('drawer-open');
        body.innerHTML = '';
        document.dispatchEvent(new CustomEvent('wbui:drawer-close'));
    }

    WBUI.register(function (scope) {
        // 抽屉是页面级唯一元素，增强一次即可。
        var el = document.querySelector('[data-drawer]');
        var mk = document.querySelector('[data-drawer-mask]');
        if (!el || !mk) { return; }
        if (!WBUI.markOnce(el, 'Drawer')) { return; }
        drawer = el;
        mask = mk;
        body = drawer.querySelector('[data-drawer-body]');
        titleEl = drawer.querySelector('[data-drawer-title]');
        if (!body) { return; }

        document.addEventListener('click', function (e) {
            var opener = e.target.closest('[data-drawer-open]');
            if (opener) {
                e.preventDefault();
                openDrawer(opener.getAttribute('data-drawer-open'),
                    opener.getAttribute('data-drawer-title') || '');
                return;
            }
            if (e.target.closest('[data-drawer-close]') || e.target === mask) {
                closeDrawer();
            }
        });
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') { closeDrawer(); }
        });
    });

    // 供业务侧主动关闭（如提交成功后）。
    WBUI.closeDrawer = closeDrawer;
})(window);
