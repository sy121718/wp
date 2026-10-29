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
function setup(fetch, props = { productSource: 'specific' }, type = 'core.productSelector') {
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

test('规格展示器搜索使用公开事实，名称按纯文本且没有作者固定规格选择', async () => {
 const calls=[];const ui=setup(async url=>{calls.push(url);return response(url.includes('/catalog')?{items:[{id:'p',name:'<b>设计杯</b>'}]}:{id:'p',name:'<img src=x>',variants:[{id:'v'}]});});
 await flush();const products=ui.byLabel('已发布商品');assert.equal(products.options[1].textContent,'<b>设计杯</b>');
 products.value='p';await products.fire('change');await flush();assert.equal(ui.node.props.productId,'p');assert.equal(ui.byLabel('商品规格'),undefined);assert.ok(calls.every(url=>url.startsWith('/api/site/product/')));
});

test('切换规格展示器来源一次历史清理旧引用',async()=>{
 const ui=setup(async()=>response({items:[]}),{productSource:'specific',productId:'one'});
 await elements(ui.panel).find(el=>el.tagName==='BUTTON'&&el.textContent==='当前商品').fire('click');
 assert.equal(ui.state.snapshots,1);assert.equal(ui.state.syncs,1);assert.equal(ui.node.props.productId,'');await flush();
});

test('加购旧接线明确转换，保留文案数量和样式，不保留其它商品或动作',()=>{
 const code=fs.readFileSync(__dirname+'/methods/controls/addtocart.js','utf8').replace(/^import.*$/gm,'').replace(/export (function|const) /g,'$1 ');
 const sandbox=vm.createContext({Object});vm.runInContext(code,sandbox);
 const a={props:{label:'放入购物袋',showQuantity:true,color:'#111',advanced:{customClass:'x'},productSource:'specific',productId:'another',variantId:'fixed',currency:'¥',variantMode:'perVariant'},actions:{success:{kind:'external',url:'https://invalid.test'}}};
 assert.equal(sandbox.hasRetiredCartWiring(a),true);sandbox.clearRetiredCartWiring(a);
 assert.deepEqual(a,{props:{label:'放入购物袋',showQuantity:true,color:'#111',advanced:{customClass:'x'}}});assert.equal(sandbox.hasRetiredCartWiring(a),false);
 const b={props:{label:'新按钮'}};assert.equal(sandbox.hasRetiredCartWiring(b),false);
});
