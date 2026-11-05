const test = require('node:test');
const assert = require('node:assert/strict');
const { env } = require('./write-base-test-env.cjs');

test('响应中的全部 HX 指令在导航与最终 trigger 前清除', () => {
    const e = env(), ctx = e.start();
    ctx.response = { headers: new Headers({ 'HX-Redirect': '/wrong', 'HX-Trigger': 'wrong' }), raw: { redirected: false } };
    ctx.hx = { redirect: '/wrong', trigger: 'wrong' };
    e.hooks.htmx_before_response(e.form, { ctx });
    assert.deepEqual(Object.keys(ctx.hx), []);
    e.error(ctx);
    assert.match(e.status.textContent, /不得携带/);
    assert.deepEqual(e.nav, []); assert.equal(e.toasts.length, 1);
});

test('访客轮换只更新明确访客会话，后台请求不混用 token', () => {
    for (const visitor of [true, false]) {
        const e = env(visitor ? { 'data-write-session': 'visitor' } : {});
        e.window.WPSiteSession = { csrfToken: 'visitor-token' };
        const ctx = e.start();
        assert.equal(ctx.request.headers['X-CSRF-Token'], visitor ? 'visitor-token' : 'csrf-token');
        ctx.response = { headers: new Headers({ 'X-CSRF-Token': 'rotated' }), raw: { redirected: false } };
        e.hooks.htmx_before_response(e.form, { ctx });
        assert.equal(e.window.WPSiteSession.csrfToken, visitor ? 'rotated' : 'visitor-token');
    }
});

test('不同表单乱序响应不能用旧令牌覆盖已完成的访客轮换', () => {
    const e = env({ 'data-write-session': 'visitor' });
    const session = e.window.WPSiteSession = { csrfToken: 'visitor-token' };
    const old = e.start();
    const other = e.node({ 'data-write': '', 'data-write-session': 'visitor' });
    const otherStatus = e.node();
    other.querySelector = () => otherStatus;
    const login = { request: { form: other, method: 'POST', action: '/login', headers: {} } };
    e.hooks.htmx_config_request(other, { ctx: login });
    e.hooks.htmx_before_request(other, { ctx: login });
    login.response = { headers: new Headers({ 'X-CSRF-Token': 'rotated' }), raw: { redirected: false } };
    e.hooks.htmx_before_response(other, { ctx: login });
    old.response = { headers: new Headers({ 'X-CSRF-Token': 'visitor-token' }), raw: { redirected: false } };
    e.hooks.htmx_before_response(e.form, { ctx: old });
    assert.equal(session.csrfToken, 'rotated');
    assert.equal(e.configure().request.headers['X-CSRF-Token'], 'rotated');
    assert.equal(e.events.length, 0);
});

test('表单移除或结果过期不阻断会话轮换，但旧会话响应不能更新替换后的会话', () => {
    for (const kind of ['removed', 'stale', 'replaced']) {
        const e = env({ 'data-write-session': 'visitor' });
        e.window.WPSiteSession = { csrfToken: 'visitor-token' };
        const ctx = e.start();
        if (kind === 'removed') e.form.isConnected = false;
        if (kind === 'stale') e.start();
        if (kind === 'replaced') e.window.WPSiteSession = { csrfToken: 'visitor-token' };
        ctx.response = { headers: new Headers({ 'X-CSRF-Token': 'rotated' }), raw: { redirected: false } };
        e.hooks.htmx_before_response(e.form, { ctx });
        assert.equal(e.window.WPSiteSession.csrfToken, kind === 'replaced' ? 'visitor-token' : 'rotated', kind);
        assert.equal(e.events.length, 0);
        assert.deepEqual(e.nav, []);
    }
});

test('重复加载不清除请求对象注册，也不恢复 JSON 结果处理 API', () => {
    const e = env({ 'data-write-builder': 'rows' });
    e.base.registerPayloadBuilder('rows', () => ({ rows: [] }));
    e.reload();
    assert.deepEqual(JSON.parse(e.start().request.body), { rows: [] });
    assert.equal(e.base.registerResultHandler, undefined);
});

test('只读预览在请求发送前拒绝 HTMX 写，同时拦截原生 POST，不影响 GET', () => {
    const e = env({}, { preview: true });
    assert.throws(() => e.start(), /模拟预览/);
    const read = e.configure(null, '/read', 'GET');
    assert.equal(e.base.owns(read), true);
    assert.doesNotThrow(() => e.hooks.htmx_before_request(e.form, { ctx: read }));
    e.finish(read);
    let stopped = false;
    e.listeners.submit({ target: e.form, preventDefault() { stopped = true; } });
    assert.equal(stopped, true); assert.match(e.status.textContent, /模拟预览/);
});

test('传输异常只提示一次且不泄漏内部异常', () => {
    const e = env(), ctx = e.start();
    e.error(ctx); e.error(ctx);
    assert.equal(e.status.getAttribute('data-tone'), 'error');
    assert.doesNotMatch(e.status.textContent, /private-network-detail/);
    assert.equal(e.toasts.length, 1);
});
