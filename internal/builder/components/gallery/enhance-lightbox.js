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

