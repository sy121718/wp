/* 静态产物的数据读取基座。业务数据只能进入前端视图，接口不携带 HTML。 */
(function (global) {
    'use strict';
    var views = Object.create(null), preparations = Object.create(null), previews = Object.create(null), pending = new Map(), ready = false;
    var selector = '[data-site-read][data-site-view]';

    function endpoint(raw) {
        var url = new URL(raw, global.location.href);
        if (url.origin !== global.location.origin || !url.pathname.startsWith('/api/site/')) throw new Error('无效的数据地址');
        return url.pathname + url.search;
    }
    function readError(message, status) {
        var error = new Error(message);
        error.status = Number(status) || 0;
        return error;
    }
    function request(url) {
        if (pending.has(url)) return pending.get(url);
        var work = global.fetch(url, {credentials:'same-origin', headers:{Accept:'application/json'}, redirect:'error'})
            .then(function (res) {
                var csrf = res.headers && res.headers.get('X-CSRF-Token');
                if (csrf) { global.WPSiteSession = global.WPSiteSession || {}; global.WPSiteSession.csrfToken = csrf; }
                return res.json().catch(function () { throw readError('数据读取失败', res.status); }).then(function (body) {
                    if (!res.ok || !body || (body.code !== 0 && body.code !== 200)) throw readError(body && body.message || '数据读取失败', res.status);
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
        return Promise.resolve().then(function () {
                var name = el.getAttribute('data-site-view');
                if (global.WPSitePreview && global.WPSitePreview.active) {
                    el.setAttribute('data-site-simulated', 'true');
                    var preview = previews[name];
                    if (preview) {
                        var state = global.WPSitePreview.state;
                        if (preview.states && preview.states.indexOf(state) === -1) state = 'default';
                        preview.render(el, state);
                    }
                    return null;
                }
                var prepare = preparations[name];
                if (prepare) prepare(el);
                return request(endpoint(el.getAttribute('data-site-read')));
            })
            .then(function (data) {
                if (!el.isConnected || el._siteGeneration !== generation) return;
                if (global.WPSitePreview && global.WPSitePreview.active) return;
                render(el, data);
                el.removeAttribute('data-site-error');
                el.dispatchEvent(new CustomEvent('site:data', {bubbles:true, detail:{data:data}}));
            }).catch(function (err) {
                if (!el.isConnected || el._siteGeneration !== generation) return;
                // 保留上一次内容和用户输入，错误由可见提示或页面监听器接收。
                el.setAttribute('data-site-error', 'true');
                var status = el.querySelector('[data-site-status]');
                if (status) status.textContent = err.message;
                el.dispatchEvent(new CustomEvent('site:data:error', {bubbles:true, detail:{message:err.message,status:Number(err.status)||0}}));
            }).finally(function () {
                if (el._siteGeneration === generation) el.setAttribute('aria-busy', 'false');
            });
    }
    function mount(el) {
        if (el._siteMounted || !views[el.getAttribute('data-site-view')]) return;
        el._siteMounted = true;
        refresh(el);
        var seconds = Number(el.getAttribute('data-site-poll'));
        if (seconds >= 30 && !(global.WPSitePreview && global.WPSitePreview.active)) {
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
        register:function (name, render, prepare) { views[name] = render; preparations[name] = prepare; if (ready) scan(global.document); },
        registerPreview:function (name, render, states) {
            previews[name] = {render:render, states:states};
            if (ready && global.WPSitePreview && global.WPSitePreview.active) {
                var roots=global.document.querySelectorAll(selector);
                roots.forEach(function(root){ if(root.getAttribute('data-site-view')===name) refresh(root); });
            }
        },
        refresh:refresh
    };
    if (global.document.readyState === 'loading') global.document.addEventListener('DOMContentLoaded', start, {once:true});
    else Promise.resolve().then(start);
})(window);
