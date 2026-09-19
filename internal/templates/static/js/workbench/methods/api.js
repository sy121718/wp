// workbench/methods/api.js — API 接线（草稿保存 / 构建 / 发布）（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER,
} from '../core.js';

export const apiMethods = {
            // ---------------- API 接线（草稿保存 / 构建 / 发布） ----------------
            api(path, body, onDone) {
                var self = this;
                self.busy = true;
                self.renderUI();
                // saveBase='block'（全局块编辑）时走 /api/block/ 前缀，默认页面 /api/page/。
                fetch('/api/' + (meta.saveBase || 'page') + '/' + path, {
                    method: 'POST',
                    headers: csrfHeaders({ 'Content-Type': 'application/json' }),
                    body: JSON.stringify(body)
                }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
                  .then(function (res) {
                      self.busy = false;
                      self.renderUI();
                      if (!res.ok || (res.j.code && res.j.code >= 400)) {
                          self.saveState = 'error';
                          self.renderUI();
                          alert((res.j && res.j.message) || '请求失败');
                          return;
                      }
                      if (onDone) onDone(res.j.data || {});
                      self.renderUI();
                  })
                  .catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
            },
            // saveDraft 保存草稿（审计 EDT-017）。
            //
            // 保存端点与请求体键名全部来自 meta.target —— 后端注册表下发的编辑目标描述符。
            // 此前这里是 page / block / template 三段 if：接入一种新文档类型要在保存、预览、
            // 校验、历史四处各加一条分支，而分派散在 JS 里，漏改一处不会编译失败，
            // 只会在用户点保存时表现为「什么都没发生」（最难查的一类问题）。
            //
            // 注意 body 的键名由描述符给（page 用 draftDocument + expectedVersion + draftPath、
            // block 用 document 且额外带 name、template 用 draftDocument 无版本），
            // 前端不认识任何一种目标。
            saveDraft() {
                var self = this;
                var target = meta.target || null;
                if (!target || !target.save || !target.save.path) {
                    // 描述符缺失（旧缓存页面或后端未注册该类型）：退回手工页面保存，
                    // 而不是静默什么都不做。
                    this.api('draft/save', {
                        id: meta.pageId,
                        expectedVersion: this.draftVersion,
                        draftPath: meta.draftPath,
                        draftDocument: this.doc
                    }, function (data) {
                        self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                        self.saveState = 'saved';
                        self.clearBackup();
                        self.flushCanvas();
                    });
                    return;
                }
                var spec = target.saveBody || {};
                var body = {};
                body[spec.idKey || 'id'] = meta.pageId;
                body[spec.documentKey || 'draftDocument'] = this.doc;
                if (spec.versionKey) { body[spec.versionKey] = this.draftVersion; }
                if (spec.pathKey && meta.draftPath !== undefined) { body[spec.pathKey] = meta.draftPath; }
                if (spec.extras) {
                    for (var key in spec.extras) {
                        if (!Object.prototype.hasOwnProperty.call(spec.extras, key)) { continue; }
                        var from = spec.extras[key];
                        if (meta[from] !== undefined) { body[key] = meta[from]; }
                    }
                }
                self.busy = true; self.renderUI();
                // 双轨（迁移 282）：实例保存可能触发「转为独立文档」。服务端回 409 时
                // 先确认再重试 —— 判据（文档结构是否真的变了）只有服务端算得准，
                // 所以顺序是「先请求、再确认」，而不是一进编辑就弹窗打断浏览。
                function send(confirmDetach) {
                    var payload = body;
                    if (confirmDetach) {
                        payload = JSON.parse(JSON.stringify(body));
                        payload.confirmDetach = true;
                    }
                    return fetch(target.save.path, {
                        method: 'POST',
                        headers: csrfHeaders({ 'Content-Type': 'application/json' }),
                        body: JSON.stringify(payload)
                    }).then(function (r) {
                        // 服务端可能直接 303 回跳（「块保存后回菜单编辑器」那条链，PRG）：
                        // fetch 自动跟随重定向，跟随后的响应是整页 HTML 而不是 JSON ——
                        // 直接 r.json() 会把一次成功的保存判成失败。这里只取最终 URL，
                        // 由调用点跳转（跟随行为与 302 的语义一致，只是由前端落地）。
                        if (r.redirected) { return { redirected: true, url: r.url }; }
                        return r.json();
                    });
                }
                send(false).then(function (j) {
                    if (j && j.redirected) {
                        self.busy = false; self.saveState = 'saved';
                        window.location.href = j.url;
                        return;
                    }
                    // 用描述符判定实例目标（分派一律走 target，见上面的说明）。
                    if (j && j.code === 409 && target.type === 'instance') {
                        if (window.confirm(j.message || '这次改动会让本商品转为独立文档，继续？')) {
                            return send(true).then(function (j2) {
                                self.busy = false;
                                if (j2 && j2.redirected) {
                                    self.saveState = 'saved';
                                    window.location.href = j2.url;
                                    return;
                                }
                                if (j2 && j2.code && j2.code >= 400) {
                                    self.saveState = 'error'; self.renderUI();
                                    alert(j2.message || '保存失败');
                                    return;
                                }
                                self.afterSave((j2 && j2.data) || {});
                            });
                        }
                        self.busy = false; self.renderUI();
                        return;
                    }
                    self.busy = false;
                    if (j && j.code && j.code >= 400) {
                        self.saveState = 'error'; self.renderUI();
                        alert(j.message || '保存失败');
                        return;
                    }
                    self.afterSave((j && j.data) || {});
                }).catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
            },
            // afterSave 保存成功后的统一收尾（含双轨状态条即时切换）。
            afterSave(data) {
                this.draftVersion = data.draftVersion || (this.draftVersion + 1);
                this.saveState = 'saved';
                this.clearBackup();
                this.flushCanvas();
                if (data.renderMode && meta.renderMode && data.renderMode !== meta.renderMode) {
                    // 刚保存的这次改动已让商品转为独立文档：状态条即时切换，
                    // 免得用户以为还在跟随模板（下一次模板更新不再同步到这里）。
                    meta.renderMode = data.renderMode;
                    var badge = document.getElementById('wb-instance-mode');
                    if (badge) { badge.textContent = '独立文档（只影响这个商品）'; }
                }
            },
            publishFlow() {
                var self = this;
                if (meta.saveBase === 'block' || meta.saveBase === 'template') { this.saveDraft(); return; }
                // 先保存草稿 → Build（预期版本为服务端新版本）→ Publish。
                this.api('draft/save', {
                    id: meta.pageId,
                    expectedVersion: this.draftVersion,
                    draftPath: meta.draftPath,
                    draftDocument: this.doc
                }, function (data) {
                    self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                    self.api('build', { id: meta.pageId, expectedVersion: self.draftVersion }, function () {
                        self.api('publish', { id: meta.pageId }, function () {
                            self.saveState = 'saved';
                            self.clearBackup();
                        });
                    });
                });
            },
            buildAndPublish() {
                if (meta.saveBase === 'block' || meta.saveBase === 'template') { this.saveDraft(); return; }
                var self = this;
                // 用基座的确认框而不是原生 confirm：发布是不可逆的对外动作，
                // 自绘框能给出明确标题与按钮文案（原生框在 Linux 上是系统对话框，只有确定/取消）。
                WBUI.confirm('将保存当前草稿并构建发布到线上，确认？', function () { self.publishFlow(); },
                    { title: '发布到线上', ok: '保存并发布' });
            },

};
