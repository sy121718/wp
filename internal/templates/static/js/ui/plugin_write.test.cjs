'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
// Reuse the existing DOM fixture without registering or changing its shared tests.
const fixture = fs.readFileSync(__dirname + '/json_write.test.cjs', 'utf8');
const start = fixture.indexOf("test('create ");
assert.ok(start > 0, 'shared fixture boundary exists');
const context = { require, __dirname, console, setTimeout, setImmediate };
vm.runInNewContext(fixture.slice(0, start) + '\nthis.fixture = {setup, makeEl, tick};', context);
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
