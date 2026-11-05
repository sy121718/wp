const fs = require('node:fs');
const vm = require('node:vm');

// 这里只验证配置与生命周期钩子；HTML 解析、真实编码和交换由浏览器测试覆盖。
function env(attrs = {}, options = {}) {
    function node(values = {}) {
        const attributes = { ...values };
        return {
            textContent: '', hidden: false, isConnected: true,
            getAttribute: name => attributes[name] ?? null,
            hasAttribute: name => Object.hasOwn(attributes, name),
            setAttribute: (name, value) => { attributes[name] = String(value); },
            removeAttribute: name => { delete attributes[name]; },
            querySelector: () => null,
            querySelectorAll: () => [],
            closest: () => null,
        };
    }
    const status = node(), form = node({ 'data-write': '', ...attrs });
    form.method = 'post';
    form.querySelector = selector => selector === '[data-write-status]' ? status : { value: 'csrf-token' };
    const events = [], toasts = [], nav = [], listeners = {};
    form.dispatchEvent = event => events.push(event);
    const document = {
        baseURI: 'https://example.test/',
        addEventListener: (name, fn) => { listeners[name] = fn; },
        querySelector: () => null,
    };
    let hooks;
    const window = {
        WBUI: { toast: (text, options) => toasts.push({ text, options }) },
        location: { origin: 'https://example.test', assign: url => nav.push(url), reload: () => nav.push('reload') },
        WPSitePreview: { active: !!options.preview },
    };
    const context = vm.createContext({
        window, document, location: window.location, URL,
        htmx: { config: {}, registerExtension: (_name, value) => { hooks = value; } },
        CustomEvent: class { constructor(type) { this.type = type; } },
    });
    const source = fs.readFileSync(__dirname + '/write-base.js', 'utf8');
    vm.runInContext(source, context);
    const base = window.WBUI.writeBase;
    function configure(submitter = null, action = '/write', method = 'POST') {
        const ctx = { request: { form, submitter, method, action, headers: {}, body: new URLSearchParams('title=keep') } };
        hooks.htmx_config_request(form, { ctx });
        return ctx;
    }
    function start(submitter = null) {
        const ctx = configure(submitter);
        hooks.htmx_before_request(form, { ctx });
        return ctx;
    }
    function finish(ctx) { hooks.htmx_finally_request(form, { ctx }); }
    function error(ctx) {
        hooks.htmx_error(form, { ctx, error: new Error('private-network-detail') });
        finish(ctx);
    }
    return { base, hooks, form, status, events, toasts, nav, listeners, window, document, node,
        configure, start, finish, error, reload: () => vm.runInContext(source, context) };
}
module.exports = { env };
