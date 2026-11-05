const test = require('node:test');
const assert = require('node:assert/strict');
const { env } = require('./write-base-test-env.cjs');

test('请求代数默认隔离迟到网络失败，不需要 latest 开关', () => {
    const e = env(), old = e.start(), latest = e.start();
    e.error(old);
    assert.equal(e.status.textContent, ''); assert.equal(e.toasts.length, 0);
    assert.equal(e.form.getAttribute('aria-busy'), 'true');
    e.error(latest);
    assert.equal(e.status.textContent, '请求失败，请重试');
    assert.equal(e.toasts.length, 1);
    assert.equal(e.form.getAttribute('aria-busy'), null);
});

test('较新请求失败后旧请求收尾不能覆盖结论或重复提示', () => {
    const e = env(), old = e.start(), latest = e.start();
    e.error(latest);
    assert.equal(e.form.getAttribute('aria-busy'), 'true');
    e.error(old);
    assert.equal(e.toasts.length, 1);
    assert.equal(e.form.getAttribute('aria-busy'), null);
});

test('owner 或成功目标已移除时，网络失败无界面与导航副作用', () => {
    for (const removed of ['owner', 'target']) {
        const e = env({ 'data-write-reload': '' });
        const ctx = e.configure();
        if (removed === 'owner') e.form.isConnected = false;
        else {
            // target 在配置阶段由 HTMX 提供，单独构造带目标的请求。
            ctx.target = { isConnected: true };
            e.hooks.htmx_config_request(e.form, { ctx });
            ctx.target.isConnected = false;
        }
        e.hooks.htmx_before_request(e.form, { ctx });
        e.error(ctx);
        assert.equal(e.status.textContent, ''); assert.equal(e.toasts.length, 0);
        assert.deepEqual(e.nav, []); assert.equal(e.events.length, 0);
        assert.equal(e.form.getAttribute('aria-busy'), null);
    }
});

test('未开始请求的 finally 不影响其他活动请求', () => {
    const e = env(), running = e.start(), cancelled = e.configure();
    e.finish(cancelled);
    assert.equal(e.form.getAttribute('aria-busy'), 'true');
    e.finish(running);
    assert.equal(e.form.getAttribute('aria-busy'), null);
});
