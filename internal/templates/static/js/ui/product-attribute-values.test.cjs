const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

// 属性值是嵌套数组：脚本只注册请求对象提供器，发送与响应由写基座/HTMX 负责。
function setup() {
    const listeners = {window: {}, document: {}};
    function listen(surface) {
        return (name, fn) => { (listeners[surface][name] ||= []).push(fn); };
    }
    function fire(name, target) {
        for (const surface of ['document']) {
            for (const fn of listeners[surface][name] || []) fn({target});
        }
    }
    function makeFields(values) {
        return Object.entries(values).map(([name, value]) => ({
            name, value: String(value), checked: value === true,
            getAttribute(n) { return n === 'data-attribute-field' ? name : null; },
            focus() { this.focused = true; },
        }));
    }
    const rows = [];
    function row(values) {
        const items = makeFields(values);
        return {
            fields: items,
            querySelectorAll(selector) { return selector === '[data-attribute-field]' ? items : []; },
            querySelector(selector) {
                const m = /\[data-attribute-field="([^"]+)"\]/.exec(selector);
                return m ? items.find(f => f.name === m[1]) || null : null;
            },
            remove() {
                const at = rows.indexOf(this);
                if (at >= 0) rows.splice(at, 1);
            },
        };
    }
    rows.push(row({id: 'v1', key: 'red', label: '红', sort: '0', enabled: true}));
    rows.push(row({id: 'v2', key: 'blue', label: '蓝', sort: '1', enabled: true}));

    const list = {children: [], appendChild(node) { this.children.push(node); }};
    const prototypeRow = () => {
        const created = row({id: '', key: '', label: '', sort: '0', enabled: true});
        created.isNew = true;
        return created;
    };
    const valueList = {
        lastElementChild: null,
        appendChild(fragment) {
            const created = fragment.querySelector();
            rows.push(created);
            this.lastElementChild = created;
        },
    };
    const host = {
        querySelector(selector) {
            if (selector === '[data-attribute-prototype]') {
                return {content: {cloneNode() {
                    const created = prototypeRow();
                    return {querySelector() { return created; }};
                }}};
            }
            if (selector === '[data-attribute-value-list]') return valueList;
            return null;
        },
    };
    const form = {
        querySelectorAll(selector) {
            return selector === '[data-attribute-value-row]' ? rows : [];
        },
    };
    class FakeFormData {
        get(name) { return ({projectId: 'p1', id: 'attr-1'})[name] ?? null; }
    }
    // 同一个脚本注册两个 provider（值行 / 属性组）：按名字取，不能用「最后一次注册」
    // 冒充其中一个，否则两个 provider 的语义会互相覆盖测试。
    const builders = {};
    const sandbox = {
        window: {WBUI: {writeBase: {registerPayloadBuilder(name, fn) { builders[name] = fn; }}}},
        document: {addEventListener: listen('document')},
        FormData: FakeFormData,
        console,
    };
    vm.createContext(sandbox);
    vm.runInContext(fs.readFileSync(path.join(UI_SRC, 'product-attribute-values.js'), 'utf8'), sandbox);
    assert.equal(typeof builders['product-attribute-values'], 'function', '脚本必须注册值行 provider');
    assert.equal(typeof builders['product-attribute-group'], 'function', '脚本必须注册属性组 provider');
    return {
        rows, form, fire,
        payload(name = 'product-attribute-values') {
            return JSON.parse(JSON.stringify(builders[name](form)));
        },
        add() {
            fire('click', {closest: selector => selector === '[data-attribute-add]' ? {closest: () => host} : null});
        },
        remove(r) {
            fire('click', {closest: selector => selector === '[data-attribute-remove]' ? {closest: () => r} : null});
        },
    };
}

test('属性值按当前 DOM 组装完整数组，复选框映射为布尔', () => {
    const s = setup();
    s.rows[0].fields.find(f => f.name === 'label').value = '深红';
    s.rows[0].fields.find(f => f.name === 'enabled').checked = false;
    const body = s.payload();
    assert.equal(body.projectId, 'p1');
    assert.equal(body.id, 'attr-1');
    assert.deepEqual(body.values[0], {id: 'v1', key: 'red', label: '深红', sort: 0, enabled: false});
    assert.deepEqual(body.values[1], {id: 'v2', key: 'blue', label: '蓝', sort: 1, enabled: true});
});

test('增删行后仍按顺序提交，新增行默认启用', () => {
    const s = setup();
    s.remove(s.rows[0]);
    s.add();
    const body = s.payload();
    assert.equal(body.values.length, 2);
    assert.equal(body.values[0].id, 'v2');
    assert.equal(body.values[1].label, '');
    assert.equal(body.values[1].enabled, true);
});

test('删光属性值后提交空数组，不省略字段', () => {
    const s = setup();
    for (const r of [...s.rows]) s.remove(r);
    const body = s.payload();
    assert.ok(Array.isArray(body.values));
    assert.equal(body.values.length, 0);
});

// 属性组编辑走 /api/product/attribute/update：DTO 不含 values，编辑抽屉也没有值行。
// 提交空 values 只会多一个服务端不认识的键，所以没有行时不带它。
test('属性组编辑没有值行时不提交 values', () => {
    const s = setup();
    for (const r of [...s.rows]) s.remove(r);
    const body = s.payload('product-attribute-group');
    assert.equal(body.values, undefined);
    assert.equal(body.id, 'attr-1');
});
