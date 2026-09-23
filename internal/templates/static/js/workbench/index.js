// workbench/index.js — 工作台前端入口：组装各功能模块并启动（docs/09 §3 拆分）。
//
// 拆分说明：原 workbench.js（4700 行单文件）按功能拆为
//   core.js                共享内核（meta/CSRF/通用控件/组件清单）
//   methods/state.js       视图动作、组件库、快照与草稿备份
//   methods/canvas.js      画布直改、画布联动、剪贴板
//   methods/history.js     修订历史面板
//   methods/nodes.js       节点查找、右键菜单、选择联动
//   methods/panels.js      面板切换、页面设置、全局设置
//   methods/tree.js        结构树
//   methods/inspector.js   检查器面板与全部控件
//   methods/media.js       媒体库选择器
//   methods/api.js         草稿保存/构建/发布接线
//   methods/shortcuts.js   快捷键与初始化
// 各模块方法以 `this` 互调，此处合并为同一个实例（Object.assign）。
import { meta, initialDoc } from './core.js';
import { stateMethods } from './methods/state.js';
import { canvasMethods } from './methods/canvas.js';
import { historyMethods } from './methods/history.js';
import { nodesMethods } from './methods/nodes.js';
import { panelsMethods } from './methods/panels.js';
import { treeMethods } from './methods/tree.js';
import { inspectorMethods } from './methods/inspector.js';
import { mediaMethods } from './methods/media.js';
import { apiMethods } from './methods/api.js';
import { shortcutsMethods } from './methods/shortcuts.js';

/** workbench 实例工厂：初始状态 + 各功能模块方法合并。 */
export function workbench() {
    return Object.assign({
        pageId: meta.pageId,
        draftVersion: meta.version,
        doc: initialDoc,

        // 视图状态
        device: 'desktop',
        tab: 'content',         // 检查器页签：content / style（与 layout.html 页签按钮初始高亮一致）
        view: 'edit',           // 左侧面板视图：edit / library / settings / global / history
        paletteOpen: { basic: true },  // 组件库手风琴展开状态（默认展开基础组件）
        libraryFilter: '',
        pendingInsertTarget: null, // 画布「+ 插入组件」浮标记住的插入位置
        immersive: false,
        navigatorOpen: true,
        filter: '',
        busy: false,
        saveState: '',

        // 选择与剪贴板
        selectedId: '',
        // 结构槽位选中态（页眉 / 页脚）：它不是本页的 AST 节点，单独记录选中，
        // 由槽位面板负责渲染（panels.js 的 renderSlotPanel）。与 selectedId 互斥。
        selectedSlot: null,
        // 一次性提示（状态栏文案）：像「槽位块已被拦下」这种**操作没发生**的反馈
        // 必须有出口，否则用户看到的是「点了没反应」。
        notice: '',
        clipboard: null,      // { mode: 'copy'|'cut', node }
        styleClipboard: null, // 仅样式

        // Undo / Redo
        undoStack: [],
        redoStack: [],

        // ------------------------------------------------------------------
        get canUndo() { return this.undoStack.length > 0; },
        get canRedo() { return this.redoStack.length > 0; },
        statusText() {
            if (this.busy) return '处理中…';
            if (this.notice) return this.notice;
            if (this.saveState === 'saved') return '已保存';
            if (this.saveState === 'dirty') return '有未保存修改';
            if (this.saveState === 'error') return '操作失败';
            return '就绪';
        },
        // setNotice 显示一次性提示（默认 6 秒后自动回到常规状态文案）。
        setNotice(text) {
            var self = this;
            this.notice = text || '';
            this.renderUI();
            clearTimeout(this._noticeTimer);
            if (this.notice) {
                this._noticeTimer = setTimeout(function () { self.notice = ''; self.renderUI(); }, 6000);
            }
        },
    },
        stateMethods, canvasMethods, historyMethods, nodesMethods,
        panelsMethods, treeMethods, inspectorMethods, mediaMethods,
        apiMethods, shortcutsMethods);
}

// 直接装配原生事件，避免宿主 Webview 的 Alpine 表达式缓存影响工作台操作。
function boot() {
    if (window.__wb) return;
    workbench().init();
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
else boot();

window.workbench = workbench;
