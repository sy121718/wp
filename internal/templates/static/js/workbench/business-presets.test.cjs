const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup(blocks = []) {
    let document;
    class Element {
        constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.events = {}; this.attributes = {}; this.isConnected = true; this.value = ''; this.textContent = ''; this.dataset = {}; this.style = {}; this.classList = {toggle(){}}; }
        append(...nodes) { nodes.forEach(node => { node.parent = this; this.children.push(node); }); }
        appendChild(node) { this.append(node); }
        setAttribute(key, value) { this.attributes[key] = value; }
        getAttribute(key) { return this.attributes[key] ?? null; }
        addEventListener(key, fn) { this.events[key] = fn; }
        querySelector(selector) { if (selector === '.wb-palette-icon') return {innerHTML:''}; return all(this).find(node => selector === '[name="name"]' && node.name === 'name'); }
        focus() { document.activeElement = this; }
        select() { this.selected = true; }
        showModal() { this.open = true; }
        close() { this.open = false; if (this.events.close) this.events.close(); }
        remove() { this.parent.children = this.parent.children.filter(node => node !== this); this.isConnected = false; }
        fire(key) { const event = { prevented:false, preventDefault() { this.prevented = true; } }; if (this.events[key]) this.events[key](event); return event; }
    }
    function all(root) { return [root, ...root.children.flatMap(all)]; }
    document = { body:new Element('body'), activeElement:null, createElement:tag=>new Element(tag), createTextNode:text=>Object.assign(new Element('text'),{textContent:text}), getElementById:id=>all(document.body).find(node=>node.id===id) };
    const opener = new Element('button'); document.activeElement = opener;
    const handlers = new Map(), state = { inserts:[], notices:[], renders:0, edits:0, registrations:0 };
    const base = { registerResultHandler(name, fn) { assert.equal(handlers.has(name),false,'能力不能重复注册'); handlers.set(name,fn); state.registrations++; } };
    const self = {
        pendingInsertTarget:{id:'target',placement:'inside'},
        findNode:id=>id==='target' ? {id} : null,
        insertComponent(...args) { state.inserts.push(args); },
        globalRefItem:block=>({type:'core.globalref',props:{blockId:block.id}}),
        renderPaletteBlocks() { state.renders++; }, showEdit() { state.edits++; }, setNotice:text=>state.notices.push(text)
    };
    const meta = {projectId:'project-A',blocks};
    const preset = {presetKey:'user.account',presetVersion:2,label:'账号中心',category:'general'};
    const window = { WBUI:{writeBase:base} };
    const sandbox = vm.createContext({document,window});
    vm.runInContext(fs.readFileSync(__dirname+'/business-presets.js','utf8').replace(/export function /g,'function '),sandbox);
    function open() { sandbox.openBusinessPreset(self,preset,meta); return document.getElementById('wb-business-preset-dialog'); }
    return {document,opener,handlers,state,self,meta,preset,window,open,all,sandbox};
}

test('首次命名只声明来源，省略文档并沿唯一写基座提交', () => {
    const e = setup(), dialog = e.open(), nodes = e.all(dialog), form = nodes.find(node=>node.tagName==='FORM');
    assert.equal(form.getAttribute('data-write-url'),'/api/block/create');
    assert.equal(form.getAttribute('data-write-handler'),'workbench.presetCreate');
    assert.equal(nodes.some(node=>node.name==='document'),false);
    const inputs=nodes.filter(node=>node.tagName==='INPUT');
    assert.deepEqual(inputs.map(node=>node.name).sort(),['name','projectId','presetKey','presetVersion','kind','category','reuseMode'].sort());
    assert.equal(inputs.filter(node=>node.type!=='hidden').length,1);
    assert.equal(e.document.activeElement.name,'name'); assert.equal(e.document.activeElement.selected,true);
    assert.equal(e.state.inserts.length,0);
    e.document.activeElement.value='用户刚命名'; e.open();
    assert.equal(e.all(e.document.body).filter(node=>node.tagName==='DIALOG').length,1);
    assert.equal(e.document.activeElement.value,'用户刚命名'); assert.equal(e.state.registrations,1);
});

test('成功回执只插全局引用，回执AST不留作前端预设源', () => {
    const e=setup(),dialog=e.open(),form=e.all(dialog).find(node=>node.tagName==='FORM');
    const data={id:'block-A',projectId:'project-A',name:'账号中心',kind:'block',category:'general',reuseMode:'global',version:1,presetKey:'user.account',presetVersion:2,document:{root:[{type:'unexpected-authority'}]}};
    e.self.pendingInsertTarget={id:'changed',placement:'after'};
    assert.equal(e.handlers.get('workbench.presetCreate')({form,data}),true);
    assert.equal(e.state.inserts.length,1); assert.equal(e.state.inserts[0][0].type,'core.globalref'); assert.equal(e.state.inserts[0][0].props.blockId,'block-A');
    assert.deepEqual(e.state.inserts[0].slice(1),['target','inside']);
    assert.equal('document' in e.meta.blocks[0],false); assert.equal(e.state.renders,1);
    assert.equal(e.document.getElementById(dialog.id),undefined); assert.equal(e.document.activeElement,e.opener);
    e.open(); assert.equal(e.state.registrations,1);
});

test('来源回执不匹配保留命名与弹窗，不插空壳或跨工程引用', () => {
    const e=setup(),dialog=e.open(),nodes=e.all(dialog),form=nodes.find(node=>node.tagName==='FORM'),name=nodes.find(node=>node.name==='name');
    name.value='我的账号区块';
    e.handlers.get('workbench.presetCreate')({form,data:{id:'wrong',projectId:'other',reuseMode:'global',presetKey:'user.account',presetVersion:2}});
    assert.equal(e.state.inserts.length,0);assert.equal(name.value,'我的账号区块');assert.equal(dialog.open,true);
    assert.match(nodes.find(node=>node.id==='wb-business-preset-status').textContent,/来源回执不匹配/);
});

test('已有实例显式引用，不自动选第一项，不创建第二份', () => {
    const blocks=[{id:'one',projectId:'project-A',name:'站点账号',reuseMode:'global',presetKey:'user.account',presetVersion:2},{id:'two',projectId:'project-A',name:'页脚账号',reuseMode:'global',presetKey:'user.account',presetVersion:2},{id:'old',name:'旧版本',reuseMode:'global',presetKey:'user.account',presetVersion:1}];
    const e=setup(blocks),dialog=e.open();
    assert.equal(e.state.inserts.length,0);
    const choices=e.all(dialog).filter(node=>(node.getAttribute('aria-label')||'').startsWith('引用已有区块：'));
    assert.equal(choices.length,2);choices[1].fire('click');
    assert.equal(e.state.inserts.length,1);assert.equal(e.state.inserts[0][0].props.blockId,'two');assert.equal(e.meta.blocks.length,3);
});

test('写入期间不能关闭或改引用，取消后回焦点且不留绑定', () => {
    const e=setup([{id:'one',projectId:'project-A',name:'站点账号',reuseMode:'global',presetKey:'user.account',presetVersion:2}]),dialog=e.open(),nodes=e.all(dialog),form=nodes.find(node=>node.tagName==='FORM');
    form.setAttribute('aria-busy','true');
    assert.equal(dialog.fire('cancel').prevented,true);
    nodes.find(node=>node.textContent==='取消').fire('click');
    nodes.find(node=>node.getAttribute('aria-label')==='引用已有区块：站点账号').fire('click');
    assert.equal(dialog.open,true);assert.equal(e.state.inserts.length,0);
    form.setAttribute('aria-busy','false');nodes.find(node=>node.textContent==='取消').fire('click');
    assert.equal(e.document.activeElement,e.opener);
    assert.equal(e.handlers.get('workbench.presetCreate')({form,data:{}}),false);
});

test('缺项目或写基座明确反馈，不打开无法提交的对话框', () => {
    const e=setup();e.meta.projectId='';assert.equal(e.open(),undefined);assert.match(e.state.notices[0],/站点工程/);
    e.meta.projectId='project-A';delete e.window.WBUI;assert.equal(e.open(),undefined);assert.match(e.state.notices[1],/写入基座未就绪/);
});

test('真实调色板保留基础组件拖放，业务预设点击命名且元数据缺席不报错', () => {
    const e=setup(),root=e.document.createElement('div');root.id='wb-palette';e.document.body.append(root);
    Object.assign(e.sandbox,{meta:e.meta,starterPresets:[],libraryCategories:[{key:'other',title:'其他'},{key:'presets',title:'预设'}],paletteItems:[{type:'core.text',label:'文本',hint:'p'}],categoryOf:()=> 'other',elementAppearance:()=>['text','p']});
    const source=fs.readFileSync(__dirname+'/methods/canvas.js','utf8').replace(/^import[\s\S]*?;\n/gm,'').replace('export const canvasMethods','var canvasMethods');
    vm.runInContext(source,e.sandbox);
    const self={...e.self,libraryCategory:'all',libraryFilter:'',paletteOpen:{},insertPreset:p=>e.sandbox.openBusinessPreset(e.self,p,e.meta)};
    e.sandbox.canvasMethods.renderPaletteComponents.call(self);
    const basic=e.all(root).find(node=>node.dataset.type==='core.text');assert.equal(basic.draggable,true);assert.equal(typeof basic.events.dragstart,'function');
    e.meta.businessPresets=[e.preset];e.sandbox.canvasMethods.renderPaletteComponents.call(self);
    const business=e.all(root).find(node=>node.className==='wb-palette-item wb-preset-item');
    assert.equal(business.draggable,false);assert.equal(business.events.dragstart,undefined);business.fire('click');
    assert.ok(e.document.getElementById('wb-business-preset-dialog'));assert.equal(e.state.inserts.length,0);
});
