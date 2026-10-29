const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function element(attrs) {
    return { attrs:{...attrs}, textContent:'原内容', isConnected:true, events:[],
        classes:new Set(), getAttribute(k) { return this.attrs[k] ?? null; },
        setAttribute(k,v) { this.attrs[k]=String(v); }, removeAttribute(k) { delete this.attrs[k]; },
        querySelector() { return null; }, dispatchEvent(ev) { this.events.push(ev); },
        get classList() { return {toggle:(k,on)=>on?this.classes.add(k):this.classes.delete(k)}; }
    };
}
function env(fetch, preview) {
    const document={readyState:'loading',addEventListener(){},querySelectorAll(){return [];}};
    const window={document,WPSitePreview:preview,location:{href:'https://shop.test/shop',origin:'https://shop.test'},fetch};
    const ctx=vm.createContext({window,URL,Map,Promise,CustomEvent:class {constructor(type, options){this.type=type;Object.assign(this,options);}}});
    for (const name of ['site-data','site-product']) vm.runInContext(fs.readFileSync(__dirname+'/'+name+'.js','utf8'),ctx);
    return window.WPSiteData;
}
const labels={ 'data-label-delisted':'已下架','data-label-updated':'价格更新为 %s',
    'data-label-unknown':'以结算为准','data-label-out':'缺货','data-label-low':'仅剩 %d 件','data-label-in':'充足'};
function target(view) { return element({...labels,'data-site-view':view,'data-site-read':'/api/site/product/variants?projectId=p&variantIds=v','data-variant-id':'v','data-price':'9900','data-currency':'¥'}); }

test('同一实时数据请求同时服务价格和库存，事实不携带 HTML', async()=>{
    let calls=0;
    const api=env(async()=>{calls++;return {ok:true,json:async()=>({code:200,data:[{id:'v',known:true,enabled:true,price:12950,available:3}]})};});
    const price=target('variantPrice'),stock=target('variantStock');
    await Promise.all([api.refresh(price),api.refresh(stock)]);
    assert.equal(calls,1);
    assert.equal(price.textContent,'价格更新为 ¥129.5');
    assert.equal(stock.textContent,'仅剩 3 件');
    assert.equal(price.attrs['aria-busy'],'false');
});

test('价格一致、未知、下架和库存零值的展示各自正确', async()=>{
    let value;
    const api=env(async()=>({ok:true,json:async()=>({code:0,data:[value]})}));
    for (const [fact,priceText,stockText] of [
        [{known:true,enabled:true,price:9900,available:9},'','充足'],
        [{known:true,enabled:false,price:9900,available:9},'已下架','缺货'],
        [{known:true,enabled:true,price:12900,available:0},'价格更新为 ¥129','缺货'],
        [{known:false,enabled:false,price:null,available:null},'','以结算为准'],
        [{known:true,enabled:true,price:9900,available:null},'','以结算为准']
    ]) {
        value={id:'v',...fact};
        const price=target('variantPrice'),stock=target('variantStock');
        await api.refresh(price); await api.refresh(stock);
        assert.equal(price.textContent,priceText); assert.equal(stock.textContent,stockText);
    }
});

test('拒绝外站或非公开数据地址；失败保留内容，迟到响应不能覆盖新数据', async()=>{
    let calls=0, release;
    const api=env(()=>{ calls++; return new Promise(resolve=>{release=resolve;}); });
    const price=target('variantPrice'); price.attrs['data-site-read']='https://evil.test/api/site/product/variants';
    await api.refresh(price);
    assert.equal(calls,0); assert.equal(price.textContent,'原内容');
    assert.equal(price.attrs['data-site-error'],'true');
    price.attrs['data-site-read']='/api/site/product/variants?a=1';
    const first=api.refresh(price); await Promise.resolve(); await Promise.resolve();
    const finishFirst=release;
    price.attrs['data-site-read']='/api/site/product/variants?a=2';
    const second=api.refresh(price); await Promise.resolve(); await Promise.resolve();
    release({ok:true,json:async()=>({code:200,data:[{id:'v',known:true,enabled:true,price:14900}]})}); await second;
    finishFirst({ok:true,json:async()=>({code:200,data:[{id:'v',known:true,enabled:true,price:12900}]})}); await first;
    assert.equal(price.textContent,'价格更新为 ¥149');
});

test('非成功状态和业务错误不清空原内容，恢复后移除错误态', async()=>{
    let ok=false;
    const api=env(async()=>({ok,json:async()=>ok?{code:200,data:[]}:{code:500,message:'暂不可用'}}));
    const price=target('variantPrice'); await api.refresh(price);
    assert.equal(price.textContent,'原内容'); assert.equal(price.attrs['data-site-error'],'true');
    assert.equal(price.events.at(-1).detail.message,'暂不可用');
    ok=true; await api.refresh(price);
    assert.equal(price.textContent,''); assert.equal(price.attrs['data-site-error'],undefined);
});

test('模拟预览零真实读取，跳过prepare并只消费组件声明的状态',async()=>{
    let fetches=0,preparations=0,realRenders=0;
    const api=env(async()=>{fetches++;throw new Error('预览不得读取真实session');},{active:true,state:'forbidden'});
    const states=[];
    api.register('privateAccount',()=>realRenders++,()=>preparations++);
    api.registerPreview('privateAccount',(root,state)=>{states.push(state);root.textContent='模拟 '+state;},['default','forbidden']);
    const root=element({'data-site-view':'privateAccount','data-site-read':'/api/site/user/account'});
    await api.refresh(root);
    assert.equal(fetches,0);assert.equal(preparations,0);assert.equal(realRenders,0);
    assert.deepEqual(states,['forbidden']);assert.equal(root.textContent,'模拟 forbidden');assert.equal(root.attrs['data-site-simulated'],'true');
    assert.equal(root.attrs['aria-busy'],'false');assert.equal(root.events.length,0);
});
test('组件未声明当前模拟状态则回default，未接模拟renderer仍不得读真实数据',async()=>{
    let fetches=0;
    const api=env(async()=>{fetches++;},{active:true,state:'out-of-stock'});
    api.register('privateAccount',()=>{throw new Error('不能用真实renderer');});
    const root=element({'data-site-view':'privateAccount','data-site-read':'/api/site/user/account'});
    await api.refresh(root);assert.equal(root.textContent,'原内容');
    api.registerPreview('privateAccount',(node,state)=>node.textContent=state,['default','unauthenticated']);
    await api.refresh(root);assert.equal(root.textContent,'default');assert.equal(fetches,0);
});


test('读取失败事件携带真实HTTP状态，非JSON错误不泄漏解析原文', async()=>{
    let response;
    const api=env(async()=>response);
    const root=target('variantPrice');
    for(const status of [401,403,404,500]) {
        response={ok:false,status,json:async()=>({code:status,message:'请求未完成'})};
        await api.refresh(root);
        assert.equal(root.events.at(-1).detail.status,status);
        assert.equal(root.events.at(-1).detail.message,'请求未完成');
        assert.equal(root.textContent,'原内容');
    }
    response={ok:false,status:502,json:async()=>{throw new SyntaxError('内部网关响应原文');}};
    await api.refresh(root);
    assert.equal(root.events.at(-1).detail.status,502);
    assert.equal(root.events.at(-1).detail.message,'数据读取失败');
});
