'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
// Reuse the existing DOM fixture without registering or changing its shared tests.
// 复用 json_write.test.cjs 的 DOM 桩与 setup，但**只取定义、不注册它的用例**。
//
// 原做法是「从头切到 `test('create ` 之前」，问题有两个：
//   1. 那个边界之间**混着别的用例**（`customer template submits ...` 在第 55 行），
//      它们会被一起注册执行 —— 而它们依赖定义在文件后半段的辅助函数（formBlockOf，
//      第 1128 行），于是必然 ReferenceError；
//   2. 夹具函数定义在第一个 test 之后（setup 在第 240 行），切到第 465 行能拿到它们，
//      但切片一旦前移就会漏 —— 边界随用例顺序漂移。
//
// 现在：把切片里所有 `test(` 注册调用**剥掉**（连同它们后面的回调体用简单配平跳过），
// 只保留函数与常量定义。这样拿到的是纯夹具，不依赖任何位置。
const fixture = fs.readFileSync(__dirname + '/json_write.test.cjs', 'utf8');
const start = fixture.indexOf("test('create ");
assert.ok(start > 0, 'shared fixture boundary exists');

function stripTestRegistrations(code) {
    const out = [];
    let i = 0;
    while (i < code.length) {
        const at = code.indexOf('test(', i);
        if (at < 0) { out.push(code.slice(i)); break; }
        // 保留 test( 之前的内容
        out.push(code.slice(i, at));
        // 跳过整个 test(...) 调用：从 test( 开始按括号配平（跳过字符串里的括号）
        let depth = 0;
        let j = at + 'test'.length;
        let quote = null;
        for (; j < code.length; j++) {
            const ch = code[j];
            if (quote) {
                if (ch === '\\') { j++; continue; }
                if (ch === quote) { quote = null; }
                continue;
            }
            if (ch === "'" || ch === '"' || ch === '`') { quote = ch; continue; }
            if (ch === '(') depth++;
            else if (ch === ')') {
                depth--;
                if (depth === 0) { j++; break; }
            }
        }
        // 吃掉结尾分号
        while (j < code.length && (code[j] === ';' || code[j] === '\n')) { j++; }
        i = j;
    }
    return out.join('');
}

const fixtureOnly = stripTestRegistrations(fixture.slice(0, start));
const context = { require, __dirname, console, setTimeout, setImmediate };
vm.runInNewContext(fixtureOnly + '\nthis.fixture = {setup, makeEl, tick};', context);
const {setup, makeEl, tick} = context.fixture;

function outsideHost(env) {
    const body = env.sandbox.document.body;
    body.removeChild(env.form);
    const host = makeEl('div', {'data-write-host': ''});
    body.appendChild(host);
    host.appendChild(env.form);
    env.form.setAttribute('class', 'hidden-form');
    env.form.removeChild(env.status);
    host.appendChild(env.status);
    env.status.hidden = true;
    return host;
}

test('plugin disable sends false and failure reaches visible sibling host status', async () => {
    const env = setup({attrs: {'data-write-api':'/api/plugin/toggle', 'data-write-mode':'create', 'data-write-bool':'enabled', 'data-write-nav':'/admin/plugins'}, createFields:[{name:'id',value:'hello'},{name:'enabled',value:'false'}], buttons:[{outside:true,form:'plugin-toggle-hello'}], respond:{code:500,message:'Internal server error. Please try again later.'}});
    const host = outsideHost(env);
    env.fireWith(env.buttons[0]);
    await tick();
    assert.equal(env.requests.length, 1);
    assert.equal(env.requests[0].body.id, 'hello');
    assert.equal(env.requests[0].body.enabled, false);
    assert.equal(env.status.hidden, false);
    assert.equal(host.hidden, false);
    assert.equal(env.status.textContent, 'Internal server error. Please try again later.');
    assert.equal(env.calls.assign.length, 0);
    assert.equal(env.calls.reload, 0);
});

test('plugin uninstall external submit confirms then sends single id and preserves failure', async () => {
    const env = setup({attrs: {'data-write-api':'/api/plugin/uninstall', 'data-write-mode':'single', 'data-confirm':'Uninstall plugin?'}, id:'hello', buttons:[{outside:true,form:'plugin-uninstall-hello'}], respond:{code:500,message:'Failed to uninstall the plugin.'}});
    outsideHost(env);
    env.fireWith(env.buttons[0]);
    assert.equal(env.requests.length, 0);
    env.acceptConfirm();
    await tick();
    assert.equal(env.requests.length, 1);
    assert.equal(env.requests[0].body.id, 'hello');
    assert.equal(Object.keys(env.requests[0].body).length, 1);
    assert.equal(env.status.hidden, false);
    assert.equal(env.status.textContent, 'Failed to uninstall the plugin.');
    assert.equal(env.calls.reload, 0);
    assert.equal(env.form.getAttribute('data-confirm'), 'Uninstall plugin?');
});
