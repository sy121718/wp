// workbench/methods/shortcuts.js — 快捷键体系与初始化（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER,
} from '../core.js';

export const shortcutsMethods = {
            // ---------------- 快捷键体系（§5.2） ----------------
            onKeydown(e) {
                var mod = e.ctrlKey || e.metaKey;
                // 自定义下拉展开时 Escape 先收起下拉（与原生 select 一致），不触发组件快捷键。
                if (e.key === 'Escape' && document.querySelector('.wbs.is-open')) {
                    window.WBUI.select.closeAll();
                    e.preventDefault();
                    return;
                }
                // 焦点在输入控件内时不触发任何组件快捷键（否则输入框里按退格会误删组件）。
                // 自定义下拉（.wbs）由 button 组成，不在这三类里，必须单独排除：
                // 选中选项后焦点停在按钮上，此时按 Delete 会误删整个组件。
                var ae = document.activeElement;
                var inField = ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' || ae.tagName === 'SELECT'
                    || ae.isContentEditable || (ae.closest && ae.closest('.wbs')));
                if (inField) return;
                if (!mod) {
                    if (e.key === 'Delete' || e.key === 'Backspace') { this.deleteSelected(); e.preventDefault(); }
                    if (e.key === 'Escape') {
                        var parent = this.findParent(this.selectedId);
                        if (parent) this.select(parent.id);
                    }
                    return;
                }
                switch (e.key.toLowerCase()) {
                    case 's': this.saveDraft(); e.preventDefault(); break;                 // 存草稿
                    case 'p': this.immersive = !this.immersive; e.preventDefault(); break;  // 沉浸折叠
                    case 'z': e.shiftKey ? this.redo() : this.undo(); e.preventDefault(); break;
                    case 'y': this.redo(); e.preventDefault(); break;
                    case 'd': this.duplicate(); e.preventDefault(); break;
                    case 'c': this.copyNode(); break;
                    case 'x': this.cutNode(); break;
                    case 'v':
                        if (e.shiftKey) this.pasteStyle(); else this.pasteInto('');
                        break;
                }
            },

            init() {
                var self = this;
                // 不再强制包裹「页面主体」容器：骨架由 settings 层稳定
                //（structure/版心/主题），正文顶级 Section 平铺是一等形态，
                // 容器按需添加；空文档/多根文档打开即干净（不再误标 dirty）。
                window.__wb = this;
                this.registerPluginPalette();
                this.setDevice(this.device);
                this.renderPalette();
                this.renderTree();
                this.renderUI();
                // 崩溃恢复：检测未保存的本地备份（在预载媒体库之前弹出，避免遮挡）。
                this.maybeOfferBackup();
                // 预载媒体库列表：检查器媒体字段缩略图解析依赖 _mediaCache（URL 直出）。
                this.loadMediaList();
                document.addEventListener('keydown', function (e) { self.onKeydown(e); });
                // iframe 内点击经 postMessage 上报选中（editor=1 注入桥接脚本）。
                window.addEventListener('message', function (ev) {
                    if (ev.origin !== window.location.origin || !ev.data) return;
                    if (ev.data.type === 'wb-select' && ev.data.id) { self.select(ev.data.id); return; }
                    if (ev.data.type === 'wb-insert-here' && ev.data.id) {
                        // 画布「+ 插入组件」浮标：目标是容器则插入其内部，否则插到其后。
                        var node = self.findNode(ev.data.id);
                        self.pendingInsertTarget = {
                            id: ev.data.id,
                            placement: node && node.type === 'core.container' ? 'inside' : 'after'
                        };
                        self.showLibrary();
                        return;
                    }
                    if (ev.data.type === 'wb-canvas-drop' && ev.data.nodeID) {
                        // 画布内元素拖动重排：桥接已算好落点，容器中带按内部插入。
                        var t = ev.data.targetID ? self.findNode(ev.data.targetID) : null;
                        var placement = ev.data.placement;
                        if (t && t.type === 'core.container' && ev.data.inMiddle) placement = 'inside';
                        if (!t) return;
                        self.moveNode(ev.data.nodeID, ev.data.targetID, placement);
                    }
                    // ===== 画布直改：双击编辑文本回写 =====
                    if (ev.data.type === 'wb-edit-text' && ev.data.id) {
                        var node = self.findNode(ev.data.id);
                        if (!node || typeof ev.data.text !== 'string') return;
                        // 文本字段映射：按组件类型回写对应文本 prop。
                        var textField = self.textFieldOf(node.type);
                        if (textField) {
                            self.snapshot();
                            node.props = node.props || {};
                            node.props[textField] = ev.data.text.slice(0, 2000);
                            self.saveState = 'dirty';
                            self.renderTree(); self.refreshCanvas(); self.renderUI();
                        }
                    }
                    // ===== 画布直改：右键菜单/快捷条操作 =====
                    if (ev.data.type === 'wb-ctx' && ev.data.id) {
                        self.handleCanvasCtx(ev.data.id, ev.data.op, ev.data.value);
                    }
                });
                var search = document.querySelector('#wb-navigator .wb-search');
                if (search) search.addEventListener('input', function () {
                    self.filter = search.value;
                    self.renderTree();
                });
                // 检查器三个页签：布局(内容) / 样式 / 扩展——切换后重渲染面板。
                // 检查器两个页签：内容 / 样式（样式类属性全部归样式页签）。
                var tabKeys = ['content', 'style'];
                var tabButtons = document.querySelectorAll('.wb-tabs button');
                tabButtons.forEach(function (button, index) {
                    button.addEventListener('click', function () {
                        self.tab = tabKeys[index] || 'content';
                        tabButtons.forEach(function (b, i) { b.classList.toggle('is-active', i === index); });
                        self.syncInspector();
                    });
                });
                // 面板头部显隐/锁定快捷开关。
                var editHideBtn = document.getElementById('wb-edit-hide');
                if (editHideBtn) editHideBtn.addEventListener('click', function () {
                    var n = self.findNode(self.selectedId);
                    if (!n) return;
                    self.toggleHidden(n.id);
                    editHideBtn.textContent = n.hidden ? '🚫' : '👁';
                });
                var editLockBtn = document.getElementById('wb-edit-lock');
                if (editLockBtn) editLockBtn.addEventListener('click', function () {
                    var n = self.findNode(self.selectedId);
                    if (!n) return;
                    self.toggleLocked(n.id);
                    editLockBtn.textContent = n.locked ? '🔒' : '🔓';
                });
                // 左侧双视图：+ 打开组件库；× 收起面板；🗑 删除选中组件。
                var back = document.getElementById('wb-back-library');
                if (back) back.addEventListener('click', function () { self.showLibrary(); });
                // 「+ 添加组件」与「SEO」并入左侧面板顶部标签条（对齐 Elementor 的顶部入口）。
                var railAdd = document.getElementById('wb-rail-add');
                if (railAdd) railAdd.addEventListener('click', function () {
                    var inspector = document.querySelector('.wb-inspector');
                    if (inspector) inspector.classList.add('is-open');
                    self.showLibrary();
                });
                var railSeo = document.getElementById('wb-rail-seo');
                if (railSeo) railSeo.addEventListener('click', function () {
                    self.showPanel('settings');
                    // 页面设置打开后滚到 SEO 区块（标题输入处）。
                    setTimeout(function () {
                        var wrap = Array.prototype.slice.call(document.querySelectorAll('#wb-settings-body .wb-field')).filter(function (w) {
                            var l = w.querySelector('label');
                            return l && l.textContent === 'SEO 标题';
                        })[0];
                        if (wrap) wrap.scrollIntoView({ block: 'start', behavior: 'smooth' });
                    }, 150);
                });
                var libClose = document.getElementById('wb-library-close');
                if (libClose) libClose.addEventListener('click', function () {
                    document.getElementById('wb-panel-library').hidden = true;
                    if (!self.selectedId) self.showEdit();
                });
                var editDelete = document.getElementById('wb-edit-delete');
                if (editDelete) editDelete.addEventListener('click', function () { self.deleteSelected(); });
                var libSearch = document.getElementById('wb-library-search');
                if (libSearch) libSearch.addEventListener('input', function () {
                    self.libraryFilter = libSearch.value;
                    self.renderPalette();
                });
                // 左侧图标栏：面板切换（编辑视图归入「组件」入口）。
                [['wb-rail-library', 'library'], ['wb-rail-settings', 'settings'],
                 ['wb-rail-global', 'global'], ['wb-rail-history', 'history']].forEach(function (pair) {
                    var b = document.getElementById(pair[0]);
                    if (b) b.addEventListener('click', function () { self.showPanel(pair[1]); });
                });
                var railNav = document.getElementById('wb-rail-navigator');
                if (railNav) railNav.addEventListener('click', function () {
                    self.navigatorOpen = !self.navigatorOpen;
                    document.getElementById('wb-navigator').classList.toggle('is-hidden', !self.navigatorOpen);
                });
                // 组件库页签：组件 | 全局块 | SEO。
                document.querySelectorAll('.wb-lib-tabs button').forEach(function (btn) {
                    btn.addEventListener('click', function () {
                        document.querySelectorAll('.wb-lib-tabs button').forEach(function (b) { b.classList.toggle('is-active', b === btn); });
                        var tab = btn.dataset.libtab;
                        var p1 = document.getElementById('wb-palette');
                        var p2 = document.getElementById('wb-palette-blocks');
                        var p3 = document.getElementById('wb-palette-seo');
                        if (p1) p1.hidden = tab !== 'components';
                        if (p2) p2.hidden = tab !== 'blocks';
                        if (p3) p3.hidden = tab !== 'seo';
                    });
                });
                // 媒体库弹层：关闭/遮罩/上传。
                var mediaClose = document.getElementById('wb-media-close');
                if (mediaClose) mediaClose.addEventListener('click', function () { self.closeMediaPicker(); });
                // 媒体弹窗：名称搜索（防抖）与分类树搜索。
                var mediaSearch = document.getElementById('wb-media-search');
                var mediaSearchTimer = null;
                if (mediaSearch) mediaSearch.addEventListener('input', function () {
                    clearTimeout(mediaSearchTimer);
                    mediaSearchTimer = setTimeout(function () {
                        self._mediaSearch = mediaSearch.value.trim();
                        self.loadMediaList();
                    }, 300);
                });
                var mediaTreeSearch = document.getElementById('wb-media-tree-search');
                if (mediaTreeSearch) mediaTreeSearch.addEventListener('input', function () {
                    self.renderMediaTree();
                });
                var mediaMask = document.querySelector('#wb-media-modal .wb-media-mask');
                if (mediaMask) mediaMask.addEventListener('click', function () { self.closeMediaPicker(); });
                var mediaUpBtn = document.getElementById('wb-media-upload-btn');
                var mediaUpInput = document.getElementById('wb-media-upload-input');
                if (mediaUpBtn && mediaUpInput) {
                    mediaUpBtn.addEventListener('click', function () { mediaUpInput.click(); });
                    mediaUpInput.addEventListener('change', function () {
                        if (mediaUpInput.files && mediaUpInput.files[0]) self.uploadMedia(mediaUpInput.files[0]);
                        mediaUpInput.value = '';
                    });
                }
                var controls = {
                    'wb-device-desktop': function () { self.setDevice('desktop'); },
                    'wb-device-laptop': function () { self.setDevice('laptop'); },
                    'wb-device-tablet': function () { self.setDevice('tablet'); },
                    'wb-device-mobileL': function () { self.setDevice('mobileL'); },
                    'wb-device-mobile': function () { self.setDevice('mobile'); },
                    'wb-save-draft': function () { self.saveDraft(); },
                    'wb-publish': function () { self.buildAndPublish(); },
                    'wb-page-settings': function () { self.showPanel('settings'); },
                    'wb-navigator-toggle': function () {
                        self.navigatorOpen = !self.navigatorOpen;
                        document.getElementById('wb-navigator').classList.toggle('is-hidden', !self.navigatorOpen);
                    },
                    'wb-navigator-close': function () {
                        self.navigatorOpen = false;
                        document.getElementById('wb-navigator').classList.add('is-hidden');
                    },
                    'wb-undo': function () { self.undo(); self.renderUI(); },
                    'wb-redo': function () { self.redo(); self.renderUI(); },
                    'wb-immersive': function () {
                        self.immersive = !self.immersive;
                        document.getElementById('wb-main').classList.toggle('is-immersive', self.immersive);
                        this.textContent = self.immersive ? '退出沉浸模式' : '沉浸模式 (Ctrl+P)';
                    }
                };
                Object.keys(controls).forEach(function (id) {
                    var button = document.getElementById(id);
                    if (button) button.addEventListener('click', controls[id]);
                });
                // 画布加载后重新绑定拖放，并同步大纲树高亮。
                var frame = document.getElementById('wb-canvas');
                if (frame) frame.addEventListener('load', function () {
                    self.bindCanvasDrop();
                    self.markTreeSelection();
                });
            }
};
