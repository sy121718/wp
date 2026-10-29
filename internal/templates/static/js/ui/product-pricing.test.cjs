const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup() {
    const listeners = {};
    const handlers = {};
    const made = [];
    const node = (attribute, value) => ({textContent: '', getAttribute: n => n === attribute ? value : null});
    const counts = ['targetCount', 'changedCount', 'unchangedCount', 'skippedCount'].map(x => node('data-preview-count', x));
    const summaries = Object.fromEntries(['rule','rounding','scope'].map(x => ['[data-preview-' + x + ']',node()]));
    const body = {rows: [], replaceChildren(){this.rows=[];}, appendChild(row){this.rows.push(row);}};
    const statusLabels = ['changed','unchanged','skipped'].map(x => ({getAttribute: () => x, textContent: x + '-translated'}));
    const reasonLabels = [{getAttribute: () => 'PricingSkipCostMissing', textContent: '成本缺失'}];
    function row() {
        const values = ['productName','skuCode'].map(x => node('data-value',x));
        const money = ['costPrice','oldPrice','newPrice','diff'].map(x => node('data-money',x));
        const status = {textContent:'', classes:[],classList:{add(x){status.classes.push(x);}}};
        const reason = node();
        const result = {values,money,status,reason,querySelectorAll(s){return s==='[data-value]' ? values : money;},querySelector(s){return s==='[data-preview-status]' ? status : reason;}};
        made.push(result); return result;
    }
    const preview = {hidden:true,focused:false,
        focus(){this.focused=true;},
        querySelector(s){return summaries[s] || {'[data-preview-lines]':body,'[data-preview-row]':{content:{cloneNode:row}},'[data-preview-empty]':{content:{cloneNode:()=>({empty:true})}}}[s];},
        querySelectorAll(s){return s==='[data-preview-count]' ? counts : s==='[data-preview-status-label]' ? statusLabels : reasonLabels;}
    };
    const page = {querySelector:()=>preview};
    const form = {hasAttribute:n=>n==='data-pricing-form',getAttribute:()=>'/api/product/pricing/preview',closest:()=>page,querySelectorAll(s){return [{value: s.includes('ruleType') ? 'fixed_price' : s.includes('rounding') ? 'none' : 'product',textContent:'已翻译选项'}];}};
    const sandbox = {window:{WBUI:{writeBase:{registerResultHandler(name,fn){assert.equal(handlers[name],undefined);handlers[name]=fn;}}}},document:{addEventListener(n,fn){listeners[n]=fn;}},WeakMap};
    vm.createContext(sandbox);
    const source=fs.readFileSync(__dirname+'/product-pricing.js','utf8');
    vm.runInContext(source,sandbox);
    const data = {ruleType:'fixed_price',rounding:'none',scope:'product',ruleValues:{HasValue:true,Value:12.5},targetCount:1,changedCount:1,unchangedCount:0,skippedCount:0,lines:[{productName:'<script>恶意名称</script>',skuCode:'SKU',costPrice:null,oldPrice:10,newPrice:12.5,diff:2.5,status:'changed',reason:''}]};
    function success(value=data){return handlers['product.pricing.preview']({form,data:value,payload:{code:200,data:value}});}
    return {listeners,handlers,form,preview,body,made,data,success,counts,summaries,reload(){vm.runInContext(source,sandbox);}};
}

test('预览消费业务数值并用 textContent 显示，无金额计算与HTML注入',()=>{
    const s=setup(); s.listeners.submit({target:s.form});assert.equal(s.success(),true);
    assert.equal(s.preview.hidden,false);assert.equal(s.preview.focused,true);
    assert.equal(s.made[0].values[0].textContent,'<script>恶意名称</script>');
    assert.deepEqual(s.made[0].money.map(x=>x.textContent),['—','10.00','12.50','+2.50']);
    assert.equal(s.made[0].status.textContent,'changed-translated');
    assert.equal(s.counts[0].textContent,'1');
    assert.equal(s.summaries['[data-preview-rule]'].textContent,'已翻译选项 12.5');
});

test('空试算由具名结果处理器消费并显示模板空态',()=>{
    const s=setup();s.listeners.submit({target:s.form});
    assert.equal(s.success({...s.data,lines:[]}),true);
    assert.equal(s.body.rows[0].empty,true);
});

test('输入改变和重新提交隐藏旧结果，请求期间修改参数后不展示旧响应',()=>{
    const s=setup();s.listeners.submit({target:s.form});s.success();
    s.listeners.input({target:{closest:()=>s.form}});assert.equal(s.preview.hidden,true);
    s.listeners.submit({target:s.form});s.listeners.change({target:{closest:()=>s.form}});assert.equal(s.success(),true);
    assert.equal(s.preview.hidden,true);
    s.listeners.submit({target:s.form});s.success();assert.equal(s.preview.hidden,false);
    s.listeners.submit({target:s.form});assert.equal(s.preview.hidden,true);
});

test('重复加载只注册一次，预览按钮声明handler而应用按钮保留刷新',()=>{
    const s=setup();s.reload();
    assert.deepEqual(Object.keys(s.handlers),['product.pricing.preview']);
    const template=fs.readFileSync(__dirname+'/../../../admin/product/product_pricing.html','utf8');
    const preview=template.match(/<button\b[^>]*data-write-handler="product\.pricing\.preview"[^>]*>/)[0];
    const apply=template.match(/<button\b[^>]*data-write-url="\/api\/product\/pricing\/apply"[^>]*>/)[0];
    assert.doesNotMatch(preview,/data-write-reload/);
    assert.match(apply,/data-write-reload/);
    assert.doesNotMatch(apply,/data-write-handler/);
});
