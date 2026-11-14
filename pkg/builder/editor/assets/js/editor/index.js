// editor/index.js — 可视化编辑器入口：组装各功能模块并启动。
//
//   core.js               共享内核（meta/CSRF/通用控件/宿主适配注入）
//   catalog.js            组件库算法内核（目录数据由应用注入）
//   methods/state.js      视图动作、组件库、快照与草稿备份
//   methods/canvas.js     画布直改、画布联动、剪贴板
//   methods/nodes.js      节点查找、右键菜单、选择联动
//   methods/tree.js       结构树
//   methods/inspector.js  检查器面板与全部控件
//   methods/shortcuts.js  快捷键与初始化
//   methods/api.js        保存/发布经宿主适配
//
// 工具不 import 应用模块、不内置应用端点；业务操作全部在注入的 host 上。
import { meta, initialDoc, hostBlocks } from './core.js';
import { stateMethods } from './methods/state.js';
import { canvasMethods } from './methods/canvas.js';
import { nodesMethods } from './methods/nodes.js';
import { treeMethods } from './methods/tree.js';
import { inspectorMethods } from './methods/inspector.js';
import { apiMethods } from './methods/api.js';
import { workspaceMethods } from './methods/workspace.js';
import { panelsMethods } from './methods/panels.js';
import { shortcutsMethods } from './methods/shortcuts.js';
import { previewMethods } from './preview.js';

/** editor 实例工厂：初始状态 + 各功能模块方法合并。 */
export function workbench(extraMethods) {
    return Object.assign({
        pageId: meta.pageId,
        draftVersion: meta.version,
        doc: initialDoc,

        device: 'desktop',
        previewState: 'default',
        tab: 'content',
        view: 'library',
        paletteOpen: { basic: true },
        libraryFilter: '',
        pendingInsertTarget: null,
        immersive: false,
        navigatorOpen: false,
        libraryCategory: 'basic',
        filter: '',
        busy: false,
        saveState: '',

        selectedId: '',
        selectedSlot: null,
        notice: '',
        clipboard: null,
        styleClipboard: null,

        undoStack: [],
        redoStack: [],

        blocks() { return hostBlocks(); },
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
        stateMethods, canvasMethods, nodesMethods,
        treeMethods, inspectorMethods, apiMethods,
        workspaceMethods, panelsMethods, shortcutsMethods, previewMethods, extraMethods || {});

    /** blocks 当前工程可引用的全局块事实（宿主提供，工具不读业务表）。 */
}
export function presetIdentity(preset) {
    return preset.presetKey ? 'business:' + preset.presetKey + '@' + preset.presetVersion : preset.id;
}

// mount 装配并启动编辑器；extraMethods 是应用宿主提供的方法（媒体库、业务面板等）。
// 工具不自动 boot：宿主负责在配置 host / catalog / experience 后显式调用。
export function mount(extraMethods) {
    if (window.__wb) return window.__wb;
    var wb = workbench(extraMethods);
    try {
        wb.init();
    } catch (error) {
        wb._initError = (error && error.stack) || String(error);
        throw error;
    }
    window.__wb = wb;
    // meta 是编辑器自己的实时元数据（主题设置等会被面板就地更新）；
    // 业务侧的对象提供器必须读这一份，而不是重新解析 #wb-meta 数据岛 ——
    // 数据岛是页面加载时的事实，保存后面板再打开会提交旧值。
    wb.meta = meta;
    window.workbench = workbench;
    return wb;
}
