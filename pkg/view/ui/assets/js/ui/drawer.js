/* ui/drawer.js — 抽屉开合、焦点与内容生命周期，不负责网络或 JSON 展示。
 * 本地：data-drawer-open="#template"；远程：data-drawer-open + hx-get + hx-target。
 * Jet 提供外壳、错误区和片段；远程请求与交换统一交给 HTMX。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    var drawer = null, mask = null, body = null, titleEl = null;
    var returnFocus = null, inertSiblings = [];

    function resetBody() {
        // 每次打开和关闭都换掉目标实例，旧 GET 即使返回也不能命中新一轮抽屉。
        var next = body.cloneNode(false);
        next.removeAttribute('aria-busy');
        body.replaceWith(next);
        body = next;
        var status = drawer.querySelector('[data-write-status]');
        if (status) {
            status.replaceChildren();
            status.hidden = true;
            status.removeAttribute('data-tone');
        }
    }

    function focusableElements() {
        return Array.prototype.filter.call(drawer.querySelectorAll(
            'a[href], button, input:not([type=hidden]), select, textarea, [tabindex], [contenteditable=true]'
        ), function (el) {
            return !el.disabled && el.tabIndex >= 0 && !el.closest('[hidden], [inert]') && el.getClientRects().length > 0;
        });
    }

    function requestHtmxScan(el) {
        document.dispatchEvent(new CustomEvent('wb:rescan', { detail: { el: el } }));
    }

    function activateDrawer(tplSel) {
        // 先增强再聚焦，避免焦点留在视觉隐藏的原生 select 上。
        if (WBUI.scan) WBUI.scan(body);
        document.dispatchEvent(new CustomEvent('wbui:drawer-open', { detail: { body: body, template: tplSel } }));
        var first = focusableElements().find(function (el) { return body.contains(el); });
        (first || drawer).focus();
    }

    function openDrawer(opener) {
        var remote = opener.hasAttribute('hx-get');
        var tplSel = opener.getAttribute('data-drawer-open');
        var tpl = !remote && tplSel && document.querySelector(tplSel);
        if (!tpl && !remote) return;
        var wasHidden = drawer.hidden;
        if (wasHidden) returnFocus = document.activeElement;
        resetBody();
        if (titleEl) titleEl.textContent = opener.getAttribute('data-drawer-title') || '';
        drawer.hidden = false;
        drawer.setAttribute('aria-hidden', 'false');
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
        if (remote) {
            drawer.focus();
        } else {
            body.appendChild(tpl.content.cloneNode(true));
            requestHtmxScan(body);
            activateDrawer(tplSel);
        }
    }

    function closeDrawer() {
        if (!drawer || drawer.hidden) return;
        resetBody();
        inertSiblings.forEach(function (el) { el.inert = false; });
        inertSiblings = [];
        if (returnFocus && returnFocus.isConnected) returnFocus.focus();
        returnFocus = null;
        drawer.hidden = true;
        drawer.setAttribute('aria-hidden', 'true');
        mask.hidden = true;
        document.body.classList.remove('drawer-open');
        document.dispatchEvent(new CustomEvent('wbui:drawer-close'));
    }

    WBUI.register(function () {
        var el = document.querySelector('[data-drawer]');
        var mk = document.querySelector('[data-drawer-mask]');
        if (!el || !mk || !el.querySelector('[data-drawer-body]')) return;
        if (!WBUI.markOnce(el, 'Drawer')) return;
        drawer = el;
        mask = mk;
        body = drawer.querySelector('[data-drawer-body]');
        titleEl = drawer.querySelector('[data-drawer-title]');
        drawer.setAttribute('role', 'dialog');
        drawer.setAttribute('aria-modal', 'true');
        drawer.setAttribute('tabindex', '-1');
        if (titleEl) {
            if (!titleEl.id) titleEl.id = 'wbui-drawer-title';
            drawer.setAttribute('aria-labelledby', titleEl.id);
        }

        // 捕获阶段先准备新目标；HTMX 随后的点击处理才解析 hx-target。
        document.addEventListener('click', function (e) {
            var opener = e.target.closest('[data-drawer-open]');
            if (opener) {
                if (!opener.hasAttribute('hx-get')) e.preventDefault();
                openDrawer(opener);
            }
        }, true);
        document.addEventListener('click', function (e) {
            if (e.target.closest('[data-drawer-close]') || e.target === mask) {
                e.preventDefault();
                closeDrawer();
            }
        });
        document.addEventListener('wb:content-ready', function (e) {
            if (!drawer.hidden && e.detail.el === body) activateDrawer(null);
        });
        document.addEventListener('keydown', function (e) {
            if (drawer.hidden || e.defaultPrevented) return;
            if (e.key === 'Escape') { e.preventDefault(); closeDrawer(); return; }
            if (e.key !== 'Tab') return;
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

    WBUI.closeDrawer = closeDrawer;
})(window);
