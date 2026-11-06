const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 属主按真实位置显式定位：商品控件在 pkg 公共资产，site-commerce 仍是应用专用脚本。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');
const APP_ONLY = new Set(['site-commerce']);
const source = name => fs.readFileSync(path.join(APP_ONLY.has(name) ? __dirname : UI_SRC, name + '.js'), 'utf8');
const node = (attrs = {}) => ({
    textContent: '', hidden: false, children: [], classes: [],
    getAttribute(name) { return attrs[name] ?? null; },
    setAttribute(name, value) { attrs[name] = String(value); },
    removeAttribute(name) { delete attrs[name]; },
    appendChild(child) { this.children.push(child); return child; },
    replaceChildren() { this.children = []; },
    classList: {add() {}},
});
function queries(entries) { return {querySelector(selector) { assert.ok(entries[selector], selector); return entries[selector]; }}; }
function tagFixture(words = {}) {
    const entries=Object.fromEntries(['name','total','page-info','result','status'].map(key=>['[data-tag-' + key + ']',node()]));
    entries['[data-tag-rows]']=node();entries['[data-tag-page="-1"]']=node();entries['[data-tag-page="1"]']=node();
    const made=[];
    entries['[data-tag-row]']={content:{cloneNode(){const fields=Object.fromEntries(['name','slug','status'].map(key=>['[data-tag-product-'+key+']',node()]));const row={...queries(fields),fields};made.push(row);return row;}}};
    entries['[data-tag-empty]']={content:{cloneNode(){return {empty:true};}}};
    const panel={...queries(entries),...node({'data-page-info':'共 %s 条，第 %s-%s 条','data-count-text':'{count} 个商品','data-hit-text':'命中 {count} 个商品','data-failed':'读取失败',...words})};
    const trigger=node();const ui={},listeners={},pending=[];
    panel.isConnected=true;
    vm.runInNewContext(source('product-tag-hits'),{window:{WBUI:ui},document:{querySelector:()=>panel,getElementById:()=>trigger,addEventListener(name,handler){listeners[name]=handler;}},AbortController,URLSearchParams,fetch(url,options){const work=deferred();pending.push({url,options,...work});return work.promise;}});
    return {panel,entries,made,trigger,listeners,pending,render:ui.productTagHits.render};
}
test('标签总数、页码和行来自同一JSON结果，前端只算分页区间',()=>{
    const s=tagFixture(),state={};
    s.render(s.panel,{tagId:'t',tagName:'测试标签',total:21,page:2,pageSize:20,items:[{name:'<b>商品</b>',slug:'x',status:1}]},state);
    assert.equal(s.entries['[data-tag-page-info]'].textContent,'共 21 条，第 21-21 条');
    assert.equal(s.trigger.textContent,'21 个商品');
    assert.equal(s.entries['[data-tag-page="-1"]'].disabled,false);
    assert.equal(s.entries['[data-tag-page="1"]'].disabled,true);
    assert.equal(s.made[0].fields['[data-tag-product-name]'].textContent,'<b>商品</b>');
    assert.equal(state.page,2);
});
test('标签计数和命中说明使用完整译文，允许数量在句尾并覆盖零值',()=>{
    for (const count of [0, 21]) {
        const s=tagFixture({'data-count-text':'Products: {count}','data-hit-text':'Matching products: {count}'});
        s.render(s.panel,{tagId:'t',tagName:'标签',total:count,page:1,pageSize:20,items:[]},{});
        assert.equal(s.trigger.textContent,'Products: '+count);
        assert.equal(s.entries['[data-tag-total]'].textContent,'Matching products: '+count);
    }
});
test('标签无命中仍有空行与0-0分页，不保留旧记录',()=>{
    const s=tagFixture();s.render(s.panel,{tagId:'t',tagName:'空',total:0,page:1,pageSize:20,items:[]},{});
    assert.equal(s.entries['[data-tag-page-info]'].textContent,'共 0 条，第 0-0 条');
    assert.equal(s.entries['[data-tag-rows]'].children[0].empty,true);
    assert.equal(s.entries['[data-tag-page="1"]'].disabled,true);
});
function previewFixture(response) {
    const ui={register(){}},requests=[],listeners={},parsed=[];
    const frame=node(),body=node();frame.style={};body.style={};
    const popup={closed:false,close(){this.closed=true;},document:{title:'',body,createElement:()=>frame}};
    class DOMParser {
        parseFromString(html) {
            const base=node();
            parsed.push({html,base});
            return {head:{querySelector:()=>null,prepend(value){assert.equal(value,base);}},createElement:()=>base,documentElement:{outerHTML:html}};
        }
    }
    vm.runInNewContext(source('product-preview'),{window:{WBUI:ui,location:{href:'https://shop.test/admin/products/edit'},open:()=>popup},document:{addEventListener(name,handler){listeners[name]=handler;},title:'商品'},fetch:async(url,options)=>{requests.push(options);return typeof response==='function'?response():response;},URL,AbortController,DOMParser,FormData:class {}});
    return {api:ui.productPreview,requests,listeners,parsed,popup,frame};
}
test('预览失败保留JSON外部文案，禁止跟随重定向',async()=>{
    const s=previewFixture({ok:false,status:403,headers:{get:()=> 'application/json'},json:async()=>({code:403,message:'没有权限'})});
    await assert.rejects(s.api.readDocument('/preview',{},'失败'),error=>error.message==='没有权限' && error.productPreviewMessage===true);
    assert.equal(s.requests[0].redirect,'error');
});
test('预览的非成功HTML或非HTML响应不被误显示为成功文档',async()=>{
    for(const [ok,type] of [[false,'text/html'],[true,'text/plain']]) {
        const s=previewFixture({ok,headers:{get:()=>type}});
        await assert.rejects(s.api.readDocument('/preview',{},'读取失败'),/读取失败/);
    }
});
test('POST预览只消费统一JSON中的HTML产物，GET仍读取页面文档',async()=>{
    const html='<html><head></head><body>商品预览</body></html>';
    for(const method of ['GET','POST']) {
        const s=previewFixture(method==='POST'?jsonResponse({html},200,'成功'):{ok:true,headers:{get:()=> 'text/html'},text:async()=>html});
        assert.equal(await s.api.readDocument('/preview',{method},'读取失败'),'<!doctype html>\n'+html);
        assert.equal(s.parsed[0].html,html);
        assert.equal(s.parsed[0].base.href,'https://shop.test/');
        assert.equal(s.requests[0].redirect,'error');
        if(method==='POST') assert.equal(s.requests[0].headers.Accept,'application/json');
    }
});
test('POST预览拒绝旧HTML协议和非法JSON成功结构',async()=>{
    const responses=[
        {ok:true,headers:{get:()=> 'text/html'},text:async()=>'<html>旧协议</html>'},
        ...[null,{}, {html:''},{html:'  '},{html:123}].map(data=>jsonResponse(data,200,'成功')),
        {ok:true,headers:{get:()=> 'application/json'},json:async()=>null},
        {ok:true,headers:{get:()=> 'application/json'},json:async()=>({data:{html:'<html>缺code</html>'}})}
    ];
    for(const response of responses) {
        const s=previewFixture(response);
        await assert.rejects(s.api.readDocument('/preview',{method:'POST'},'读取失败'),/读取失败/);
        assert.equal(s.parsed.length,0);
    }
});
test('POST预览业务失败保留message，拒绝把失败data显示成文档',async()=>{
    for(const status of [200,403]) {
        const response=jsonResponse({html:'<html>不能展示</html>'},403,'没有权限');
        response.ok=status===200;
        const s=previewFixture(response);
        await assert.rejects(s.api.readDocument('/preview',{method:'POST'},'读取失败'),error=>error.message==='没有权限' && error.productPreviewMessage===true);
        assert.equal(s.parsed.length,0);
    }
});
test('POST预览失败关闭预开窗口，恢复按钮并释放重复提交锁',async()=>{
    for(const kind of ['business','parse','network']) {
        const response=kind==='business'?jsonResponse(null,403,'没有权限'):kind==='network'?async()=>{throw new TypeError('private network detail');}:{ok:true,headers:{get:()=> 'application/json'},json:async()=>{throw new SyntaxError('private JSON detail');}};
        const s=previewFixture(response),message=node(),button={type:'submit',disabled:false},disabled={type:'submit',disabled:true};
        const host={querySelector:()=>message};
        const form={...node({'data-preview-failed':'读取失败'}),matches:()=>true,closest:()=>host,elements:[button,disabled],action:'/preview'};
        const event={target:form,preventDefault(){}};
        const pending=s.listeners.submit(event);
        assert.equal(button.disabled,true);
        await s.listeners.submit(event);
        assert.equal(s.requests.length,1,'进行中的请求不能重复提交');
        await pending;
        assert.equal(s.popup.closed,true);
        assert.equal(button.disabled,false);
        assert.equal(disabled.disabled,true);
        assert.equal(message.textContent,kind==='business'?'没有权限':'读取失败');
        assert.equal(s.frame.srcdoc,undefined);
        await s.listeners.submit(event);
        assert.equal(s.requests.length,2,'失败后必须可以再次提交');
    }
});
test('POST预览JSON成功在沙箱展示，按钮恢复且不关闭窗口',async()=>{
    const html='<html><head></head><body>选中模板</body></html>';
    const s=previewFixture(jsonResponse({html},200,'成功')),message=node(),button={type:'submit',disabled:false};
    const form={...node({'data-preview-failed':'读取失败'}),matches:()=>true,closest:()=>({querySelector:()=>message}),elements:[button],action:'/preview'};
    await s.listeners.submit({target:form,preventDefault(){}});
    assert.equal(s.popup.closed,false);
    assert.equal(s.popup.opener,null);
    assert.equal(button.disabled,false);
    assert.equal(message.textContent,'');
    assert.equal(s.frame.srcdoc,'<!doctype html>\n'+html);
    assert.equal(s.frame.getAttribute('sandbox'),'allow-scripts');
});
test('新窗口只接收已验证文档，并保留iframe沙箱',()=>{
    const s=previewFixture({}),frame=node(),body=node();body.style={};frame.style={};
    const popup={closed:false,document:{title:'',body,createElement:()=>frame}};
    assert.equal(s.api.showDocument(popup,'<h1>商品</h1>','预览'),true);
    assert.equal(frame.getAttribute('sandbox'),'allow-scripts');
    assert.doesNotMatch(frame.getAttribute('sandbox'),/allow-same-origin/);
    assert.equal(frame.srcdoc,'<h1>商品</h1>');
    assert.equal(body.children[0],frame);
});

function deferred() { let resolve, reject; const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject}; }
const flush = () => new Promise(resolve => setImmediate(resolve));
const jsonResponse = (data, status=200, message='') => ({ok:status<400,headers:{get:()=> 'application/json'},json:async()=>({code:status,data,message})});
test('标签快速切换忽略迟到响应，错误停留在当前列表面板',async()=>{
    const s=tagFixture();
    function click(id){const trigger={getAttribute:name=>name==='data-product-tag-hits'?id:'project'};s.listeners.click({target:{closest:selector=>selector==='[data-product-tag-hits]'?trigger:null}});}
    click('old');click('new');
    assert.equal(s.pending[0].options.signal.aborted,true);
    assert.equal(s.pending[1].options.redirect,'error');
    s.pending[1].resolve(jsonResponse({tagId:'new',tagName:'新标签',total:0,page:1,pageSize:20,items:[]}));await flush();
    s.pending[0].resolve(jsonResponse({tagId:'old',tagName:'旧标签',total:0,page:1,pageSize:20,items:[]}));await flush();
    assert.equal(s.entries['[data-tag-name]'].textContent,'新标签');
    click('forbidden');s.pending[2].resolve(jsonResponse(null,403,'没有权限'));await flush();
    assert.equal(s.entries['[data-tag-status]'].textContent,'没有权限');
    assert.equal(s.entries['[data-tag-status]'].hidden,false);
    assert.equal(s.entries['[data-tag-result]'].hidden,true);
});
test('父分类候选只更新当前搜索，既有选择不被请求响应清空',async()=>{
    const listeners={},timers=[],pending=[],results=node();
    const own={value:'self'},select={value:'chosen'};
    const picker={...node({'data-category-project':'p','data-category-empty':'无匹配','data-category-failed':'读取失败'}),isConnected:true,querySelector:selector=>selector==='[data-category-parent-results]'?results:select,closest:()=>({querySelector:()=>own})};
    const input={value:'old',matches:()=>true,closest:()=>picker};
    const context={document:{addEventListener(name,handler){listeners[name]=handler;},createElement:()=>node()},window:{},WeakMap,URLSearchParams,AbortController,setTimeout(fn){timers.push(fn);return timers.length;},clearTimeout(){},fetch(url,options){const work=deferred();pending.push({url,options,...work});return work.promise;}};
    vm.runInNewContext(source('product-category-tree'),context);
    listeners.input({target:input});timers[0]();input.value='new';listeners.input({target:input});timers[1]();
    assert.equal(pending[0].options.signal.aborted,true);
    pending[1].resolve(jsonResponse([{id:'self',name:'自身',depth:0},{id:'new',name:'<img onerror=bad()>',depth:1}]));await flush();
    pending[0].resolve(jsonResponse([{id:'old',name:'旧结果',depth:0}]));await flush();
    assert.equal(results.children.length,1);
    assert.equal(results.children[0].getAttribute('data-category-parent-choice'),'new');
    assert.equal(results.children[0].textContent,'　<img onerror=bad()>');
    assert.equal(select.value,'chosen');
});

// 两次只读查询间发生删除时，列表可能暂时为空，不能显示“21-0”。
test('标签分页展示区间按实际行数，非零总数的空页也是0-0', () => {
    const s=tagFixture();
    s.render(s.panel,{tagId:'t',tagName:'标签',total:21,page:2,pageSize:20,items:[]},{});
    assert.match(s.entries['[data-tag-page-info]'].textContent,/0-0/);
});

test('分类和品牌编辑表单的默认提交仍是保存，SEO为显式次要操作',()=>{
    for (const [name, marker] of [['product_category_form','<form data-write hx-post='],['product_brands','<template id="tpl-brand-edit-']]) {
        const raw=fs.readFileSync(__dirname+'/../../../admin/product/'+name+'.html','utf8');
        const start=raw.indexOf(marker), form=raw.slice(start,raw.indexOf('</form>',start));
        const submits=Array.from(form.matchAll(/<button\b[^>]*type="submit"[^>]*>/g)).map(match=>match[0]);
        assert.equal(submits.length,1,name);
        assert.doesNotMatch(submits[0],/data-write-handler|seo-score|hx-post="[^"]*seo-score"/);
        assert.match(submits[0],/btn-primary/);
        const seo=raw.indexOf('include "product_seo_form.html"',start);
        assert.ok(seo > raw.indexOf('</form>',start),'SEO 必须处于保存表单外');
        assert.doesNotMatch(raw,/data-write-handler="product\.seo"/);
    }
});
test('标签读取的网络和JSON解析错误只显示模板失败文案',async()=>{
    for (const parseFailure of [false,true]) {
        const s=tagFixture(),trigger={getAttribute:name=>name==='data-product-tag-hits'?'tag':'project'};
        s.listeners.click({target:{closest:selector=>selector==='[data-product-tag-hits]'?trigger:null}});
        if (parseFailure) s.pending[0].resolve({ok:true,headers:{get:()=> 'application/json'},json:async()=>{throw new SyntaxError('secret transport detail');}});
        else s.pending[0].reject(new TypeError('Failed to fetch secret endpoint'));
        await flush();
        assert.equal(s.entries['[data-tag-status]'].textContent,'读取失败');
        assert.equal(s.entries['[data-tag-status]'].hidden,false);
    }
});
test('父分类读取只透出JSON业务提示，网络及解析错误统一失败文案',async()=>{
    const listeners={},timers=[],pending=[],results=node(),select={value:'chosen'};
    const picker={...node({'data-category-project':'p','data-category-empty':'无匹配','data-category-failed':'读取失败'}),isConnected:true,querySelector:selector=>selector==='[data-category-parent-results]'?results:select,closest:()=>({querySelector:()=>null})};
    const input={value:'x',matches:()=>true,closest:()=>picker};
    vm.runInNewContext(source('product-category-tree'),{document:{addEventListener:(name,handler)=>listeners[name]=handler,createElement:()=>node()},window:{},WeakMap,URLSearchParams,AbortController,setTimeout(fn){timers.push(fn);return timers.length;},clearTimeout(){},fetch(){const work=deferred();pending.push(work);return work.promise;}});
    for (const [index,expected] of ['读取失败','读取失败','没有权限'].entries()) {
        listeners.input({target:input});timers[index]();
        if(index===0) pending[index].reject(new TypeError('Failed to fetch secret endpoint'));
        else if(index===1) pending[index].resolve({ok:true,headers:{get:()=> 'application/json'},json:async()=>{throw new SyntaxError('unexpected secret JSON');}});
        else pending[index].resolve(jsonResponse(null,403,expected));
        await flush();assert.equal(results.textContent,expected);assert.equal(select.value,'chosen');
    }
});
test('商品缩略、放大与新窗口均允许脚本但不给同源权限，popup拦截即时提示',()=>{
    const edit=fs.readFileSync(__dirname+'/../../../admin/product/product_edit.html','utf8');
    assert.match(edit,/<iframe[^>]*sandbox="allow-scripts"[^>]*data-product-preview-frame/);
    const elements=[],messages=[],rootEvents={},frame=node({'srcdoc':'<h1>商品</h1>'}),host=node({'data-preview-failed':'无法打开预览'});
    const root={...node({'data-preview-zoom':'详情预览'}),querySelector:()=>frame,closest:()=>host,addEventListener:(name,handler)=>rootEvents[name]=handler};
    let init;
    const ui={register(fn){init=fn;},$$:()=>[root],markOnce:()=>true,toast:(text,options)=>messages.push({text,options}),productPreview:{openWindow:()=>null,showDocument:popup=>!!popup}};
    function create(tag){const element={...node(),tag,events:{},style:{setProperty(){}},clientWidth:900,addEventListener(name,handler){this.events[name]=handler;},focus(){},remove(){}};elements.push(element);return element;}
    const body=create('body');
    vm.runInNewContext(source('preview-zoom'),{window:{WBUI:ui},document:{body,createElement:create,addEventListener(){},removeEventListener(){}}});
    init({});rootEvents.click();
    const zoomFrame=elements.find(element=>element.tag==='iframe');
    assert.equal(zoomFrame.getAttribute('sandbox'),'allow-scripts');
    assert.equal(zoomFrame.srcdoc,'<h1>商品</h1>');
    const full=elements.find(element=>element.tag==='a');let prevented=false;
    full.events.click({preventDefault(){prevented=true;}});
    assert.equal(prevented,true);assert.equal(messages.length,1);
    assert.equal(messages[0].text,'无法打开预览');assert.equal(messages[0].options.type,'error');
});
test('opaque srcdoc加载真实预览基座和购物车模拟控件，不执行真实数据请求',async()=>{
    let fetches=0;
    const root={...node({'data-site-view':'cartCount','data-site-read':'/api/site/cart'}),isConnected:true,querySelector:()=>null,dispatchEvent(){}};
    const document={readyState:'loading',baseURI:'https://shop.test/',querySelector:selector=>selector.includes('-mode')?{content:'readonly'}:{content:'default'},querySelectorAll:()=>[],addEventListener(){}};
    const window={document,location:{href:'about:srcdoc',origin:'null'},WBUI:{writeBase:{registerResultHandler(){}}},fetch:async()=>{fetches++;throw new Error('禁止真实读取');}};
    const context=vm.createContext({window,URL,Map,Promise,Number,String,CustomEvent:class {constructor(type,options){this.type=type;Object.assign(this,options);}}});
    for(const name of ['site-preview','site-data','site-commerce']) vm.runInContext(source(name),context);
    await window.WPSiteData.refresh(root);
    assert.equal(window.WPSitePreview.active,true);
    assert.equal(root.textContent,'2');assert.equal(root.getAttribute('data-site-simulated'),'true');
    assert.equal(root.getAttribute('data-site-error'),null);assert.equal(fetches,0);
});
