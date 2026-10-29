const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(__dirname+'/write-base.js','utf8');
class Body {constructor(){this.values=new Map();}append(k,v){this.values.set(k,String(v));}set(k,v){this.values.set(k,String(v));}delete(k){this.values.delete(k);}get(k){return this.values.get(k);}getAll(k){return this.values.has(k)?[this.values.get(k)]:[];}entries(){return this.values.entries();}}
function env(attrs={},response={ok:true,status:200,payload:{code:200,message:'已保存',data:{id:'a1'}}}){
 const listeners={},nav=[],calls=[],status={textContent:'',setAttribute(){},querySelector(){return null;},appendChild(){}};
 const values={'data-write':'custom','data-write-url':'/api/site/user/login',...attrs};
 const instance={getAttribute:()=> 'instance-A'},input={name:'account',value:'用户已填写的值',type:'text',disabled:false,tagName:'INPUT',getAttribute:n=>n==='name'?'account':null,hasAttribute:()=>false};
 const form={tagName:'FORM',getAttribute:n=>values[n]??null,hasAttribute:n=>n in values,setAttribute(){},closest:()=>instance,dispatchEvent(){},
 querySelector:s=>s==='[data-write-status]'?status:null,querySelectorAll:s=>s==='[name]'?[input]:[]};
 const document={readyState:'loading',addEventListener:(n,f)=>{listeners[n]=f;},querySelector:selector=>response.preview && selector==='meta[name="site-preview-mode"][content="readonly"]'?{}:null,createElement:()=>({setAttribute(){},appendChild(){},textContent:''})};
 const window={document,location:{assign:url=>nav.push(url),reload:()=>nav.push('reload')},WPSiteSession:{csrfToken:'visitor-csrf'},confirm:()=>true};
 const context={window,document,FormData:Body,CustomEvent:class{},sessionStorage:{},fetch:async(url,opts)=>{calls.push({url,opts});return{ok:response.ok,status:response.status,redirected:!!response.redirected,url:'/admin/login',headers:{get:n=>n==='Content-Type'?(response.type||'application/json'):null},json:async()=>response.payload};}};
 vm.createContext(context);vm.runInContext(source,context);context.WBUI=window.WBUI;document.activeElement=input;
 return {window,form,status,input,nav,calls,context,listeners,submit:async()=>{listeners.submit({target:form,submitter:null,preventDefault(){}});await new Promise(setImmediate);await new Promise(setImmediate);}};
}
test('具名成功处理只执行一次，带当前实例与完整上下文并可接管导航',async()=>{
 const e=env({'data-write-handler':'user.forms','data-write-nav':'/account'});let seen=[];
 e.window.WBUI.writeBase.registerResultHandler('user.forms',ctx=>{seen.push(ctx);return true;});
 await e.submit();assert.equal(e.calls.length,1);assert.equal(seen.length,1);assert.equal(seen[0].instanceId,'instance-A');assert.equal(seen[0].form,e.form);assert.equal(seen[0].data.id,'a1');assert.equal(seen[0].payload.code,200);assert.equal(seen[0].text,'已保存');assert.deepEqual(e.nav,[]);
 assert.equal(e.calls[0].opts.headers['X-CSRF-Token'],'visitor-csrf');
});
test('返回false保留原导航；重复注册拒绝，基座重复加载保留已有注册',async()=>{
 const e=env({'data-write-handler':'user.forms','data-write-nav':'/account'});let calls=0;
 const fn=()=>{calls++;return false;};e.window.WBUI.writeBase.registerResultHandler('user.forms',fn);
 assert.throws(()=>e.window.WBUI.writeBase.registerResultHandler('user.forms',fn),/重复注册/);
 vm.runInContext(source,e.context);await e.submit();assert.equal(calls,1);assert.deepEqual(e.nav,['/account']);
});
test('handler与整页reload互斥、未知能力均在请求前拒绝',async()=>{
 const conflict=env({'data-write-handler':'user.forms','data-write-reload':''});conflict.window.WBUI.writeBase.registerResultHandler('user.forms',()=>true);await conflict.submit();assert.equal(conflict.calls.length,0);assert.match(conflict.status.textContent,/不能同时/);
 const unknown=env({'data-write-handler':'unknown'});await unknown.submit();assert.equal(unknown.calls.length,0);assert.match(unknown.status.textContent,/未注册/);
});
test('401错误不调用成功处理、不清空输入、不导航',async()=>{
 const e=env({'data-write-handler':'user.forms'},{ok:false,status:401,payload:{code:401,message:'请重新登录',data:null}});let called=false;e.window.WBUI.writeBase.registerResultHandler('user.forms',()=>{called=true;return true;});
 await e.submit();assert.equal(called,false);assert.equal(e.input.value,'用户已填写的值');assert.equal(e.context.document.activeElement,e.input);assert.equal(e.status.textContent,'请重新登录');assert.deepEqual(e.nav,[]);
});
test('访客收到HTML登录页时明确失败且不跳转后台',async()=>{
 const e=env({}, {ok:true,status:200,type:'text/html',redirected:true});await e.submit();assert.deepEqual(e.nav,[]);assert.match(e.status.textContent,/content-type/);assert.equal(e.input.value,'用户已填写的值');
});

test('编辑预览统一阻止JSON写、原生POST和HTMX写，保留输入且零网络',async()=>{
 const e=env({}, {preview:true});await e.submit();assert.equal(e.calls.length,0);assert.match(e.status.textContent,/模拟预览/);assert.equal(e.input.value,'用户已填写的值');
 const native={...e.form,hasAttribute:()=>false,getAttribute:n=>n==='method'?'post':null};let stopped=false;
 e.listeners.submit({target:native,preventDefault(){stopped=true;}});assert.equal(stopped,true);assert.equal(e.calls.length,0);
 let htmxStopped=false;e.listeners['htmx:configRequest']({detail:{verb:'POST',elt:{closest:()=>e.form}},preventDefault(){htmxStopped=true;}});assert.equal(htmxStopped,true);
 let readStopped=false;e.listeners['htmx:configRequest']({detail:{verb:'GET'},preventDefault(){readStopped=true;}});assert.equal(readStopped,false);
});
