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

