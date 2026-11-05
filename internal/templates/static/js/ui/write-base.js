/* HTML 写请求接线：HTMX 负责标准表单、multipart、请求与成功交换。
 * Jet 声明成功目标与本宿主的 data-write-status 错误区；失败不替换编辑内容。
 * 复杂编辑器只注册请求对象，不注册 JSON 结果处理器。
 */
(function () {
    'use strict';
    var ui = window.WBUI = window.WBUI || {};
    if (ui.writeBase) return;
    var requests = new WeakMap();
    var generations = new WeakMap();
    var active = new WeakMap();
    var builders = Object.create(null);

    function attr(state, name) {
        if (state.submitter?.hasAttribute(name)) return state.submitter.getAttribute(name) || '';
        return state.owner.getAttribute(name) || '';
    }

    function hasAttr(state, name) {
        return !!state.submitter?.hasAttribute(name) || state.owner.hasAttribute(name);
    }

    function statusOf(owner) {
        var own = owner.querySelector('[data-write-status]');
        if (own) return own;
        var host = owner.closest('[data-write-host]');
        if (!host) return null;
        // 宿主错误区不能借用其他表单或嵌套宿主的区域；不猜测多个候选的归属。
        var boxes = Array.from(host.querySelectorAll('[data-write-status]')).filter(function (box) {
            return !box.closest('form') && box.closest('[data-write-host]') === host;
        });
        return boxes.length === 1 ? boxes[0] : null;
    }

    function stale(state) {
        return !state.owner.isConnected || generations.get(state.scope) !== state.generation ||
            (state.target && !state.target.isConnected);
    }

    function toast(message) {
        if (typeof ui.toast === 'function') ui.toast(message, { type: 'error', duration: 6000 });
    }

    function fail(state, message, fragment) {
        if (state.done || stale(state)) return;
        state.done = true;
        var box = state.errorBox;
        if (box) {
            if (fragment) box.replaceChildren(fragment);
            else box.textContent = message;
            box.hidden = false;
            box.setAttribute('data-tone', 'error');
        }
        // 业务错误已有 Jet 错误区；传输/协议异常或缺失错误区才使用通用提示。
        if (!fragment || !box) toast(message || fragment.textContent.trim());
    }

    function fragmentOf(text) {
        if (!text?.trim() || /<!doctype\b|<\/?(?:html|head|body)\b/i.test(text)) return null;
        var template = document.createElement('template');
        template.innerHTML = text;
        return template.content;
    }

    function errorOf(fragment) {
        var root = fragment.firstElementChild;
        if (fragment.children.length !== 1 || !root?.matches('[data-ui-error][role="alert"]')) return null;
        // 错误模板是静态事实展示，不接受脚本、表单或跨区域交换指令。
        for (var node of [root, ...root.querySelectorAll('*')]) {
            if (/^(SCRIPT|STYLE|LINK|META|IFRAME|OBJECT|EMBED|FORM|INPUT|BUTTON|SELECT|TEXTAREA|TEMPLATE)$/.test(node.tagName) || node.tagName.startsWith('HX-')) return null;
            for (var attribute of node.attributes) {
                if (/^(hx-|data-hx-|on)/i.test(attribute.name)) return null;
            }
        }
        return root;
    }

    function drawerFragmentOf(fragment) {
        var root = fragment.firstElementChild;
        if (fragment.children.length !== 1 || !root?.matches('[data-drawer-fragment]')) return false;
        if (!root.matches('[data-drawer-readonly], form') && !root.querySelector('form')) return false;
        // 抽屉只接纳显式声明的单根片段；拒绝活动内容与外站表单/资源。
        for (var node of [root, ...root.querySelectorAll('*')]) {
            if (/^(SCRIPT|STYLE|LINK|META|BASE|IFRAME|OBJECT|EMBED|SVG|MATH|TEMPLATE|NOSCRIPT)$/.test(node.tagName) || node.tagName.startsWith('HX-')) return false;
            for (var attribute of node.attributes) {
                if (/^on/i.test(attribute.name) || /^(srcdoc|formaction|is)$/i.test(attribute.name)) return false;
                if (/^(href|src|action|xlink:href|(?:data-)?hx-(?:get|post|put|patch|delete))$/i.test(attribute.name)) {
                    var url = new URL(attribute.value, document.baseURI);
                    if (!/^https?:$/.test(url.protocol) || url.origin !== location.origin || url.username || url.password) return false;
                }
            }
        }
        return true;
    }

    function nextURL(state, fragment) {
        var selector = attr(state, 'data-write-nav');
        if (!selector) return '';
        var link = fragment.querySelector(selector);
        if (!link?.matches('a[href]')) throw new Error('成功响应缺少声明的下一步链接');
        var url = new URL(link.getAttribute('href'), document.baseURI);
        if (url.origin !== location.origin) throw new Error('成功导航必须使用本站链接');
        return url.href;
    }

    function succeed(state, ctx) {
        if (state.done || generations.get(state.scope) !== state.generation) return;
        state.done = true;
        if (state.errorBox?.isConnected) {
            state.errorBox.replaceChildren();
            state.errorBox.hidden = true;
            state.errorBox.removeAttribute('data-tone');
        }
        if (!state.write) return;
        var eventTarget = state.owner.isConnected ? state.owner : document;
        eventTarget.dispatchEvent(new CustomEvent('write:success', { bubbles: true }));
        var close = attr(state, 'data-write-close');
        if (close) {
            var panel = document.querySelector(close);
            if (panel?.matches('[data-drawer]') && typeof ui.closeDrawer === 'function') ui.closeDrawer();
            else if (panel) {
                panel.hidden = true;
                panel.dispatchEvent(new CustomEvent('wbui:drawer-close', { bubbles: true }));
            }
        }
        if (state.next) window.location.assign(state.next);
        else if (hasAttr(state, 'data-write-reload')) window.location.reload();
    }

    htmx.registerExtension('write-base', {
        htmx_config_request: function (elt, detail) {
            var ctx = detail.ctx;
            var request = ctx.request;
            var owner = request.form || elt.form || elt.closest('form') || elt;
            var submitter = request.submitter || (elt !== owner ? elt : null);
            var state = { owner: owner, submitter: submitter, target: ctx.target, errorBox: statusOf(owner) };
            var unsafe = !/^(GET|HEAD|OPTIONS)$/i.test(request.method);
            if (unsafe && !hasAttr(state, 'data-write')) return;
            state.write = unsafe;
            // 抽屉入口在外壳之外，错误区仍由目标所在的 Jet 宿主明确提供。
            if (!unsafe && state.target?.matches('[data-drawer-body]')) state.errorBox = statusOf(state.target);
            // 多个查询入口可以更新同一区域；查询按目标、写操作按表单隔离迟到结果。
            state.scope = unsafe ? owner : ctx.target || owner;
            requests.set(ctx, state);
            request.headers.Accept = 'text/html';
            request.redirect = 'error';
            var url = new URL(request.action, document.baseURI);
            if (url.origin !== location.origin) state.error = '界面请求必须使用本站端点';
            if (!unsafe) return;
            if (window.WPSitePreview?.active) state.error = '模拟预览：不会提交真实数据，请在发布页面中操作。';
            var session = attr(state, 'data-write-session');
            state.visitor = session === 'visitor';
            if (session && session !== 'visitor') state.error = '未知的写请求会话类型';
            state.session = state.visitor ? window.WPSiteSession : null;
            var token = state.visitor ? state.session?.csrfToken :
                owner.querySelector('[name="csrf_token"]')?.value || document.querySelector('meta[name="csrf-token"]')?.content;
            state.csrfToken = token;
            if (!token) state.error = state.visitor ? '访客会话尚未就绪，请稍后重试' : '缺少 CSRF 令牌，请刷新页面';
            else request.headers['X-CSRF-Token'] = token;
        },
        htmx_before_request: function (_elt, detail) {
            var ctx = detail.ctx;
            var state = requests.get(ctx);
            if (!state) return;
            state.generation = (generations.get(state.scope) || 0) + 1;
            generations.set(state.scope, state.generation);
            if (state.error) throw new Error(state.error);
            var name = state.write && attr(state, 'data-write-builder');
            if (name) {
                if (!builders[name]) throw new Error('未注册的业务对象提供器: ' + name);
                var payload = builders[name](state.owner, state.submitter);
                if (!payload || typeof payload !== 'object' || Array.isArray(payload) || typeof payload.then === 'function') throw new Error('业务对象提供器必须返回对象');
                ctx.request.body = JSON.stringify(payload);
                ctx.request.headers['Content-Type'] = 'application/json';
            }
            if (ctx.request.timeout == null && !htmx.config.defaultTimeout) ctx.request.timeout = 30000;
            state.started = true;
            active.set(state.owner, (active.get(state.owner) || 0) + 1);
            state.owner.setAttribute('aria-busy', 'true');
        },
        htmx_before_response: function (_elt, detail) {
            var ctx = detail.ctx;
            var state = requests.get(ctx);
            if (!state) return;
            if (state.visitor && state.session && window.WPSiteSession === state.session) {
                var token = ctx.response.headers.get('X-CSRF-Token');
                // 会话轮换独立于表单代数；迟到响应只能更新发出时仍有效的令牌。
                if (token && state.session.csrfToken === state.csrfToken) state.session.csrfToken = token;
            }
            for (var header of ctx.response.headers.keys()) {
                if (header.toLowerCase().startsWith('hx-')) state.error = '界面响应不得携带后端导航或交换指令头';
            }
            if (ctx.response.raw.redirected) state.error = '界面响应不得重定向';
            // 在读取正文与任何后端导航、trigger、历史更新之前清除响应指令。
            ctx.hx = {};
        },
        htmx_after_request: function (_elt, detail) {
            var ctx = detail.ctx;
            var state = requests.get(ctx);
            if (!state) return;
            if (stale(state)) return false;
            var html = /^text\/html(?:;|$)/i.test(ctx.response.headers.get('Content-Type') || '');
            var fragment = html && fragmentOf(ctx.text);
            if (state.error || !fragment) {
                fail(state, state.error || '接口未返回有效 HTML 片段');
                return false;
            }
            if (!ctx.response.raw.ok) {
                var error = errorOf(fragment);
                fail(state, '请求失败，请重试', error);
                return false;
            }
            try {
                if (state.target?.matches('[data-drawer-body]') && !drawerFragmentOf(fragment)) {
                    fail(state, '接口未返回有效抽屉片段');
                    return false;
                }
                state.next = state.write ? nextURL(state, fragment) : '';
            }
            catch (error) { fail(state, '接口未返回有效 HTML 片段'); return false; }
            state.accepted = true;
            // 成功交回 HTMX 原生交换，不解释 JSON 或生成界面。
        },
        htmx_before_swap: function (_elt, detail) {
            var state = requests.get(detail.ctx);
            if (state && (!state.accepted || stale(state))) return false;
        },
        htmx_after_swap: function (_elt, detail) {
            var state = requests.get(detail.ctx);
            if (state?.accepted) succeed(state, detail.ctx);
        },
        htmx_error: function (_elt, detail) {
            var state = detail.ctx && requests.get(detail.ctx);
            if (state) fail(state, state.error || attr(state, 'data-msg-failed') || '请求失败，请重试');
        },
        htmx_finally_request: function (_elt, detail) {
            var state = requests.get(detail.ctx);
            if (!state?.started) return;
            var count = Math.max(0, (active.get(state.owner) || 1) - 1);
            active.set(state.owner, count);
            if (!count) state.owner.removeAttribute('aria-busy');
        }
    });

    ui.writeBase = {
        owns: function (ctx) { return !!ctx && requests.has(ctx); },
        registerPayloadBuilder: function (name, fn) {
            if (!name || typeof fn !== 'function') throw new Error('注册能力需要名称和函数');
            if (Object.prototype.hasOwnProperty.call(builders, name)) throw new Error('能力重复注册: ' + name);
            builders[name] = fn;
        }
    };

    // 静态编辑器预览也拦截未被 HTMX 认领的原生写提交，不发送真实业务请求。
    document.addEventListener('submit', function (event) {
        var form = event.target;
        if (window.WPSitePreview?.active && (form.method.toLowerCase() !== 'get' || form.hasAttribute('data-write'))) {
            event.preventDefault();
            var box = statusOf(form);
            if (box) {
                box.textContent = '模拟预览：不会提交真实数据，请在发布页面中操作。';
                box.hidden = false;
            }
        }
    }, true);
})();
