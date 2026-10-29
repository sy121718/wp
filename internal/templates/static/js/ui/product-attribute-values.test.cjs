const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup() {
    const listeners = {window: {}, document: {}};
    const requests = [];
    const submitTrace = [];
    function listen(surface) {
        return (name, fn, options) => {
            const capture = options === true || !!(options && options.capture);
            (listeners[surface][name] ||= []).push({fn, capture});
        };
    }
    function fire(name, target) {
        const event = {
            target, submitter: null, defaultPrevented: false,
            preventDefault() { this.defaultPrevented = true; },
            stopPropagation() { this.stopped = true; },
        };
        // 按真实传播方向执行；同一节点内保持注册顺序，不跳过写基座监听器。
        for (const [surface, capture] of [['window', true], ['document', true], ['document', false], ['window', false]]) {
            for (const listener of listeners[surface][name] || []) {
                if (listener.capture !== capture) continue;
                if (name === 'submit') submitTrace.push(surface + ':' + (capture ? 'capture' : 'bubble'));
                listener.fn(event);
            }
            if (event.stopped) break;
        }
        return event;
    }
    const input = (name, value, type = 'text') => ({
        name, value, type, checked: value === true, tagName: 'INPUT', disabled: false,
        getAttribute(n) { return n === 'name' || n === 'data-attribute-field' ? this.name : null; },
        hasAttribute() { return false; },
        focus() { this.focused = true; },
    });
    const rows = [];
    function row(values) {
        const fields = Object.entries(values).map(([name, value]) => input(name, value, name === 'enabled' ? 'checkbox' : 'text'));
        const content = {
            fields: [],
            replaceChildren() { this.fields = []; },
            appendChild(field) { this.fields.push(field); },
            querySelectorAll() { return this.fields; },
        };
        const result = {
            fields, template: {content},
            querySelector(selector) { return selector.startsWith('template') ? this.template : fields.find(f => f.name === 'label'); },
            querySelectorAll() { return fields; },
            remove() { rows.splice(rows.indexOf(this), 1); },
        };
        fields.forEach(field => { field.closest = () => result; });
        return result;
    }
    rows.push(
        row({id: 'v1', key: 'red', label: '红', sort: '0', enabled: true}),
        row({id: 'v2', key: 'blue', label: '蓝', sort: '1', enabled: true}),
    );
    const host = {
        querySelector(selector) {
            if (selector === '[data-attribute-prototype]') {
                return {content: {cloneNode() {
                    const added = row({id: '', key: '', label: '', sort: '0', enabled: true});
                    return {querySelector() { return added; }, row: added};
                }}};
            }
            return {appendChild(fragment) { rows.push(fragment.row); }};
        },
    };
    const attrs = {
        'data-write': 'custom',
        'data-write-url': '/api/product/attribute/set-values',
        'data-write-json': 'values',
    };
    const form = {
        tagName: 'FORM',
        getAttribute(name) { return attrs[name] || null; },
        hasAttribute(name) { return Object.hasOwn(attrs, name); },
        setAttribute(name, value) { attrs[name] = value; },
        querySelector() { return null; },
        querySelectorAll(selector) {
            if (selector === '[data-attribute-value-row]') return rows;
            if (selector.startsWith('template[')) return rows.map(r => r.template);
            return [];
        },
    };
    const document = {
        readyState: 'complete', querySelector() { return null; },
        addEventListener: listen('document'), createElement() { return input('', ''); },
    };
    const sandbox = {
        window: {CSS: {escape: s => s}, addEventListener: listen('window')},
        document, FormData, CustomEvent: class {}, console,
        fetch(url, options) {
            requests.push({url, body: options.body});
            // 本组只观察真实 onSubmit 交给 fetch 的请求体，响应收尾由写基座测试覆盖。
            return new Promise(() => {});
        },
    };
    vm.createContext(sandbox);
    // 与 ui_scripts.html 一致：写基座先注册 document 捕获监听，控件后加载。
    vm.runInContext(fs.readFileSync(__dirname + '/write-base.js', 'utf8'), sandbox);
    const initializers = [];
    sandbox.window.WBUI.register = fn => initializers.push(fn);
    sandbox.window.WBUI.markOnce = () => true;
    vm.runInContext(fs.readFileSync(__dirname + '/product-attribute-values.js', 'utf8'), sandbox);
    initializers.forEach(init => init(form));

    function payload() {
        const before = requests.length;
        const event = fire('submit', form);
        assert.equal(event.defaultPrevented, true, '统一写基座应接管提交');
        assert.equal(requests.length, before + 1, '一次提交应发出一次真实写基座请求');
        const request = requests.at(-1);
        assert.equal(request.url, '/api/product/attribute/set-values');
        return JSON.parse(request.body.get('values'));
    }
    return {
        rows, fire, payload, submitTrace,
        add() { fire('click', {closest: selector => selector === '[data-attribute-add]' ? {closest: () => host} : null}); },
        remove(r) { fire('click', {closest: selector => selector === '[data-attribute-remove]' ? {closest: () => r} : null}); },
    };
}

test('生产加载顺序下直接提交未触发input/change的当前值', () => {
    const s = setup();
    const first = s.rows[0];
    assert.equal(first.template.content.fields.find(f => f.name === 'label').value, '红');
    first.fields.find(f => f.name === 'label').value = '深红';
    first.fields.find(f => f.name === 'sort').value = '9';
    first.fields.find(f => f.name === 'enabled').checked = false;
    // 不派发 input/change：旧快照仍是红色，必须在 document 组包前更新。
    assert.equal(first.template.content.fields.find(f => f.name === 'label').value, '红');
    assert.deepEqual(s.payload()[0], {id: 'v1', key: 'red', label: '深红', sort: '9', enabled: 'false'});
    assert.deepEqual(s.submitTrace, ['window:capture', 'document:capture']);
});

test('属性值增删、修改及取消勾选经统一基座形成完整数组', () => {
    const s = setup();
    s.remove(s.rows[0]);
    s.rows[0].fields.find(f => f.name === 'enabled').checked = false;
    s.rows[0].fields.find(f => f.name === 'label').value = '蓝色';
    s.add();
    const added = s.rows[1];
    added.fields.find(f => f.name === 'label').value = '绿色';
    added.fields.find(f => f.name === 'key').value = 'green';
    assert.equal(added.fields.find(f => f.name === 'label').focused, true);
    assert.deepEqual(s.payload(), [
        {id: 'v2', key: 'blue', label: '蓝色', sort: '1', enabled: 'false'},
        {id: '', key: 'green', label: '绿色', sort: '0', enabled: 'true'},
    ]);
});

test('删光属性值后仍提交values=[]，不变成省略字段', () => {
    const s = setup();
    for (const row of [...s.rows]) s.remove(row);
    assert.deepEqual(s.payload(), []);
});
