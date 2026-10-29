const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
class Element {
 constructor(attrs={},children=[]) {this.attrs={...attrs};this.children=children;this.hidden=true;this.events=[];children.forEach(child=>child.parent=this);}
 getAttribute(key){return this.attrs[key]??null;}
 hasAttribute(key){return key in this.attrs;}
 setAttribute(key,value){this.attrs[key]=String(value);}
 closest(selector){assert.equal(selector,'[data-component-instance]');return this.hasAttribute('data-component-instance')?this:this.parent?.closest(selector)||null;}
 querySelectorAll(selector){const key=selector.slice(1,-1);return this.children.flatMap(child=>[...(child.hasAttribute(key)?[child]:[]),...child.querySelectorAll(selector)]);}
 dispatchEvent(event){this.events.push(event);}
 focus(){this.focused=true;}
 scrollIntoView(){this.scrolled=true;}
}
function env(roots){
 const events={},navigation=[],opened=[];
 const document={querySelectorAll(selector){assert.equal(selector,'[data-component-instance]');return roots.flatMap(root=>[root,...root.querySelectorAll(selector)]);},addEventListener:(key,fn)=>events[key]=fn};
 const window={document,location:{href:'https://site.test/zh/shop',origin:'https://site.test',assign:url=>navigation.push(url)},open:(...args)=>opened.push(args)};
 vm.runInNewContext(fs.readFileSync(__dirname+'/site-actions.js','utf8'),{window,URL,CustomEvent:class{constructor(type,opts){this.type=type;Object.assign(this,opts);}}});
 return {api:window.WPSiteActions,events,navigation,opened};
}
function control(kind,instance,part,key='checkout'){return new Element({'data-component-action':key,'data-action-kind':kind,'data-action-instance':instance,'data-action-part':part});}

test('跨实例只显示并聚焦显式目标part，不隐藏购物车、不串其它实例',()=>{
 const button=control('instance','checkout-2','checkout');
 const cartPart=new Element({'data-component-part':'cart'},[button]);cartPart.hidden=false;
 const cart=new Element({'data-component-instance':'cart-1'},[cartPart]);
 const other=new Element({'data-component-part':'checkout'});
 const part=new Element({'data-component-part':'checkout'});
 const checkout=new Element({'data-component-instance':'checkout-2'},[part]);
 const {api}=env([cart,new Element({'data-component-instance':'checkout-other'},[other]),checkout]);
 assert.equal(api.run(button,'checkout'),true);
 assert.equal(part.hidden,false);assert.equal(part.focused,true);assert.equal(other.hidden,true);assert.equal(cartPart.hidden,false);
 assert.equal(checkout.events[0].type,'site:action');assert.equal(checkout.events[0].detail.sourceInstanceId,'cart-1');
});

test('local不得跨实例，重复目标或不存在的part一律不执行',()=>{
 for(const mode of ['cross-local','duplicate','missing']){
  const button=control(mode==='cross-local'?'local':'instance','other','result');
  const roots=[new Element({'data-component-instance':'self'},[button]),new Element({'data-component-instance':'other'},mode==='missing'?[]:[new Element({'data-component-part':'result'})])];
  if(mode==='duplicate') roots.push(new Element({'data-component-instance':'other'},[new Element({'data-component-part':'result'})]));
  assert.equal(env(roots).api.run(button,'checkout'),false);
 }
});

test('嵌套实例的同名action不能被父实例调用，同一实例区域ID按值匹配',()=>{
 const innerButton=control('local','inner','result');
 const inner=new Element({'data-component-instance':'inner'},[innerButton,new Element({'data-component-part':'result'})]);
 const outer=new Element({'data-component-instance':'outer'},[inner]);
 const {api}=env([outer]);
 assert.equal(api.run(outer,'checkout'),false);
 assert.equal(api.run(innerButton,'checkout'),true);
});

test('导航仅消费已解析URL，页内目标限制同源，外链拒绝危险协议和凭据',()=>{
 for(const [kind,url,allowed] of [['page','/zh/pay',true],['page','https://evil.test/pay',false],['external','javascript:alert(1)',false],['external','https://u:p@site.test/a',false],['external','https://docs.test/help',true]]){
  const button=new Element({'data-component-action':'go','data-action-kind':kind,'data-action-url':url});
  const {api,navigation}=env([new Element({'data-component-instance':'root'},[button])]);
  assert.equal(api.run(button,'go'),allowed);
  assert.equal(navigation.length,allowed?1:0);
 }
});

test('外链新标签使用noopener，点击增强保留修饰键的浏览器行为',()=>{
 const button=new Element({'data-component-action':'go','data-action-kind':'external','data-action-url':'https://docs.test/help','data-action-new-tab':'true'});
 const {api,opened,events}=env([new Element({'data-component-instance':'root'},[button])]);
 assert.equal(api.run(button,'go'),true);assert.deepEqual(opened[0],['https://docs.test/help','_blank','noopener,noreferrer']);
 let prevented=0;
 const click={target:{closest:()=>button},button:0,ctrlKey:true,preventDefault:()=>prevented++};
 events.click(click);assert.equal(opened.length,1);assert.equal(prevented,0);
});
