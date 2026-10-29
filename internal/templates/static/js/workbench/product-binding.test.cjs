const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

class Element {
    constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.events = {}; this.attributes = {}; this.isConnected = true; this.value = ''; this.textContent = ''; this.classList = { toggle() {} }; }
    append(...items) { this.children.push(...items); }
    appendChild(item) { this.append(item); }
    prepend(item) { this.children.unshift(item); }
    replaceChildren(...items) { this.children = items; }
    setAttribute(key, value) { this.attributes[key] = value; }
    addEventListener(key, action) { this.events[key] = action; }
    get options() { return this.children; }
    async fire(key) { return this.events[key]({ key: '', preventDefault() {} }); }
}
function elements(root) { return [root, ...root.children.flatMap(elements)]; }
function setup(fetch, props = { productSource: 'specific' }, type = 'core.addToCart') {
    const panel = new Element('div'), node = { id: 'a', type, props };
    const state = { snapshots: 0, syncs: 0 };
    const get = (ctx, path) => path.split('.').reduce((v, key) => v && v[key], ctx.node);
    const set = (ctx, path, value) => { const keys = path.split('.'); let target = ctx.node; for (const key of keys.slice(0, -1)) target = target[key] || (target[key] = {}); target[keys.at(-1)] = value; };
    const ctx = { panel, node, self: { findNode: () => node, syncInspector() { state.syncs++; } } };
    const script = fs.readFileSync(__dirname + '/methods/controls/product.js', 'utf8').replace(/^import.*$/gm, '').replace(/export function /g, 'function ');
    const sandbox = vm.createContext({ document: { createElement: tag => new Element(tag) }, URLSearchParams, setTimeout, clearTimeout, fetch,
        meta: { projectId: 'project', lang: 'zh-CN' }, get, set, commit(c, path, value, after) { state.snapshots++; set(c, path, value); if (after) after(); } });
    vm.runInContext(script, sandbox); sandbox.productBindingPanel(ctx);
    const byLabel = label => elements(panel).find(el => el.attributes['aria-label'] === label);
    return { panel, node, state, sandbox, byLabel };
}
const response = data => ({ ok: true, json: async () => ({ code: 200, data }) });
const flush = () => new Promise(resolve => setImmediate(resolve));

test('共享选品入口使用公开事实，切商品清旧规格且规格名称按纯文本输出', async () => {
    const calls = [];
    const ui = setup(async url => { calls.push(url); return response(url.includes('/catalog') ? { items: [{ id: 'p', name: '<b>设计杯</b>' }] } : { id: 'p', name: '<b>设计杯</b>', variants: [{ id: 'v', sku: 'CUP', options: { color: 'red' } }], options: [{ key: 'color', values: [{ key: 'red', name: '<img onerror=alert(1)>' }] }] }); }, { productSource: 'specific', variantId: 'old' });
    await flush(); const products = ui.byLabel('已发布商品');
    assert.equal(products.options[1].textContent, '<b>设计杯</b>');
    products.value = 'p'; await products.fire('change'); await flush();
    assert.equal(ui.node.props.productId, 'p'); assert.equal(ui.node.props.variantId, ''); assert.equal(ui.state.snapshots, 1);
    const variants = ui.byLabel('商品规格'); assert.equal(variants.options[1].textContent, '<img onerror=alert(1)>');
    assert.ok(calls.every(url => url.startsWith('/api/site/product/')));
    assert.ok(calls.every(url => url.includes('projectId=project')));
});

test('迟到的旧商品规格不能覆盖新选择，读取失败保留原规格', async () => {
    const pending = {};
    const ui = setup(url => url.includes('/catalog') ? Promise.resolve(response({ items: [{ id: 'one', name: '一' }, { id: 'two', name: '二' }] })) : new Promise(resolve => { pending[new URL(url, 'http://test').searchParams.get('productId')] = resolve; }), { productSource: 'specific', productId: 'one', variantId: 'original' });
    await flush(); const products = ui.byLabel('已发布商品');
    assert.equal(ui.byLabel('商品规格').value, 'original');
    products.value = 'two'; await products.fire('change');
    pending.two(response({ id: 'two', name: '二', variants: [{ id: 'new', sku: '新规格' }] })); await flush();
    pending.one(response({ id: 'one', name: '一', variants: [{ id: 'old', sku: '过期规格' }] })); await flush();
    assert.deepEqual(ui.byLabel('商品规格').options.map(v => v.value), ['', 'new']);
    assert.equal(ui.node.props.productId, 'two');
    const failed = setup(async url => url.includes('/catalog') ? response({ items: [] }) : { ok: false, json: async () => ({ code: 404, message: '商品不可用' }) }, { productSource: 'specific', productId: 'one', variantId: 'original' });
    await flush(); assert.equal(failed.node.props.variantId, 'original'); assert.equal(failed.byLabel('商品规格').value, 'original');
    assert.match(elements(failed.panel).find(el => el.attributes.role === 'status').textContent, /原选择已保留/);
});

test('切换商品来源只记一次历史并清理旧引用，规格选择器共用选品但不重复选规格', async () => {
    const ui = setup(async () => response({ items: [] }), { productSource: 'specific', productId: 'one', variantId: 'old' });
    await elements(ui.panel).find(el => el.tagName === 'BUTTON' && el.textContent === '当前商品').fire('click');
    assert.equal(ui.state.snapshots, 1); assert.equal(ui.state.syncs, 1); assert.equal(ui.node.props.productId, ''); assert.equal(ui.node.props.variantId, '');
    const selector = setup(async () => response({ items: [] }), { productSource: 'specific' }, 'core.productSelector');
    assert.ok(selector.byLabel('已发布商品')); assert.equal(selector.byLabel('商品规格'), undefined);
    await flush();
});
