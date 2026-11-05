/* 构建预览成功后才展示HTML；业务错误保留在发起页面，不把JSON当作预览文档。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    var requests = new WeakMap();
    function status(host, message) {
        var container = host.matches('form') ? (host.closest('[data-write-host]') || host.parentElement) : host;
        var node = container.querySelector('[data-product-preview-status]');
        if (!node) {
            node = document.createElement('p'); node.className = 'form-hint';
            node.setAttribute('data-product-preview-status', ''); node.setAttribute('role', 'status');
            container.appendChild(node);
        }
        node.textContent = message || '';
        node.hidden = !message;
    }
    function normalizeDocument(html, url) {
        var parsed = new DOMParser().parseFromString(html, 'text/html');
        if (!parsed.head.querySelector('base')) {
            var base = parsed.createElement('base');
            base.href = new URL('/', url || global.location.href).href;
            parsed.head.prepend(base);
        }
        return '<!doctype html>\n' + parsed.documentElement.outerHTML;
    }
    async function readDocument(url, options, failed) {
        var post = options && options.method === 'POST';
        var response = await fetch(url, Object.assign({credentials: 'same-origin', redirect: 'error', headers: {'Accept': post ? 'application/json' : 'text/html, application/json'}}, options));
        var type = response.headers.get('content-type') || '';
        if (type.includes('application/json')) {
            var payload = await response.json();
            if (post && response.ok) {
                if (!payload || typeof payload.code !== 'number') throw new Error(failed);
                if (payload.code === 200) {
                    var html = payload.data && payload.data.html;
                    if (typeof html !== 'string' || !html.trim()) throw new Error(failed);
                    return normalizeDocument(html, response.url || new URL(url, global.location.href).href);
                }
            }
            var failure = new Error(payload && payload.message || failed);
            failure.productPreviewMessage = true;
            throw failure;
        }
        if (post || !response.ok || !type.includes('text/html')) throw new Error(failed);
        return normalizeDocument(await response.text(), response.url || new URL(url, global.location.href).href);
    }
    function openWindow() {
        var popup = global.open('', '_blank');
        if (popup) popup.opener = null;
        return popup;
    }
    function showDocument(popup, html, title) {
        if (!popup || popup.closed) return false;
        var doc = popup.document;
        doc.title = title || document.title;
        doc.body.replaceChildren(); doc.body.style.margin = '0';
        var frame = doc.createElement('iframe');
        frame.setAttribute('sandbox', 'allow-scripts'); frame.title = doc.title;
        frame.style.width = '100vw'; frame.style.height = '100vh'; frame.style.border = '0';
        frame.srcdoc = html;
        doc.body.appendChild(frame);
        return true;
    }
    function errorMessage(error, host) {
        return error.productPreviewMessage ? error.message : host.getAttribute('data-preview-failed');
    }
    async function loadFrame(host, popup) {
        var previous = requests.get(host);
        if (previous) previous.abort();
        var controller = new AbortController();
        requests.set(host, controller);
        status(host, ''); host.setAttribute('aria-busy', 'true');
        var frame = host.querySelector('[data-product-preview-frame]');
        try {
            var html = await readDocument(host.getAttribute('data-preview-url'), {signal: controller.signal}, host.getAttribute('data-preview-failed'));
            if (requests.get(host) !== controller || !host.isConnected) { if (popup) popup.close(); return; }
            frame.srcdoc = html;
            frame.hidden = false;
            if (popup && !showDocument(popup, html, frame.title)) status(host, host.getAttribute('data-preview-failed'));
        } catch (error) {
            if (popup && !popup.closed) popup.close();
            if (error.name !== 'AbortError' && requests.get(host) === controller) {
                frame.hidden = true;
                frame.removeAttribute('srcdoc');
                status(host, errorMessage(error, host));
            }
        } finally {
            if (requests.get(host) === controller) host.removeAttribute('aria-busy');
        }
    }
    document.addEventListener('click', function (event) {
        var trigger = event.target.closest('[data-product-preview-refresh], [data-product-preview-open]');
        if (!trigger) return;
        var host = trigger.closest('[data-product-preview]');
        if (!host) return;
        var popup = trigger.hasAttribute('data-product-preview-open') ? openWindow() : null;
        if (trigger.hasAttribute('data-product-preview-open') && !popup) { status(host, host.getAttribute('data-preview-failed')); return; }
        loadFrame(host, popup);
    });
    document.addEventListener('submit', async function (event) {
        var form = event.target;
        if (!form.matches('[data-product-preview-form]')) return;
        event.preventDefault();
        if (requests.has(form)) return;
        var popup = openWindow();
        if (!popup) { status(form, form.getAttribute('data-preview-failed')); return; }
        var controller = new AbortController();
        requests.set(form, controller);
        var buttons = Array.from(form.elements).filter(function (element) { return element.type === 'submit' && !element.disabled; });
        buttons.forEach(function (button) { button.disabled = true; });
        status(form, '');
        try {
            var html = await readDocument(form.action, {method: 'POST', body: new FormData(form), signal: controller.signal}, form.getAttribute('data-preview-failed'));
            if (!showDocument(popup, html, document.title)) status(form, form.getAttribute('data-preview-failed'));
        } catch (error) {
            if (!popup.closed) popup.close();
            status(form, errorMessage(error, form));
        } finally {
            requests.delete(form);
            buttons.forEach(function (button) { button.disabled = false; });
        }
    });
    ui.productPreview = {readDocument: readDocument, showDocument: showDocument, openWindow: openWindow};
    ui.register(function (scope) {
        ui.$$('[data-product-preview]', scope).forEach(function (host) {
            if (ui.markOnce(host, 'ProductPreview') && host.querySelector('[data-product-preview-frame]')) loadFrame(host);
        });
    });
})(window);
