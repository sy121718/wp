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
    const {categoryOf,inspectorTabOf,initialInspectorTabOf}=await import('./experience.js');
    for (const type of ['container','heading','text','button','image','list','quote','video','table']) assert.equal(categoryOf('core.'+type),'basic');
    assert.equal(categoryOf('core.productList'),'commerce');
    assert.equal(categoryOf('plugin.new'),'plugins');
    for (const type of ['articleList','searchResults','nav','form']) assert.equal(categoryOf('core.'+type),'site');
    assert.equal(inspectorTabOf('content','text'),'content');
    assert.equal(inspectorTabOf('collection','collection.source'),'content');
    assert.equal(inspectorTabOf('style','columns.desktop'),'layout');
    assert.equal(inspectorTabOf('style','layout'),'layout');
    assert.equal(inspectorTabOf('style','advanced.padding.desktop'),'layout');
    assert.equal(inspectorTabOf('border','advanced.border.width'),'style');
    assert.equal(inspectorTabOf('style','transform'),'style');
    assert.equal(inspectorTabOf('background','visual.bgColor'),'style');
    assert.equal(inspectorTabOf('style','advanced.interaction.entrance'),'motion');
    assert.equal(initialInspectorTabOf('core.container'),'layout');
    assert.equal(initialInspectorTabOf('core.spacer'),'layout');
    assert.equal(initialInspectorTabOf('core.divider'),'style');
    assert.equal(initialInspectorTabOf('core.list'),'content');
});

test('自动选择布局页签时，高亮和无障碍选中态一起同步', () => {
    const buttons = ['content','layout','style','motion'].map(tab => ({ dataset: {tab},
        classList: {toggle(_name, selected) { this.selected = selected; }},
        setAttribute(_name, value) { this.selected = value; } }));
    const inspector = methods('inspector', 'inspectorMethods', { document: {
        querySelectorAll: () => buttons, getElementById: () => null
    } });
    inspector.syncInspector.call({ tab: 'layout' });
    assert.deepEqual(buttons.map(b => b.selected), ['false','true','false','false']);
    assert.deepEqual(buttons.map(b => b.classList.selected), [false,true,false,false]);
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

function controlContext(globals = {}) {
    const context = vm.createContext({ console, ...globals });
    for (const file of ['base', 'text', 'repeater', 'structured']) {
        const source = fs.readFileSync(__dirname + '/methods/controls/' + file + '.js', 'utf8')
            .replace(/^import[^;]+;/gm, '').replace(/export /g, '');
        vm.runInContext(source, context);
    }
    return context;
}

test('重复项先保留旧快照，连续编辑与增删都能逐步撤销', () => {
    const control = controlContext({ clone: v => JSON.parse(JSON.stringify(v)) });
    const { wb } = editor();
    wb.doc.root[0].props.items = [{ text: '第一项', question: '问题', answer: '旧答案' }];
    wb.findNode = id => wb.doc.root.find(n => n.id === id);
    wb.patchCanvas = () => {};
    wb.invalidateInspector = () => {};
    const ctx = { self: wb, node: wb.doc.root[0] };
    control.updateItems(ctx, items => { items[0].text = '第二项'; });
    control.updateItems(ctx, items => { items[0].text = '第三项'; });
    wb.undo();
    assert.equal(wb.doc.root[0].props.items[0].text, '第二项');
    wb.undo();
    assert.equal(wb.doc.root[0].props.items[0].text, '第一项');
    ctx.node = wb.doc.root[0];
    control.commit(ctx, 'props.items.0.answer', '富文本新答案');
    control.updateItems(ctx, items => { items[0].question = '新问题'; });
    assert.equal(ctx.node.props.items[0].answer, '富文本新答案');
    control.updateItems(ctx, items => { items.push({ text: '新增项' }); });
    control.updateItems(ctx, items => { items.splice(0, 1); });
    wb.undo();
    assert.equal(wb.doc.root[0].props.items.length, 2);
    wb.undo();
    assert.equal(wb.doc.root[0].props.items.length, 1);
    assert.equal(wb.doc.root[0].props.items[0].question, '新问题');
});

test('弹层图标选择器能打开、搜索和回填，并拒绝把未知图标作为 HTML', () => {
    function element() {
        return { children: [], style: {}, attrs: {}, events: {}, classList: { add() {}, toggle() {} },
            appendChild(child) { this.children.push(child); },
            contains(child) { return child === this || this.children.includes(child); },
            setAttribute(key, value) { this.attrs[key] = value; },
            addEventListener(key, fn) { this.events[key] = fn; },
            set innerHTML(value) { this.html = value; this.children = []; },
            get innerHTML() { return this.html; } };
    }
    const document = { createElement: element, events: {},
        addEventListener(key, fn) { this.events[key] = fn; }, removeEventListener(key) { delete this.events[key]; } };
    const names = ['check', 'shield'];
    const WPIcons = { names, svg: name => '<svg>' + name + '</svg>', label: name => name,
        filter: (_category, _style, keyword) => names.filter(name => name.includes(keyword)) };
    const control = controlContext({ document, window: { WPIcons } });
    let selected;
    const picker = control.iconPopupPicker({}, '列表图标', 'check', value => { selected = value; });
    const button = picker.btn, popup = picker.pop;
    assert.equal(button.innerHTML, '<svg>check</svg>');
    button.events.click();
    assert.equal(button.attrs['aria-expanded'], 'true');
    const svgChild = element();
    button.children.push(svgChild);
    document.events.click({ target: svgChild });
    assert.equal(button.attrs['aria-expanded'], 'true');
    const search = popup.children[0].children[0];
    search.value = 'shield';
    search.events.input();
    const grid = popup.children[1];
    assert.equal(grid.children.length, 1);
    grid.children[0].events.click();
    assert.equal(selected, 'shield');
    assert.equal(button.attrs['aria-expanded'], 'false');
    const fallback = element();
    control.renderIcon(fallback, '<img src=x>', '');
    assert.equal(fallback.innerHTML, undefined);
    assert.equal(fallback.textContent, '<img src=x>');
});

test('表格增删列同时维护每行，且一次撤销恢复整个结构', () => {
    const control = controlContext({ clone: value => JSON.parse(JSON.stringify(value)) });
    const { wb } = editor();
    wb.doc.root[0].props = { headers: ['名称','价格'], rows: [['A','10'],['B','20']], advanced: {customId:'prices'} };
    wb.findNode = id => wb.doc.root.find(node => node.id === id);
    wb.patchCanvas = () => {};
    wb.invalidateInspector = () => {};
    const ctx = { node: wb.doc.root[0], self: wb };
    const before = JSON.stringify(wb.doc);
    control.updateProps(ctx, control.addTableColumn);
    assert.equal(ctx.node.props.headers.length, 3);
    assert.ok(ctx.node.props.rows.every(row => row.length === ctx.node.props.headers.length));
    assert.equal(ctx.node.props.advanced.customId, 'prices');
    wb.undo();
    assert.equal(JSON.stringify(wb.doc), before);
    ctx.node = wb.doc.root[0];
    control.updateProps(ctx, props => { control.removeTableColumn(props, 0); });
    assert.equal(ctx.node.props.headers[0], '价格');
    assert.equal(ctx.node.props.rows[0][0], '10');
    wb.undo();
    assert.equal(JSON.stringify(wb.doc), before);
    const headerless = {rows:[['A']]};
    control.addTableColumn(headerless);
    assert.equal(headerless.headers, undefined);
    assert.equal(headerless.rows[0].length, 2);
});

test('表单字段结构操作读取最新值，撤销恢复标签和顺序', () => {
    const control = controlContext({ clone: value => JSON.parse(JSON.stringify(value)) });
    const { wb } = editor();
    wb.doc.root[0].props = {fields:[{type:'text',label:'姓名',name:'name'},{type:'email',label:'邮箱',name:'email'}],action:'/contact'};
    wb.findNode = id => wb.doc.root.find(node => node.id === id);
    wb.patchCanvas = () => {};
    wb.invalidateInspector = () => {};
    const ctx = { node: wb.doc.root[0], self: wb };
    control.commit(ctx, 'props.fields.0.label', '联系人');
    const before = JSON.stringify(wb.doc);
    control.updateFormFields(ctx, fields => { fields.reverse(); });
    assert.equal(ctx.node.props.fields[1].label, '联系人');
    assert.equal(ctx.node.props.action, '/contact');
    wb.undo();
    assert.equal(JSON.stringify(wb.doc), before);
    ctx.node = wb.doc.root[0];
    control.updateFormFields(ctx, fields => { fields.splice(0, 1); });
    wb.undo();
    assert.equal(JSON.stringify(wb.doc), before);
});

function canvasEditor() {
    const pending = [];
    const messages = [];
    const timers = [];
    const submissions = [];
    const frame = { name: 'wb-canvas', contentWindow: { postMessage(message) { messages.push(message); } } };
    const form = {
        elements: { id: {}, expectedVersion: {}, draftDocument: {}, csrf_token: {} },
        submit() { submissions.push(JSON.parse(this.elements.draftDocument.value)); }
    };
    const document = { getElementById(id) { return id === 'wb-canvas' ? frame : id === 'wb-preview-form' ? form : null; } };
    const DOMParser = class {
        parseFromString(html) {
            return { querySelector(selector) { return selector === 'style' ? { textContent: 'css:' + html } : { outerHTML: html }; } };
        }
    };
    const wb = Object.assign({ doc: { root: [{ id: 'n1', props: { text: '原文' } }] }, draftVersion: 1 },
        methods('canvas', 'canvasMethods', {
            document, DOMParser, location: { origin: 'https://example.test' }, getCSRFToken: () => 'csrf',
            setTimeout(fn) { timers.push(fn); return timers.length; }, clearTimeout() {},
        }));
    wb.fetchCanvasHTML = () => new Promise((resolve, reject) => pending.push({ resolve, reject }));
    return { wb, pending, messages, timers, submissions };
}

const settleCanvas = () => new Promise(resolve => setImmediate(resolve));

for (const mode of ['refreshCanvas', 'flushCanvas']) {
    test('画布全页刷新必须拒绝此前迟到的局部结果：' + mode, async () => {
        const { wb, pending, messages, timers, submissions } = canvasEditor();
        wb.patchCanvas('n1');
        wb.doc.root[0].props.text = '撤销后的正文';
        wb[mode]();
        if (mode === 'refreshCanvas') timers.shift()();
        pending[0].resolve('<p>撤销前的旧正文</p>');
        await settleCanvas();
        assert.equal(submissions.length, 1);
        assert.equal(submissions[0].root[0].props.text, '撤销后的正文');
        assert.equal(messages.length, 0, '旧局部结果不得覆盖全页刷新后的文档');
    });
}

test('画布较早请求失败不得干扰更新后的预览', async () => {
    const { wb, pending, messages, timers } = canvasEditor();
    wb.patchCanvas('n1');
    wb.patchCanvas('n1');
    pending[1].resolve('<p>最新正文</p>');
    await settleCanvas();
    pending[0].reject(new Error('旧请求失败'));
    await settleCanvas();
    assert.equal(messages.length, 1);
    assert.equal(messages[0].html, '<p>最新正文</p>');
    assert.equal(timers.length, 0, '过期失败不得再次重载最新画布');
});

test('画布只回填最新局部结果并正常恢复当前失败', async () => {
    const { wb, pending, messages, timers } = canvasEditor();
    wb.patchCanvas('n1');
    wb.patchCanvas('n1');
    pending[1].resolve('<p>最新正文</p>');
    await settleCanvas();
    pending[0].resolve('<p>旧正文</p>');
    await settleCanvas();
    assert.equal(messages.length, 1);
    assert.equal(messages[0].html, '<p>最新正文</p>');
    wb.patchCanvas('n1');
    pending[2].reject(new Error('当前请求失败'));
    await settleCanvas();
    assert.equal(timers.length, 1, '只有当前失败仍应回退全页刷新');
});
