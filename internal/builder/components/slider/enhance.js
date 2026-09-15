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
                // 滚动容器是根节点（.sky-slider 有 overflow-x:auto），不是轨道：
                // 轨道的 overflow 是 visible，对它调 scrollTo 不会产生任何位移。
                root.scrollTo({ left: slideWidth() * idx, behavior: 'smooth' });
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
            // 键盘等价入口：容器 tabindex=0（模板输出），左右方向键翻页、Home/End 跳首尾。
            // 与 cardstack 的方向键实现同源 —— 键盘用户不依赖鼠标也能切换轮播。
            // 焦点落在内部交互元素（链接 / 按钮 / 表单控件）上时不劫持方向键，页面照常滚动。
            root.addEventListener('keydown', function (e) {
                var t = e.target;
                if (t && t.closest && t.closest('a,button,input,textarea,select,[contenteditable]')) return;
                if (e.key === 'ArrowLeft') { prev(); e.preventDefault(); }
                else if (e.key === 'ArrowRight') { next(); e.preventDefault(); }
                else if (e.key === 'Home') { go(0); e.preventDefault(); }
                else if (e.key === 'End') { go(total - 1); e.preventDefault(); }
            });
            // 滑动同步索引（含触摸/原生滚动）—— 监听的是滚动容器（根节点）。
            var scrollTimer = null;
            root.addEventListener('scroll', function () {
                clearTimeout(scrollTimer);
                scrollTimer = setTimeout(function () {
                    var w = slideWidth();
                    if (w > 0) { idx = Math.round(root.scrollLeft / w); updateDots(); }
                }, 80);
            });
            // 循环：滑到末尾回到开头。
            if (root.dataset.loop) {
                root.addEventListener('scroll', function () {
                    if (root.scrollLeft >= root.scrollWidth - root.clientWidth - 2) {
                        root.scrollTo({ left: 0, behavior: 'smooth' });
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

