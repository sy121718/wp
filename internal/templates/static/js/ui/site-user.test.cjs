const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function item(attrs={}){const values={...attrs};return{hidden:false,disabled:false,value:'',textContent:'',getAttribute:n=>values[n]??null,setAttribute:(n,v)=>{values[n]=String(v);},removeAttribute:n=>delete values[n],hasAttribute:n=>n in values};}
function env(){
 const views={},events={},handlers={},refreshes=[],roots=[],nav=[],registrations=[],document={documentElement:{lang:'en-US'},addEventListener:(n,f)=>events[n]=f,querySelectorAll:()=>roots};
 const window={document,location:{search:'?key=safe-key&email=person%40example.com',href:'https://shop.example/account',origin:'https://shop.example',assign:n=>nav.push(n)},WPSiteData:{register:(n,f)=>views[n]=f,refresh:n=>refreshes.push(n)},WBUI:{writeBase:{registerResultHandler:(n,f)=>{registrations.push(n);handlers[n]=f;}}}};
 const source=fs.readFileSync(__dirname+'/site-user.js','utf8');
 function runAgain(){vm.runInNewContext(source,{window,URL,URLSearchParams,Date,Intl,Number,Object,String,Array});}
 runAgain();
 function root(id,mode='login'){
  const r=item({'data-user-form':mode,'data-component-instance':id}),sections={},forms={},fieldsets=[],facts=[item({'data-user-value':'displayName'}),item({'data-user-value':'registeredAt'})],status=item(),guest=item(),retry=item();
  for(const name of ['login','register','forgot','reset','account','profile','preference','password','sessions']){
   const sec=item({'data-user-section':name}),form=item({'data-write-url':'/api/site/user/'+(name==='profile'?'account/profile':name)}),fieldset=item();fieldset.closest=()=>sec;fieldsets.push(fieldset);
   let fields=[item({name:'nickname'})];fields[0].name='nickname';fields[0].type='text';
   if(name==='profile')fields.push(Object.assign(item({name:'locale','data-user-field':'profileLocale'}),{name:'locale',type:'hidden'}));
   if(name==='login'||name==='password'||name==='reset')fields=[Object.assign(item({name:'password'}),{name:'password',type:'password'})];
   form.fields=fields;form.closest=()=>r;form.querySelectorAll=s=>s==='input[type="password"]'?fields.filter(f=>f.type==='password'):fields;form.reset=()=>fields.forEach(f=>f.value='');
   fields.forEach(f=>{f.closest=s=>s==='form'?form:sec;f.focus=()=>{document.activeElement=f;};});
   sec.querySelector=()=>fields[0];sections[name]=sec;forms[name]=form;
  }
  const accountRead=item(),devicesRead=item();accountRead.closest=devicesRead.closest=()=>r;
  r.closest=()=>r;
  r.querySelector=s=>{const m=s.match(/^\[data-user-section="([^"]+)"\]$/);if(m)return sections[m[1]];if(s==='[data-user-account-read]')return accountRead;if(s==='[data-user-devices-read]')return devicesRead;if(s==='[data-user-guest]')return guest;if(s==='[data-site-status]')return status;if(s==='[data-user-retry]')return retry;return null;};
  r.querySelectorAll=s=>s==='[data-user-fields]'?fieldsets:s==='[data-user-section]'?Object.values(sections):s==='[data-user-value]'?facts:s.includes('[data-user-section="profile"] form')?[forms.profile,forms.preference]:s==='input[type="password"]'?Object.values(forms).flatMap(f=>f.fields.filter(x=>x.type==='password')):[];
  roots.push(r);return{r,sections,forms,fieldsets,facts,status,guest,retry,accountRead,devicesRead};
 }
 return{views,events,handlers,refreshes,roots,nav,document,root,runAgain,registrations};
}
test('未登录账号模式显示受控登录入口，私有字段保持禁用',()=>{
 const e=env(),a=e.root('A','profile');e.views.userSession(a.r,{authenticated:false});assert.equal(a.sections.profile.hidden,true);assert.equal(a.guest.hidden,false);assert.equal(a.fieldsets[5].disabled,true);
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
 assert.equal(e.handlers['user.forms']({form:a.forms.login,instanceId:'A'}),true);assert.equal(a.r.getAttribute('data-user-active'),'account');assert.equal(b.r.getAttribute('data-user-active'),null);assert.equal(a.forms.login.fields[0].value,'');assert.equal(b.forms.login.fields[0].value,'other-secret');assert.deepEqual(e.nav,[]);
});
test('显式站内Next由前端导航，退出后清除私有事实',()=>{
 const e=env(),a=e.root('A');a.r.setAttribute('data-user-next','/members?tab=profile');e.handlers['user.forms']({form:a.forms.login,instanceId:'A'});assert.deepEqual(e.nav,['/members?tab=profile']);
 e.views.userSession(a.r,{authenticated:true});e.views.userAccount(a.accountRead,{nickname:'私有昵称',registeredAt:0});e.views.userSession(a.r,{authenticated:false});assert.equal(a.forms.profile.fields[0].value,'');assert.equal(a.facts[0].textContent,'');assert.equal(a.fieldsets[5].disabled,true);
});

test('账号脚本重复初始化只注册一次具名处理，不重复提交监听',()=>{const e=env();const original=e.handlers['user.forms'];e.runAgain();assert.deepEqual(e.registrations,['user.forms']);assert.equal(e.handlers['user.forms'],original);});
