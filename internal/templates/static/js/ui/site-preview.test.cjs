const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function preview(mode,state){
 const events={};
 const window={location:{href:'https://site.test/zh/shop'},document:{querySelector(selector){return selector.includes('-mode')?(mode?{content:mode}:null):(state?{content:state}:null);},addEventListener:(key,fn)=>events[key]=fn}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/site-preview.js','utf8'),{window,URL});
 return {api:window.WPSitePreview,events};
}
test('模拟状态只来自预览meta，发布页面忽略遗留状态且上下文不可变',()=>{
 const published=preview(null,'success').api;assert.equal(published.active,false);assert.equal(published.state,'default');
 const {api}=preview('readonly','success');assert.equal(api.active,true);assert.equal(api.state,'success');assert.equal(Object.isFrozen(api),true);assert.equal(Object.isFrozen(api.states),true);
 assert.equal(preview('readonly','arbitrary-json').api.state,'default');
 assert.equal(preview('readonly','forbidden').api.state,'forbidden');
 assert.equal(preview('readonly','variant-required').api.state,'variant-required');
});
test('模拟预览阻止旧片段/私人JSON的HTMX读取，不影响静态资源',()=>{
 const {events}=preview('readonly');
 for(const [path,blocked] of [['/api/site/user/account',true],['/_fragments/orderList',true],['/static/logo.svg',false]]){
  let prevented=0;events['htmx:config:request']({detail:{ctx:{request:{action:path,method:'GET'}}},preventDefault:()=>prevented++});assert.equal(prevented,blocked?1:0);
 }
 assert.equal(preview(null).events['htmx:config:request'],undefined);
});
