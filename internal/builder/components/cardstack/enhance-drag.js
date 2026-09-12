    // ---------- 卡片堆叠：拖拽旋转 360°（指针 + 左右方向键） ----------
    // 只改写一个 CSS 变量（--sky-cardstack-rot），几何全部由构建期生成的静态 CSS 负责；
    // 没有增强脚本时该变量恒为 0deg，卡片静态成环，功能不缺失（路径 C 的降级要求）。
    function initCardStacks() {
        var roots = document.querySelectorAll('[data-cardstack-drag]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            var track = root.querySelector('[data-cardstack-track]');
            if (!track) return;
            var rot = 0;
            var drag = null;
            var moved = false;
            var KEY_STEP = 15;   // 方向键每步 15°
            var PER_PX = 0.5;    // 指针每像素 0.5°：转满一圈约 720px 行程
            function apply() { track.style.setProperty('--sky-cardstack-rot', rot.toFixed(2) + 'deg'); }
            function down(x) { drag = { x: x, rot: rot }; moved = false; root.classList.add('is-dragging'); }
            function move(x) {
                if (!drag) return;
                var dx = x - drag.x;
                if (Math.abs(dx) > 5) moved = true;
                rot = drag.rot + dx * PER_PX;
                apply();
            }
            function up() { drag = null; root.classList.remove('is-dragging'); }

            root.addEventListener('pointerdown', function (e) {
                if (e.button && e.button !== 0) return;   // 只响应主键
                down(e.clientX);
                if (root.setPointerCapture) root.setPointerCapture(e.pointerId);
                // 不用 preventDefault：卡片是 label，点击放大必须照旧可用。
            });
            root.addEventListener('pointermove', function (e) { move(e.clientX); });
            root.addEventListener('pointerup', up);
            root.addEventListener('pointercancel', up);
            root.addEventListener('lostpointercapture', up);

            // 拖动结束若落在卡片上会触发一次 click → 误放大；位移超过阈值时吞掉这次点击。
            root.addEventListener('click', function (e) {
                if (!moved) return;
                moved = false;
                e.preventDefault();
                e.stopPropagation();
            }, true);

            // 键盘等价入口：左右方向键步进旋转（容器 tabindex=0）。
            root.addEventListener('keydown', function (e) {
                if (e.key === 'ArrowLeft') { rot -= KEY_STEP; apply(); e.preventDefault(); }
                else if (e.key === 'ArrowRight') { rot += KEY_STEP; apply(); e.preventDefault(); }
            });
        });
    }

