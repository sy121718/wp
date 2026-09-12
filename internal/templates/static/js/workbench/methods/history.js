// workbench/methods/history.js — 修订历史面板（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER, morphHTML, wbBusy,
} from '../core.js';

export const historyMethods = {
            // ---- 修订历史面板：草稿快照列表 + 一键恢复 ----
            // ---- 修订历史 HTMX 路径（docs/09 §3）----
            // 列表由服务端渲染（fragments/history_list）；恢复动作服务端覆盖保存后整页刷新。
            loadHistoryHtmx() {
                var body = document.getElementById('wb-history-body');
                if (!body) return;
                var self = this;
                if (!body.firstChild) body.innerHTML = '<p class="wb-empty">加载中…</p>'; // 已有内容时保留旧 DOM：容器高度不归零，滚动位置不会被钳回顶部
                var form = new URLSearchParams();
                form.append('pageId', meta.pageId || '');
                fetch('/workbench/history', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: form.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    // morph 替换：修订列表原地更新，滚动位置不跳回顶部（docs/06-C §四）。
                    morphHTML(body, html);
                    self.bindHistoryHtmx(body);
                }).catch(function () {
                    morphHTML(body, '<p class="wb-empty">加载失败</p>');
                });
            },
            bindHistoryHtmx(body) {
                var self = this;
                body.onclick = function (e) {
                    var btn = e.target && e.target.closest ? e.target.closest('[data-wb-restore]') : null;
                    if (!btn) return;
                    var version = btn.getAttribute('data-wb-restore');
                    if (!confirm('恢复到 v' + version + '？当前草稿将覆盖保存为新修订。')) return;
                    var form = new URLSearchParams();
                    form.append('pageId', meta.pageId || '');
                    form.append('version', version);
                    var done = wbBusy(btn);
                    fetch('/workbench/history/restore', {
                        method: 'POST',
                        credentials: 'same-origin',
                        headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                        body: form.toString()
                    }).then(function (r) { return r.json(); }).then(function (j) {
                        if (!j || j.code !== 200) {
                            done();
                            alert((j && j.message) || '恢复失败');
                            return;
                        }
                        // 恢复即覆盖草稿：整页刷新以重新注入文档与版本号（最可靠）。
                        window.location.reload();
                    }).catch(function () {
                        done();
                        alert('恢复失败');
                    });
                };
            },

            // 修订历史：列表与恢复均由服务端渲染（loadHistoryHtmx），旧 DOM 拼装路径已删除。
            loadHistory() { this.loadHistoryHtmx(); },

            markTreeSelection() {
                var hitEl = null;
                document.querySelectorAll('#wb-tree .wb-node').forEach(function (el) {
                    var hit = el.dataset.id === window.__wb.selectedId;
                    el.classList.toggle('is-selected', hit);
                    if (hit) hitEl = el;
                });
                // 画布选中后把结构树滚到该节点（对齐 WP/Elementor：选中即定位）。
                if (hitEl) {
                    var scroller = hitEl.closest('.wb-tree-scroll') || document.getElementById('wb-tree');
                    if (scroller) {
                        var er = hitEl.getBoundingClientRect();
                        var sr = scroller.getBoundingClientRect();
                        if (er.top < sr.top || er.bottom > sr.bottom) {
                            scroller.scrollTop += (er.top - sr.top) - (sr.height - er.height) / 2;
                        }
                    }
                }
            },

            // 大纲树：HTML 由服务端渲染（renderTreeHtmx → /workbench/outline），
            // 旧递归建 DOM 路径已删除（事件委托在 bindTreeHtmx）。
            renderTree() {
                var rootUl = document.getElementById('wb-tree');
                if (!rootUl) return;
                this.renderTreeHtmx(rootUl);
            },


};