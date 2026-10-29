const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function node(attrs={}) {
 const values={...attrs},classes=new Set();
 return {value:'',textContent:'',hidden:false,disabled:false,classes,
 getAttribute:k=>values[k]??null,setAttribute:(k,v)=>{values[k]=String(v);},hasAttribute:k=>k in values,removeAttribute:k=>{delete values[k];},
 classList:{toggle:(k,on)=>on?classes.add(k):classes.delete(k)}};
}
function row() {
 const r=node(),parts=new Map();
 for(const s of ['[data-cart-name]','[data-cart-variant]','[data-cart-unit]','[data-cart-line]','[data-cart-stock]','[name="quantity"]','[name="variantId"]','button:not([data-cart-remove])'])parts.set(s,node());
 r.querySelector=s=>parts.get(s)||null;r.parts=parts;return r;
}
function env() {
 const views={},previews={},events={},handlers={},actions=[],refreshed=[],document={activeElement:null,addEventListener:(name,fn)=>{events[name]=fn;},querySelectorAll:()=>[]};
 const window={document,WBUI:{writeBase:{registerResultHandler:(name,fn)=>{handlers[name]=fn;}}},WPSiteActions:{run:(form,key)=>actions.push({form,key})},crypto:{randomUUID:()=> 'generated-request'},WPSiteData:{register:(name,fn)=>{views[name]=fn;},registerPreview:(name,fn)=>{previews[name]=fn;},refresh:r=>refreshed.push(r)}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/site-commerce.js','utf8'),{window,Map,Number,String});
 return {views,previews,events,handlers,actions,document,refreshed};
}
function cartRoot() {
 const rows=[],host={querySelectorAll:()=>rows.slice(),appendChild(r){const old=rows.indexOf(r);if(old>=0)rows.splice(old,1);rows.push(r);r.remove=()=>rows.splice(rows.indexOf(r),1);}},parts=new Map();
 for(const s of ['[data-cart-empty]','[data-cart-total-wrap], [data-cart-clear], [data-cart-checkout]','[data-cart-total]','[data-site-status]'])parts.set(s,node());
 parts.set('[data-cart-items]',host);parts.set('template[data-cart-row]',{content:{firstElementChild:{cloneNode:()=>row()}}});
 const root=node({'data-label-off-shelf':'已下架','data-label-out':'缺货','data-label-low':'仅剩 %d 件','data-label-in':'充足','data-label-unknown':'结算时确认'});
 root.querySelector=s=>parts.get(s)||null;root.querySelectorAll=s=>parts.has(s)?[parts.get(s)]:[];
 return {root,rows,parts};
}
function item(extra={}){return {variantId:'v1',productName:'<img src=x onerror=bad()>',variantLabel:'红色',unitPrice:1234,lineTotal:2468,quantity:2,available:null,maxQuantity:99,missing:false,...extra};}
test('金额只展示后端事实，名称作为纯文本，未知库存与零库存分开',()=>{
 const e=env(),c=cartRoot();
 e.views.cart(c.root,{items:[item()],total:987,currency:'CNY',empty:false});
 assert.equal(c.parts.get('[data-cart-total]').textContent,'CNY 9.87');
 assert.equal(c.rows[0].parts.get('[data-cart-line]').textContent,'CNY 24.68');
 assert.equal(c.rows[0].parts.get('[data-cart-name]').textContent,'<img src=x onerror=bad()>');
 assert.equal(c.rows[0].parts.get('[data-cart-stock]').textContent,'结算时确认');
 e.views.cart(c.root,{items:[item({available:0,maxQuantity:0})],total:987,currency:'CNY',empty:false});
 assert.equal(c.rows.length,1);assert.equal(c.rows[0].parts.get('[data-cart-stock]').textContent,'缺货');
 assert.equal(c.rows[0].parts.get('[name="quantity"]').max,'0');
});
test('刷新保留正在编辑的数量，真实删除只移除对应行，下架商品仍可移除',()=>{
 const e=env(),c=cartRoot();e.views.cart(c.root,{items:[item()],total:2468,currency:'CNY',empty:false});
 const input=c.rows[0].parts.get('[name="quantity"]');input.value='7';e.document.activeElement=input;
 e.views.cart(c.root,{items:[item({available:1})],total:2468,currency:'CNY',empty:false});assert.equal(input.value,'7');
 e.document.activeElement=null;e.views.cart(c.root,{items:[item({missing:true})],total:0,currency:'CNY',empty:false});
 assert.equal(input.disabled,true);assert.equal(c.rows[0].parts.get('button:not([data-cart-remove])').hidden,true);
 e.views.cart(c.root,{items:[],total:0,currency:'CNY',empty:true});assert.equal(c.rows.length,0);
 assert.equal(c.parts.get('[data-cart-empty]').hidden,false);
});
test('结算幂等键只在运行时首次生成，失败响应不会清空输入或生成第二个键',()=>{
 const e=env(),key=node(),root={querySelector:()=>key};
 e.views.cartState(root);assert.equal(key.value,'generated-request');key.value='already-entered';e.views.cartState(root);assert.equal(key.value,'already-entered');
});
test('具名结果只更新提交的结算实例，未付款保留订单号且禁止重复建单',()=>{
 const e=env(),parts=new Map(),button=node();
 for(const s of ['[data-checkout-result]','[data-checkout-title]','[data-checkout-order]','[data-checkout-total]','[data-checkout-account]','[data-checkout-payment-error]'])parts.set(s,node());
 const root=node({'data-label-paid':'完成','data-label-pending':'待付款'});
 root.querySelector=s=>parts.get(s)||null;root.querySelectorAll=s=>parts.has(s)?[parts.get(s)]:[];
 const form=node();form.closest=()=>root;form.querySelectorAll=s=>s==='button[type="submit"]'?[button]:[];
 const otherTitle=node();otherTitle.textContent='另一个实例';
 const reader=node();e.document.querySelectorAll=()=>[reader];
 assert.equal(e.handlers['commerce.checkout']({form,data:{paid:false,orderNo:'ORDER-42',total:1234,currency:'CNY',accountMailed:false,paymentError:'declared.error.key'}}),true);
 assert.equal(parts.get('[data-checkout-title]').textContent,'待付款');assert.equal(parts.get('[data-checkout-order]').textContent,'ORDER-42');assert.equal(button.disabled,true);assert.equal(e.refreshed[0],reader);
 assert.equal(otherTitle.textContent,'另一个实例');assert.equal(e.actions[0].form,form);assert.equal(e.actions[0].key,'success');
 assert.equal(e.events['write:success'],undefined);
});
test('一个实例的数量成功只清理本表单脏标记，另一实例失败输入在失焦刷新后仍保留',()=>{
 const e=env(),a=cartRoot(),b=cartRoot();
 for(const cart of [a,b])e.views.cart(cart.root,{items:[item()],total:2468,currency:'CNY',empty:false});
 const qa=a.rows[0].parts.get('[name="quantity"]'),qb=b.rows[0].parts.get('[name="quantity"]');
 qa.value='3';qa.setAttribute('data-cart-dirty','');qb.value='';qb.setAttribute('data-cart-dirty','');
 e.document.activeElement=null;e.document.querySelectorAll=()=>[a.root,b.root];
 e.handlers['commerce.cart']({form:{querySelector:()=>qa}});
 assert.equal(qa.hasAttribute('data-cart-dirty'),false);assert.equal(qb.hasAttribute('data-cart-dirty'),true);
 e.views.cart(b.root,{items:[item({quantity:3})],total:3702,currency:'CNY',empty:false});
 assert.equal(qb.value,'');assert.equal(e.refreshed.length,2);
});

test('购物车模拟预览使用固定事实区分空车与缺货，不刷新真实私有数据',()=>{
 const e=env(),c=cartRoot();e.previews.cart(c.root,'out-of-stock');
 assert.equal(c.rows[0].parts.get('[data-cart-stock]').textContent,'缺货');
 assert.equal(c.parts.get('[data-cart-total]').textContent,'CNY 98.00');
 e.previews.cart(c.root,'empty');assert.equal(c.rows.length,0);assert.equal(c.parts.get('[data-cart-empty]').hidden,false);assert.equal(e.refreshed.length,0);
});
test('结算模拟校验错误保留本表单内容且不生成真实请求键',()=>{
 const e=env(),status=node(),key=node(),email=node();email.value='buyer@example.test';
 const root=node({'data-site-checkout':''});root.querySelector=s=>s==='[data-write-status]'?status:s==='[name="requestId"]'?key:email;root.querySelectorAll=s=>s==='[data-write-status]'?[status]:[];
 e.previews.cartState(root,'validation-error');assert.match(status.textContent,/检查必填/);assert.equal(email.value,'buyer@example.test');assert.equal(key.value,'');assert.equal(e.refreshed.length,0);
});
