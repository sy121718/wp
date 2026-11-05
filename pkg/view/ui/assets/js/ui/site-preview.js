/* 编译器输出的预览事实是唯一来源；状态不进入业务请求或作者文档。 */
(function (global) {
    'use strict';
    var mode = global.document.querySelector('meta[name="site-preview-mode"]');
    var active = !!mode && mode.content === 'readonly';
    var marker = global.document.querySelector('meta[name="site-preview-state"]');
    var states = ['default','loading','empty','unauthenticated','validation-error','service-error','submitting','success','out-of-stock','variant-required','forbidden','not-found'];
    var state = active && marker && states.indexOf(marker.content) !== -1 ? marker.content : 'default';
    global.WPSitePreview = Object.freeze({active:active, state:state, states:Object.freeze(states)});
    // 迁移期间残余HTMX读取同样不能把真实访客账号或订单装进模拟画布。
    if (active) global.document.addEventListener('htmx:config:request', function (event) {
        var ctx = event.detail && event.detail.ctx;
        var path = ctx && ctx.request && ctx.request.action;
        if (typeof path !== 'string') return;
        var url;
        try { url = new URL(path, global.location.href); } catch (_) { event.preventDefault(); return; }
        if (url.pathname.indexOf('/api/site/') === 0 || url.pathname.indexOf('/_fragments/') === 0) event.preventDefault();
    });
})(window);
