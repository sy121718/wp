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

