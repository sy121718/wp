/**
 * sky-enhance.js — 构建产物客户端增强（纯客户端交互，静态站运行）。
 * 零依赖 IIFE；按 data-* 属性按需初始化，无交互组件时静默跳过。
 * 覆盖：轮播（箭头/自动播放/循环/圆点高亮同步）、计数器（仅小数位模式——
 * 整数模式已由 @property + counter() 零 JS 实现）、倒计时（CSS 无时钟）。
 * 手风琴严格单开已改 <details name> 原生互斥；圆点导航已改构建期锚点；
 * tabs（radio hack）与基础 accordion（details）原生零 JS。
 */
(function () {
    'use strict';
    function onReady(fn) {
        if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
        else fn();
    }

    // ---------- 计数器：视口进入时从 start 递增到 end ----------
    function initCounters() {
        var els = document.querySelectorAll('[data-counter]');
        if (!els.length) return;
        els.forEach(function (el) {
            var start = parseFloat(el.dataset.start || 0);
            var end = parseFloat(el.dataset.end || 0);
            var decimals = parseInt(el.dataset.decimals || 0, 10);
            var duration = parseFloat(el.dataset.duration || 2) * 1000;
            var value = el.querySelector('.sky-counter-value');
            if (!value) return;
            var done = false;
            function animate() {
                var t0 = null;
                function step(ts) {
                    if (!t0) t0 = ts;
                    var p = Math.min((ts - t0) / duration, 1);
                    // easeOutCubic。
                    var eased = 1 - Math.pow(1 - p, 3);
                    value.textContent = (start + (end - start) * eased).toFixed(decimals);
                    if (p < 1) requestAnimationFrame(step);
                }
                requestAnimationFrame(step);
            }
            if ('IntersectionObserver' in window) {
                var io = new IntersectionObserver(function (entries) {
                    entries.forEach(function (e) {
                        if (e.isIntersecting && !done) { done = true; animate(); io.disconnect(); }
                    });
                }, { threshold: 0.3 });
                io.observe(el);
            } else {
                animate();
            }
        });
    }

    // ---------- 轮播：箭头 / 圆点 / 自动播放 / 循环 ----------
    function initSliders() {
        var roots = document.querySelectorAll('[data-slider]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            var track = root.querySelector('[data-track]');
            if (!track) return;
            var slides = root.querySelectorAll('.sky-slide');
            if (!slides.length) return;
            var idx = 0;
            var total = slides.length;
            function slideWidth() { return slides[0] ? slides[0].offsetWidth : 0; }
            function go(i) {
                idx = Math.max(0, Math.min(i, total - 1));
                track.scrollTo({ left: slideWidth() * idx, behavior: 'smooth' });
                updateDots();
            }
            function next() { go(idx + 1); }
            function prev() { go(idx - 1); }
            // 圆点。
            // 圆点由构建期生成 <a> 锚点（点击原生滚动到对应 slide）；
            // 此处仅做「当前张」高亮同步（滑动/自动播放时更新 is-active）。
            var dots = [].slice.call(root.querySelectorAll('.sky-slider-dot'));
            function updateDots() {
                dots.forEach(function (d, i) { d.classList.toggle('is-active', i === idx); });
            }
            // 箭头。
            var prevBtn = root.querySelector('[data-prev]');
            var nextBtn = root.querySelector('[data-next]');
            if (prevBtn) prevBtn.addEventListener('click', prev);
            if (nextBtn) nextBtn.addEventListener('click', next);
            // 滑动同步索引（含触摸/原生滚动）。
            var scrollTimer = null;
            track.addEventListener('scroll', function () {
                clearTimeout(scrollTimer);
                scrollTimer = setTimeout(function () {
                    var w = slideWidth();
                    if (w > 0) { idx = Math.round(track.scrollLeft / w); updateDots(); }
                }, 80);
            });
            // 循环：滑到末尾回到开头。
            if (root.dataset.loop) {
                track.addEventListener('scroll', function () {
                    if (track.scrollLeft >= track.scrollWidth - track.clientWidth - 2) {
                        track.scrollTo({ left: 0, behavior: 'smooth' });
                    }
                });
            }
            // 自动播放（悬停暂停）。
            var autoplay = parseFloat(root.dataset.autoplay || 0);
            var timer = null;
            if (autoplay > 0) {
                function play() { timer = setInterval(function () { go(idx + 1 >= total ? 0 : idx + 1); }, autoplay * 1000); }
                function stop() { if (timer) { clearInterval(timer); timer = null; } }
                play();
                root.addEventListener('mouseenter', stop);
                root.addEventListener('mouseleave', function () { if (!timer) play(); });
            }
            updateDots();
        });
    }


    // ---------- 图集轮播：箭头 / 自动播放 / 循环 ----------
    // 圆点导航已改构建期锚点（零 JS 可点）；此处仅做箭头与自动播放增强。
    function initCarousels() {
        var roots = document.querySelectorAll('[data-carousel]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            var track = root.querySelector('.gallery-track');
            if (!track) return;
            var slides = root.querySelectorAll('.gallery-slide');
            if (!slides.length) return;
            var cfg = {};
            try { cfg = JSON.parse(root.getAttribute('data-carousel')) || {}; } catch (e) { cfg = {}; }
            var perView = Math.max(1, parseInt((cfg.slidesPerView || {}).desktop || 1, 10));
            var idx = 0;
            function maxIdx() { return Math.max(0, slides.length - perView); }
            function width() { return slides[0] ? slides[0].offsetWidth * perView : 0; }
            function go(i) {
                idx = Math.max(0, Math.min(i, maxIdx()));
                track.scrollTo({ left: width() * idx, behavior: 'smooth' });
            }
            var prevBtn = root.querySelector('.gallery-prev');
            var nextBtn = root.querySelector('.gallery-next');
            if (prevBtn) prevBtn.addEventListener('click', function () { go(idx - 1); });
            if (nextBtn) nextBtn.addEventListener('click', function () { go(idx + 1); });
            // 滑动同步索引（触摸 / 原生滚动）。
            var st = null;
            track.addEventListener('scroll', function () {
                clearTimeout(st);
                st = setTimeout(function () {
                    var w = width();
                    if (w > 0) idx = Math.round(track.scrollLeft / w);
                }, 80);
            });
            // 循环：滚到末尾回到开头。
            if (cfg.infinite) {
                track.addEventListener('scroll', function () {
                    if (track.scrollLeft >= track.scrollWidth - track.clientWidth - 2) {
                        idx = 0;
                        track.scrollTo({ left: 0, behavior: 'smooth' });
                    }
                });
            }
            // 自动播放（可选悬停暂停）。
            if (cfg.autoplay) {
                var interval = parseInt(cfg.interval || 4000, 10);
                var timer = null;
                function play() {
                    timer = setInterval(function () {
                        if (idx >= maxIdx()) { go(0); } else { go(idx + 1); }
                    }, interval);
                }
                function stop() { if (timer) { clearInterval(timer); timer = null; } }
                play();
                if (cfg.pauseOnHover) {
                    root.addEventListener('mouseenter', stop);
                    root.addEventListener('mouseleave', function () { if (!timer) play(); });
                }
            }
        });
    }

    // ---------- 倒计时：data-countdown 按 data-target 计算剩余时间 ----------
    function initCountdowns() {
        var els = document.querySelectorAll('[data-countdown]');
        if (!els.length) return;
        els.forEach(function (el) {
            var target = new Date(el.getAttribute('data-target')).getTime();
            if (isNaN(target)) return;
            // 服务端输出的是 "1"/"0"（见 countdown/jet.go ShowDaysData），
            // 这里原判 === 'true' 永不成立，ShowDays 一直等价于关闭。
            var showDaysAttr = el.getAttribute('data-show-days');
            var showDays = showDaysAttr === '1' || showDaysAttr === 'true';
            var nums = el.querySelectorAll('.cd-num');
            if (!nums.length) return;
            function pad(n) { return n < 10 ? '0' + n : '' + n; }
            function units(ms) {
                var s = Math.max(0, Math.floor(ms / 1000));
                var d = Math.floor(s / 86400);
                var h = Math.floor((s % 86400) / 3600);
                var m = Math.floor((s % 3600) / 60);
                var sec = s % 60;
                return showDays ? [d, h, m, sec] : [d * 24 + h, m, sec];
            }
            function tick() {
                var diff = target - Date.now();
                var u = units(diff);
                for (var i = 0; i < nums.length && i < u.length; i++) {
                    nums[i].textContent = pad(u[i]);
                }
            }
            tick();
            setInterval(tick, 1000);
        });
    }

    // ---------- 图集灯箱：点击 [data-lightbox] 打开浮层大图 ----------
    // 服务端产物里的 data-lightbox 此前没有任何消费方：点击等于普通链接跳走，
    // 用户离开页面去看原图。这里接管为无依赖浮层；脚本未加载时仍是原有的
    // 「点击直开原图」降级行为（href 不变）。
    function initLightboxes() {
        var links = document.querySelectorAll("[data-lightbox]");
        if (!links.length) return;
        var overlay = null;
        function onKey(e) { if (e.key === "Escape") close(); }
        function close() {
            if (overlay && overlay.parentNode) overlay.parentNode.removeChild(overlay);
            overlay = null;
            document.removeEventListener("keydown", onKey);
        }
        links.forEach(function (a) {
            a.addEventListener("click", function (e) {
                var href = a.getAttribute("href");
                if (!href) return;
                e.preventDefault();
                if (overlay) close();
                overlay = document.createElement("div");
                overlay.style.cssText = "position:fixed;inset:0;z-index:9999;display:flex;align-items:center;justify-content:center;background:rgba(0,0,0,.86);cursor:zoom-out;padding:24px;";
                var big = document.createElement("img");
                big.src = href;
                var inner = a.querySelector("img");
                big.alt = inner ? (inner.alt || "") : "";
                big.style.cssText = "max-width:100%;max-height:100%;object-fit:contain;";
                overlay.appendChild(big);
                overlay.addEventListener("click", close);
                document.body.appendChild(overlay);
                document.addEventListener("keydown", onKey);
            });
        });
    }

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
            root.addEventListener('click', function (e) {
                if (moved) {
                    moved = false;
                    e.preventDefault();
                    e.stopPropagation();
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

    // ---------- 全屏分页：入场动画与当前屏高亮 ----------
    // 为什么用脚本而不是 animation-timeline: view()：slide 的轨道是**内嵌滚动容器**，
    // 实测 view() 在该场景下不驱动动画 —— 时间线对象创建成功、进度随滚动正常变化，
    // 但元素的计算值（transform / filter）恒为初始值，动画等于没跑（同元素普通动画正常）。
    // 用 IntersectionObserver 以轨道为 root 判断卡片可见比例，切换类名触发普通 animation。
    function initSlideStacks() {
        var roots = document.querySelectorAll('[data-cardstack-slide]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            var track = root.querySelector('[data-cardstack-track]');
            if (!track) return;
            var cards = [].slice.call(track.querySelectorAll('.sky-cardstack-card'));
            if (!cards.length) return;
            watchTrack(track, cards);
        });
    }

    // watchTrack 按卡片与轨道视口的交叠比例切换类名：进入播一次入场动画、占满则当前屏高亮。
    //
    // 用 scroll 事件 + getBoundingClientRect 而不是 IntersectionObserver：后者的回调依赖
    // 渲染帧调度，在无头/自动化环境里对页面脚本创建的实例不回调（手动创建的同参数实例却正常），
    // 导致「实现看着对、行为完全不发生」且无法自动验证。几何计算是同步的、可断言的，
    // 顺带天然满足「只在用户操作时触发」—— 初始只标当前屏，不播入场动画。
    function watchTrack(track, cards) {
        var lastRatio = new WeakMap();

        function measure() {
            var tr = track.getBoundingClientRect();
            var out = [];
            cards.forEach(function (c) {
                var r = c.getBoundingClientRect();
                var overlap = Math.min(r.bottom, tr.bottom) - Math.max(r.top, tr.top);
                out.push({ card: c, ratio: Math.max(0, Math.min(1, r.height > 0 ? overlap / r.height : 0)) });
            });
            return out;
        }

        function apply(allowEnter) {
            measure().forEach(function (m) {
                var c = m.card;
                var r = m.ratio;
                var prev = lastRatio.get(c);
                lastRatio.set(c, r);
                if (r >= 0.6) {
                    c.classList.add('is-current');
                    // prev === undefined 表示这是初始化那一次：不播入场，避免页面一加载就自己动
                    if (allowEnter && prev !== undefined) { c.classList.add('is-enter'); }
                } else if (r > 0) {
                    c.classList.remove('is-current');
                } else {
                    // 完全离开：清掉入场类，往回滚能重播
                    c.classList.remove('is-current', 'is-enter');
                }
            });
        }

        apply(false); // 初始：只标当前屏

        // 用 setTimeout 而不是 requestAnimationFrame 做节流：rAF 依赖渲染帧调度，
        // 在无头/自动化环境里不执行（与 IntersectionObserver 不回调同源），
        // 会让整段逻辑静默失效。setTimeout 在两种环境都可靠，80ms 足够跟上手动翻页。
        var ticking = false;
        track.addEventListener('scroll', function () {
            if (ticking) return;
            ticking = true;
            setTimeout(function () {
                ticking = false;
                apply(true);
            }, 80);
        }, { passive: true });
    }

    onReady(function () {
        // 每个增强各自独立执行：任何一个抛错都不该拖累其余。
        // 这不是防御性编程 —— 排在最前面的增强一旦抛错，后面的全部不会执行，
        // 表现为「组件看着正常但某个功能完全不生效」，且控制台外的排查成本很高。
        [
            initCounters, initSliders, initCarousels, initCountdowns, initLightboxes,
            initCardStacks, initCardDecks, initSlideStacks,
        ].forEach(function (fn) {
            try {
                fn();
            } catch (e) {
                // 单个增强失败不影响其余；真要排查时在控制台里单独调该函数即可。
            }
        });
    });
})();