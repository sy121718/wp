"""给已建页面补上页眉 / 页脚导航节点（menu=header / footer，构建期读导航模块数据）。

用法：python3 scripts/wp-import/nav_attach.py
"""
from __future__ import annotations

import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

# 源站资源直接用线上地址（用户约定：图片/logo/商品图/文章图都从线上拿，不复制到本地库）。
SOURCE_ORIGIN = "https://vapecentralau.com"
LIVE_LOGO_URL = SOURCE_ORIGIN + "/wp-content/uploads/2026/07/logo.svg"

P = C.project_id()
INTERVAL = 60.0 / 90.0

# 工程默认语言（站点设置的 default_lang）。发布与构建都必须显式带上它。
DEFAULT_LANG = os.environ.get("WP_DEFAULT_LANG", "en-AU")
_last = [0.0]


def throttle():
    dt = time.time() - _last[0]
    if dt < INTERVAL:
        time.sleep(INTERVAL - dt)
    _last[0] = time.time()


def call(path, payload, method="POST"):
    for i in range(5):
        throttle()
        code, body = C.api(path, payload, method=method)
        if body.get("code") == 200:
            return body.get("data")
        msg = str(body.get("message", ""))
        if "频繁" in msg:
            time.sleep(6 * (i + 1))
            continue
        raise RuntimeError(f"{path} → {msg[:250]}")
    raise RuntimeError(f"{path} → 限流重试耗尽")


def header_node():
    return {
        "id": "site-header", "type": "core.container",
        "props": {"tag": "header",
                  "layout": {"engine": "flex", "flex": {"direction": "row", "justify": "space-between", "align": "center", "gap": "24px"}},
                  "box": {"padding": {"desktop": "16px 24px", "tablet": "14px 20px", "mobile": "12px 16px"}},
                  "visual": {"bgColor": "#ffffff"}},
        "children": [
            # 站点 logo 直接用**源站的线上地址**（矢量 SVG，743px 宽）。
            # 不再用文字标题代替：源站的品牌标识是图形，用文字顶替会让页眉
            # 与源站在字形、留白、比例上都不一致。
            # 宽度按源站比例给 min(100%, 150px)（源站渲染约 130px 高 30px）。
            {"id": "site-header-logo", "type": "core.image",
             "props": {"src": LIVE_LOGO_URL, "alt": "Vape Store OZ",
                       "width": "150px", "loading": "off", "fetchPriority": "high"}},
            {"id": "site-header-nav", "type": "core.nav",
             "props": {"menu": "header", "orientation": "horizontal", "gap": "22px", "align": "right",
                       "color": "#111827", "hoverColor": "#2563eb", "fontSize": "15px",
                       "mobileCollapse": True, "toggleLabel": "Menu"}},
        ],
    }


def footer_node():
    return {
        "id": "site-footer", "type": "core.container",
        "props": {"tag": "footer",
                  "layout": {"engine": "flex", "flex": {"direction": "column", "gap": "14px", "align": "center"}},
                  "box": {"padding": {"desktop": "32px 24px", "tablet": "28px 20px", "mobile": "24px 16px"}},
                  "visual": {"bgColor": "#111827"}},
        "children": [
            {"id": "site-footer-nav", "type": "core.nav",
             "props": {"menu": "footer", "orientation": "horizontal", "gap": "18px", "align": "center",
                       "color": "#e5e7eb", "hoverColor": "#ffffff", "fontSize": "14px", "mobileCollapse": True}},
            {"id": "site-footer-copy", "type": "core.text",
             "props": {"mode": "plaintext", "text": "© 2026 VapeStoreOZ · 18+ only · Australia",
                       "color": "#9ca3af", "typography": {
                           "desktop": {"fontSize": "13px", "textAlign": "center"},
                           "tablet": {"fontSize": "13px", "textAlign": "center"},
                           "mobile": {"fontSize": "12px", "textAlign": "center"}}}},
        ],
    }


def main():
    C.login()
    pages = C.load_map("pages.json")
    for key, p in pages.items():
        try:
            detail = call(f"/api/page/detail?projectId={P}&id={p['id']}", None, method="GET")
        except Exception as e:  # noqa: BLE001
            print(f"  {key}: 读详情失败 {str(e)[:140]}")
            continue
        doc = detail.get("draftDocument") or {}
        root = doc.get("root") or []
        root = [n for n in root if n.get("id") not in ("site-header", "site-footer")]
        doc["root"] = [header_node()] + root + [footer_node()]
        try:
            call("/api/page/draft/save", {"id": p["id"], "expectedVersion": detail.get("draftVersion", 0),
                                          "draftPath": detail.get("draftPath", p["path"]),
                                          "draftDocument": doc})
            # **必须显式传工程默认语言**：build/publish 不传 lang 时会回落到
            # i18n.default_lang（config 里的 zh-CN），而工程默认语言是 en-AU ——
            # 两者不一致时页面会被发到 /zh/shop 这种带前缀的路径上，根路径直接 404。
            # PublishReq.Lang 的注释写着「必须与构建语言一致」，就是这条。
            call("/api/page/build", {"id": p["id"], "lang": DEFAULT_LANG})
            call("/api/page/publish", {"id": p["id"], "lang": DEFAULT_LANG})
            print(f"  {key}: 页眉页脚已加并发布")
        except Exception as e:  # noqa: BLE001
            print(f"  {key}: 保存/发布失败 {str(e)[:180]}")


if __name__ == "__main__":
    main()
