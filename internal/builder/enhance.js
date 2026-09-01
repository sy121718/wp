/**
 * wp-enhance.js — 构建产物客户端增强（纯客户端交互，静态站运行）。
 * 零依赖 IIFE；按 data-* 属性按需初始化，无交互组件时静默跳过。
 * 覆盖：轮播（箭头/圆点/自动播放/循环）、计数器（视口进入动画）、
 * 手风琴（严格单开）。tabs（radio hack）与基础 accordion（details）原生零 JS。
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
            var dots = [];
            var dotsWrap = root.querySelector('[data-dots]');
            if (dotsWrap) {
                slides.forEach(function (_, i) {
                    var d = document.createElement('button');
                    d.type = 'button';
                    d.setAttribute('aria-label', '第 ' + (i + 1) + ' 张');
                    d.addEventListener('click', function () { go(i); });
                    dotsWrap.appendChild(d);
                    dots.push(d);
                });
            }
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

    // ---------- 手风琴：严格单开（data-one-open） ----------
    function initAccordions() {
        var roots = document.querySelectorAll('[data-one-open]');
        if (!roots.length) return;
        roots.forEach(function (root) {
            root.querySelectorAll('details').forEach(function (d) {
                var summary = d.querySelector('summary');
                if (!summary) return;
                summary.addEventListener('click', function () {
                    root.querySelectorAll('details[open]').forEach(function (o) {
                        if (o !== d) o.removeAttribute('open');
                    });
                });
            });
        });
    }

    onReady(function () {
        initCounters();
        initSliders();
        initAccordions();
    });
})();
