/* 只执行构建器已解析的动作，不读取AST、CSS选择器或推断全站路径。 */
(function (global) {
    'use strict';
    function owned(root, selector, attribute, value) {
        return Array.from(root.querySelectorAll(selector)).filter(function (el) {
            return el.getAttribute(attribute) === value && el.closest('[data-component-instance]') === root;
        });
    }
    function run(source, key) {
        var root = source && source.closest && source.closest('[data-component-instance]');
        if (!root || !key) return false;
        var controls = owned(root, '[data-component-action]', 'data-component-action', key);
        if (controls.length !== 1) return false;
        var control = controls[0], kind = control.getAttribute('data-action-kind');
        if (kind === 'page' || kind === 'presentation' || kind === 'external') {
            var url;
            try { url = new URL(control.getAttribute('data-action-url') || '', global.location.href); } catch (_) { return false; }
            if (!control.getAttribute('data-action-url') || !/^https?:$/.test(url.protocol) || url.username || url.password) return false;
            if (kind !== 'external' && url.origin !== global.location.origin) return false;
            if (kind === 'external' && control.getAttribute('data-action-new-tab') === 'true') global.open(url.href, '_blank', 'noopener,noreferrer');
            else global.location.assign(url.href);
            return true;
        }
        if (kind !== 'local' && kind !== 'instance') return false;
        var instance = control.getAttribute('data-action-instance'), partKey = control.getAttribute('data-action-part');
        if (!instance || !partKey || (kind === 'local' && instance !== root.getAttribute('data-component-instance'))) return false;
        var targets = Array.from(global.document.querySelectorAll('[data-component-instance]')).filter(function (node) {
            return node.getAttribute('data-component-instance') === instance;
        });
        if (targets.length !== 1) return false;
        var target = targets[0], parts = owned(target, '[data-component-part]', 'data-component-part', partKey);
        if (parts.length !== 1) return false;
        var part = parts[0];
        part.hidden = false;
        if (!part.hasAttribute('tabindex')) part.setAttribute('tabindex', '-1');
        if (part.scrollIntoView) part.scrollIntoView({block:'nearest', behavior:'auto'});
        if (part.focus) part.focus({preventScroll:true});
        target.dispatchEvent(new CustomEvent('site:action', {bubbles:true, detail:{key:key, kind:kind, part:partKey, instanceId:instance, sourceInstanceId:root.getAttribute('data-component-instance')}}));
        return true;
    }
    global.WPSiteActions = Object.freeze({run:run});
    global.document.addEventListener('click', function (event) {
        if (event.defaultPrevented || event.button > 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
        var control = event.target.closest && event.target.closest('[data-component-action]');
        if (!control || control.disabled || control.getAttribute('aria-disabled') === 'true') return;
        if (run(control, control.getAttribute('data-component-action'))) event.preventDefault();
    });
})(window);
