/* 静态产物的数据读取基座。业务数据只能进入前端视图，接口不携带 HTML。 */
(function (global) {
    'use strict';
    var views = Object.create(null), pending = new Map(), ready = false;
    var selector = '[data-site-read][data-site-view]';

    function endpoint(raw) {
        var url = new URL(raw, global.location.href);
        if (url.origin !== global.location.origin || !url.pathname.startsWith('/api/site/')) throw new Error('无效的数据地址');
        return url.pathname + url.search;
    }
    function request(url) {
        if (pending.has(url)) return pending.get(url);
        var work = global.fetch(url, {credentials:'same-origin', headers:{Accept:'application/json'}, redirect:'error'})
            .then(function (res) {
                return res.json().then(function (body) {
                    if (!res.ok || !body || (body.code !== 0 && body.code !== 200)) throw new Error(body && body.message || '数据读取失败');
                    return body.data;
                });
            });
        pending.set(url, work);
        work.finally(function () { if (pending.get(url) === work) pending.delete(url); }).catch(function () {});
        return work;
    }
    function refresh(el) {
        var render = views[el.getAttribute('data-site-view')];
        if (!render || !el.isConnected) return Promise.resolve();
        var generation = (el._siteGeneration || 0) + 1;
        el._siteGeneration = generation;
        el.setAttribute('aria-busy', 'true');
        return Promise.resolve().then(function () { return request(endpoint(el.getAttribute('data-site-read'))); })
            .then(function (data) {
                if (!el.isConnected || el._siteGeneration !== generation) return;
                render(el, data);
                el.removeAttribute('data-site-error');
                el.dispatchEvent(new CustomEvent('site:data', {bubbles:true, detail:{data:data}}));
            }).catch(function (err) {
                if (!el.isConnected || el._siteGeneration !== generation) return;
                // 保留上一次内容和用户输入，错误由可见提示或页面监听器接收。
                el.setAttribute('data-site-error', 'true');
                var status = el.querySelector('[data-site-status]');
                if (status) status.textContent = err.message;
                el.dispatchEvent(new CustomEvent('site:data:error', {bubbles:true, detail:{message:err.message}}));
            }).finally(function () {
                if (el._siteGeneration === generation) el.setAttribute('aria-busy', 'false');
            });
    }
    function mount(el) {
        if (el._siteMounted || !views[el.getAttribute('data-site-view')]) return;
        el._siteMounted = true;
        refresh(el);
        var seconds = Number(el.getAttribute('data-site-poll'));
        if (seconds >= 30) {
            function poll() {
                if (!el.isConnected) return;
                if (global.document.visibilityState !== 'hidden') refresh(el);
                global.setTimeout(poll, seconds * 1000);
            }
            global.setTimeout(poll, seconds * 1000);
        }
    }
    function scan(root) {
        if (root.matches && root.matches(selector)) mount(root);
        root.querySelectorAll(selector).forEach(mount);
    }
    function start() {
        ready = true;
        scan(global.document);
        new MutationObserver(function (changes) {
            changes.forEach(function (change) { change.addedNodes.forEach(function (node) { if (node.nodeType === 1) scan(node); }); });
        }).observe(global.document.documentElement, {childList:true, subtree:true});
    }
    global.WPSiteData = {
        register:function (name, render) { views[name] = render; if (ready) scan(global.document); },
        refresh:refresh
    };
    if (global.document.readyState === 'loading') global.document.addEventListener('DOMContentLoaded', start, {once:true});
    else Promise.resolve().then(start);
})(window);
