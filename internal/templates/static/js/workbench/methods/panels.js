// workbench/methods/panels.js — 面板切换 / 页面设置 / 全局设置（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER, morphHTML, wbBusy,
} from '../core.js';

export const panelsMethods = {
            // ---- 左侧面板双视图切换 ----
            // ---- 左侧面板统一切换（library/edit/settings/global/history） ----
            showPanel(view) {
                var views = ['library', 'edit', 'settings', 'global', 'history'];
                views.forEach(function (v) {
                    var el = document.getElementById('wb-panel-' + v);
                    if (el) el.hidden = (v !== view);
                });
                // 图标栏高亮：编辑视图归入「组件」入口。
                var railView = (view === 'edit') ? 'library' : view;
                document.querySelectorAll('.wb-rail-btn').forEach(function (b) {
                    b.classList.toggle('is-active', b.id === 'wb-rail-' + railView);
                });
                if (view === 'library') this.showLibraryPanel();
                if (view === 'edit') this.showEditPanel();
                if (view === 'settings') this.renderSettingsPanel();
                if (view === 'global') this.renderGlobalPanel();
                if (view === 'history') this.loadHistory();
            },
            showLibraryPanel() {
                var hint = document.getElementById('wb-library-hint');
                if (hint) {
                    var pending = this.pendingInsertTarget;
                    if (pending) {
                        var node = this.findNode(pending.id);
                        hint.textContent = '点击组件，插入到「' + (node ? (node.name || node.id) : pending.id) + '」' + (pending.placement === 'inside' ? '内部' : '之后');
                        hint.style.display = 'block';
                    } else {
                        hint.style.display = 'none';
                    }
                }
                var search = document.getElementById('wb-library-search');
                if (search) search.focus();
            },
            showEditPanel() {
                if (this.selectedSlot) { this.renderSlotPanel(); return; }
                var node = this.findNode(this.selectedId);
                var title = document.getElementById('wb-edit-title');
                if (title) title.textContent = node ? (controlLabel(String(node.type).replace('core.', '')) + ' · ' + (node.name || node.id)) : '组件';
                // 从槽位态回到普通节点：恢复被槽位面板隐藏的三个节点操作按钮。
                ['wb-edit-hide', 'wb-edit-lock', 'wb-edit-delete'].forEach(function (id) {
                    var el = document.getElementById(id);
                    if (el) el.hidden = false;
                });
                this.syncInspector();
            },

            // renderSlotPanel 结构槽位面板（页眉 / 页脚）。
            //
            // 槽位在页面文档里没有节点，能做的只有「去编辑它引用的那份全局块 / 结构模板」
            // —— 与 WP 的 header / footer 模板同一范式：页面编辑器默认就渲染页眉页脚，
            // 点进去改的是**全局那一份**，不是本页副本。本页副本这条路已经堵掉：
            // 画布里槽位不可拖、不可就地改文本，块面板里该块是禁用态。
            renderSlotPanel() {
                var slot = this.selectedSlot;
                var panel = document.getElementById('inspector-panel');
                var title = document.getElementById('wb-edit-title');
                var labels = { header: '页眉', footer: '页脚', announcement: '公告条' };
                var label = labels[slot.slot] || slot.slot;
                if (title) title.textContent = label + ' · 站点结构';
                // 隐藏 / 锁定 / 删除对槽位没有意义：它不是本页文档里的节点。
                ['wb-edit-hide', 'wb-edit-lock', 'wb-edit-delete'].forEach(function (id) {
                    var el = document.getElementById(id);
                    if (el) el.hidden = true;
                });
                if (!panel) return;
                var refName = '';
                (meta.blocks || []).forEach(function (b) { if (b.id === slot.ref) refName = b.name; });
                var isTemplate = slot.refKind === 'template';
                var editURL = (isTemplate ? '/workbench?template=' : '/workbench?block=') +
                    encodeURIComponent(slot.ref) +
                    '&returnUrl=' + encodeURIComponent(location.pathname + location.search);
                var html = '<div class="wb-slot-card">' +
                    '<p class="wb-slot-note">' + label + '来自<strong>站点结构</strong>，本页只是引用它：' +
                    '这一份不在页面文档里，删除 / 拖动 / 改文字都改不到它。</p>';
                if (slot.ref) {
                    html += '<p class="wb-slot-ref">当前内容：<strong></strong></p>';
                    html += slot.degradable
                        ? '<p class="wb-slot-warn">这份内容当前没能展开（引用可能已失效），页面上这个' + label + '是空的。</p>'
                        : '<a class="wb-btn wb-btn-primary" href="' + editURL + '">编辑' + (isTemplate ? '结构模板' : '全局块') + '</a>';
                } else {
                    html += '<p class="wb-slot-warn">本页还没有绑定' + label + '，到「全局设置 → 站点结构」里选一个块即可。</p>';
                }
                html += '</div>';
                panel.innerHTML = html;
                // 块名用 textContent 写入（块名是作者输入，不能拼进 HTML）。
                var refEl = panel.querySelector('.wb-slot-ref strong');
                if (refEl) refEl.textContent = refName || slot.ref;
                panel.scrollTop = 0;
            },
            showLibrary() { this.showPanel('library'); },
            showEdit() { this.showPanel('edit'); },

            // ---- 页面设置面板 HTMX 路径（docs/09 §3）----
            // 表单与评分均由服务端渲染；客户端只做「值变更 → 回写 doc.settings → 刷新画布/评分」。
            renderSettingsPanelHtmx() {
                var body = document.getElementById('wb-settings-body');
                if (!body) return;
                var self = this;
                var form = new URLSearchParams();
                form.append('document', JSON.stringify(this.doc));
                fetch('/workbench/settings', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: form.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    // morph 替换：设置表单原地保留滚动位置与输入焦点（docs/06-C §四）。
                    morphHTML(body, html);
                    self.bindSettingsHtmx(body);
                    self.refreshSeoScoreHtmx();
                }).catch(function () {
                    morphHTML(body, '<p class="wb-empty">设置面板加载失败</p>');
                });
            },
            // 访问密码重设（PIPE-6 AccessGuard）：明文只在这一跳里出现，服务端 bcrypt
            // 后借 data-wb-apply 的隐藏字段回填哈希 —— 明文绝不写进文档
            // （文档会进 page_revisions 与历史版本，密码不该出现在那里）。
            applyAccessPassword() {
                var body = document.getElementById('wb-settings-body');
                if (!body) return;
                var input = body.querySelector('[name=wb-access-password]');
                if (!input) return;
                var pwd = input.value || '';
                if (!pwd) return;
                var self = this;
                var form = new URLSearchParams();
                form.append('document', JSON.stringify(this.doc));
                form.append('access-password', pwd);
                fetch('/workbench/settings', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: form.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    morphHTML(body, html);
                    self.bindSettingsHtmx(body);
                    self.refreshSeoScoreHtmx();
                }).catch(function () {
                    morphHTML(body, '<p class="wb-empty">密码设置失败</p>');
                });
            },
            refreshSeoScoreHtmx() {
                var box = document.getElementById('wb-seo-score');
                if (!box) return;
                var form = new URLSearchParams();
                form.append('document', JSON.stringify(this.doc));
                form.append('url', (meta.draftPath || ''));
                fetch('/workbench/seo-score-panel', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: form.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    morphHTML(box, html);
                }).catch(function () {
                    morphHTML(box, '<p class="wb-empty">评分加载失败</p>');
                });
            },
            bindSettingsHtmx(body) {
                var self = this;
                function setByPath(path, value) {
                    var parts = path.split('.');
                    var target = self.doc;
                    for (var i = 0; i < parts.length - 1; i++) {
                        if (!target[parts[i]]) target[parts[i]] = {};
                        target = target[parts[i]];
                    }
                    target[parts[parts.length - 1]] = value;
                    self.saveState = 'dirty';
                    self.refreshCanvas();
                }
                // 覆盖式绑定：面板 DOM 每次整块替换，避免监听器累积。
                body.onchange = function (e) {
                    var el = e.target && e.target.closest ? e.target.closest('[data-wb-setting]') : null;
                    if (!el || el.tagName === 'BUTTON') return;
                    var path = el.getAttribute('data-wb-setting');
                    var value = el.getAttribute('data-wb-kind') === 'list'
                        ? el.value.split(/\s+/).filter(Boolean) : el.value;
                    self.snapshot();
                    setByPath(path, value);
                    if (path.indexOf('settings.seo.') === 0) self.refreshSeoScoreHtmx();
                };
                body.onclick = function (e) {
                    // 访问密码：明文不回写文档，交给服务端 bcrypt（PIPE-6）。
                    var setPwd = e.target && e.target.closest ? e.target.closest('[data-wb-access-set]') : null;
                    if (setPwd) { self.applyAccessPassword(); return; }
                    var btn = e.target && e.target.closest ? e.target.closest('button[data-wb-setting]') : null;
                    if (!btn) return;
                    var path = btn.getAttribute('data-wb-setting');
                    self.snapshot();
                    setByPath(path, btn.getAttribute('data-wb-value'));
                    var seg = btn.parentNode;
                    Array.prototype.forEach.call(seg.querySelectorAll('.wb-seg-btn'), function (x) {
                        x.classList.toggle('is-active', x === btn);
                    });
                    if (path.indexOf('settings.seo.') === 0) self.refreshSeoScoreHtmx();
                };
                // 服务端就地下发的值（目前只有刚设置的访问密码哈希）：它不是用户输入，
                // 不会触发 change —— 面板整块替换后在这里应用一次。
                // 值没变就不写回，避免每次打开面板都把文档标脏、顺便刷一次画布。
                Array.prototype.forEach.call(body.querySelectorAll('[data-wb-setting][data-wb-apply]'), function (el) {
                    var path = el.getAttribute('data-wb-setting');
                    var value = el.getAttribute('data-wb-kind') === 'list'
                        ? el.value.split(/\s+/).filter(Boolean) : el.value;
                    var cur = self.doc;
                    var parts = path.split('.');
                    for (var i = 0; i < parts.length - 1 && cur; i++) { cur = cur[parts[i]]; }
                    if (!cur || cur[parts[parts.length - 1]] === value) return;
                    self.snapshot();
                    setByPath(path, value);
                });
                // 评分建议跳转（data-wb-jump）：settings.seo.x → 聚焦对应输入；node:type → 选中首个该类型组件。
                var box = document.getElementById('wb-seo-score');
                if (!box) return;
                box.onclick = function (e) {
                    var li = e.target && e.target.closest ? e.target.closest('[data-wb-jump]') : null;
                    if (!li) return;
                    var target = li.getAttribute('data-wb-jump');
                    if (target.indexOf('settings.seo.') === 0) {
                        var map = {
                            title: 'SEO 标题', description: 'SEO 描述', focusKeyword: '焦点关键词',
                            secondaryKeywords: '次级关键词', canonical: 'Canonical 链接', schemaType: '结构化数据'
                        };
                        var label = map[target.replace('settings.seo.', '')];
                        var wrap = Array.prototype.slice.call(body.querySelectorAll('.wb-field')).filter(function (w) {
                            var l = w.querySelector('label');
                            return l && l.textContent === label;
                        })[0];
                        if (wrap) {
                            var inp = wrap.querySelector('input, textarea');
                            if (inp) { inp.focus(); inp.scrollIntoView({ block: 'center', behavior: 'smooth' }); }
                        }
                        return;
                    }
                    if (target.indexOf('node:') === 0) {
                        var found = self.findNodeByTypeHtmx(self.doc.root, target.slice(5));
                        if (found) self.select(found.id);
                    }
                };
            },
            // findNodeByTypeHtmx 深度优先查找首个指定组件类型（评分建议跳转用）。
            findNodeByTypeHtmx(nodes, type) {
                var want = 'core.' + type;
                var found = null;
                var walk = function (list) {
                    (list || []).forEach(function (n) {
                        if (found) return;
                        if (n.type === want) { found = n; return; }
                        walk(n.children);
                    });
                };
                walk(nodes);
                return found;
            },
            // 页面设置：表单与评分均由服务端渲染（renderSettingsPanelHtmx），旧 DOM 拼装路径已删除。
            renderSettingsPanel() { this.renderSettingsPanelHtmx(); },


            // ---- 全局设置面板 HTMX 路径（docs/09 §3）----
            // 字段表与渲染在服务端（fragments/global_panel）；客户端只做取色器增强与保存。
            renderGlobalPanelHtmx() {
                var body = document.getElementById('wb-global-body');
                if (!body) return;
                var self = this;
                if (!meta.themeId) {
                    body.innerHTML = '<p class="wb-empty">当前页面未挂接主题。<br>到后台「主题管理」创建并激活主题后，这里可就地调整。</p>';
                    return;
                }
                if (!body.firstChild) body.innerHTML = '<p class="wb-empty">加载中…</p>'; // 已有内容时保留旧 DOM：容器高度不归零，滚动位置不会被钳回顶部
                var form = new URLSearchParams();
                form.append('themeId', meta.themeId);
                form.append('settings', JSON.stringify(meta.themeSettings || {}));
                fetch('/workbench/global', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: form.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    // morph 替换：全局设置表单原地保留滚动位置与输入焦点（docs/06-C §四）。
                    morphHTML(body, html);
                    self.bindGlobalHtmx(body);
                }).catch(function () {
                    morphHTML(body, '<p class="wb-empty">全局设置加载失败</p>');
                });
            },
            bindGlobalHtmx(body) {
                var self = this;
                var t = meta.themeSettings || {};
                function getByPath(obj, path) {
                    return path.split('.').reduce(function (o, k) { return o == null ? undefined : o[k]; }, obj);
                }
                // 颜色槽：hidden input（提交名）+ 内联取色器（复用旧控件）。
                Array.prototype.forEach.call(body.querySelectorAll('[data-wb-theme-color]'), function (slot) {
                    var name = slot.getAttribute('data-wb-theme-color');
                    var path = slot.getAttribute('data-wb-theme-path') || name;
                    var hidden = document.createElement('input');
                    hidden.type = 'hidden';
                    hidden.name = name;
                    hidden.value = getByPath(t, path) || '';
                    slot.appendChild(hidden);
                    slot.appendChild(wbColorPicker({
                        value: hidden.value,
                        onInput: function (v) { hidden.value = v; }
                    }));
                });
                var saveBtn = document.createElement('button');
                saveBtn.type = 'button';
                saveBtn.className = 'btn btn-primary';
                saveBtn.textContent = '保存并应用到全部页面';
                saveBtn.addEventListener('click', function () {
                    var form = new URLSearchParams();
                    form.append('id', meta.themeId);
                    Array.prototype.forEach.call(body.querySelectorAll('[name]'), function (el) {
                        form.append(el.name, (el.value || '').trim());
                    });
                    form.append('headerBlockId', t.headerBlockId || '');
                    form.append('footerBlockId', t.footerBlockId || '');
                    var done = wbBusy(saveBtn, { label: '保存中…' });
                    // redirect: 'manual' 是这条判断的关键 —— 服务端**成功走 303**（PRG 跳回设置页）、
                    // **失败走 200 + 就地重渲整页**（52 字段的长表单不能跳页，否则用户填的全丢）。
                    // 让 fetch 自动跟随重定向的话，两种结果在 r.ok 眼里都是 true：失败会被报成
                    // 「已保存」，而用户以为改好了、实际一个值都没落库。不跟随之后：
                    //   成功 → opaqueredirect（status 0、body 不可读）；
                    //   失败 → 200 + 一整页 HTML（里面的 .badge-warning 就是原因）。
                    fetch('/admin/themes/settings/save', {
                        method: 'POST',
                        headers: csrfHeaders({}),
                        body: form,
                        redirect: 'manual'
                    })
                        .then(function (r) {
                            done();
                            if (r.type !== 'opaqueredirect' && r.status !== 303) {
                                return self.explainSaveFailure(r);
                            }
                            // 本地缓存按提交键名回写（与后端 SaveThemeSettings 的约定一致）。
                            Array.prototype.forEach.call(body.querySelectorAll('[name]'), function (el) {
                                self.setThemePath(t, el.name, (el.value || '').trim());
                            });
                            meta.themeSettings = t;
                            alert('已保存，主题将合入全部页面；页面需重新构建后生效。');
                        })
                        .catch(function () {
                            done();
                            alert('保存失败，请重试');
                        });
                });
                body.appendChild(saveBtn);
            },
            // explainSaveFailure 保存失败时把服务端的归口提示读出来再弹。
            //
            // 失败响应是**一整页 HTML**（服务端就地重渲），原因就写在那句 `.badge-warning` 里
            //（值不合法 / 校验不过 / 整站刷新失败）—— 把原文透出来比一句「请重试」有用得多，
            // 否则用户只会反复点同一个按钮。取不到就往「主题管理 → 设置」指路。
            explainSaveFailure(resp) {
                return resp.text().then(function (html) {
                    var text = '';
                    try {
                        var doc = new DOMParser().parseFromString(html, 'text/html');
                        var el = doc.querySelector('.badge-warning');
                        text = (el && el.textContent) ? el.textContent.trim() : '';
                    } catch (e) {
                        text = '';
                    }
                    alert('保存失败：' + (text || '服务端拒绝了这次保存，请到「主题管理 → 设置」查看详细提示。'));
                });
            },
            setThemePath(obj, path, value) {
                var parts = path.split('.');
                var o = obj;
                for (var i = 0; i < parts.length - 1; i++) {
                    if (!o[parts[i]]) o[parts[i]] = {};
                    o = o[parts[i]];
                }
                o[parts[parts.length - 1]] = value;
            },

            // 全局设置：字段表与渲染在服务端（renderGlobalPanelHtmx），旧 DOM 拼装路径已删除。
            renderGlobalPanel() { this.renderGlobalPanelHtmx(); },

};
