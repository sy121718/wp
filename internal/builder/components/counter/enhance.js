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
