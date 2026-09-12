import { inspectorMethods } from '/static/js/workbench/methods/inspector.js';
import { stateMethods } from '/static/js/workbench/methods/state.js';
import { bindRepeaterPanel } from '/static/js/workbench/methods/controls/repeater.js';
import { paletteItems, buildInsertNode, alignKeyOf } from '/static/js/workbench/palette.js';

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
