/**
 * wp-enhance.js — 构建产物客户端增强（纯客户端交互，静态站运行）。
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
            var value = el.querySelector('.wp-counter-value');
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
            var slides = root.querySelectorAll('.wp-slide');
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
            var dots = [].slice.call(root.querySelectorAll('.wp-slider-dot'));
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

    onReady(function () {
        initCounters();
        initSliders();
        initCarousels();
        initCountdowns();
        initLightboxes();
    });
})();