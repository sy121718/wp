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
 *         data-write-nav="/admin/products/edit?project=p&id={id}"
 *                                                成功之后跳到这里，{id} 取响应 data.id（可选）
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

    // —— 刷新前的结论暂存 ——
    //
    // 有一类写动作成功后**必须整页刷新**（服务端以库为准重算了结果），而刷新会把
    // toast 一起抹掉 —— 用户什么都没看到。结论因此在 reload 前存进 sessionStorage，
    // 落地后由本文件写到页面上的 [data-write-flash-target="同名"]，读完即删。
    //
    // 键前缀与 ui/json_write.js 保持一致：两套基座共用同一份 flash 契约，
    // 同一张页面上一半表单迁了、一半没迁时，结论不会跑到两个地方去。
    var FLASH_PREFIX = 'json_write_flash:';

    function storeFlash(src, text) {
        var name = src && src.closest && src.closest('[data-write-flash]');
        if (!name) return;
        name = name.getAttribute('data-write-flash');
        if (!name || !text) return;
        try {
            window.sessionStorage.setItem(FLASH_PREFIX + name, text);
        } catch (e) { /* 隐私模式下写不进去：结论丢了，但不能让写操作变红 */ }
    }

    function renderFlash() {
        var targets = document.querySelectorAll('[data-write-flash-target]');
        for (var i = 0; i < targets.length; i++) {
            var box = targets[i];
            var name = box.getAttribute('data-write-flash-target');
            var text = '';
            try {
                text = window.sessionStorage.getItem(FLASH_PREFIX + name) || '';
                window.sessionStorage.removeItem(FLASH_PREFIX + name);
            } catch (e) { text = ''; }
            if (!text) continue;
            box.textContent = '';
            var line = document.createElement('span');
            line.textContent = text;
            box.appendChild(line);
            box.hidden = false;
        }
    }

    // idOf 从响应正文里取 data.id（写动作的成功回执是 {code,message,data:{id:...}}）。
    //
    // 只在这一处解析 JSON：data-write-nav 是唯一需要「拿返回值做下一步」的声明，
    // 其余写动作的结论都走 X-Message 头，不需要碰正文。
    function idOf(text) {
        if (!text) return '';
        var body;
        try { body = JSON.parse(text); } catch (e) { return ''; }
        var data = (body && body.data) || {};
        return data.id == null ? '' : String(data.id);
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

        // data-write-nav：成功后按响应里的 id 跳到目标页（新建 → 编辑页就是这种）。
        // {id} 从响应 JSON 的 data.id 取；取不到时退回刷新 —— 跳一个 id 为空的地址
        // 会让用户看到「商品不存在」，比留在原页更难理解。
        var nav = declared(src, 'data-write-nav');
        if (nav) {
            var id = idOf(ctx.text);
            if (id) {
                window.location.assign(nav.replace('{id}', encodeURIComponent(id)));
                return;
            }
            window.location.reload();
            return;
        }

        // 声明了 data-reload-on-ok 或 data-write-flash 的要刷新：先把结论存起来。
        // data-write-flash 的语义是「刷新后仍要让用户看到那句话」，而 toast 活不过刷新。
        var needsReload = declared(src, 'data-reload-on-ok') ||
            (src && src.closest && src.closest('[data-write-flash]'));
        if (needsReload) {
            storeFlash(src, messageOf(res));
            window.location.reload();
            return;
        }
        closeTarget(declared(src, 'data-close-on-ok'));
    });

    // 页面装载时先把上一次写动作留下的结论落到结果区（读完即删）。
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', renderFlash);
    } else {
        renderFlash();
    }
})();
