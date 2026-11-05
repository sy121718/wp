const test = require('node:test');
const assert = require('node:assert/strict');
const { env } = require('./write-base-test-env.cjs');

test('批量拒绝明细由 Jet 负责，基座不提供 JSON 明细格式化 API', () => {
    const e = env({ 'data-write': 'bulk' });
    assert.equal(e.base.itemsOf, undefined);
    assert.equal(e.base.itemText, undefined);
    assert.equal(e.base.registerResultHandler, undefined);
});

test('批量字段与数量由 DTO 校验，基座不解析或整形原生集合', () => {
    const e = env({ 'data-write': 'bulk' }), ctx = e.configure();
    const body = new URLSearchParams([['ids', 'a'], ['ids', 'b']]);
    ctx.request.body = body;
    e.hooks.htmx_before_request(e.form, { ctx });
    assert.equal(ctx.request.body, body);
    assert.deepEqual(ctx.request.body.getAll('ids'), ['a', 'b']);
});

test('缺少本表单错误区时不借用同宿主中其他表单的错误区', () => {
    const e = env();
    e.form.querySelector = selector => selector.includes('csrf_token') ? { value: 'csrf-token' } : null;
    const host = e.node(), otherForm = e.node(), otherBox = e.node();
    otherBox.closest = selector => selector === 'form' ? otherForm : host;
    host.querySelectorAll = () => [otherBox];
    e.form.closest = () => host;
    e.error(e.start());
    assert.equal(otherBox.textContent, ''); assert.equal(e.toasts.length, 1);
});

test('仅接受当前宿主唯一的共享错误区，不借用嵌套宿主或猜测多个候选', () => {
    for (const kind of ['unique', 'nested', 'ambiguous']) {
        const e = env();
        e.form.querySelector = selector => selector.includes('csrf_token') ? { value: 'csrf-token' } : null;
        const host = e.node(), nested = e.node(), box = e.node(), second = e.node();
        box.closest = selector => selector === 'form' ? null : kind === 'nested' ? nested : host;
        second.closest = selector => selector === 'form' ? null : host;
        host.querySelectorAll = () => kind === 'ambiguous' ? [box, second] : [box];
        e.form.closest = () => host;
        e.error(e.start());
        assert.equal(box.textContent, kind === 'unique' ? '请求失败，请重试' : '');
        assert.equal(second.textContent, ''); assert.equal(e.toasts.length, 1);
    }
});
