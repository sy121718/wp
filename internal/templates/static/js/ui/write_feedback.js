/* ui/write_feedback.js — 写动作的**全局提示与收尾**（htmx 版）。
 *
 * 替代 json_write.js 里那套 outcome 分支：写动作的结论只有一句话 ——
 * 服务端取好词放进 X-Message 头（pkg/response/write.go 统一写），这里 decode 后交给 WBUI.toast。
 *
 * 契约（模板侧全部声明式，不写内联 JS）：
 *
 *   <form hx-post="/api/product/category/create"
 *         hx-swap="none"                         响应不换 DOM（写动作没有要渲染的内容）
 *         hx-disabled-elt="find button[type=submit]"  提交期间禁按钮（防重复提交）
 *         data-close-on-ok="#drawer">             成功之后关掉的容器（可选）
 *
 * 为什么 msg 读头而不是读 body：`hx-swap="none"` 时响应体不会被使用，body 里的 message
 * 前端拿不到；而让前端去 JSON.parse 响应文本等于让前端懂后端契约（{code,message,data}），
 * 那条知识应该留在后端 —— 头是元数据，body 才是数据。
 *
 * 为什么成功后默认**不刷新**：写动作分两类，刷新与否不该由这里猜。
 *   - 需要刷新列表的（父级路径、派生列由服务端重算）显式写 data-reload-on-ok；
 *   - 关掉抽屉就够的（新建/编辑一小块）什么都不写。
 * 猜错的两种代价不对称：该刷新没刷新 → 用户看到旧数据但能用；不该刷新却刷新 →
 * 抽屉被整个换掉、滚动位置与已展开区域全丢。
 */
(function () {
    'use strict';

    if (!window.htmx || typeof window.htmx.on !== 'function') return;

    // 从响应头取提示文案。后端按 QueryEscape 编码（HTTP 头只能是 ASCII）。
    function messageOf(res) {
        if (!res || !res.headers || typeof res.headers.get !== 'function') return '';
        var raw = res.headers.get('X-Message');
        if (!raw) return '';
        try {
            return decodeURIComponent(raw);
        } catch (e) {
            return raw;
        }
    }

    function toast(msg, ok) {
        if (!msg || !window.WBUI || typeof WBUI.toast !== 'function') return;
        WBUI.toast(msg, { type: ok ? 'ok' : 'error', duration: ok ? 3000 : 6000 });
    }

    // 找触发元素上声明的容器选择器（沿 closest 向上找，允许声明在外层包装上）。
    function declared(src, attr) {
        if (!src || typeof src.closest !== 'function') return '';
        var host = src.closest('[' + attr + ']');
        return host ? (host.getAttribute(attr) || '') : '';
    }

    function closeTarget(sel) {
        if (!sel) return;
        var el = document.querySelector(sel);
        if (!el) return;
        // 抽屉与弹窗各有自己的关闭入口；先按 data 属性找，再退回 hidden。
        var closer = el.querySelector('[data-drawer-close], [data-modal-close]');
        if (closer && typeof closer.click === 'function') {
            closer.click();
            return;
        }
        el.hidden = true;
    }

    window.htmx.on('htmx:after:request', function (evt) {
        var ctx = (evt && evt.detail && evt.detail.ctx) || {};
        var res = ctx.response || {};
        var status = Number(res.status) || 0;
        var ok = status >= 200 && status < 400;

        // 只处理声明了写契约的请求：普通 GET 片段（抽屉内容、SEO 面板）不该弹 toast。
        var src = ctx.sourceElement;
        var isWrite = !!(src && typeof src.closest === 'function' &&
            src.closest('[hx-post], [hx-put], [hx-patch], [hx-delete]'));
        if (!isWrite) return;

        toast(messageOf(res), ok);

        if (!ok) return; // 失败什么都不动：抽屉不关、已填内容一个字段都不碰

        var reloadSel = declared(src, 'data-reload-on-ok');
        if (reloadSel) {
            window.location.reload();
            return;
        }
        closeTarget(declared(src, 'data-close-on-ok'));
    });
})();
