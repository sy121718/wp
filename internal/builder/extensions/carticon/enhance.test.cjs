const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
function setup(width,side){
 const events={},attrs={'data-cart-icon':'dropdown'},dattrs={open:''};
 const panelWidth=Math.min(width-32,340),left=side==='left'?100-panelWidth:width-100;
 const panel={style:{},getClientRects:()=>[{}],getBoundingClientRect:()=>({left,right:left+panelWidth,bottom:360})};
 const input={},doc={activeElement:input,documentElement:{clientWidth:width,clientHeight:800},addEventListener:(k,fn)=>events[k]=fn};
 const summary={focus(){doc.activeElement=summary;}};
 const details={open:true,hasAttribute:k=>k in dattrs,removeAttribute:k=>delete dattrs[k],querySelector:()=>summary,addEventListener:(k,fn)=>events['details:'+k]=fn};
 const root={getAttribute:k=>attrs[k],setAttribute:(k,v)=>attrs[k]=v,removeAttribute:k=>delete attrs[k],querySelector:s=>s==='.sky-cart-icon-panel'?panel:s==='details[data-cart-icon-panel]'?details:null,getBoundingClientRect:()=>({top:40,bottom:84}),addEventListener:(k,fn)=>events['root:'+k]=fn,contains:el=>el===input||el===summary};
 doc.querySelectorAll=()=>[root];
 vm.runInNewContext(fs.readFileSync(__dirname+'/enhance.js','utf8')+'\ninitCartIconPanels();',{document:doc,window:{addEventListener(){}},Math});
 return {events,panel,summary,doc,details,dattrs,left,panelWidth};
}
test('375/768/1440视口中左右边缘的浮层都保留16px边距',()=>{
 for(const width of [375,768,1440])for(const side of ['left','right']){
  const e=setup(width,side);e.events['details:toggle']();
  const shift=Number(e.panel.style.transform.match(/\((-?[\d.]+)px\)/)[1]);
  assert.ok(e.left+shift>=16);assert.ok(e.left+shift+e.panelWidth<=width-16);
 }
});
test('面板内按Esc关闭原生details并将焦点还给自己的触发器',()=>{
 const e=setup(375,'left');e.events.keydown({key:'Escape'});
 assert.equal('open' in e.dattrs,false);assert.equal(e.doc.activeElement,e.summary);
});
