/* 账号界面消费原始 JSON 事实；读取与写入复用站点基座，不保存会话令牌。 */
(function (global) {
    'use strict';
    if (global.__wpSiteUserReady) return;
    var publicModes = ['login', 'register', 'forgot', 'reset'];
    function owner(node) { return node && node.closest('[data-user-form]'); }
    function writeText(root, selector, value) { var node = root.querySelector(selector); if (node) node.textContent = value == null ? '' : String(value); }
    function time(value) {
        if (typeof value !== 'number' || !Number.isFinite(value)) return '—';
        var date = new Date(value);
        return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat(global.document.documentElement.lang || undefined, {dateStyle:'medium', timeStyle:'short'}).format(date);
    }
    function enable(root) {
        root.querySelectorAll('[data-user-fields]').forEach(function (fieldset) {
            var section = fieldset.closest('[data-user-section]');
            var mode = section && section.getAttribute('data-user-section');
            fieldset.disabled = !!(mode && publicModes.indexOf(mode) < 0 && (!root._userAuthenticated || (['profile','preference'].indexOf(mode) >= 0 && !root._userAccountReady)));
        });
    }
    function read(root, kind, endpoint, view) {
        var node = root.querySelector('[data-user-' + kind + '-read]');
        if (!node) return;
        node.setAttribute('data-site-read', '/api/site/user/' + endpoint);
        node.setAttribute('data-site-view', view);
        global.WPSiteData.refresh(node);
    }
    function show(root, mode, focus) {
        if (!root.querySelector('[data-user-section="' + mode + '"]')) return;
        root.setAttribute('data-user-active', mode);
        var permitted = publicModes.indexOf(mode) >= 0 || root._userAuthenticated;
        root.querySelectorAll('[data-user-section]').forEach(function (section) { section.hidden = !permitted || section.getAttribute('data-user-section') !== mode; });
        var guest = root.querySelector('[data-user-guest]');
        if (guest) guest.hidden = !!permitted;
        var title = root.querySelector('[data-user-mode-title="' + mode + '"]');
        if (focus && title) writeText(root, '[data-user-title]', title.textContent);
        if (root._userAuthenticated && ['account','profile','preference'].indexOf(mode) >= 0) read(root, 'account', 'account', 'userAccount');
        if (root._userAuthenticated && mode === 'sessions') read(root, 'devices', 'sessions', 'userDevices');
        if (focus) {
            var active = root.querySelector('[data-user-section="' + mode + '"]');
            var input = active && active.querySelector('input:not([type="hidden"]), button');
            if (input && !active.hidden) input.focus();
        }
    }
    function fillForm(form, data) {
        if (form.hasAttribute('data-user-dirty')) return;
        form.querySelectorAll('input[name],select[name],textarea[name]').forEach(function (input) {
            var key=input.getAttribute('data-user-field') || input.name;
            if (!Object.prototype.hasOwnProperty.call(data, key)) return;
            if (input.type === 'checkbox') input.checked = data[key] === true;
            else input.value = data[key] == null ? '' : String(data[key]);
        });
    }
    function refreshAll() {
        global.document.querySelectorAll('[data-user-form][data-site-read]').forEach(function (root) { global.WPSiteData.refresh(root); });
    }
    function nextURL(raw) {
        if (!raw || raw.charAt(0) !== '/' || raw.charAt(1) === '/' || /[\\\x00-\x20]/.test(raw)) return '';
        var url = new URL(raw, global.location.href);
        return url.origin === global.location.origin ? url.pathname + url.search + url.hash : '';
    }
    global.WPSiteData.register('userSession', function (root, data) {
        var first = !root._userReady;
        root._userReady = true;
        var authenticated = !!(data && data.authenticated);
        if (!authenticated) {
            root._userAccountReady = false;
            root.querySelectorAll('[data-user-section="profile"] form, [data-user-section="preference"] form').forEach(function(form){form.reset();form.removeAttribute('data-user-dirty');});
            root.querySelectorAll('[data-user-value]').forEach(function(node){node.textContent='';});
            var list=root.querySelector('[data-user-device-list]');if(list)list.replaceChildren();
        }
        root._userAuthenticated = authenticated;
        var mode = root.getAttribute('data-user-active') || root.getAttribute('data-user-form') || 'login';
        if (first) {
            var params = new URLSearchParams(global.location.search);
            root.querySelectorAll('[data-user-section="reset"] [name="key"], [data-user-section="reset"] [name="email"]').forEach(function (input) { input.value = params.get(input.name) || ''; });
        }
        enable(root);
        show(root, mode, false);
        writeText(root, '[data-site-status]', '');
        var retry=root.querySelector('[data-user-retry]');if(retry)retry.hidden=true;
    });
    global.WPSiteData.register('userAccount', function (node, data) {
        var root = owner(node);
        if (!root || !root._userAuthenticated || !data) return;
        root.querySelectorAll('[data-user-value]').forEach(function (target) {
            var key = target.getAttribute('data-user-value'), value = data[key];
            if (key === 'displayName') value = data.displayName || data.nickname || data.username;
            if (key === 'registeredAt' || key === 'lastLoginTime') value = time(value);
            target.textContent = value == null ? '' : String(value);
        });
        root.querySelectorAll('[data-user-section="profile"] form, [data-user-section="preference"] form').forEach(function (form) { fillForm(form, data); });
        root._userAccountReady=true;enable(root);
    });
    global.WPSiteData.register('userDevices', function (node, devices) {
        var root = owner(node), host = root && root.querySelector('[data-user-devices]');
        if (!host || !root._userAuthenticated || !Array.isArray(devices)) return;
        var list = host.querySelector('[data-user-device-list]'), template = host.querySelector('template[data-user-device]');
        if (!list || !template) return;
        list.replaceChildren();
        devices.forEach(function (device) {
            var row = template.content.firstElementChild.cloneNode(true);
            writeText(row, '[data-device-agent]', device.userAgent || host.getAttribute('data-label-unknown'));
            writeText(row, '[data-device-meta]', [device.ip, device.location, host.getAttribute('data-label-active') + ' ' + time(device.lastActiveAt), host.getAttribute('data-label-created') + ' ' + time(device.createdAt)].filter(Boolean).join(' · '));
            row.querySelector('[data-device-current]').hidden = !device.current;
            var form = row.querySelector('form');
            form.hidden = !!device.current;
            form.querySelector('[name="id"]').value = device.id;
            row.querySelectorAll('[data-user-fields]').forEach(function(fieldset){fieldset.disabled=false;});
            list.appendChild(row);
        });
        host.querySelector('[data-user-empty]').hidden = devices.length !== 0;
    });
    global.document.addEventListener('click', function (event) {
        var retry=event.target.closest('[data-user-retry]');if(retry){var retryRoot=owner(retry);if(retryRoot)global.WPSiteData.refresh(retryRoot);return;}
        var button = event.target.closest('[data-user-mode]'), root = owner(button);
        if (root) show(root, button.getAttribute('data-user-mode'), true);
    });
    global.document.addEventListener('site:data:error',function(event){
        var root=owner(event.target);if(!root)return;
        writeText(root,'[data-site-status]',event.detail && event.detail.message);
        var retry=root.querySelector('[data-user-retry]');if(retry)retry.hidden=false;
    });
    global.document.addEventListener('input', function (event) {
        var form = event.target.closest('form');
        if (form && owner(form)) form.setAttribute('data-user-dirty', 'true');
    });
    global.WBUI.writeBase.registerResultHandler('user.forms', function (ctx) {
        var root = owner(ctx.form), url = ctx.form.getAttribute('data-write-url') || '';
        var event = {target:ctx.form};
        if (!root || root.getAttribute('data-component-instance') !== ctx.instanceId || url.indexOf('/api/site/user/') !== 0) return false;
        event.target.removeAttribute('data-user-dirty');
        event.target.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
        if (url === '/api/site/user/login') {
            var next = nextURL(root.getAttribute('data-user-next'));
            if (next) { global.location.assign(next); return true; }
            root.setAttribute('data-user-active', 'account');
        }
        if (url === '/api/site/user/logout' || url === '/api/site/user/account/password') {
            root.setAttribute('data-user-active', 'login');
            root.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
        }
        refreshAll();
        return true;
    });
    global.__wpSiteUserReady=true;
})(window);
