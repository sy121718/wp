const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=name=>fs.readFileSync(__dirname+'/'+name+'.js','utf8').replace(/^import[\s\S]*?;\n/gm,'').replace(/export (function|const) /g,'$1 ');
function env(responses={}){
 class Element{
  constructor(tag){this.tagName=tag.toUpperCase();this.children=[];this.attributes={};this.events={};this.value='';this.style={};this.elements={};}
  appendChild(node){node.parent=this;this.children.push(node);if(node.name)this.elements[node.name]=node;return node;}
  setAttribute(k,v){this.attributes[k]=v;}getAttribute(k){return this.attributes[k]??null;}
  addEventListener(k,fn){this.events[k]=fn;}contains(node){return node===this||this.children.some(child=>child.contains(node));}
  get options(){return this.children;}submit(){this.submitted=true;}
 }
 const body=new Element('body'),panel=new Element('div'),frame={name:'wb-canvas',contentDocument:{documentElement:{lang:'zh-CN'}}};
 body.appendChild(panel);const elements={'wb-canvas':frame};
 const document={body,createElement:tag=>new Element(tag),getElementById:id=>elements[id]||walk(body).find(node=>node.id===id)||null};
 const requests=[];const window={location:{href:'https://editor.test/workbench?block=block-A'}};
 const ctx=vm.createContext({window,document,URL,URLSearchParams,Promise,fetch:async url=>{requests.push(url);let data=responses[url.split('?')[0]];if(data instanceof Error)throw data;return {ok:true,json:async()=>({code:200,data:data||[]})};},getCSRFToken:()=> 'csrf',csrfHeaders:x=>x,meta:{projectId:'project-A',pageId:'block-A',saveBase:'block'}});
 vm.runInContext(source('actions')+'\n'+source('preview')+'\n'+source('methods/canvas')+'\nthis.previewMethods=previewMethods;this.canvasMethods=canvasMethods;',ctx);
 return {ctx,document,body,panel,frame,elements,requests,Element};
}
function walk(root){return[root,...(root.children||[]).flatMap(walk)];}
const published=(id,project='project-A',lang='zh-CN')=>({id,projectId:project,publications:[{lang,activePath:'/zh/'+id,artifactId:'artifact-'+id,artifactHash:'hash-'+id}]});
const settle=async()=>{await new Promise(setImmediate);await new Promise(setImmediate);};
test('目标只列当前工程精确语言的真实发布，当前商品加购不生成选择器',()=>{
 const e=env(),rows=[published('ok'),published('other','project-B'),published('en','project-A','en-US'),{id:'draft',projectId:'project-A',activePath:'/latest',publications:[]}];
 assert.deepEqual(JSON.parse(JSON.stringify(e.ctx.publishedTargets(rows,'page','project-A','zh-CN'))),[{kind:'page',id:'ok',label:'/zh/ok'}]);
 const node={id:'buy',type:'core.addToCart'},self={tab:'content'};
 e.ctx.renderActionTargets({self,node,panel:e.panel},{projectId:'project-A',componentActions:{}});
 assert.equal(e.panel.children.length,0);assert.equal(e.requests.length,0);
});
test('单侧列表无权限仍能选另一侧，动作只保存ID并进入原快照链',async()=>{
 const e=env({'/api/page/list':[published('shop')],'/api/presentation/list':new Error('无读取权限')});
 const node={id:'products',type:'core.productList',actions:{list:{kind:'page',pageId:'old'},other:{kind:'local',partKey:'list'}}},snapshots=[];
 const self={tab:'content',findNode:()=>node,snapshot:()=>snapshots.push(JSON.stringify(node)),renderTree(){},refreshCanvas(){},renderUI(){}};
 e.ctx.renderActionTargets({self,node,panel:e.panel},{projectId:'project-A',componentActions:{'core.productList':[{key:'list',allowedKinds:['page','presentation']}]}});await settle();
 const select=walk(e.panel).find(n=>n.tagName==='SELECT');assert.equal(select.disabled,false);assert.equal(node.actions.list.pageId,'old');assert.equal(select.value,'page:old');
 assert.ok(walk(e.panel).some(n=>(n.textContent||'').includes('部分目标无法读取')));
 select.value='page:shop';select.events.change();assert.equal(snapshots.length,1);
 assert.deepEqual(JSON.parse(JSON.stringify(node.actions)),{list:{kind:'page',pageId:'shop'},other:{kind:'local',partKey:'list'}});assert.doesNotMatch(JSON.stringify(node),/activePath|artifactHash|previewState/);
});
test('全部读取失败保留目标与可重试状态，迟到响应不能覆盖新选中节点',async()=>{
 const e=env({'/api/page/list':new Error('稍后重试'),'/api/presentation/list':new Error('稍后重试')});
 const node={id:'account',type:'core.userForms',actions:{authenticated:{kind:'instance',nodeId:'same',partKey:'account'}}};let retries=0;
 const self={tab:'content',findNode:()=>node,syncInspector:()=>retries++};const meta={projectId:'project-A',componentActions:{'core.userForms':[{key:'authenticated',allowedKinds:['page','presentation','instance']}]}};
 e.ctx.renderActionTargets({self,node,panel:e.panel},meta);await settle();
 assert.equal(walk(e.panel).find(n=>n.tagName==='SELECT').value,'keep');assert.equal(node.actions.authenticated.nodeId,'same');
 const retry=walk(e.panel).find(n=>n.tagName==='BUTTON');assert.ok(retry);retry.events.click();assert.equal(retries,1);
 const e2=env({'/api/page/list':[published('ok')]});e2.ctx.renderActionTargets({self:{...self,findNode:()=>({id:'other'})},node,panel:e2.panel},meta);await settle();assert.equal(walk(e2.panel).find(n=>n.tagName==='SELECT').disabled,true);
});
test('块未保存预览走独立POST，状态仅在query且不进入文档，Page和实例仍走各自入口',async()=>{
 const e=env();let url,options;e.ctx.fetch=async(path,opts)=>{url=path;options=opts;return{ok:true,text:async()=>'<html>preview</html>'};};
 const self={doc:{settings:{},root:[]},draftVersion:4,previewState:'empty'};
 await e.ctx.canvasMethods.fetchCanvasHTML.call(self);assert.equal(url,'/workbench/block/preview?previewState=empty');
 const body=new URLSearchParams(options.body);assert.equal(body.get('projectId'),'project-A');assert.equal(body.get('expectedVersion'),'4');assert.equal(body.get('id'),'block-A');assert.equal(body.has('previewState'),false);assert.equal(body.get('draftDocument'),JSON.stringify(self.doc));
 e.ctx.canvasMethods.submitCanvas.call(self);let form=e.document.getElementById('wb-preview-form');assert.equal(form.action,url);assert.equal(form.submitted,true);assert.equal(form.elements.projectId.value,'project-A');assert.equal(form.elements.draftDocument.value,body.get('draftDocument'));
 e.ctx.meta.saveBase='page';await e.ctx.canvasMethods.fetchCanvasHTML.call(self);assert.equal(url,'/workbench/preview?previewState=empty');
 e.ctx.meta.saveBase='instance';await e.ctx.canvasMethods.fetchCanvasHTML.call(self);assert.equal(url,'/workbench/template/preview?previewState=empty');assert.equal(new URLSearchParams(options.body).get('instance'),'block-A');
 assert.equal(e.ctx.previewRequestURL('/workbench/preview?id=a&previewState=error','default'),'/workbench/preview?id=a');
});
test('下拉只显示实际支持的状态，切换不触碰草稿和撤销栈',()=>{
 const e=env(),select=new e.Element('select'),wrapper=new e.Element('label');select.children=[{value:'default'},{value:'empty'},{value:'success'}];e.elements['wb-preview-state']=select;e.elements['wb-preview-states']=wrapper;
 e.frame.contentWindow={WPSitePreview:{active:true,state:'default'},WPSiteData:{supportedPreviewStates:()=>['default','empty']}};
 let reloads=0;const self={previewState:'default',doc:{root:[]},undoStack:['original'],flushCanvas:()=>reloads++};
 e.ctx.previewMethods.syncPreviewControls.call(self);assert.equal(wrapper.hidden,false);assert.equal(select.options[2].hidden,true);
 e.ctx.previewMethods.setPreviewState.call(self,'success');assert.equal(reloads,0);
 e.ctx.previewMethods.setPreviewState.call(self,'empty');assert.equal(reloads,1);assert.deepEqual(self.doc,{root:[]});assert.deepEqual(self.undoStack,['original']);assert.equal(self.previewState,'empty');
});
