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
    //
    // 只用于**带值**的属性（data-close-on-ok="#drawer"、data-write-nav="..."）。
    // 布尔属性要用下方的 declaredFlag —— 它们的值通常是空串，走这里会恒为 falsy。
    function declared(src, attr) {
        if (!src || typeof src.closest !== 'function') return '';
        var host = src.closest('[' + attr + ']');
        return host ? (host.getAttribute(attr) || '') : '';
    }

    // declaredFlag 判**布尔属性是否存在**（data-reload-on-ok、data-write-flash 这类无值属性）。
    //
    // 为什么不能用 declared：`data-reload-on-ok` 在模板里写的是裸属性（没有值），
    // getAttribute 返回空串，`|| ''` 之后仍是 falsy —— 于是「成功后整页刷新」这件事
    // 在 12 处声明上全部失效：删除 / 批量删除 / 调价成功后列表不动，用户以为没生效。
    // 判据必须是 hasAttribute，不是取值。
    function declaredFlag(src, attr) {
        if (!src || typeof src.closest !== 'function') return null;
        return src.closest('[' + attr + ']');
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

    // ── 确认框守卫（与 ui/json_write.js 的 hasConfirm 同一语义）──
    //
    // ui/confirm.js 在**捕获阶段**拦 submit、preventDefault、弹框，确认后临时摘掉
    // data-confirm 再 requestSubmit。但 htmx 4 的事件处理器**不检查 defaultPrevented**
    // （运行时源码里该标识 0 次出现），于是它照样发出请求 —— 而确认后 requestSubmit
    // 会再发一次。实测：点删除按钮、还没点「确定」时就已经发出 1 个 POST，
    // 点确定后再发 1 个 = 一次删除动作两个请求。幂等端点第二次会报「不存在」。
    //
    // 守卫放在 before:request（比发出请求早，比 after:request 早得多）：
    // 表单或本次 submitter 上还有 data-confirm 就说明**确认流程还没走完**，直接拦下。
    // confirm.js 放行时摘掉了那个属性，所以正常路径不受影响。
    function hasPendingConfirm(src) {
        if (!src || typeof src.closest !== 'function') return false;
        var el = src;
        // 按钮级声明：data-confirm 可能在 submitter 上（而不是表单上）。
        if (el.hasAttribute && el.hasAttribute('data-confirm')) return true;
        var form = el.closest && el.closest('form');
        return !!(form && form.hasAttribute && form.hasAttribute('data-confirm'));
    }

    if (typeof window.htmx.on === 'function') {
        window.htmx.on('htmx:before:request', function (evt) {
            var ctx = (evt && evt.detail && evt.detail.ctx) || {};
            var src = ctx.sourceElement;
            if (hasPendingConfirm(src)) {
                // 返回 false 让 htmx 放弃这次请求（v4 的取消协议）。
                evt.preventDefault();
            }
        });
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
        // 两个都是**布尔属性**（模板里写裸属性、没有值），必须按存在性判断。
        var needsReload = declaredFlag(src, 'data-reload-on-ok') ||
            declaredFlag(src, 'data-write-flash');
        if (needsReload) {
            storeFlash(src, messageOf(res));
            window.location.reload();
            return;
        }
        closeTarget(declared(src, 'data-close-on-ok'));
    });

    // ── data-requires-js：无 JS 时不放出来的控件 ──
    //
    // 模板把它们写成 hidden + disabled：无 JS 时表单没有可用端点（写请求走 X-CSRF-Token 头），
    // 所以按钮必须**真的按不动** —— 浏览器对「默认提交按钮 disabled」的表单不做隐式提交，
    // 回车也不会把整表 POST 到一个不存在的页面上。
    //
    // 这段逻辑原先住在 ui/json_write.js 里，是**隐式依赖**：那个文件一旦被删（等所有模块
    // 迁完 htmx 就会删），按钮会永远不可点，而页面上看不出任何报错。搬到本文件后，
    // 它跟着「写动作」这条线走，与写动作是谁提交的无关。
    function enableRequiresJS(scope) {
        var els = (scope || document).querySelectorAll('[data-requires-js]');
        for (var i = 0; i < els.length; i++) {
            var el = els[i];
            if (el.getAttribute('data-wbui-requires-js') === '1') continue;
            el.setAttribute('data-wbui-requires-js', '1');
            el.hidden = false;
            el.removeAttribute('hidden');
            if (el.disabled) el.disabled = false;
        }
    }

    // htmx 换进来的片段里也可能有这类控件：每次 swap 后补一遍。
    if (typeof window.htmx.on === 'function') {
        window.htmx.on('htmx:after:swap', function (evt) {
            var ctx = (evt && evt.detail && evt.detail.ctx) || {};
            enableRequiresJS(ctx.target || document);
        });
    }

    // 页面装载时先把上一次写动作留下的结论落到结果区（读完即删）。
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function () {
            enableRequiresJS(document);
            renderFlash();
        });
    } else {
        enableRequiresJS(document);
        renderFlash();
    }
})();
