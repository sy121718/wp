// editor-app/methods/app-panels.js — 应用业务面板：页面设置、全局主题与修订历史。
// 端点、评分服务与主题保存属于应用；通用检查器面板在 pkg/builder/editor。
import { meta, clone, wbColorPicker } from '../../editor/core.js';

// panelTrigger 触发一次服务端渲染的面板请求。
//
// 发送、CSRF、HTML/状态校验、失败区与迟到响应隔离全部在写基座
// （ui/write-base.js + 布局里的 data-write 表单）；这里只广播触发事件，
// 不再另写执行器 —— 否则同一页会出现两套 CSRF/失败语义。
function panelTrigger(id) {
    // 触发体是 body：布局里写成 `from:body`，在 document 上派发不会冒泡到子节点。
    (document.body || document).dispatchEvent(new CustomEvent(id, { bubbles: true }));
}

export const appPanelMethods = {
            // ---- 页面设置面板 HTMX 路径（docs/09 §3）----
            // 表单与评分均由服务端渲染；客户端只做「值变更 → 回写 doc.settings → 刷新画布/评分」。
            renderSettingsPanelHtmx() {
                var host = document.getElementById('wb-settings-body');
                if (!host) return;
                var self = this;
                // 成功监听必须挂在**存活的外壳**上：面板与「设置访问密码」是两个兄弟
                // 表单写同一个区域，挂在某一个表单上时，另一个表单成功就收不到事件
                // （密码哈希回填链因此断开）。write:success 从发起表单冒泡，按
                // data-write-builder 区分是谁成功。
                if (!host.__wbPanelBound) {
                    host.__wbPanelBound = true;
                    host.addEventListener('write:success', function (e) {
                        var owner = e.target;
                        // 一律读 id 属性，不用 `.id`：表单里有 name="id" 的控件时，
                        // 该控件会遮蔽表单自身的 id 属性（真实 Chrome 实测）。
                        var formID = owner && owner.getAttribute ? owner.getAttribute('id') || '' : '';
                        if (formID === 'wb-settings-form') {
                            self.bindSettingsHtmx(host);
                            self.refreshSeoScoreHtmx();
                            return;
                        }
                        // 「设置访问密码」只换同一块面板：回填新哈希后不必再请求评分。
                        if (formID === 'wb-settings-password-form') {
                            self.bindSettingsHtmx(host);
                            return;
                        }
                        if (formID === 'wb-seo-form') self.bindSettingsHtmx(host);
                    });
                }
                // 面板输入是整份编辑器文档：由 workbench-settings 提供对象，HTMX 发送。
                panelTrigger('wb:settings-panel');
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
                var form = document.getElementById('wb-settings-password-form');
                if (!form) return;
                var hidden = form.querySelector('[name=wb-access-password]');
                if (!hidden) {
                    hidden = document.createElement('input');
                    hidden.type = 'hidden';
                    hidden.name = 'wb-access-password';
                    form.appendChild(hidden);
                }
                hidden.value = pwd;
                panelTrigger('wb:settings-password');
            },
            refreshSeoScoreHtmx() {
                // 评分区是设置面板内的子区域：请求同样走写基座，失败只落本宿主的错误区。
                panelTrigger('wb:seo-panel');
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
            // 页面设置：表单与评分均由服务端渲染（renderSettingsPanelHtmx）。
            renderSettingsPanel() { this.renderSettingsPanelHtmx(); },


            // ---- 全局设置面板 HTMX 路径（docs/09 §3）----
            // 字段表与渲染在服务端（fragments/global_panel）；客户端只做取色器增强与保存。
            renderGlobalPanelHtmx() {
                var body = document.getElementById('wb-global-body');
                var content = document.getElementById('wb-global-content');
                if (!body || !content) return;
                var self = this;
                if (!meta.themeId) {
                    content.innerHTML = '<p class="wb-empty">当前页面未挂接主题。<br>到后台「主题管理」创建并激活主题后，这里可就地调整。</p>';
                    return;
                }
                // 主题设置是多层对象：由 workbench-global 提供对象，HTMX 发送并交换。
                // 保存表单在服务端片段里，每次交换都会换新节点：两个表单都只能靠
                // 委托到**存活的外壳**上识别（write:success 从发起表单冒泡）。
                if (!body.__wbSaveBound) {
                    body.__wbSaveBound = true;
                    body.addEventListener('write:success', function (e) {
                        // 一律用 getAttribute('id')：表单里有 name="id" 的隐藏控件时，
                        // 该控件会遮蔽表单的 `.id` 属性（真实 Chrome 实测为 INPUT 对象）。
                        var owner = e.target;
                        var formID = owner && owner.getAttribute ? owner.getAttribute('id') || '' : '';
                        if (formID === 'wb-global-load') self.bindGlobalHtmx(body);
                        else if (formID === 'wb-global-save') self.commitGlobalSave(owner, e.detail?.requestBody);
                    });
                }
                if (!content.firstChild) content.innerHTML = '<p class="wb-empty">加载中…</p>';
                panelTrigger('wb:global-panel');
            },
            bindGlobalHtmx(body) {
                var self = this;
                var t = meta.themeSettings || {};
                function getByPath(obj, path) {
                    return path.split('.').reduce(function (o, k) { return o == null ? undefined : o[k]; }, obj);
                }
                // 颜色槽：hidden input（提交名）+ 内联取色器（复用旧控件）。
                // hidden 只在首次增强时补一次：morph 会复用这些节点，重复补会让同名
                // 字段提交两遍（Gin 首值绑定下等同于值漂移）。
                Array.prototype.forEach.call(body.querySelectorAll('[data-wb-theme-color]'), function (slot) {
                    var name = slot.getAttribute('data-wb-theme-color');
                    var path = slot.getAttribute('data-wb-theme-path') || name;
                    var hidden = slot.querySelector('input[type=hidden][name="' + name + '"]');
                    if (!hidden) {
                        hidden = document.createElement('input');
                        hidden.type = 'hidden';
                        hidden.name = name;
                        hidden.value = getByPath(t, path) || '';
                        slot.appendChild(hidden);
                        slot.appendChild(wbColorPicker({
                            value: hidden.value,
                            onInput: function (v) { hidden.value = v; }
                        }));
                    }
                });
            },
            // 保存成功：把刚提交的字段合回本地主题状态并刷新画布。
            // 服务端的成功回执只说明「这一跳成功」，本地状态仍要按本次提交值更新。
            commitGlobalSave(form, requestBody) {
                if (!form || typeof requestBody !== 'string') return;
                var submitted = new URLSearchParams(requestBody);
                var current = new FormData(form);
                var t = clone(meta.themeSettings || {});
                var self = this;
                var pending = false;
                Array.prototype.forEach.call(form.querySelectorAll('[data-wb-theme-path]'), function (el) {
                    var name = el.getAttribute('data-wb-theme-color') || el.getAttribute('name');
                    var path = el.getAttribute('data-wb-theme-path');
                    if (!name || !path || !submitted.has(name)) return;
                    // 只合回发出的值；等待期间的新输入留在原控件里，不冒认为已保存。
                    self.setThemePath(t, path, submitted.get(name));
                    var colorText = el.querySelector('.wb-color-text');
                    if ((colorText ? colorText.value : current.get(name)) !== submitted.get(name)) pending = true;
                });
                meta.themeSettings = t;
                this.doc.settings = this.doc.settings || {};
                this.doc.settings.theme = clone(t);
                this.setNotice(pending ? '本次提交已保存；面板仍有未保存修改' :
                    '主题已保存，重新构建后应用到已发布页面');
                this.refreshCanvas();
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

            // 全局设置：字段表与渲染在服务端（renderGlobalPanelHtmx）。
            renderGlobalPanel() { this.renderGlobalPanelHtmx(); },

};
