import { inspectorMethods } from '/qa-static/js/workbench/methods/inspector.js';
import { stateMethods } from '/qa-static/js/workbench/methods/state.js';
import { bindRepeaterPanel } from '/qa-static/js/workbench/methods/controls/repeater.js';
import { paletteItems, buildInsertNode, alignKeyOf } from '/qa-static/js/workbench/palette.js';
import { field } from '/qa-static/js/workbench/methods/controls/base.js';
import { shortcutsMethods } from '/qa-static/js/workbench/methods/shortcuts.js';

let seq = 0;
let compileSeq = 0;
const status = document.getElementById('qa-status');
const error = document.getElementById('qa-error');
window.addEventListener('error', e => { error.textContent = e.message; });
window.addEventListener('unhandledrejection', e => { error.textContent = String(e.reason); });
const wb = Object.assign({}, inspectorMethods, {
    snapshot: stateMethods.snapshot, undo: stateMethods.undo, redo: stateMethods.redo,
    undoStack: [], redoStack: [], tab: 'content',
    findNode(id) { return this.doc.root.find(n => n.id === id); },
    makeIdAllocator() { return base => base + '-qa-' + (++seq); },
    backupDoc() {}, renderTree() {}, copyNode() {}, pasteStyle() {},
    refreshFieldHints() {},
    renderUI() { document.getElementById('qa-document').textContent = JSON.stringify(this.doc, null, 2); },
    async patchCanvas() {
        this.renderUI();
        const at = ++compileSeq;
        const r = await fetch('/qa/compile', { method: 'POST', body: JSON.stringify(this.doc) });
        const result = await r.text();
        if (at !== compileSeq) return;
        const n = this.doc.root[0];
        status.textContent = `${r.ok ? '编译成功' : '编译失败'}；条目 ${n.props[alignKeyOf(n.type)].length}；面板 ${n.children.length}；撤销栈 ${this.undoStack.length}`;
        error.textContent = r.ok ? '' : result;
    },
    refreshCanvas() { return this.patchCanvas(); }
});
function select(type) {
    const n = buildInsertNode(paletteItems.find(p => p.type === type), wb.makeIdAllocator());
    wb.doc = { settings: { layout: { mode: 'full' } }, root: [n] };
    wb.selectedId = n.id;
    wb.undoStack = []; wb.redoStack = []; wb.tab = 'content';
    wb.syncInspector(); wb.patchCanvas();
}
document.getElementById('tabs').onclick = () => select('core.tabs');
document.getElementById('accordion').onclick = () => select('core.accordion');
document.getElementById('content').onclick = () => { wb.tab = 'content'; wb.syncInspector(); };
document.getElementById('style').onclick = () => { wb.tab = 'style'; wb.syncInspector(); };
document.getElementById('rebind').onclick = () => bindRepeaterPanel({ panel: document.getElementById('inspector-panel'), node: wb.doc.root[0], self: wb });
document.getElementById('undo').onclick = () => wb.undo();
document.getElementById('redo').onclick = () => wb.redo();
document.getElementById('toast').onclick = () => WBUI.toast('控件已就绪', {type:'success', duration:3000});
select('core.tabs');

// 真实复杂字段入口 + 原生表单入口共享控件。输出只记录交互结果，不复刻控件逻辑。
const uiForm = document.getElementById('qa-ui-form');
const dynamic = document.getElementById('qa-dynamic');
const sampleNode = {props:{choice:'a'}};
let changes = 0, deleted = 0;
function uiStatus() {
    document.getElementById('qa-ui-status').textContent = `原生 ${uiForm.elements.choice.value}；动态 ${sampleNode.props.choice}；变更 ${changes}；误删 ${deleted}`;
}
function dynamicField() {
    dynamic.replaceChildren();
    field({panel:dynamic, node:sampleNode, self:{snapshot(){changes++;},renderTree(){},renderUI:uiStatus,refreshCanvas(){}}},
        '动态字段', 'props.choice', 'select', [['a','选项 A'],['z','选项 Z']]);
}
dynamicField();
uiStatus();
uiForm.addEventListener('change', e => { if(e.target.id==='qa-native') changes++; uiStatus(); });
uiForm.addEventListener('reset', () => setTimeout(uiStatus,0));
document.getElementById('qa-replace').onclick = () => {
    const keys = WBUI.select.openKeys(document.getElementById('qa-ui'));
    const old = uiForm.elements.choice;
    const replacement = old.closest('.wbs').cloneNode(true);
    old.closest('.wbs').replaceWith(replacement);
    dynamicField();
    WBUI.scan(uiForm);
    WBUI.select.restoreOpen(document.getElementById('qa-ui'),keys);
    uiStatus();
};
document.getElementById('qa-options').onclick = () => {
    for(let i=1;i<=20;i++) uiForm.elements.choice.add(new Option(`附加选项 ${i}`,`extra-${i}`));
};
document.getElementById('qa-disable').onclick = () => {uiForm.elements.choice.disabled = !uiForm.elements.choice.disabled;};
document.getElementById('qa-set').onclick = () => {
    sampleNode.props.choice='z'; dynamicField(); uiStatus();
};
document.addEventListener('keydown', e => shortcutsMethods.onKeydown.call({deleteSelected(){deleted++;uiStatus();},findParent(){return null;}},e));
