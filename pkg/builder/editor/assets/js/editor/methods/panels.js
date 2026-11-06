// editor/methods/panels.js — 左侧面板切换与结构槽位面板（通用编辑器区域）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个编辑器实例。
import { controlLabel, getHost, normalizeAssetURL } from '../core.js';

export const panelsMethods = {
    showPanel(view) {
        this.view = view;
        this.setPreviewMode(false);
        var titles = { library: '组件库', edit: '元素属性', settings: '页面设置', global: '全局设置', history: '修订历史' };
        var heading = document.querySelector('.wb-sidebar-top > span');
        if (heading) heading.textContent = titles[view] || '组件库';
        document.querySelectorAll('.wb-document-menu[open]').forEach(function (menu) { menu.open = false; });
        var views = ['library', 'edit', 'settings', 'global', 'history'];
        views.forEach(function (v) {
            var el = document.getElementById('wb-panel-' + v);
            if (el) el.hidden = (v !== view);
        });
        if (view === 'library') this.showLibraryPanel();
        if (view === 'edit') this.showEditPanel();
        // 业务设置 / 主题 / 修订历史属于应用宿主面板；未装配时由宿主自行提供区域。
        if (view === 'settings' && this.renderSettingsPanel) this.renderSettingsPanel();
        if (view === 'global' && this.renderGlobalPanel) this.renderGlobalPanel();
        if (view === 'history' && this.loadHistory) this.loadHistory();
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
        if (title) title.textContent = node ? (node.name || controlLabel(String(node.type).replace('core.', ''))) : '组件';
        var subtitle = document.getElementById('wb-edit-subtitle');
        if (subtitle) subtitle.textContent = node ? node.type.replace('core.', '') + ' · ' + node.id : '';
        var selection = document.getElementById('wb-selection-label');
        if (selection) selection.textContent = node ? ' / ' + (node.name || node.type.replace('core.', '')) : '';
        this.syncInspector();
    },

    // renderSlotPanel 结构槽位面板（页眉 / 页脚）。站点结构属于应用事实：
    // 槽位数据、可编辑目标与跳转动作由宿主提供，工具只渲染通用面板结构。
    renderSlotPanel() {
        var slot = this.selectedSlot;
        var panel = document.getElementById('inspector-panel');
        if (!panel) return;
        var host = getHost();
        var facts = host.slotFacts ? host.slotFacts(this, slot) : null;
        if (!facts) {
            panel.innerHTML = '<p class="wb-empty">当前槽位不可编辑。</p>';
            return;
        }
        var title = document.getElementById('wb-edit-title');
        if (title) title.textContent = facts.label + ' · 站点结构';
        var html = '<div class="wb-slot-card">' +
            '<p class="wb-slot-note">' + facts.label + '来自<strong>站点结构</strong>，本页只是引用它：' +
            '这一份不在页面文档里，删除 / 拖动 / 改文字都改不到它。</p>';
        if (facts.ref) {
            html += '<p class="wb-slot-ref">当前内容：<strong></strong></p>';
            html += facts.degradable
                ? '<p class="wb-slot-warn">这份内容当前没能展开（引用可能已失效），页面上这个' + facts.label + '是空的。</p>'
                : '<a class="wb-btn wb-btn-primary" href="' + facts.editURL + '">编辑' + facts.editLabel + '</a>';
        } else {
            html += '<p class="wb-slot-warn">本页还没有绑定' + facts.label + '，到「全局设置 → 站点结构」里选一个块即可。</p>';
        }
        html += '</div>';
        panel.innerHTML = html;
        var refEl = panel.querySelector('.wb-slot-ref strong');
        if (refEl) refEl.textContent = facts.refName || facts.ref;
        panel.scrollTop = 0;
    },
    showLibrary() { this.showPanel('library'); },
    showEdit() { this.showPanel('edit'); },
};
