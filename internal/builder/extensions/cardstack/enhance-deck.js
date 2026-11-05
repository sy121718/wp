    // ---------- 卡片堆叠轮播：把选中的卡切到主位（滑动 / 点击 / 方向键） ----------
    // 只改写每张卡的两个 CSS 变量（--sky-deck-off / --sky-deck-abs），位移、倾斜、缩放、
    // 层级的关系全部留在构建期生成的静态 CSS 里；脚本端不碰任何几何数值。
    function initCardDecks() {
        var roots = document.querySelectorAll('[data-cardstack-deck]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            var cards = [].slice.call(root.querySelectorAll('.sky-cardstack-card'));
            if (cards.length < 2) return;
            var total = cards.length;
            var active = parseInt(root.getAttribute('data-cardstack-deck'), 10) || 0;
            if (active < 0 || active >= total) active = Math.floor(total / 2);
            var loop = root.getAttribute('data-cardstack-loop') === '1';
            // 纵向切换：拖动看 Y 轴、方向键用上下键（其余逻辑与横向完全共用）。
            var axisY = root.getAttribute('data-cardstack-deck-axis') === 'y';
            var drag = null;
            var moved = false;
            var SWIPE_PX = 70;   // 拖动超过这个距离才算一次切换
            function axisOf(e) { return axisY ? e.clientY : e.clientX; }

            function apply() {
                // 循环模式下偏移要取「最短方向」：否则最后一↔第一之间会绕半圈，
                // 卡片横穿整排飞过去。half 取半圈，取模后落在 [−half, half) 内。
                var half = total / 2;
                cards.forEach(function (c, i) {
                    var off = i - active;
                    if (loop && total > 1) {
                        off = ((off + half) % total + total) % total - half;
                    }
                    c.style.setProperty('--sky-deck-off', String(off));
                    c.style.setProperty('--sky-deck-abs', String(Math.abs(off)));
                    c.classList.toggle('is-active', off === 0);
                });
            }
            function go(i) {
                var next;
                if (loop) {
                    next = ((i % total) + total) % total;   // 越界即绕回另一端
                } else {
                    next = Math.max(0, Math.min(i, total - 1));
                }
                if (next === active) return;
                active = next;
                apply();
            }

            root.addEventListener('pointerdown', function (e) {
                if (e.button && e.button !== 0) return;
                drag = { x: axisOf(e), dx: 0 };
                moved = false;
                if (root.setPointerCapture) root.setPointerCapture(e.pointerId);
            });
            root.addEventListener('pointermove', function (e) {
                if (!drag) return;
                drag.dx = axisOf(e) - drag.x;
                if (Math.abs(drag.dx) > 8) {
                    moved = true;
                    root.classList.add('is-dragging');
                }
            });
            function endDrag() {
                if (!drag) return;
                var dx = drag.dx;
                drag = null;
                root.classList.remove('is-dragging');
                // 卡片向滑动方向的反向滑走 = 看下一张（与触摸惯性一致）：
                // 横向左滑、纵向上滑都前进。
                if (dx <= -SWIPE_PX) go(active + 1);
                else if (dx >= SWIPE_PX) go(active - 1);
            }
            root.addEventListener('pointerup', endDrag);
            root.addEventListener('pointercancel', endDrag);
            root.addEventListener('lostpointercapture', endDrag);

            // 捕获阶段统一处理点击：拖动产生的补发点击吞掉；点侧卡切主位（阻止放大），
            // 点主卡放行（主卡点击才进入放大）。
            var clickNext = root.getAttribute('data-deck-click') === 'next';
            root.addEventListener('click', function (e) {
                if (moved) {
                    moved = false;
                    e.preventDefault();
                    e.stopPropagation();
                    return;
                }
                if (clickNext) {
                    // 「看书」语义：点哪儿都往后翻一页（点侧卡也是下一页，不是切到那张）
                    e.preventDefault();
                    e.stopPropagation();
                    go(active + 1);
                    return;
                }
                var el = e.target;
                var card = el && el.closest ? el.closest('.sky-cardstack-card') : null;
                var idx = card ? cards.indexOf(card) : -1;
                if (idx >= 0 && idx !== active) {
                    e.preventDefault();
                    e.stopPropagation();
                    go(idx);
                }
            }, true);

            // 翻页按钮：与方向键等价。按钮不是卡片，所以下面那个 click 捕获处理器
            // 不会拦它（closest('.sky-cardstack-card') 为 null → idx = -1）。
            var prevBtn = root.querySelector('[data-cardstack-prev]');
            var nextBtn = root.querySelector('[data-cardstack-next]');
            if (prevBtn) prevBtn.addEventListener('click', function () { go(active - 1); });
            if (nextBtn) nextBtn.addEventListener('click', function () { go(active + 1); });

            var prevKey = axisY ? 'ArrowUp' : 'ArrowLeft';
            var nextKey = axisY ? 'ArrowDown' : 'ArrowRight';
            root.addEventListener('keydown', function (e) {
                if (e.key === prevKey) { go(active - 1); e.preventDefault(); }
                else if (e.key === nextKey) { go(active + 1); e.preventDefault(); }
            });

            // 滚轮 / 触摸板：累计位移过阈值切一张，随后短暂冷却。
            // 触摸板的 wheel 事件又密又碎（每次几 px），鼠标滚轮一次就是 100+，
            // 所以必须累积；冷却用来防止一次滑动连翻好几张。
            //
            // 关键：preventDefault 只在「确实切了」时才调 —— 非循环模式下滚到头就放行，
            // 页面照常滚动，不会把用户困在组件里。监听器必须 passive: false，
            // 否则浏览器忽略 preventDefault（默认 passive）。
            var WHEEL_STEP = 50;
            var WHEEL_COOLDOWN = 380;
            var acc = 0;
            var lockUntil = 0;
            root.addEventListener('wheel', function (e) {
                var d = e.deltaY !== 0 ? e.deltaY : e.deltaX;
                if (!d) return;
                var now = Date.now();
                if (now < lockUntil) {
                    // 冷却期内继续吞掉：本轮滑动已经在切了，别让它顺手再翻一张。
                    acc = 0;
                    e.preventDefault();
                    return;
                }
                acc += d;
                if (Math.abs(acc) < WHEEL_STEP) {
                    e.preventDefault();
                    return;
                }
                var dir = acc > 0 ? 1 : -1;
                acc = 0;
                var next = active + dir;
                if (!loop && (next < 0 || next >= total)) return;   // 到头了：放行给页面
                go(next);
                lockUntil = now + WHEEL_COOLDOWN;
                e.preventDefault();
            }, { passive: false });

            apply();
        });
    }

