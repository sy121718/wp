/* analytics-view.js — 把「当前看哪一维」同步进页面里要带 view 的 GET 表单。
 *
 * 为什么需要：标签切换是纯前端行为（admin.js 的 tabs 基座只改 aria-selected /
 * tabindex / hidden，不改 URL），而工程选择与时间筛选两张表单提交时读的是隐藏域，
 * 于是「切到按路径再改筛选」会带着旧 view 重载，用户被弹回原标签。
 *
 * 为什么订阅事件而不是自己监听点击：analytics-view.js 在 ui_scripts.html 里、
 * 早于 admin.js 加载，自己挂 click 会先于 activate 跑到，读到的还是旧选中态。
 * 基座在 activate 完成后广播 wbui:tabs-change，这里只订阅 —— 顺序由基座保证，
 * 也不需要复制一份键盘选择算法。
 *
 * 视图状态的唯一来源仍是标签自己（aria-controls / id），Go 侧不派生。
 */
(function (global) {
    'use strict';

    var ui = global.WBUI = global.WBUI || {};
    if (ui.analyticsView) { return; }

    var INPUT_SEL = '[data-view-input]';

    /* 从标签取视图名：优先 aria-controls="an-panel-paths"，退化用 id="an-tab-paths"。
       两者都没有返回空串 —— 宁可不写，也不把无关标签的文本当 view 塞进表单。 */
    function viewOf(tab) {
        if (!tab || !tab.getAttribute) { return ''; }
        var m = /-panel-(.+)$/.exec(tab.getAttribute('aria-controls') || '');
        if (m) { return m[1]; }
        m = /-tab-(.+)$/.exec(tab.getAttribute('id') || '');
        return m ? m[1] : '';
    }

    /* 写进所有声明过 data-view-input 的隐藏域，返回写入个数。 */
    function sync(doc, view) {
        if (!doc || !view) { return 0; }
        var inputs = doc.querySelectorAll(INPUT_SEL);
        for (var i = 0; i < inputs.length; i++) { inputs[i].value = view; }
        return inputs.length;
    }

    /* 事件 detail 里直接给 tab / panel，避免再查一次 aria-selected。
       万一没有 detail（老页面或手动派发），退回读当前选中态。 */
    function syncFromEvent(doc, tab) {
        if (!doc || !tab) { return 0; }
        return sync(doc, viewOf(tab));
    }

    function selectedTab(doc) {
        var tabs = doc.querySelectorAll('[data-tabs] [role="tab"]');
        for (var i = 0; i < tabs.length; i++) {
            if (tabs[i].getAttribute('aria-selected') === 'true') { return tabs[i]; }
        }
        return null;
    }

    function bind(doc) {
        if (!doc || !doc.addEventListener) { return false; }
        doc.addEventListener('wbui:tabs-change', function (e) {
            var tab = e && e.detail && e.detail.tab;
            if (tab) { sync(doc, viewOf(tab)); }
        });
        return true;
    }

    ui.analyticsView = { viewOf: viewOf, sync: sync, syncFromEvent: syncFromEvent, selectedTab: selectedTab, bind: bind };

    if (global.document && global.document.querySelectorAll) {
        bind(global.document);
        // 首屏：标签由服务端按 ?view= 选中，这里按 DOM 现状对齐一次。
        var first = selectedTab(global.document);
        if (first) { sync(global.document, viewOf(first)); }
    }
})(typeof window !== 'undefined' ? window : globalThis);
