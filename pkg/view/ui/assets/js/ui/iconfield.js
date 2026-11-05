/* ui/iconfield.js — 原始控件：图标选择器 + 列表图标渲染。
 *
 * 两部分职责（都依赖懒加载的图标库 /static/js/icons.js，766KB，只在用到时才拉）：
 *   1. 表单里的 <div data-icon-field></div> + <input type="hidden" name="icon">
 *      → 挂一个 WPIcons.picker（可不选，清除即空）；
 *   2. 列表里的 <span data-icon-name="xxx"> → 就位后填入 SVG。
 *
 * 迁进基座的实质改进：列表图标从「页面加载时跑一次」改为**随扫描执行**。
 * 旧实现在 htmx 局部替换后新出现的图标列不会被渲染（永远是空的），
 * 现在 WBUI.scan 覆盖到哪儿就渲染到哪儿。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    // 图标库懒加载：状态 0=未开始 1=加载中 2=就绪 3=失败
    var libState = 0;
    var libCbs = [];

    function ensureIconLib(cb) {
        if (global.WPIcons && global.WPIcons.picker) { cb(); return; }
        if (libState === 3) { return; }
        libCbs.push(cb);
        if (libState === 1) { return; }
        libState = 1;
        var s = document.createElement('script');
        s.src = '/static/js/icons.js';
        s.onload = function () {
            libState = (global.WPIcons && global.WPIcons.picker) ? 2 : 3;
            libCbs.splice(0).forEach(function (fn) { try { fn(); } catch (e) {} });
        };
        s.onerror = function () { libState = 3; libCbs.length = 0; };
        document.head.appendChild(s);
    }

    // 列表图标列：扫描 [data-icon-name]，图标库就绪后填充 SVG。
    function renderListIcons(scope) {
        var cells = (scope || document).querySelectorAll('[data-icon-name]');
        if (!cells.length) { return; }
        ensureIconLib(function () {
            WBUI.each(cells, function (el) {
                if (el.dataset.iconDone === '1') { return; }
                var name = el.getAttribute('data-icon-name');
                if (name && global.WPIcons && global.WPIcons.svg(name)) {
                    el.innerHTML = global.WPIcons.svg(name);
                    el.title = global.WPIcons.label(name);
                    el.classList.add('has-icon');
                } else if (name) {
                    el.classList.add('is-unknown');
                    el.title = name + '（图标库无此项）';
                } else {
                    el.classList.add('is-empty');
                }
                el.dataset.iconDone = '1';
            });
        });
    }

    // 表单图标字段：挂 WPIcons.picker。
    function initIconFields(scope) {
        var hosts = (scope || document).querySelectorAll('[data-icon-field]');
        if (!hosts.length) { return; }
        ensureIconLib(function () {
            if (!global.WPIcons || !global.WPIcons.picker) { return; }
            WBUI.each(hosts, function (host) {
                if (host.dataset.iconReady === '1') { return; }   // 防重复挂载
                var wrap = host.closest('.form-group') || host.parentElement;
                var hidden = wrap ? wrap.querySelector('input[name="icon"]') : null;
                var picker = global.WPIcons.picker({
                    value: hidden ? hidden.value : '',
                    placeholder: '选择图标（可不选）',
                    onPick: function (name) { if (hidden) { hidden.value = name || ''; } }
                });
                host.appendChild(picker.el);
                host.dataset.iconReady = '1';
            });
        });
    }

    WBUI.register(function (scope) {
        renderListIcons(scope);
        initIconFields(scope);
    });
})(window);
