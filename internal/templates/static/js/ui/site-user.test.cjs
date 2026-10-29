const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function item(attrs={}){const values={...attrs};return{hidden:false,disabled:false,value:'',textContent:'',getAttribute:n=>values[n]??null,setAttribute:(n,v)=>{values[n]=String(v);},removeAttribute:n=>delete values[n],hasAttribute:n=>n in values,isConnected:true,dispatchEvent(){}};}
function env(options={}){
 const views={},previews={},events={},handlers={},refreshes=[],roots=[],nav=[],registrations=[],document={documentElement:{lang:'en-US'},addEventListener:(n,f)=>events[n]=f,querySelectorAll:()=>roots};
 const window={document,location:{search:'',href:'https://shop.example/account',origin:'https://shop.example',assign:n=>nav.push(n)},WPSitePreview:Object.freeze({active:!!options.preview,state:options.state||'default'}),WPSiteData:{register:(n,f)=>views[n]=f,registerPreview:(n,f,states)=>previews[n]={render:f,states},refresh:n=>refreshes.push(n)},WBUI:{writeBase:{registerResultHandler:(n,f)=>{registrations.push(n);handlers[n]=f;}}}};
 const fetches=[];window.fetch=async(...args)=>{fetches.push(args);throw new Error('模拟预览不应发网络请求');};
 if(options.realData){document.readyState='loading';vm.runInNewContext(fs.readFileSync(__dirname+'/site-data.js','utf8'),{window,URL,Map,Promise,Number,Object,Array,CustomEvent:class{}});}
 const source=fs.readFileSync(__dirname+'/site-user.js','utf8');
 function runAgain(){vm.runInNewContext(source,{window,URL,URLSearchParams,Date,Intl,Number,Object,String,Array});}
 runAgain();
 function root(id,mode='login'){
  const r=item({'data-user-form':mode,'data-component-instance':id,'data-site-read':'/api/site/user/session','data-site-view':'userSession'}),sections={},forms={},fieldsets=[],facts=[item({'data-user-value':'displayName'}),item({'data-user-value':'registeredAt'})],status=item(),guest=item(),retry=item(),title=item(),formStatuses={};
  for(const name of ['login','register','forgot','reset','account','profile','preference','password','sessions']){
   const sec=item({'data-user-section':name}),form=item({'data-write-url':'/api/site/user/'+(name==='profile'?'account/profile':name)}),fieldset=item();fieldset.closest=()=>sec;fieldsets.push(fieldset);
   let fields=[item({name:'nickname'})];fields[0].name='nickname';fields[0].type='text';
   if(name==='profile')fields.push(Object.assign(item({name:'locale','data-user-field':'profileLocale'}),{name:'locale',type:'hidden'}));
   if(name==='login'||name==='password'||name==='reset')fields=[Object.assign(item({name:'password'}),{name:'password',type:'password'})];
   if(name==='reset')fields.unshift(Object.assign(item({name:'key'}),{name:'key',type:'hidden'}),Object.assign(item({name:'email'}),{name:'email',type:'email'}));
   form.fields=fields;form.closest=()=>r;form.querySelectorAll=s=>s==='input[type="password"]'?fields.filter(f=>f.type==='password'):fields;form.reset=()=>fields.forEach(f=>f.value='');
   fields.forEach(f=>{f.closest=s=>s==='form'?form:sec;f.focus=()=>{document.activeElement=f;};});
   formStatuses[name]=item();sec.querySelector=s=>s==='[data-write-status]'?formStatuses[name]:fields[0];sec.focus=()=>{document.activeElement=sec;};sections[name]=sec;forms[name]=form;
  }
  const accountRead=item(),devicesRead=item();accountRead.closest=devicesRead.closest=()=>r;
  r.closest=()=>r;
  r.querySelector=s=>{if(s==='[data-user-title]')return title;if(s==='[data-component-action="authenticated"]')return r.authAction||null;const titleMode=s.match(/^\[data-user-mode-title="([^"]+)"\]$/);if(titleMode)return {textContent:'title-'+titleMode[1]};const label=s.match(/^\[data-user-preview-label="([^"]+)"\]$/);if(label)return {textContent:'preview-'+label[1]};const m=s.match(/^\[data-user-section="([^"]+)"\]$/);if(m)return sections[m[1]];if(s==='[data-user-account-read]')return accountRead;if(s==='[data-user-devices-read]')return devicesRead;if(s==='[data-user-guest]')return guest;if(s==='[data-site-status]')return status;if(s==='[data-user-retry]')return retry;return null;};
  r.querySelectorAll=s=>s.startsWith('[data-user-section="reset"]')?forms.reset.fields.filter(f=>f.name==='key'||f.name==='email'):s==='[data-user-fields]'?fieldsets:s==='[data-user-section]'?Object.values(sections):s==='[data-user-value]'?facts:s.includes('[data-user-section="profile"] form')?[forms.profile,forms.preference]:s==='input[type="password"]'?Object.values(forms).flatMap(f=>f.fields.filter(x=>x.type==='password')):[];
  roots.push(r);return{r,sections,forms,fieldsets,facts,status,guest,retry,accountRead,devicesRead,title,formStatuses};
 }
 return{views,previews,window,fetches,events,handlers,refreshes,roots,nav,document,root,runAgain,registrations};
}
test('未登录账号模式直接显示登录表单，私有字段保持禁用',()=>{
 const e=env(),a=e.root('A','profile');e.views.userSession(a.r,{authenticated:false});assert.equal(a.sections.profile.hidden,true);assert.equal(a.guest.hidden,true);assert.equal(a.sections.login.hidden,false);assert.equal(a.fieldsets[5].disabled,true);
 e.events.click({target:{closest:s=>s==='[data-user-mode]'?{closest:()=>a.r,getAttribute:()=> 'login'}:null}});assert.equal(a.sections.login.hidden,false);assert.equal(a.guest.hidden,true);
});
test('资料读取前不能提交，读取后回填事实，刷新不覆盖脏输入及资料独立语言',()=>{
 const e=env(),a=e.root('A','profile');e.views.userSession(a.r,{authenticated:true});assert.equal(a.fieldsets[5].disabled,true);
 e.views.userAccount(a.accountRead,{nickname:'原昵称',displayName:'<img onerror=x>',profileLocale:'en-US',locale:'zh-CN',registeredAt:0});assert.equal(a.fieldsets[5].disabled,false);assert.equal(a.forms.profile.fields[0].value,'原昵称');assert.equal(a.forms.profile.fields[1].value,'en-US');assert.equal(a.facts[0].textContent,'<img onerror=x>');
 a.forms.profile.fields[0].value='尚未提交';e.events.input({target:a.forms.profile.fields[0]});e.views.userAccount(a.accountRead,{nickname:'后台刷新',profileLocale:'fr-FR'});assert.equal(a.forms.profile.fields[0].value,'尚未提交');assert.equal(a.forms.profile.fields[1].value,'en-US');
});
test('读取失败保留输入和焦点，给可键盘操作的重试入口',()=>{
 const e=env(),a=e.root('A');const field=a.forms.login.fields[0];field.value='保留密码';e.document.activeElement=field;
 e.events['site:data:error']({target:a.accountRead,detail:{message:'连接失败'}});assert.equal(a.status.textContent,'连接失败');assert.equal(a.retry.hidden,false);assert.equal(field.value,'保留密码');assert.equal(e.document.activeElement,field);
});
test('登录成功切换限当前实例，危险Next不导航，凭据仅成功后清空',()=>{
 const e=env(),a=e.root('A'),b=e.root('B');a.r.setAttribute('data-user-next','//evil.example');a.forms.login.fields[0].value='secret';b.forms.login.fields[0].value='other-secret';
 assert.equal(e.handlers['user.forms']({form:a.forms.login,instanceId:'wrong'}),false);assert.equal(a.forms.login.fields[0].value,'secret');
 assert.equal(e.handlers['user.forms']({form:a.forms.login,instanceId:'A',data:{authenticated:true}}),true);assert.equal(a.r.getAttribute('data-user-active'),'account');assert.equal(b.r.getAttribute('data-user-active'),null);assert.equal(a.forms.login.fields[0].value,'');assert.equal(b.forms.login.fields[0].value,'other-secret');assert.deepEqual(e.nav,[]);
});
test('显式站内Next由前端导航，退出后清除私有事实',()=>{
 const e=env(),a=e.root('A');a.r.setAttribute('data-user-next','/members?tab=profile');e.handlers['user.forms']({form:a.forms.login,instanceId:'A',data:{authenticated:true}});assert.deepEqual(e.nav,['/members?tab=profile']);
 e.views.userSession(a.r,{authenticated:true});e.views.userAccount(a.accountRead,{nickname:'私有昵称',registeredAt:0});e.views.userSession(a.r,{authenticated:false});assert.equal(a.forms.profile.fields[0].value,'');assert.equal(a.facts[0].textContent,'');assert.equal(a.fieldsets[5].disabled,true);
});

test('账号脚本重复初始化只注册一次具名处理，不重复提交监听',()=>{const e=env();const original=e.handlers['user.forms'];e.runAgain();assert.deepEqual(e.registrations,['user.forms']);assert.equal(e.handlers['user.forms'],original);});

test('模拟状态使用同一套受控表单，提交中与加载禁写、错误成功有稳定反馈',()=>{
 for(const state of ['default','loading','empty','unauthenticated','validation-error','service-error','submitting','success','forbidden']){
  const e=env(),a=e.root('A','account');
  e.previews.userSession.render(a.r,state);
  assert.equal(a.r.getAttribute('data-user-state'),state);
  assert.equal(a.r._userAuthenticated,!['unauthenticated','forbidden'].includes(state));
  assert.equal(a.fieldsets[0].disabled,['loading','submitting'].includes(state));
  if(state==='unauthenticated'||state==='forbidden')assert.equal(a.sections.login.hidden,false);
  if(['loading','empty','service-error','forbidden'].includes(state))assert.equal(a.status.textContent,'preview-'+state);
  if(['validation-error','submitting','success'].includes(state))assert.equal(a.formStatuses.account.textContent,'preview-'+state);
 }
});

test('真实site-data与账号renderer组合模拟0请求，不读取真实重置密钥或私有会话',async()=>{
 const e=env({realData:true,preview:true,state:'default'}),a=e.root('A','profile');
 e.window.location.search='?key=real-private-reset-key&email=private%40example.invalid';
 await e.window.WPSiteData.refresh(a.r);await new Promise(setImmediate);await new Promise(setImmediate);
 assert.equal(e.fetches.length,0);assert.equal(a.r._userAccountReady,true);assert.equal(a.forms.profile.fields[0].value,'Alex');
 assert.equal(a.forms.reset.fields.find(f=>f.name==='key').value,'');
 assert.equal(a.forms.reset.fields.find(f=>f.name==='email').value,'');
 assert.equal(a.sections.profile.hidden,false);
});

test('登录后唯一目标只执行一次，局部动作保持当前实例且作者自定标题不被覆盖',()=>{
 const e=env(),a=e.root('A'),b=e.root('B');a.r.authAction={};a.r.setAttribute('data-user-auto-title','false');a.title.textContent='欢迎回来';let calls=[];
 e.window.WPSiteActions={run:(source,key)=>{calls.push({source,key});e.events['site:action']({target:a.r,detail:{instanceId:'A',part:'account'}});return true;}};
 e.handlers['user.forms']({form:a.forms.login,instanceId:'A',data:{authenticated:true}});
 assert.equal(calls.length,1);assert.equal(calls[0].key,'authenticated');assert.equal(a.sections.account.hidden,false);assert.equal(a.sections.login.hidden,true);
 assert.equal(b.r.getAttribute('data-user-active'),null);assert.equal(a.title.textContent,'欢迎回来');assert.deepEqual(e.nav,[]);
 e.events['site:action']({target:b.r,detail:{instanceId:'A',part:'profile'}});assert.equal(b.r.getAttribute('data-user-active'),null);
});

test('合法重置链接自动在同一账号区块打开重置表单，无需固定业务页面',()=>{
 const e=env(),a=e.root('A','account');e.window.location.search='?key=reset-key&email=person%40example.invalid';
 e.views.userSession(a.r,{authenticated:false});assert.equal(a.sections.reset.hidden,false);assert.equal(a.r.getAttribute('data-user-active'),'reset');
 assert.equal(a.forms.reset.fields.find(f=>f.name==='key').value,'reset-key');assert.equal(a.forms.reset.fields.find(f=>f.name==='email').value,'person@example.invalid');
});

test('跨实例账号目标先验证访客会话，响应后只聚焦目标，不把动作当成登录凭据',()=>{
 const e=env(),a=e.root('A'),b=e.root('B'),c=e.root('C');
 e.views.userSession(a.r,{authenticated:false});e.views.userSession(b.r,{authenticated:false});e.views.userSession(c.r,{authenticated:false});
 a.r.authAction={};e.window.WPSiteActions={run:()=>{e.events['site:action']({target:b.r,detail:{instanceId:'B',part:'account'}});return true;}};
 e.handlers['user.forms']({form:a.forms.login,instanceId:'A',data:{authenticated:true}});
 assert.equal(b.r._userAuthenticated,false);assert.equal(b.sections.account.hidden,true);assert.equal(b.r._userPendingPart,'account');
 assert.equal(c.r._userPendingPart,undefined);
 e.views.userSession(b.r,{authenticated:true});
 assert.equal(b.sections.account.hidden,false);assert.equal(b.sections.login.hidden,true);assert.equal(e.document.activeElement,b.sections.account);
 assert.equal(c.sections.login.hidden,false);assert.equal(c.sections.account.hidden,true);
 e.events['site:action']({target:c.r,detail:{instanceId:'C',part:'account'}});e.views.userSession(c.r,{authenticated:false});
 assert.equal(c.sections.account.hidden,true);assert.equal(c.r._userPendingPart,undefined);
});

test('注册与重置成功回到同一账号区块登录，并保持可见成功反馈',()=>{
 for(const action of ['register','reset']){
  const e=env(),a=e.root('A',action);e.views.userSession(a.r,{authenticated:false});
  e.handlers['user.forms']({form:a.forms[action],instanceId:'A',data:null,text:'操作成功，请登录'});
  e.views.userSession(a.r,{authenticated:false});
  assert.equal(a.sections.login.hidden,false);assert.equal(a.sections[action].hidden,true);assert.equal(a.status.textContent,'操作成功，请登录');assert.equal(a.status.getAttribute('data-tone'),'info');
 }
});
