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
    var returnFocus = null, inertSiblings = [];
    var activeRequest = null, generation = 0, retryURL = null;

    function cancelRequest() {
        generation++;
        if (activeRequest) { activeRequest.abort(); activeRequest = null; }
    }

    function showState(message, role, retry) {
        body.innerHTML = '';
        body.removeAttribute('aria-busy');
        var state = document.createElement('div');
        state.setAttribute('role', role);
        state.textContent = message;
        if (retry) {
            var button = document.createElement('button');
            button.type = 'button';
            button.setAttribute('data-drawer-retry', '');
            button.textContent = '重试';
            state.appendChild(button);
        }
        body.appendChild(state);
        return state;
    }

    function sameOriginAttribute(value) {
        try {
            var url = new URL(value, global.location.href);
            return /^https?:$/.test(url.protocol) && url.origin === new URL(global.location.href).origin &&
                !url.username && !url.password;
        } catch (err) { return false; }
    }

    function fragmentRoot(html) {
        // Only a single trusted same-origin HTML fragment with a form may enter the live DOM.
        if (/<\s*(?:!doctype|html|head|body|script|style|link|meta|base|iframe|object|embed|svg|math|template|noscript)\b/i.test(html)) {
            throw new Error('active or document markup');
        }
        var parsed = new DOMParser().parseFromString(html, 'text/html');
        if (parsed.body.children.length !== 1) {
            throw new Error('fragment root');
        }
        var root = parsed.body.children[0];
        if (!root.matches('[data-drawer-fragment]') ||
            !(root.matches('form') || root.querySelector('form'))) {
            throw new Error('missing form fragment');
        }
        var nodes = [root].concat(Array.prototype.slice.call(root.querySelectorAll('*')));
        nodes.forEach(function (node) {
            if (/^(script|style|link|meta|base|iframe|object|embed|svg|math|template)$/i.test(node.tagName)) {
                throw new Error('active markup');
            }
            Array.prototype.forEach.call(node.attributes || [], function (attr) {
                if (/^on/i.test(attr.name) || /^(srcdoc|formaction|is)$/i.test(attr.name) ||
                    (/^(href|src|action|xlink:href|hx-(?:get|post|put|patch|delete))$/i.test(attr.name) &&
                        !sameOriginAttribute(attr.value))) {
                    throw new Error('active attribute');
                }
            });
        });
        return root;
    }

    function loadDrawer(urlText) {
        cancelRequest();
        var current = generation;
        var url;
        try {
            url = new URL(urlText, global.location.href);
            if (!/^https?:$/.test(url.protocol) || url.origin !== new URL(global.location.href).origin ||
                url.username || url.password) { throw new Error('cross-origin URL'); }
        } catch (err) {
            showState('编辑表单加载失败，请重试。', 'alert', true);
            body.querySelector('[data-drawer-retry]').focus();
            return;
        }
        var status = showState('正在加载编辑表单…', 'status', false);
        body.setAttribute('aria-busy', 'true');
        status.setAttribute('aria-live', 'polite');
        drawer.focus();
        var controller = new AbortController();
        activeRequest = controller;
        fetch(url.href, { method: 'GET', cache: 'no-store', credentials: 'same-origin', signal: controller.signal,
            headers: { Accept: 'text/html' } })
            .then(function (response) {
                if (response.status !== 200 || !/^text\/html(?:\s*;|\s*$)/i.test(response.headers.get('Content-Type') || '')) {
                    throw new Error('unexpected response');
                }
                return response.text();
            })
            .then(function (html) {
                if (current !== generation || drawer.hidden) { return; }
                var root = fragmentRoot(html);
                body.innerHTML = '';
                body.removeAttribute('aria-busy');
                body.appendChild(root);
                activateDrawer(null);
            })
            .catch(function () {
                if (current !== generation || drawer.hidden) { return; }
                showState('编辑表单加载失败，请重试。', 'alert', true);
                var retry = body.querySelector('[data-drawer-retry]');
                if (retry) { retry.focus(); }
            })
            .finally(function () { if (current === generation) { activeRequest = null; } });
    }

    function focusableElements() {
        return Array.prototype.filter.call(drawer.querySelectorAll(
            'a[href], button, input:not([type=hidden]), select, textarea, [tabindex], [contenteditable=true]'
        ), function (el) {
            return !el.disabled && el.tabIndex >= 0 && !el.closest('[hidden], [inert]') && el.getClientRects().length > 0;
        });
    }

    function openDrawer(tplSel, titleText, url) {
        var remote = url !== null;
        var tpl = !remote && tplSel && document.querySelector(tplSel);
        if ((!tpl && !remote) || !drawer) { return; }
        if (drawer.hidden) { returnFocus = document.activeElement; }
        retryURL = remote ? url : null;
        cancelRequest();
        body.innerHTML = '';
        body.removeAttribute('aria-busy');
        if (tpl) {
            body.appendChild(tpl.content.cloneNode(true));
            // Preserve the template path's original order: process before reveal.
            if (global.htmx && typeof global.htmx.process === 'function') { global.htmx.process(body); }
        }
        if (titleEl) { titleEl.textContent = titleText || ''; }
        var wasHidden = drawer.hidden;
        drawer.hidden = false;
        drawer.setAttribute('aria-hidden', 'false');
        // 背景不可交互；只恢复本控件设置的 inert，不覆盖调用方原有状态。
        if (wasHidden) {
            Array.prototype.forEach.call(drawer.parentElement.children, function (el) {
                if (el !== drawer && el !== mask && !el.inert && !el.matches('script, style, link, template')) {
                    el.inert = true;
                    inertSiblings.push(el);
                }
            });
        }
        mask.hidden = false;
        document.body.classList.add('drawer-open');
        if (remote) { loadDrawer(url); return; }
        activateDrawer(tplSel, true);
    }

    function activateDrawer(tplSel, processed) {
        // htmx 只扫描「首次加载的文档」与「它自己换进来的片段」——<template> 里的内容
        // 在克隆进 DOM 之前根本不在文档里，克隆本身不触发扫描。不在这里补一次 process，
        // 抽屉里所有 hx-* 指令（hx-post / hx-include / hx-target）都是死的：
        // 按钮点下去毫无反应（「+ 添加值」点不动就是这么来的），而页面上的同类按钮却正常。
        // 顺序：先 process（认领 hx-* 属性）再 scan（增强原生控件），两者互不依赖。
        if (!processed && global.htmx && typeof global.htmx.process === 'function') {
            global.htmx.process(body);
        }
        // 先扫描再聚焦：扫描会把原生 select 换成自绘触发器，
        // 若先聚焦原生 select，焦点会落在一个视觉隐藏的元素上。
        if (WBUI.scan) { WBUI.scan(body); }
        document.dispatchEvent(new CustomEvent('wbui:drawer-open', { detail: { body: body, template: tplSel } }));
        var fields = focusableElements();
        var first = fields.find(function (el) { return body.contains(el); });
        (first || drawer).focus();
    }

    function closeDrawer() {
        cancelRequest();
        retryURL = null;
        if (!drawer || drawer.hidden) { return; }
        inertSiblings.forEach(function (el) { el.inert = false; });
        inertSiblings = [];
        if (returnFocus && returnFocus.isConnected) { returnFocus.focus(); }
        returnFocus = null;
        drawer.hidden = true;
        drawer.setAttribute('aria-hidden', 'true');
        mask.hidden = true;
        document.body.classList.remove('drawer-open');
        body.innerHTML = '';
        body.removeAttribute('aria-busy');
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
        drawer.setAttribute('role', 'dialog');
        drawer.setAttribute('aria-modal', 'true');
        drawer.setAttribute('tabindex', '-1');
        if (titleEl) {
            if (!titleEl.id) { titleEl.id = 'wbui-drawer-title'; }
            drawer.setAttribute('aria-labelledby', titleEl.id);
        }

        document.addEventListener('click', function (e) {
            var opener = e.target.closest('[data-drawer-open], [data-drawer-url]');
            if (opener) {
                e.preventDefault();
                openDrawer(opener.getAttribute('data-drawer-open'),
                    opener.getAttribute('data-drawer-title') || '', opener.getAttribute('data-drawer-url'));
                return;
            }
            if (e.target.closest('[data-drawer-retry]')) {
                e.preventDefault();
                if (retryURL !== null) { loadDrawer(retryURL); }
                return;
            }
            if (e.target.closest('[data-drawer-close]') || e.target === mask) {
                closeDrawer();
            }
        });
        document.addEventListener('keydown', function (e) {
            if (drawer.hidden || e.defaultPrevented) { return; }
            if (e.key === 'Escape') { e.preventDefault(); closeDrawer(); return; }
            if (e.key !== 'Tab') { return; }
            var fields = focusableElements();
            var first = fields[0], last = fields[fields.length - 1];
            if (!first) { e.preventDefault(); drawer.focus(); return; }
            if (e.shiftKey && (document.activeElement === first || !fields.includes(document.activeElement))) {
                e.preventDefault(); last.focus();
            } else if (!e.shiftKey && (document.activeElement === last || !fields.includes(document.activeElement))) {
                e.preventDefault(); first.focus();
            }
        });
    });

    // 供业务侧主动关闭（如提交成功后）。
    WBUI.closeDrawer = closeDrawer;
})(window);
