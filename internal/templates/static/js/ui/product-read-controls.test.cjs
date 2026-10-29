const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = name => fs.readFileSync(__dirname + '/' + name + '.js', 'utf8');
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
function seoFixture() {
    const handlers = {}, listeners = {}, sections = node(), duplicates = node();
    const entries = Object.fromEntries(['total','grade','profile','duplicates','duplicate-count','coverage','omitted'].map(key => ['[data-seo-' + key + ']', node()]));
    ['title','url','description'].forEach(key => { entries['[data-seo-' + key + ']'] = node({'data-empty':'未填写'}); });
    entries['[data-seo-sections]'] = sections;
    entries['[data-seo-duplicate-list]'] = duplicates;
    const renderedSections = [], renderedChecks = [];
    entries['[data-seo-section]'] = {content: {cloneNode() {
        const rowEntries = Object.fromEntries(['section-name','section-value','section-weight','checks','dot'].map(key=>['[data-seo-' + key + ']',node()]));
        const row = {...queries(rowEntries), entries: rowEntries}; renderedSections.push(row); return row;
    }}};
    const checkTemplate = {...node({'data-seo-check':'title_present'}),content:{cloneNode(){
        const value=node();const item={...queries({'[data-seo-check-value]':value}), value};renderedChecks.push(item);return item;
    }}};
    const gradeLabel = {...node({'data-seo-grade-label':'green'}),textContent:'优秀'};
    const sectionLabel = {...node({'data-seo-section-label':'title'}),textContent:'标题标签'};
    const profiles = ['', 'product', 'landing', 'guide'].map(key => ({...node({'data-seo-profile-label':key}),textContent:({'':'默认','product':'商品','landing':'落地页','guide':'长文指南'})[key]}));
    const root = {...queries(entries),hidden:true,querySelectorAll(selector){return {'[data-seo-profile-label]':profiles,'[data-seo-check]':[checkTemplate],'[data-seo-grade-label]':[gradeLabel],'[data-seo-section-label]':[sectionLabel]}[selector] || [];}};
    const ui={writeBase:{registerResultHandler(name,handler){handlers[name]=handler;}},register(){}};
    vm.runInNewContext(source('product-seo'), {window:{WBUI:ui},document:{getElementById:()=>({content:root}),createElement:()=>node(),addEventListener(name,handler){listeners[name]=handler;}}});
    const form = {querySelector:()=>root};
    return {root,entries,sections,duplicates,renderedSections,renderedChecks,handler:handlers['product.seo'],form};
}
test('SEO结果只消费服务端评分，未知HTML值保持文本，未覆盖来源显式显示', () => {
    const s=seoFixture();
    const data={total:91,title:'<script>bad()</script>',url:'/a',description:'说明',sections:[{key:'title',score:3,max:10,percent:30,checks:[{key:'title_present',score:3,max:5,skipped:false},{key:'title_present',score:0,max:5,skipped:true}]}],duplicates:[{id:'1',title:'<img onerror=bad()>',path:'/b'}],coverage:{omitted:[{source:'article',reason:'project_filter_unavailable'}]}};
    assert.equal(s.handler({form:s.form,data}),true,'评分不能执行编辑表单的关抽屉/导航');
    assert.equal(s.entries['[data-seo-total]'].textContent,'91');
    assert.equal(s.entries['[data-seo-grade]'].textContent,'优秀');
    assert.equal(s.entries['[data-seo-title]'].textContent,data.title);
    assert.equal(s.renderedChecks.length,1,'跳过的检查不作为失败提示');
    assert.equal(s.renderedChecks[0].value.textContent,'3/5');
    assert.equal(s.duplicates.children[0].textContent,'<img onerror=bad()> · /b');
    assert.equal(s.entries['[data-seo-coverage]'].hidden,false);
    assert.equal(s.entries['[data-seo-omitted]'].textContent,'article');
});
function tagFixture() {
    const entries=Object.fromEntries(['name','total','page-info','result','status'].map(key=>['[data-tag-' + key + ']',node()]));
    entries['[data-tag-rows]']=node();entries['[data-tag-page="-1"]']=node();entries['[data-tag-page="1"]']=node();
    const made=[];
    entries['[data-tag-row]']={content:{cloneNode(){const fields=Object.fromEntries(['name','slug','status'].map(key=>['[data-tag-product-'+key+']',node()]));const row={...queries(fields),fields};made.push(row);return row;}}};
    entries['[data-tag-empty]']={content:{cloneNode(){return {empty:true};}}};
    const panel={...queries(entries),...node({'data-page-info':'共 %s 条，第 %s-%s 条','data-count-tail':' 个商品','data-failed':'读取失败'})};
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
test('标签无命中仍有空行与0-0分页，不保留旧记录',()=>{
    const s=tagFixture();s.render(s.panel,{tagId:'t',tagName:'空',total:0,page:1,pageSize:20,items:[]},{});
    assert.equal(s.entries['[data-tag-page-info]'].textContent,'共 0 条，第 0-0 条');
    assert.equal(s.entries['[data-tag-rows]'].children[0].empty,true);
    assert.equal(s.entries['[data-tag-page="1"]'].disabled,true);
});
function previewFixture(response) {
    const ui={register(){}},requests=[];
    vm.runInNewContext(source('product-preview'),{window:{WBUI:ui,location:{href:'https://shop.test/admin/products/edit'}},document:{addEventListener(){},title:'商品'},fetch:async(url,options)=>{requests.push(options);return response;},URL,AbortController});
    return {api:ui.productPreview,requests};
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
    for (const [name, marker] of [['product_category_form','<form data-write="custom"'],['product_brands','<template id="tpl-brand-edit-']]) {
        const raw=fs.readFileSync(__dirname+'/../../../admin/product/'+name+'.html','utf8');
        const start=raw.indexOf(marker), form=raw.slice(start,raw.indexOf('</form>',start));
        const submits=Array.from(form.matchAll(/<button\b[^>]*type="submit"[^>]*>/g)).map(match=>match[0]);
        assert.ok(submits.length>=2,name);
        assert.doesNotMatch(submits[0],/data-write-handler|seo-score/);
        assert.match(submits[0],/btn-primary/);
        assert.match(submits[1],/data-write-handler="product\.seo"/);
        assert.match(submits[1],/formnovalidate/);
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

test('SEO页型消费模板字典，维度权重直接格式化服务端事实',()=>{
    const s=seoFixture();
    for(const [profileType,expected] of [['product','商品'],['landing','落地页'],['guide','长文指南'],['','默认'],['<custom-profile>','<custom-profile>']]) {
        const data={profileType,total:42,sections:[{key:'title',score:3,max:10,percent:30,weight:0.1234,checks:[]}],duplicates:[],coverage:{omitted:[]}};
        s.handler({form:s.form,data});
        assert.equal(s.entries['[data-seo-profile]'].textContent,expected);
        assert.equal(s.renderedSections.at(-1).entries['[data-seo-section-weight]'].textContent,'12.34%');
        assert.equal(s.entries['[data-seo-total]'].textContent,'42','不得通过显示权重重新计算总分');
        assert.equal(data.sections[0].weight,0.1234);
    }
    s.handler({form:s.form,data:{profileType:'product',total:0,sections:[{key:'title',score:0,max:10,percent:0,weight:0,checks:[]}],duplicates:[],coverage:{omitted:[]}}});
    assert.equal(s.renderedSections.at(-1).entries['[data-seo-section-weight]'].textContent,'0%');
});
