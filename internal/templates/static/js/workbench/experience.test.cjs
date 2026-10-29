const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function methods(file, name, globals = {}) {
    const source = fs.readFileSync(__dirname + '/methods/' + file + '.js', 'utf8')
        .replace(/^import\s*\{[\s\S]*?\}\s*from\s*'[^']*';/gm, '')
        .replace('export const ' + name, 'var ' + name);
    const context = vm.createContext({ console, JSON, URLSearchParams, setTimeout, clearTimeout,
        meta: { pageId: 'page-1', draftPath: '/demo', target: { type: 'page', save: { path: '/api/page/draft/save' }, saveBody: { versionKey: 'expectedVersion' } } },
        csrfHeaders: (h) => h, ...globals });
    vm.runInContext(source, context);
    return context[name];
}

function editor(globals = {}) {
    const buttons = new Map(['wb-status', 'wb-save-draft', 'wb-publish', 'wb-undo', 'wb-redo'].map(id => [id, { disabled: false, textContent: '', dataset: {} }]));
    const document = { getElementById: id => buttons.get(id), querySelector: () => null };
    const wb = Object.assign({ doc: { root: [{ id: 'h', props: { text: '原文' } }] }, draftVersion: 1,
        busy: false, saveState: 'dirty', undoStack: [], redoStack: [],
        clearBackup() { this.backupCleared = true; }, backupDoc() { this.backedUp = true; },
        flushCanvas() {}, renderTree() {}, refreshCanvas() {}, syncInspector() {},
        statusText() { return this.busy ? '处理中…' : this.saveState; },
        setNotice(text) { this.notice = text; },
        get canUndo() { return this.undoStack.length > 0; }, get canRedo() { return this.redoStack.length > 0; }
    }, methods('state', 'stateMethods', { document, window: {} }), methods('api', 'apiMethods', { document, window: {}, alert() {}, ...globals }));
    return { wb, buttons };
}

test('保存成功须同步结束按钮忙碌态并更新版本与状态', async () => {
    const { wb, buttons } = editor({ fetch: async () => ({ ok: true, json: async () => ({ code: 200, data: { draftVersion: 2 } }) }) });
    await wb.saveDraft();
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(wb.draftVersion, 2);
    assert.equal(wb.saveState, 'saved');
    assert.equal(buttons.get('wb-status').textContent, 'saved');
    assert.equal(buttons.get('wb-save-draft').disabled, false);
    assert.equal(buttons.get('wb-publish').disabled, false);
});

test('保存进行中新输入须保留脏状态与备份，重复保存不得并发写入', async () => {
    let resolve;
    let calls = 0;
    const { wb } = editor({ fetch: () => { calls++; return new Promise(r => { resolve = r; }); } });
    const saving = wb.saveDraft();
    wb.doc.root[0].props.text = '保存期间的新文字';
    wb.saveDraft();
    resolve({ ok: true, json: async () => ({ code: 200, data: { draftVersion: 2 } }) });
    await saving;
    await new Promise(r => setImmediate(r));
    assert.equal(calls, 1);
    assert.equal(wb.saveState, 'dirty');
    assert.equal(wb.backupCleared, undefined);
    assert.equal(wb.doc.root[0].props.text, '保存期间的新文字');
});

for (const failure of ['conflict', 'network', 'invalid']) {
    test('保存失败恢复按钮且保留版本与文档：' + failure, async () => {
        const { wb, buttons } = editor({ fetch: async () => {
            if (failure === 'network') throw new Error('offline');
            return { ok: failure !== 'conflict', status: 409, json: async () => {
                if (failure === 'invalid') throw new Error('invalid JSON');
                return { code: 409, message: '版本已更新，请先核对修订历史' };
            } };
        } });
        const before = JSON.stringify(wb.doc);
        await wb.saveDraft();
        await new Promise(r => setImmediate(r));
        assert.equal(wb.busy, false);
        assert.equal(wb.saveState, 'error');
        assert.equal(wb.draftVersion, 1);
        assert.equal(buttons.get('wb-save-draft').disabled, false);
        assert.equal(JSON.stringify(wb.doc), before);
    });
}

test('撤销重做须使旧节点引用失效并立即更新按钮状态', () => {
    const { wb, buttons } = editor();
    wb.invalidateInspector = function () { this.invalidations = (this.invalidations || 0) + 1; };
    wb.snapshot();
    wb.doc.root[0].props.text = '修改';
    wb.undo();
    assert.equal(wb.doc.root[0].props.text, '原文');
    assert.equal(wb.invalidations, 1);
    assert.equal(buttons.get('wb-redo').disabled, false);
    wb.redo();
    assert.equal(wb.doc.root[0].props.text, '修改');
    assert.equal(wb.invalidations, 2);
    assert.equal(buttons.get('wb-redo').disabled, true);
});

for (const [type, spec] of [
    ['page', {idKey:'id',documentKey:'draftDocument',versionKey:'expectedVersion',pathKey:'draftPath'}],
    ['block', {idKey:'blockId',documentKey:'document',versionKey:'version'}],
    ['template', {idKey:'templateId',documentKey:'draft',extras:{context:'contextKey'}}]
]) test('目标描述符决定保存路径、字段和版本：' + type, async () => {
    let sent;
    const metadata = {pageId:'target-1',draftPath:'/demo',contextKey:'product',target:{type,save:{path:'/api/'+type+'/save'},saveBody:spec}};
    const {wb} = editor({meta:metadata, fetch:async (url, options)=>{
        sent={url, body:JSON.parse(options.body)};
        return {ok:true,json:async()=>({code:200,data:{draftVersion:2}})};
    }});
    const before=JSON.parse(JSON.stringify(wb.doc));
    await wb.saveDraft();
    assert.equal(sent.url,metadata.target.save.path);
    assert.equal(sent.body[spec.idKey],'target-1');
    assert.deepEqual(sent.body[spec.documentKey],before);
    if(spec.versionKey) assert.equal(sent.body[spec.versionKey],1);
    else assert.equal('expectedVersion' in sent.body,false);
    if(spec.pathKey) assert.equal(sent.body.draftPath,'/demo');
    if(spec.extras) assert.equal(sent.body.context,'product');
});

test('粘贴样式不得替换目标文字、链接或内容绑定',()=>{
    const destination={id:'b',type:'core.button',props:{text:'目标文案',value:'/destination',binding:{field:'post.title'},bg:'#ffffff'}};
    const wb=Object.assign({selectedId:'b',findNode:()=>destination,snapshot(){},renderTree(){},refreshCanvas(){},syncInspector(){},renderUI(){},setNotice(){}},methods('canvas','canvasMethods',{
        clone: v=>JSON.parse(JSON.stringify(v)),componentSchemas:{'core.button':[{key:'bg',section:'background'},{key:'text',section:'content'},{key:'value',section:'content'}]}
    }));
    wb.refreshCanvas=function(){};
    wb.syncInspector=function(){};
    wb.styleClipboard={type:'core.button',props:{text:'来源文案',value:'/source',bg:'#18181b'}};
    wb.pasteStyle();
    assert.deepEqual(JSON.parse(JSON.stringify(destination.props)),{text:'目标文案',value:'/destination',binding:{field:'post.title'},bg:'#18181b'});
});

test('基础分类和四页签覆盖常见 HTML 元素与嵌套属性',async()=>{
    const {categoryOf,inspectorTabOf}=await import('./experience.js');
    for (const type of ['container','heading','text','button','image','list','quote','video','table']) assert.equal(categoryOf('core.'+type),'basic');
    assert.equal(categoryOf('core.productList'),'commerce');
    assert.equal(categoryOf('plugin.new'),'commerce');
    assert.equal(inspectorTabOf('content','text'),'content');
    assert.equal(inspectorTabOf('style','advanced.padding.desktop'),'layout');
    assert.equal(inspectorTabOf('border','advanced.border.width'),'style');
    assert.equal(inspectorTabOf('style','transform'),'style');
    assert.equal(inspectorTabOf('background','visual.bgColor'),'style');
    assert.equal(inspectorTabOf('style','advanced.interaction.entrance'),'motion');
});


test('复制区块同时重写内部锚点',()=>{
    let next=0;
    const wb=Object.assign({makeIdAllocator:()=>()=> 'copy-'+(++next)},methods('canvas','canvasMethods'));
    const nodes=[{id:'a',type:'core.heading',props:{advanced:{customId:'section'}}},{id:'b',type:'core.button',props:{action:'anchor',value:'section'}}];
    wb.makeIdAllocator = ()=>()=> 'copy-'+(++next);
    wb.prepareInsertedNodes(nodes);
    assert.equal(nodes[0].id,'copy-1');
    assert.equal(nodes[0].props.advanced.customId,'copy-1-anchor');
    assert.equal(nodes[1].props.value,'copy-1-anchor');
});


test('撤销后迟到的富文本事件不能污染历史和当前文档',()=>{
    const source=fs.readFileSync(__dirname+'/methods/controls/text.js','utf8').replace(/^import[^;]+;/gm,'').replace(/export /g,'');
    const context=vm.createContext({get:()=> '旧值',set:()=>{throw new Error('旧控件不得写入');}});
    vm.runInContext(source,context);
    const oldNode={id:'text-1'};
    let snapshots=0;
    context.syncRichValue({node:oldNode,self:{findNode:()=>({id:'text-1'}),snapshot(){snapshots++;}}},'props.text',{value:'迟到的输入'});
    assert.equal(snapshots,0);
});
