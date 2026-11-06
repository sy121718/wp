const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path');
const vm=require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');
function element(attrs={}) {
    return {attrs:{...attrs},isConnected:true,classes:new Set(),getAttribute(k){return this.attrs[k]??null;},setAttribute(k,v){this.attrs[k]=String(v);},removeAttribute(k){delete this.attrs[k];},hasAttribute(k){return Object.hasOwn(this.attrs,k);},querySelector(){return null;},querySelectorAll(){return [];},dispatchEvent(){},get classList(){return {toggle:(k,v)=>v?this.classes.add(k):this.classes.delete(k)};}};
}
function setup(href='https://shop.test/catalog',fetch) {
    const listeners={},views={},prepared={},location=new URL(href),roots=[];
    const document={readyState:'loading',addEventListener(k,fn){listeners[k]=fn;},querySelectorAll(){return roots;}};
    const window={document,location,fetch,addEventListener(){},history:{pushState(_a,_b,url){const u=new URL(url,location);location.href=u.href;}}};
    const context=vm.createContext({window,URL,URLSearchParams,Map,Promise,CustomEvent:class {constructor(type,options){this.type=type;Object.assign(this,options);}}});
    if(fetch) vm.runInContext(fs.readFileSync(path.join(UI_SRC, 'site-data.js'),'utf8'),context);
    else window.WPSiteData={register(name,render,prepare){views[name]=render;prepared[name]=prepare;},refresh(root){prepared[root.attrs['data-site-view']](root);return Promise.resolve();}};
    vm.runInContext(fs.readFileSync(path.join(UI_SRC, 'site-catalog.js'),'utf8'),context);
    function click(root,query) {
        const link=element({'data-catalog-query':query});link.closest=()=>root;
        listeners.click({target:{closest:()=>link},button:0,preventDefault(){}});
    }
    return {window,roots,views,prepared,click,location};
}
function catalog(defaultQuery='') {return element({'data-site-view':'catalog','data-site-read':'/api/site/product/catalog?projectId=p&lang=en-US&filter=%7B%7D&sort=default&page=1&pageSize=12','data-catalog-default-query':defaultQuery});}

test('多选连续切换、默认排序恢复与清除默认筛选，数据请求只带事实参数',()=>{
    const env=setup('https://shop.test/catalog?other=keep&page=4&layout=list&columns=4');
    const root=catalog('brandIds=a&orderBy=default&pageSize=12');
    env.click(root,'brandIds=a,b&brandId=&page=');
    let u=new URL(root.attrs['data-site-read'],'https://shop.test');
    assert.deepEqual(JSON.parse(u.searchParams.get('filter')),{brandIds:'a,b'});
    assert.equal(u.searchParams.get('page'),'1');
    env.click(root,'brandIds=a,b&brandId=&page=');
    assert.equal(new URLSearchParams(env.location.search).get('brandIds'),'a');
    env.click(root,'brandIds=&brandId=&page=');
    assert.equal(new URLSearchParams(env.location.search).get('brandIds'),'');
    env.click(root,'orderBy=priceDesc&page=');
    assert.equal(new URL(root.attrs['data-site-read'],'https://shop.test').searchParams.get('sort'),'priceDesc');
    env.click(root,'orderBy=default&page=');
    u=new URL(root.attrs['data-site-read'],'https://shop.test');
    assert.equal(u.searchParams.get('sort'),'default');
    assert.deepEqual([...u.searchParams.keys()].sort(),['filter','lang','page','pageSize','projectId','sort']);
    assert.equal(new URLSearchParams(env.location.search).get('other'),'keep');
});

test('单选分类能从其它分类切回作者默认分类，旧单值地址仍正确取消',()=>{
    const env=setup('https://shop.test/catalog?categoryIds=b');
    const root=catalog('categoryId=a');root.attrs['data-catalog-category-single']='true';
    env.click(root,'categoryIds=&categoryId=&page=');
    assert.equal(new URLSearchParams(env.location.search).get('categoryIds'),'a');
    env.click(root,'categoryIds=&categoryId=&page=');
    assert.equal(new URLSearchParams(env.location.search).get('categoryIds'),'');
    const legacy=setup('https://shop.test/catalog?categoryId=a');
    legacy.click(catalog('categoryId=a'),'categoryIds=&categoryId=&page=');
    assert.equal(new URLSearchParams(legacy.location.search).get('categoryIds'),'');
});

test('搜索失败保留输入及已有结果，新请求完成后迟到响应不覆盖',async()=>{
    const pending=[];
    const env=setup('https://shop.test/search?q=old',()=>new Promise(resolve=>pending.push(resolve)));
    const root=element({'data-site-view':'search','data-site-read':'/api/site/search?projectId=p'}),input={value:''},status={textContent:''},host={textContent:'已有结果'};
    root.querySelector=(selector)=>selector==='[name="q"]'?input:selector==='[data-site-status]'?status:selector==='[data-search-results]'?host:null;
    root._searchReady=true;input.value='用户未提交内容';
    const first=env.window.WPSiteData.refresh(root);await Promise.resolve();await Promise.resolve();
    input.value='最新关键词';const second=env.window.WPSiteData.refresh(root);await Promise.resolve();await Promise.resolve();
    pending[1]({ok:false,json:async()=>({code:503,message:'服务暂不可用'})});await second;
    assert.equal(input.value,'最新关键词');assert.equal(host.textContent,'已有结果');assert.equal(status.textContent,'服务暂不可用');
    pending[0]({ok:true,json:async()=>({code:200,data:{articles:[],products:[]}})});await first;
    assert.equal(input.value,'最新关键词');assert.equal(host.textContent,'已有结果');assert.equal(status.textContent,'服务暂不可用');
});
