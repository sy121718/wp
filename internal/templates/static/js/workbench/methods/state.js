// workbench/methods/state.js — 视图动作 / 组件库 / 快照与草稿备份（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER,
} from '../core.js';

export const stateMethods = {
            // ---------------- 视图动作 ----------------
            // 设备预设：画布宽度 → iframe 视口宽度，媒体查询自动命中对应断点。
            // bp 仅用于画布外框样式（desktop/tablet/mobile 三档真实断点）。
            setDevice(d) {
                this.device = d;
                var DEF = {
                    desktop: { w: 0, bp: 'desktop' },
                    laptop: { w: 1024, bp: 'desktop' },
                    tablet: { w: 768, bp: 'tablet' },
                    mobileL: { w: 640, bp: 'mobile' },
                    mobile: { w: 375, bp: 'mobile' }
                };
                var cfg = DEF[d] || DEF.desktop;
                var frame = document.getElementById('wb-canvas-frame');
                if (frame) {
                    frame.className = 'wb-canvas-frame is-' + cfg.bp;
                    frame.style.width = cfg.w ? cfg.w + 'px' : '';
                }
                Object.keys(DEF).forEach(function (k) {
                    var button = document.getElementById('wb-device-' + k);
                    if (button) button.classList.toggle('is-active', k === d);
                });
            },
            renderUI() {
                var status = document.getElementById('wb-status');
                if (status) status.textContent = this.statusText();
                var undo = document.getElementById('wb-undo');
                if (undo) undo.disabled = !this.canUndo;
                var redo = document.getElementById('wb-redo');
                if (redo) redo.disabled = !this.canRedo;
                ['wb-save-draft', 'wb-publish'].forEach(function (id) {
                    var button = document.getElementById(id);
                    if (button) button.disabled = window.__wb && window.__wb.busy;
                });
            },

            // ---------------- 组件库与插入 ----------------
            // registerPluginPalette 把启用插件组件注入组件库（docs/06 §5）：
            // meta.plugins = [{type,label,hint,props}]，转成 paletteItems 项 +
            // 「插件组件」分组（types 动态），init 时调用一次（幂等防重复注册）。
            registerPluginPalette() {
                if (this._pluginPaletteRegistered) return;
                this._pluginPaletteRegistered = true;
                var list = (meta.plugins || []);
                if (!list.length) return;
                var types = [];
                var self = this;
                list.forEach(function (p) {
                    if (!p || !p.type) return;
                    // 防重复：同 type 已在内置 palette 则跳过。
                    if (paletteItems.some(function (e) { return e.type === p.type; })) return;
                    types.push(p.type);
                    paletteItems.push({
                        type: p.type,
                        label: p.label || p.type,
                        hint: p.hint || '插件组件',
                        props: p.props || {}
                    });
                });
                if (types.length) {
                    paletteGroups.push({ key: 'plugin', title: '插件组件', types: types });
                }
            },
            // ---------------- 快照与提交 ----------------
            snapshot() {
                this.undoStack.push(JSON.stringify(this.doc));
                if (this.undoStack.length > 100) this.undoStack.shift();
                this.redoStack.length = 0;
                this.saveState = 'dirty';
                this.backupDoc();
            },
            undo() {
                var prev = this.undoStack.pop();
                if (!prev) return;
                this.redoStack.push(JSON.stringify(this.doc));
                this.doc = JSON.parse(prev);
                this.renderTree(); this.refreshCanvas(); this.syncInspector();
                this.saveState = 'dirty';
                this.backupDoc();
            },
            redo() {
                var next = this.redoStack.pop();
                if (!next) return;
                this.undoStack.push(JSON.stringify(this.doc));
                this.doc = JSON.parse(next);
                this.renderTree(); this.refreshCanvas(); this.syncInspector();
                this.saveState = 'dirty';
                this.backupDoc();
            },

            // ---------------- 本地草稿备份（崩溃恢复） ----------------
            // 每次 AST 变更（snapshot/undo/redo）后节流写入 localStorage：
            // 页面误关/崩溃/刷新丢失时可在下次打开时恢复。保存成功即清除。
            backupKey() {
                return 'wb-backup-' + meta.pageId + '-' + (meta.saveBase || 'page');
            },
            backupDoc() {
                var self = this;
                if (this._backupTimer) return; // 节流 500ms
                this._backupTimer = setTimeout(function () {
                    self._backupTimer = null;
                    try {
                        localStorage.setItem(self.backupKey(), JSON.stringify({
                            doc: self.doc,
                            version: self.draftVersion,
                            savedAt: Date.now()
                        }));
                    } catch (e) { /* 存储满/禁用：静默降级，不影响编辑 */ }
                }, 500);
            },
            clearBackup() {
                if (this._backupTimer) {
                    clearTimeout(this._backupTimer);
                    this._backupTimer = null;
                }
                try { localStorage.removeItem(this.backupKey()); } catch (e) {}
            },
            // maybeOfferBackup 打开编辑器时检测备份：存在且与当前文档不同则浮层提示恢复。
            maybeOfferBackup() {
                var self = this;
                var raw = null;
                try { raw = localStorage.getItem(this.backupKey()); } catch (e) { return; }
                if (!raw) return;
                var backup = null;
                try { backup = JSON.parse(raw); } catch (e) { return; }
                if (!backup || !backup.doc || !Array.isArray(backup.doc.root)) return;
                if (JSON.stringify(backup.doc) === JSON.stringify(this.doc)) return; // 保存后残留，无恢复价值
                var savedAt = backup.savedAt ? new Date(backup.savedAt).toLocaleString() : '未知时间';
                var banner = document.createElement('div');
                banner.className = 'wb-backup-banner';
                var msg = document.createElement('span');
                msg.textContent = '检测到未保存的草稿备份（' + savedAt + '）';
                banner.appendChild(msg);
                var restore = document.createElement('button');
                restore.type = 'button'; restore.className = 'wb-btn wb-btn-primary wb-btn-sm';
                restore.textContent = '恢复备份';
                restore.addEventListener('click', function () {
                    self.doc = backup.doc;
                    self.draftVersion = backup.version || self.draftVersion;
                    self.selectedId = null;
                    self.saveState = 'dirty';
                    self.renderTree(); self.flushCanvas(); self.syncInspector(); self.renderUI();
                    banner.remove();
                });
                var discard = document.createElement('button');
                discard.type = 'button'; discard.className = 'wb-btn wb-btn-ghost wb-btn-sm';
                discard.textContent = '丢弃';
                discard.addEventListener('click', function () { self.clearBackup(); banner.remove(); });
                banner.appendChild(restore);
                banner.appendChild(discard);
                document.body.appendChild(banner);
            },

};
