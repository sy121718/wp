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

