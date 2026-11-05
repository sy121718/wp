const test = require('node:test');
const assert = require('node:assert/strict');
const { env } = require('./write-base-test-env.cjs');

test('写基座仅公开归属与复杂请求对象注册，不提供结果解释器或兼容别名', () => {
    const e = env();
    assert.deepEqual(Object.keys(e.base).sort(), ['owns', 'registerPayloadBuilder'].sort());
    assert.equal(e.window.WPJsonWrite, undefined);
    assert.equal(e.window.WBFormBody, undefined);
});

test('本站写端点不限路径前缀；设置 CSRF、HTML Accept 与禁止跟随重定向', () => {
    const e = env();
    for (const path of ['/write', '/admin/products', '/api/test']) {
        const ctx = e.configure(null, path);
        e.hooks.htmx_before_request(e.form, { ctx });
        assert.equal(e.base.owns(ctx), true);
        assert.equal(ctx.request.headers['X-CSRF-Token'], 'csrf-token');
        assert.equal(ctx.request.headers.Accept, 'text/html');
        assert.equal(ctx.request.redirect, 'error');
        assert.equal(ctx.request.timeout, 30000);
        e.finish(ctx);
    }
    const read = e.configure(null, '/read', 'GET');
    assert.equal(e.base.owns(read), true);
    assert.equal(read.request.headers['X-CSRF-Token'], undefined);
    assert.equal(read.request.headers.Accept, 'text/html');
    const invalid = e.configure(null, 'https://other.test/write');
    assert.throws(() => e.hooks.htmx_before_request(e.form, { ctx: invalid }), /本站端点/);
});

test('复杂对象提供器接收真实表单与具名按钮，不处理结果或强制普通表单 JSON', () => {
    const e = env({ 'data-write-builder': 'rows' });
    const button = e.node();
    e.base.registerPayloadBuilder('rows', (form, submitter) => {
        assert.equal(form, e.form); assert.equal(submitter, button);
        return { rows: [{ id: '9007199254740993', quantity: 0 }] };
    });
    const ctx = e.start(button);
    assert.deepEqual(JSON.parse(ctx.request.body), { rows: [{ id: '9007199254740993', quantity: 0 }] });
    assert.equal(ctx.request.headers['Content-Type'], 'application/json');
    assert.throws(() => e.base.registerPayloadBuilder('rows', () => ({})), /重复注册/);
    assert.throws(() => e.base.registerPayloadBuilder('', () => ({})), /名称和函数/);
    const unknown = env({ 'data-write-builder': 'missing' });
    assert.throws(() => unknown.start(), /未注册/);
});

test('普通表单保留 HTMX 原生 body，不另设 Content-Type 或执行器', () => {
    const e = env(), ctx = e.configure(), body = ctx.request.body;
    e.hooks.htmx_before_request(e.form, { ctx });
    assert.equal(ctx.request.body, body);
    assert.equal(ctx.request.headers['Content-Type'], undefined);
});

test('对象提供器拒绝空值、标量、数组和异步对象', () => {
    for (const expression of ['null', 'false', '1', '"text"', '[]', 'Promise.resolve({})']) {
        const e = env({ 'data-write-builder': 'bad' });
        // 在不同 realm 中创建 Promise，确保异步对象不会因 instanceof 失效而被接纳。
        const vm = require('node:vm');
        e.base.registerPayloadBuilder('bad', vm.runInNewContext('() => ' + expression));
        assert.throws(() => e.start(), /必须返回对象/);
    }
});
