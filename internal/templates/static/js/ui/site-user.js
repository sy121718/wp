/* 账号界面消费原始 JSON 事实；读取与写入复用站点基座，不保存会话令牌。 */
(function (global) {
    'use strict';
    if (global.__wpSiteUserReady) return;
    var publicModes = ['login', 'register', 'forgot', 'reset'];
    var privateModes = ['account', 'profile', 'preference', 'password', 'sessions'];
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
            fieldset.disabled = root._userPreviewState === 'loading' || root._userPreviewState === 'submitting' || !!(mode && publicModes.indexOf(mode) < 0 && (!root._userAuthenticated || (['profile','preference'].indexOf(mode) >= 0 && !root._userAccountReady)));
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
        if (privateModes.indexOf(mode) >= 0 && !root._userAuthenticated) mode = 'login';
        if (!root.querySelector('[data-user-section="' + mode + '"]')) return;
        root.setAttribute('data-user-active', mode);
        var permitted = publicModes.indexOf(mode) >= 0 || root._userAuthenticated;
        root.querySelectorAll('[data-user-section]').forEach(function (section) { section.hidden = !permitted || section.getAttribute('data-user-section') !== mode; });
        var guest = root.querySelector('[data-user-guest]');
        if (guest) guest.hidden = !!permitted;
        var title = root.querySelector('[data-user-mode-title="' + mode + '"]');
        if (title && root.getAttribute('data-user-auto-title') !== 'false') writeText(root, '[data-user-title]', title.textContent);
        if (root._userAuthenticated && ['account','profile','preference'].indexOf(mode) >= 0) read(root, 'account', 'account', 'userAccount');
        if (root._userAuthenticated && mode === 'sessions') read(root, 'devices', 'sessions', 'userDevices');
        if (focus) {
            var active = root.querySelector('[data-user-section="' + mode + '"]');
            var input = active && active.querySelector('input:not([type="hidden"]), button');
            if (input && !active.hidden) input.focus();
        }
        if (root._userPreviewState) applyPreviewState(root);
    }
    function flowLabel(root, key) {
        var node = root.querySelector('[data-user-flow-label="' + key + '"]');
        return node ? node.textContent : '';
    }
    function registerStep(root, verification) {
        var entry = root.querySelector('[data-user-register-entry]');
        if (entry) entry.hidden = verification;
        root.querySelectorAll('[data-user-register-verify], [data-user-register-resend]').forEach(function (form) { form.hidden = !verification; });
        root.querySelectorAll('[data-user-register-step]').forEach(function (button) { button.hidden = (button.getAttribute('data-user-register-step') === 'verify') === verification; });
        if (verification) {
            var verify = root.querySelector('[data-user-register-verify]');
            var code = verify && verify.querySelector('[name="key"]');
            if (code && code.focus) code.focus();
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
    function renderSession(root, data) {
        var first = !root._userReady;
        var previouslyAuthenticated = root._userAuthenticated;
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
        if (first && !root._userPreviewState) {
            var params = new URLSearchParams(global.location.search);
            root.querySelectorAll('[data-user-section="reset"] [name="key"], [data-user-section="reset"] [name="email"]').forEach(function (input) { input.value = params.get(input.name) || ''; });
            if (params.get('key') && params.get('email')) mode = 'reset';
        }
        if ((first || !previouslyAuthenticated) && authenticated && ['login','register'].indexOf(mode) >= 0) mode = 'account';
        var pendingPart = root._userPendingPart;
        delete root._userPendingPart;
        if (pendingPart && authenticated) mode = pendingPart;
        enable(root);
        show(root, mode, false);
        if (pendingPart && authenticated) {
            var targetPart = root.querySelector('[data-user-section="' + mode + '"]');
            if (targetPart && targetPart.focus) targetPart.focus({preventScroll:true});
        }
        if (!root._userPreviewState) {
            writeText(root, '[data-site-status]', root._userNotice || '');
            var status = root.querySelector('[data-site-status]');
            if (status) status.setAttribute('data-tone', 'info');
        }
        var retry=root.querySelector('[data-user-retry]');if(retry)retry.hidden=true;
    }
    function renderAccount(node, data) {
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
    }
    function renderDevices(node, devices) {
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
    }
    global.WPSiteData.register('userSession', renderSession);
    global.WPSiteData.register('userAccount', renderAccount);
    global.WPSiteData.register('userDevices', renderDevices);

    function previewLabel(root, state) {
        var label = root.querySelector('[data-user-preview-label="' + state + '"]');
        return label ? label.textContent : '';
    }
    function applyPreviewState(root) {
        var state = root._userPreviewState, mode = root.getAttribute('data-user-active');
        root.setAttribute('data-user-state', state);
        var section = root.querySelector('[data-user-section="' + mode + '"]');
        var globalStatus = root.querySelector('[data-site-status]');
        var formStatus = section && section.querySelector('[data-write-status]');
        var error = ['validation-error','service-error','forbidden'].indexOf(state) >= 0;
        var message = previewLabel(root, state);
        if (globalStatus) {
            globalStatus.textContent = ['loading','empty','service-error','forbidden'].indexOf(state) >= 0 ? message : '';
            globalStatus.setAttribute('data-tone', error ? 'error' : 'info');
        }
        if (formStatus) {
            formStatus.textContent = ['validation-error','submitting','success'].indexOf(state) >= 0 ? message : '';
            formStatus.setAttribute('data-tone', error ? 'error' : 'info');
        }
        enable(root);
    }
    function previewAccount(empty) {
        return {username:empty?'':'preview-user',nickname:empty?'':'Alex',displayName:empty?'':'Alex',email:empty?'':'preview@example.invalid',registeredAt:1714521600000,lastLoginTime:1714521600000,
            firstName:'',lastName:'',birthday:'',phone:'',website:'',company:'',country:'',province:'',city:'',postcode:'',gender:0,address:'',bio:'',profileLocale:'',profileTimezone:'',
            theme:'system',locale:'',pageSize:20,timezone:'Asia/Shanghai',profileVisibility:'private',emailNotify:true,smsNotify:false,showOnline:false};
    }
    var previewStates = ['default','loading','empty','unauthenticated','validation-error','service-error','submitting','success','forbidden'];
    global.WPSiteData.registerPreview('userSession', function (root, state) {
        root._userPreviewState = state;
        var mode = root.getAttribute('data-user-active') || root.getAttribute('data-user-form') || 'account';
        if (!root._userReady && state === 'success') { mode = 'account'; root.setAttribute('data-user-active', mode); }
        renderSession(root, {authenticated:state !== 'unauthenticated' && state !== 'forbidden' && (state === 'success' || privateModes.indexOf(mode) >= 0)});
        applyPreviewState(root);
    }, previewStates);
    global.WPSiteData.registerPreview('userAccount', function (node, state) {
        renderAccount(node, previewAccount(state === 'empty'));
    }, previewStates);
    global.WPSiteData.registerPreview('userDevices', function (node, state) {
        renderDevices(node, state === 'empty' ? [] : [{id:'preview-current-device',current:true,userAgent:'Desktop · Preview',ip:'192.0.2.10',location:'',lastActiveAt:1714521600000,createdAt:1714521600000},
            {id:'preview-other-device',current:false,userAgent:'Mobile · Preview',ip:'192.0.2.20',location:'',lastActiveAt:1714521600000,createdAt:1714521600000}]);
    }, previewStates);
    global.document.addEventListener('click', function (event) {
        var retry=event.target.closest('[data-user-retry]');if(retry){var retryRoot=owner(retry);if(retryRoot)global.WPSiteData.refresh(retryRoot);return;}
        var step = event.target.closest('[data-user-register-step]');
        if (step) { var stepRoot = owner(step); if (stepRoot) registerStep(stepRoot, step.getAttribute('data-user-register-step') === 'verify'); return; }
        var button = event.target.closest('[data-user-mode]'), root = owner(button);
        if (root) { delete root._userNotice; show(root, button.getAttribute('data-user-mode'), true); }
    });
    global.document.addEventListener('site:data:error',function(event){
        var root=owner(event.target);if(!root)return;
        writeText(root,'[data-site-status]',event.detail && event.detail.message);
        var status=root.querySelector('[data-site-status]');if(status)status.setAttribute('data-tone','error');
        var retry=root.querySelector('[data-user-retry]');if(retry)retry.hidden=false;
    });
    global.document.addEventListener('input', function (event) {
        var form = event.target.closest('form');
        if (form && owner(form)) form.setAttribute('data-user-dirty', 'true');
    });
    global.document.addEventListener('site:action', function (event) {
        var root = owner(event.target), detail = event.detail;
        if (!root || !detail || root.getAttribute('data-component-instance') !== detail.instanceId || publicModes.concat(privateModes).indexOf(detail.part) < 0) return;
        if (privateModes.indexOf(detail.part) >= 0 && !root._userAuthenticated && !root._userPreviewState) {
            root._userPendingPart = detail.part;
            global.WPSiteData.refresh(root);
            return;
        }
        show(root, detail.part, false);
        var part = root.querySelector('[data-user-section="' + root.getAttribute('data-user-active') + '"]');
        if (part && part.focus) part.focus({preventScroll:true});
    });
    global.WBUI.writeBase.registerResultHandler('user.forms', function (ctx) {
        var root = owner(ctx.form), url = ctx.form.getAttribute('data-write-url') || '';
        var event = {target:ctx.form};
        if (!root || root.getAttribute('data-component-instance') !== ctx.instanceId || url.indexOf('/api/site/user/') !== 0) return false;
        event.target.removeAttribute('data-user-dirty');
        event.target.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
        if (url === '/api/site/user/login') {
            root._userAuthenticated = !!(ctx.data && ctx.data.authenticated);
            var action = root.querySelector('[data-component-action="authenticated"]');
            if (action && global.WPSiteActions && global.WPSiteActions.run(ctx.form, 'authenticated')) {
                refreshAll();
                return true;
            }
            var next = action ? '' : nextURL(root.getAttribute('data-user-next'));
            if (next) { global.location.assign(next); return true; }
            show(root, 'account', false);
            if (action) writeText(root, '[data-site-status]', previewLabel(root, 'action-unavailable'));
        }
        if (url === '/api/site/user/register') {
            var resend = root.querySelector('[data-user-register-resend]');
            var email = ctx.form.querySelector('[name="email"]');
            registerStep(root, true);
            if (resend) {
                resend.hidden = false;
                var resendEmail = resend.querySelector('[name="email"]');
                if (resendEmail && email) resendEmail.value = email.value;
            }
            root.setAttribute('data-user-active', 'register');
            root._userNotice = flowLabel(root, ctx.data && ctx.data.mailQueued === false ? 'activation-mail-failed' : 'activation-sent');
            ctx.form.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
        }
        if (url === '/api/site/user/activate') {
            root.setAttribute('data-user-active', 'login');
            root._userNotice = flowLabel(root, 'activated');
            registerStep(root, false);
            ctx.form.querySelectorAll('[name="key"]').forEach(function (input) { input.value = ''; });
            root.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
        }
        if (url === '/api/site/user/resend') {
            root._userNotice = flowLabel(root, 'activation-sent');
        }
        if (url === '/api/site/user/forgot') {
            var reset = root.querySelector('[data-user-section="reset"]');
            var forgotEmail = ctx.form.querySelector('[name="email"]');
            var resetEmail = reset && reset.querySelector('[name="email"]');
            var resetKey = reset && reset.querySelector('[name="key"]');
            if (resetEmail && forgotEmail) resetEmail.value = forgotEmail.value;
            if (resetKey) resetKey.value = '';
            root.setAttribute('data-user-active', 'reset');
            root._userNotice = flowLabel(root, 'reset-sent');
            show(root, 'reset', false);
            if (resetKey && resetKey.focus) resetKey.focus();
        }
        if (url === '/api/site/user/logout' || url === '/api/site/user/account/password' || url === '/api/site/user/reset') {
            root.setAttribute('data-user-active', 'login');
            root._userNotice = ctx.text || '';
            root.querySelectorAll('input[type="password"]').forEach(function (input) { input.value = ''; });
            if (url === '/api/site/user/reset') {
                ctx.form.querySelectorAll('[name="key"], [name="password"]').forEach(function (input) { input.value = ''; });
            }
        }
        refreshAll();
        return true;
    });
    global.__wpSiteUserReady=true;
})(window);
