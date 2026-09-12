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

