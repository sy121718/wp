// 工作区交互只控制面板与画布状态，文档仍由既有节点方法维护。
export const workspaceMethods = {
    initWorkspace() {
        var self = this;
        document.querySelectorAll('[data-wb-icon]').forEach(function (el) {
            if (window.WPIcons) el.innerHTML = window.WPIcons.svg(el.dataset.wbIcon);
        });
        document.querySelectorAll('[data-wb-view]').forEach(function (button) {
            button.addEventListener('click', function () { self.showPanel(button.dataset.wbView); });
        });
        document.getElementById('wb-library-close').addEventListener('click', function () { self.setPreviewMode(true); });
        document.getElementById('wb-sidebar-restore').addEventListener('click', function () { self.setPreviewMode(false); });
        document.getElementById('wb-navigator-toggle').addEventListener('click', function () { self.toggleNavigator(!self.navigatorOpen); });
        document.getElementById('wb-navigator-close').addEventListener('click', function () { self.toggleNavigator(false); });
        var navigator = document.getElementById('wb-navigator');
        var fold = document.getElementById('wb-navigator-collapse');
        fold.addEventListener('click', function () {
            var folded = navigator.classList.toggle('is-folded');
            fold.textContent = folded ? '+' : '−';
            fold.setAttribute('aria-label', folded ? '展开结构树' : '折叠结构树');
        });
        // 标题栏可拖动；按钮及搜索不参与拖动，触控也使用相同路径。
        var head = navigator.querySelector('.wb-navigator-head');
        head.addEventListener('pointerdown', function (event) {
            if (event.target.closest('button') || event.button !== 0) return;
            var rect = navigator.getBoundingClientRect();
            var parent = navigator.parentElement.getBoundingClientRect();
            var start = { x: event.clientX, y: event.clientY, left: rect.left - parent.left, top: rect.top - parent.top };
            head.setPointerCapture(event.pointerId);
            head.onpointermove = function (move) {
                navigator.style.left = Math.max(0, Math.min(parent.width - rect.width, start.left + move.clientX - start.x)) + 'px';
                navigator.style.top = Math.max(0, Math.min(parent.height - 44, start.top + move.clientY - start.y)) + 'px';
                navigator.style.right = 'auto';
            };
            head.onpointerup = head.onpointercancel = function () { head.onpointermove = null; };
        });
    },
    toggleNavigator(open) {
        this.navigatorOpen = open;
        document.getElementById('wb-navigator').classList.toggle('is-hidden', !open);
        document.getElementById('wb-navigator-toggle').setAttribute('aria-pressed', String(open));
    },
    setPreviewMode(preview) {
        this.immersive = preview;
        document.getElementById('wb-main').classList.toggle('is-immersive', preview);
        document.getElementById('wb-sidebar-restore').hidden = !preview;
        this.syncCanvasState();
    },
    syncCanvasState() {
        var frame = document.getElementById('wb-canvas');
        if (!frame || !frame.contentWindow) return;
        var nodes = [];
        function collect(list, hidden, locked) {
            (list || []).forEach(function (node) {
                var hide = hidden || !!node.hidden;
                var lock = locked || !!node.locked;
                nodes.push({ id: node.id, name: node.name || node.type.replace('core.', ''), type: node.type, hidden: hide, locked: lock });
                collect(node.children, hide, lock);
            });
        }
        collect(this.doc.root);
        frame.contentWindow.postMessage({ type: 'wb-editor-state', nodes: nodes, selectedId: this.selectedId, preview: this.immersive }, window.location.origin);
    }
};
