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
            saveDraft() {
                var self = this;
                // 内容模板：保存走 contenttemplate API（严格校验，产生新版本）。
                if (meta.saveBase === 'template') {
                    self.busy = true; self.renderUI();
                    fetch('/api/contenttemplate/update', {
                        method: 'POST',
                        headers: csrfHeaders({ 'Content-Type': 'application/json' }),
                        body: JSON.stringify({ id: meta.pageId, draftDocument: this.doc })
                    }).then(function (r) { return r.json(); })
                      .then(function (j) {
                          self.busy = false;
                          if (j.code && j.code >= 400) {
                              self.saveState = 'error'; self.renderUI();
                              alert(j.message || '保存失败');
                              return;
                          }
                          var data = j.data || {};
                          self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                          self.saveState = 'saved';
                          self.clearBackup();
                          self.flushCanvas();
                      })
                      .catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
                    return;
                }
                // 全局块编辑：保存到 dashboard 编排端点（保存后自动传播 stale）。
                if (meta.saveBase === 'block') {
                    self.busy = true; self.renderUI();
                    fetch('/admin/blocks/save-content', {
                        method: 'POST',
                        headers: csrfHeaders({ 'Content-Type': 'application/json' }),
                        body: JSON.stringify({ id: meta.pageId, name: meta.blockName, document: this.doc })
                    }).then(function (r) { return r.json(); })
                      .then(function (j) {
                          self.busy = false;
                          if (j.code && j.code >= 400) {
                              self.saveState = 'error'; self.renderUI();
                              alert(j.message || '保存失败');
                              return;
                          }
                          self.saveState = 'saved';
                          self.clearBackup();
                          self.flushCanvas();
                      })
                      .catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
                    return;
                }
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
