const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function node(attrs = {}) {
 const values = {...attrs}, children = [], selectors = new Map();
 return {value:'',textContent:'',hidden:false,disabled:false,children,selectors,focusCount:0,
 getAttribute:k=>values[k]??null,hasAttribute:k=>k in values,setAttribute:(k,v)=>{values[k]=String(v);},removeAttribute:k=>{delete values[k];},
 querySelector:s=>selectors.get(s)||null,querySelectorAll:s=>selectors.has(s)?[selectors.get(s)]:[],
 appendChild:r=>children.push(r),replaceChildren:()=>{children.length=0;},focus(){this.focusCount++;}};
}
function row(selectors) { const r=node(); selectors.forEach(s=>r.selectors.set(s,node())); return r; }
function template(selectors) { return {content:{firstElementChild:{cloneNode:()=>row(selectors)}}}; }
function widget(id) {
 const root=node({'data-site-orders':'','data-component-instance':id,'data-site-read':'/api/site/orders?projectId=project&limit=10','data-orders-detail-url':'/api/site/orders/detail?projectId=project','data-orders-returnable-url':'/api/site/orders/returnable?projectId=project','data-orders-returns-url':'/api/site/orders/returns?projectId=project','data-label-paid':'已付款','data-label-empty':'暂无订单','data-label-notfound':'订单不存在'});
 const q=root.selectors, panels={};root.closest=()=>root;
 for(const s of ['[data-orders-list]','[data-orders-empty]','[data-orders-prev]','[data-orders-next]','[data-orders-count]','[data-orders-guest]','[data-site-status]','[data-orders-retry]','[data-detail-number]','[data-detail-items]','[data-orders-apply]','[data-orders-result-number]','[data-orders-result-amount]'])q.set(s,node());
 q.get('[data-orders-guest]').hidden=true;
 q.set('template[data-orders-row]',template(['[data-order-number]','[data-order-time]','[data-order-status]','[data-order-total]','[data-order-open]']));
 for(const name of ['list','detail','return','result']) {const p=node({'data-orders-panel':name});p.hidden=name!=='list';panels[name]=p;q.set('[data-orders-panel="'+name+'"]',p);}
 root.querySelectorAll=s=>s==='[data-orders-panel]'?Object.values(panels):q.has(s)?[q.get(s)]:[];
 const hosts={};
 for(const name of ['detail','returnable','returns']) {const host=node({'data-order-read':name});host.closest=()=>root;hosts[name]=host;q.set('[data-order-read="'+name+'"]',host);host.selectors.set('[data-site-status]',node());}
 for(const s of ['[data-detail-number]','[data-detail-time]','[data-detail-status]','[data-detail-subtotal]','[data-detail-discount]','[data-detail-shipping]','[data-detail-tax]','[data-detail-total]','[data-detail-address]','[data-detail-payment]','[data-detail-remark]','[data-detail-history]'])hosts.detail.selectors.set(s,node());
 hosts.detail.selectors.set('[data-detail-items]',q.get('[data-detail-items]'));
 hosts.detail.selectors.set('template[data-order-item-row]',template(['[data-item-name]','[data-item-variant]','[data-item-quantity]','[data-item-total]']));
 hosts.detail.selectors.set('template[data-order-log-row]',template(['[data-log-status]','[data-log-time]']));
 hosts.returnable.selectors.set('[data-orders-apply]',q.get('[data-orders-apply]'));
 hosts.returnable.selectors.set('[data-orders-no-returnable]',node());
 hosts.returns.selectors.set('[data-orders-returns]',node());
 hosts.returns.selectors.set('template[data-order-return-row]',template(['[data-return-number]','[data-return-reason]','[data-return-status]','[data-return-amount]']));
 const form=node({'data-write-handler':'commerce.orders'});form.closest=()=>root;panels.return.selectors.set('form',form);
 for(const s of ['[name="orderId"]','[name="requestId"]','[name="reason"]','[data-write-status]','[data-orders-return-lines]'])form.selectors.set(s,node());
 form.selectors.set('template[data-order-return-item]',template(['[data-return-item-name]','[data-return-item-variant]','[data-return-item-max]','[name="orderItemId"]','[name="quantity"]']));
 return {root,q,hosts,panels,form};
}
function environment() {
 const views={},previews={},events={},handlers={},refreshed=[],widgets=[];
 const document={addEventListener:(k,fn)=>{events[k]=fn;},querySelectorAll:()=>widgets.map(w=>w.root)};
 const window={document,location:{href:'https://example.test/orders'},crypto:{randomUUID:()=> 'intent-key'},WBUI:{writeBase:{registerResultHandler:(k,fn)=>{handlers[k]=fn;}}},WPSiteData:{register:(k,fn)=>{views[k]=fn;},registerPreview:(k,fn)=>{previews[k]=fn;},refresh:root=>refreshed.push(root)}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/site-orders.js','utf8'),{window,URL,Map,Number,String,Date});
 return {views,previews,events,handlers,refreshed,widgets,window};
}
function detail(id=42) {return {head:{id,orderNo:'ORDER-'+id,status:'paid',currency:'CNY',total:9999,subtotal:8800,discountTotal:100,shippingTotal:20,taxTotal:40,createTime:1793232000000},items:[{id:11,productName:'<img src=x onerror=bad()>',variantLabel:'黑色',quantity:2,lineTotal:8800}],logs:[]};}
function click(e,w,attrs){const trigger=node(attrs);trigger.closest=()=>w.root;e.events.click({target:{closest:()=>trigger}});}
function prepare(e,w){w.root._ordersSelected='42';e.views.orderDetail(w.hosts.detail,detail());e.views.orderReturnable(w.hosts.returnable,{canRequest:true,items:[{orderItemId:11,returnable:2}]});click(e,w,{'data-orders-apply':''});}
test('列表与详情只展示服务金额，商品名作为纯文本插入',()=>{
 const e=environment(),w=widget('orders-a');e.widgets.push(w);
 e.views.ordersList(w.root,{list:[detail().head],total:1});
 assert.equal(w.q.get('[data-orders-list]').children[0].querySelector('[data-order-total]').textContent,'CNY 99.99');
 e.views.orderDetail(w.hosts.detail,detail());
 assert.equal(w.hosts.detail.querySelector('[data-detail-total]').textContent,'CNY 99.99');
 assert.equal(w.q.get('[data-detail-items]').children[0].querySelector('[data-item-name]').textContent,'<img src=x onerror=bad()>');
 assert.equal(w.q.get('[data-detail-items]').children[0].querySelector('[data-item-total]').textContent,'CNY 88.00');
});
test('申请在本实例打开，返回重进及错误均保留数量、原因与请求键',()=>{
 const e=environment(),a=widget('a'),b=widget('b');e.widgets.push(a,b);prepare(e,a);
 assert.equal(a.panels.return.hidden,false);assert.equal(b.panels.list.hidden,false);assert.equal(b.panels.return.hidden,true);
 const reason=a.form.querySelector('[name="reason"]');reason.value='我的原因';
 const line=a.form.querySelector('[data-orders-return-lines]').children[0],qty=line.querySelector('[name="quantity"]');qty.value='';
 e.events['site:data:error']({target:a.hosts.returnable,detail:{status:503}});
 click(e,a,{'data-orders-show':'detail'});click(e,a,{'data-orders-apply':''});
 assert.equal(reason.value,'我的原因');assert.equal(qty.value,'');assert.equal(a.form.querySelector('[name="requestId"]').value,'intent-key');
 assert.equal(b.root.getAttribute('data-orders-active'),null);assert.equal(a.q.get('[data-orders-retry]').hidden,false);
});
test('具名写结果仅切换提交实例，其它实例刷新事实时保留申请输入',()=>{
 const e=environment(),a=widget('a'),b=widget('b');e.widgets.push(a,b);prepare(e,a);prepare(e,b);b.form.querySelector('[name="reason"]').value='未完成内容';
 assert.equal(e.handlers['commerce.orders']({form:a.form,data:{returnNo:'RMA-001',refundAmount:4400}}),true);
 assert.equal(a.panels.result.hidden,false);assert.equal(a.q.get('[data-orders-result-number]').textContent,'RMA-001');assert.equal(a.q.get('[data-orders-result-amount]').textContent,'CNY 44.00');
 assert.equal(b.panels.return.hidden,false);assert.equal(b.form.querySelector('[name="reason"]').value,'未完成内容');assert.equal(b.form.querySelector('[name="requestId"]').value,'intent-key');
 assert.ok(e.refreshed.includes(a.root));assert.ok(e.refreshed.includes(b.root));assert.equal(e.events['write:success'],undefined);
});
test('未登录隐藏私有面板，登录后只恢复本实例列表，不干扰另一个申请',()=>{
 const e=environment(),a=widget('a'),b=widget('b');e.widgets.push(a,b);prepare(e,b);
 e.events['site:data:error']({target:a.root,detail:{status:401}});assert.equal(a.q.get('[data-orders-guest]').hidden,false);
 assert.ok(Object.values(a.panels).every(p=>p.hidden));
 e.views.ordersList(a.root,{list:[],total:0});assert.equal(a.panels.list.hidden,false);assert.equal(a.q.get('[data-orders-empty]').hidden,false);assert.equal(b.panels.return.hidden,false);
 e.events['site:data']({target:{hasAttribute:()=>true},detail:{data:null}});
});
test('预览成功使用示例事实，不生成真实请求键或发起私有刷新',()=>{
 const e=environment(),w=widget('a');e.widgets.push(w);e.window.WPSitePreview={active:true,state:'success'};
 e.previews.ordersList(w.root,'success');assert.equal(w.panels.result.hidden,false);assert.equal(w.q.get('[data-orders-result-number]').textContent,'PREVIEW-RMA-001');assert.equal(w.form.querySelector('[name="requestId"]').value,'');assert.equal(e.refreshed.length,0);
});
test('多账号宿主的同一认证事实只刷新一次，订单当前申请步骤不被全局切换',()=>{
 const e=environment(),a=widget('a'),b=widget('b');e.widgets.push(a,b);prepare(e,b);
 const userRoot=node({'data-user-form':'account'}),event={target:userRoot,detail:{data:{authenticated:true}}};
 e.events['site:data'](event);e.events['site:data'](event);
 assert.deepEqual(e.refreshed,[a.root,b.root]);assert.equal(b.panels.return.hidden,false);
 assert.equal(a.root.getAttribute('data-orders-active'),null);assert.equal(b.root.getAttribute('data-orders-active'),'return');
});
