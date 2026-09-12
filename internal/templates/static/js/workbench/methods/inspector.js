// workbench/methods/inspector.js — 检查器面板与事件接线（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
// 控件函数已按功能拆分到 methods/controls/*（base/color/corners/spacing/media/text/repeater/misc）。
import { csrfHeaders, morphHTML, wbLinkedPatch, wbOpenDropdownKeys, wbRestoreDropdowns } from '../core.js';
import { fillInspectorSlots, inlineSlots, renderInspectorExtras } from './controls/misc.js';
import { set } from './controls/base.js';
import { upgradeNativeSelects, renderFieldHints, refreshFieldHints } from './controls/selects.js';

export const inspectorMethods = {
            // 说明：面板/结构树/设置的 HTML 全部由服务端渲染（fragments/*），客户端只做
            // 「值回写 + 事件委托」。旧的前端渲染路径已删除，故不再有 htmx 开关。
            // fetchInspectorPanel 拉取服务端渲染的面板片段并填入面板（done 回调在片段就绪后执行）。
            fetchInspectorPanel(node, panel, done) {
                var self = this;
                // 请求序号：快速切换页签/节点时丢弃过期响应（否则后发先至会显示错页签内容）。
                var seq = (this._inspectorReq = (this._inspectorReq || 0) + 1);
                var body = new URLSearchParams();
                body.set('nodeId', node.id);
                body.set('document', JSON.stringify(this.doc));
                body.set('tab', this.tab || 'content');   // 内容/样式页签由服务端过滤分组
                fetch('/workbench/inspector', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: body.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    if (seq !== self._inspectorReq) return;   // 过期响应：丢弃
                    // morph 替换：未变节点（输入框/展开的分组）原地保留，滚动位置与焦点不复位。
                    morphHTML(panel, html);
                    if (done) {
                        // done 里的增强渲染（slot 填充/手写面板/事件绑定）异常必须单独兜底：
                        // 否则会被下方 catch 捕获并把面板覆盖成「加载失败」，掩盖真实原因。
                        try { done(); } catch (e) {
                            console.error('[workbench] 检查器增强渲染失败', e);
                            var notice = document.createElement('p');
                            notice.className = 'wb-empty';
                            notice.setAttribute('role', 'alert');
                            notice.textContent = '部分编辑控件加载失败，请刷新页面后重试。';
                            panel.prepend(notice);
                        }
                    }
                }).catch(function () {
                    if (seq !== self._inspectorReq) return;
                    morphHTML(panel, '<p class="wb-empty">面板加载失败</p>');
                });
            },
            bindInspectorHtmx() {
                var panel = document.getElementById('inspector-panel');
                if (!panel) return;
                var self = this;
                function setPath(path, value) {
                    var node = self.findNode(self.selectedId);
                    if (!node) return;
                    var parts = ('props.' + path).split('.');
                    var target = node;
                    for (var i = 0; i < parts.length - 1; i++) {
                        if (!target[parts[i]]) target[parts[i]] = {};
                        target = target[parts[i]];
                    }
                    target[parts[parts.length - 1]] = value;
                }
                function commit(el) {
                    var path = el.getAttribute('data-wb-path');
                    if (!path) return;
                    var kind = el.getAttribute('data-wb-kind') || 'text';
                    var value;
                    if (el.type === 'checkbox') { value = el.checked; }
                    else if (kind === 'number') { value = el.value === '' ? '' : Number(el.value); }
                    else if (kind === 'mediaList') { value = el.value.split('\n').map(function (s) { return s.trim(); }).filter(Boolean); }
                    else { value = el.value; }
                    self.snapshot();
                    setPath(path, value);
                    // 字段联动（core.js WB_FIELD_LINKS）：如 button 的 action 切换后把
                    // value 归一为新动作的合法形态，避免「切动作 → 预览编译失败」。
                    var linked = self.applyLinkedFields('props.' + path);
                    self.renderTree(); self.refreshCanvas(); self.renderUI();
                    // 联动改写了其它字段：值输入框与提示必须重渲染才能看到新值。
                    if (linked) self.syncInspector(); else self.refreshFieldHints();
                }
                // 覆盖式绑定：面板 DOM 每次整块替换，避免监听器累积。
                panel.onchange = function (e) {
                    var el = e.target && e.target.closest ? e.target.closest('[data-wb-path]') : null;
                    if (el && panel.contains(el)) commit(el);
                };
                panel.onclick = function (e) {
                    var btn = e.target && e.target.closest ? e.target.closest('[data-wb-media-pick]') : null;
                    if (!btn) return;
                    var key = btn.getAttribute('data-wb-media-pick');
                    var input = panel.querySelector('[data-wb-path="' + key + '"]');
                    self.openMediaPicker(function (url) {
                        if (input) input.value = url;
                        self.snapshot();
                        setPath(key, url);
                        self.renderTree(); self.refreshCanvas(); self.renderUI();
                    });
                };
            },
            // refreshFieldHints 提交后按模型刷新联动提示（值补全 → 提示立刻消失）。
            refreshFieldHints() {
                var panel = document.getElementById('inspector-panel');
                var node = this.findNode(this.selectedId);
                if (!panel || !node) return;
                refreshFieldHints({ panel: panel, node: node, self: this });
            },

            // applyLinkedFields 字段联动落地：声明在 core.js 的 WB_FIELD_LINKS（唯一实现），
            // 两条提交路径（本文件 bindInspectorHtmx.commit 与 controls/base.js commit）
            // 都在写入源字段后调用它，避免各写一份联动逻辑。返回 true 表示有改写。
            applyLinkedFields(changedPath) {
                var node = this.findNode(this.selectedId);
                if (!node) return false;
                var patch = wbLinkedPatch(node, changedPath);
                if (!patch) return false;
                var touched = false;
                patch.forEach(function (l) {
                    if (!l.changed) return;
                    set({ node: node }, l.path, l.value);
                    touched = true;
                });
                return touched;
            },

            // ---------------- 可视化属性检查器（docs/02-C3 schema 驱动） ----------------
            syncInspector() {
                // 注意：服务端片段就绪后的填充依赖下方闭包控件函数（函数声明会提升）——
                // htmx 路径要用同一批闭包控件函数填充 slot，提前 return 会导致面板控件为空。
                // 说明：控件函数已按功能拆分到 methods/controls/*，统一通过 ctx 传递面板/节点/实例。
                var panel = document.getElementById('inspector-panel');
                if (!panel) return;
                var node = this.findNode(this.selectedId);
                var self = this;
                if (!node) {
                    // 未选中节点：没有可 morph 的旧内容，保持整块清空 + 占位。
                    panel.innerHTML = '';
                    var empty = document.createElement('p');
                    empty.className = 'wb-empty';
                    empty.textContent = '在画布或大纲树中选择组件';
                    panel.appendChild(empty);
                    return;
                }
                // 选中节点时不再预清空面板：片段返回后由 fetchInspectorPanel 走 idiomorph
                // morphing，保留滚动位置 / 输入焦点 / 展开的分组。旧控件（含 Trix 编辑器）
                // 由 morph 移除节点触发 disconnectedCallback，实例仍随 DOM 回收。
                node.props = node.props || {};

                // ---- 服务端渲染路径（docs/09 §3）：片段 + 闭包内增强填充 ----
                // 简单字段由服务端渲染（fragments/inspector_panel）；增强字段（取色器/
                // 联动锁/媒体选择等）由服务端输出 slot，这里用既有控件函数就地填充。
                // 旧「schema → DOM」渲染路径已删除，此处是唯一入口。
                var ctx = { panel: panel, node: node, self: self };
                // 展开态搬运：morph 会整体替换客户端创建的 wb-dd 子树（服务端片段里没有它），
                // 不在这里记录，重渲染就等于把用户刚点开的下拉强行收起。
                var openDdKeys = wbOpenDropdownKeys(panel);
                self.fetchInspectorPanel(node, panel, function () {
                    fillInspectorSlots(ctx);
                    // 最小/最大高度并到一行（WP 式紧凑布局）。
                    inlineSlots(ctx, 'box.minHeight', 'box.maxHeight');
                    // 原生 select → 自定义下拉（与 wb-dd 统一交互，见 controls/selects.js）。
                    upgradeNativeSelects(ctx);
                    renderInspectorExtras(ctx);
                    // 联动后仍非法的字段给出就地提示（如 native 动作缺号码）。
                    renderFieldHints(ctx);
                    wbRestoreDropdowns(panel, openDdKeys);
                    self.bindInspectorHtmx();
                });
            },

};
