/* ui/themetoggle.js — 原始控件：明暗主题切换（后台顶栏）。
 *
 * 用法：<button data-theme-toggle>◐</button>
 *
 * 原先是 <button onclick="…三行内联 JS…">，迁进基座后模板里只剩一个属性，
 * 并补上 aria-pressed —— 切换按钮的当前状态对读屏用户必须可读（原来只有 title）。
 *
 * 注意：<head> 里那段「CSS 加载前读 localStorage 设 data-theme」的防闪烁脚本**不迁**。
 * 它必须在样式表之前同步执行，放到基座（body 末尾脚本）会让页面先亮后暗闪一下。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var KEY = 'theme';

    function current() {
        return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
    }

    function apply(theme) {
        document.documentElement.setAttribute('data-theme', theme);
        try { global.localStorage.setItem(KEY, theme); } catch (e) { /* 隐私模式下写不了，忽略 */ }
        WBUI.each(document.querySelectorAll('[data-theme-toggle]'), function (btn) {
            btn.setAttribute('aria-pressed', theme === 'dark' ? 'true' : 'false');
        });
    }

    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('[data-theme-toggle]', scope), function (btn) {
            if (!WBUI.markOnce(btn, 'ThemeToggle')) { return; }
            btn.setAttribute('aria-pressed', current() === 'dark' ? 'true' : 'false');
            btn.addEventListener('click', function () {
                apply(current() === 'dark' ? 'light' : 'dark');
            });
        });
    });

    // 供其他控件/业务复用（如需要跟随后台主题的组件）。
    WBUI.theme = { current: current, apply: apply };
})(window);
