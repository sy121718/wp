"""IPI 结构块：页眉 / 页脚 + 主题槽位绑定。

页眉页脚是「全站一份」的东西，必须走全局块 + 结构槽位（不是每页复制一遍），
否则改一个导航项要改 12 个页面。
"""
from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402
import ipi_doc as D  # noqa: E402

STATE = os.path.join(C.ROOT, "tmp", "ipi", "state.json")
NAV = [
    ("Home", "/"),
    ("Keyboards", "/catalog"),
    ("Mice", "/catalog"),
    ("Catalog", "/catalog"),
    ("Blog", "/blog"),
    ("Contact", "/contact"),
    ("About", "/about"),
]


def load_state() -> dict:
    with open(STATE, encoding="utf-8") as f:
        return json.load(f)


def save_state(st: dict) -> None:
    with open(STATE, "w", encoding="utf-8") as f:
        json.dump(st, f, ensure_ascii=False, indent=1)


def header_doc() -> dict:
    D.reset_ids()
    bar = D.row([
        # 文字 logo：媒体库为 PNG 生成的变体一律转 .jpg（体积优化），透明底会被填白，
        # 白字 logo 的变体就是一块白板。字形本身用 Orbitron 还原，图只用在内容区。
        # 用固定宽容器包住：块级 h2 默认撑满，直接放进 space-between 行里会把导航顶到中间。
        D.col([D.heading("IPI", level=2, size="26px", color="#FFFFFF", weight="900",
                         spacing="6px", lh="1")], width="140px", gap="0",
              align="flex-start"),
        # 中间撑满的空白列把导航组推到右侧（手稿：logo 贴左，导航+动作贴右）。
        D.col([], width="100%", gap="0"),
        D.row([D.node("core.nav", {
            "items": [{"label": label, "url": href} for label, href in NAV],
            "orientation": "horizontal",
            "gap": "26px",
            "color": "var(--sky-c-text)",
            "hoverColor": "var(--sky-c-accent)",
            "activeColor": "var(--sky-c-accent)",
            "fontSize": "13px",
            "fontWeight": "600",
            "transform": "uppercase",
            "mobileCollapse": True,
            "toggleLabel": "",
        }),
            D.button("Shop Now", "/catalog", size="sm"),
        ], gap="32px", align="center", wrap=False),
    ], gap="0", align="center", justify="space-between", width="1360px", wrap=False)
    return {
        "settings": {"layout": {"mode": "full"}},
        "root": [D.section([bar], tag="header", bg="rgba(5,4,10,0.92)", pad="18px 40px",
                           direction="column", gap="0")],
    }


def footer_doc() -> dict:
    D.reset_ids(200)
    link = lambda label, href: D.text(  # noqa: E731
        f'<p><a href="{href}">{label}</a></p>', size="14px", color="#A99FC6", rich=True,
        link_color="var(--sky-link-color)")

    brand = D.col([
        D.heading("IPI", level=3, size="26px", color="#FFFFFF", weight="900",
                  spacing="6px", lh="1"),
        D.text("True 8K magnetic-switch keyboards and esports mice, engineered for "
               "players who win in the gap between frames.", size="14px",
               color="#8A7FA8"),
        D.node("core.social_buttons", {
            "items": [
                {"platform": "instagram", "url": "https://instagram.com"},
                {"platform": "youtube", "url": "https://youtube.com"},
                {"platform": "x", "url": "https://x.com"},
            ],
            "color": "custom", "customColor": "#ECE9FB", "size": "36px",
            "shape": "circle", "align": "left",
        }),
    ], gap="18px", width="360px", align="flex-start")

    products = D.col([D.heading("Products", level=3, size="13px", spacing="2px",
                                transform="uppercase", color="#FFFFFF", weight="700")] + [
        link("Keyboards", "/catalog"), link("Mice", "/catalog"),
        link("New Arrivals", "/catalog"), link("Technology", "/"),
        link("AURORA Series", "/catalog"),
    ], gap="10px", width="200px", align="flex-start")

    support = D.col([D.heading("Support", level=3, size="13px", spacing="2px",
                               transform="uppercase", color="#FFFFFF", weight="700")] + [
        link("Shipping & Delivery", "/supported-devices"),
        link("Warranty & RMA", "/driver"),
        link("Firmware & Drivers", "/driver"),
        link("Where To Buy", "/catalog"),
        link("Contact Us", "/contact"),
    ], gap="10px", width="200px", align="flex-start")

    company = D.col([D.heading("Company", level=3, size="13px", spacing="2px",
                               transform="uppercase", color="#FFFFFF", weight="700")] + [
        link("About IPI", "/about"), link("Technology", "/"),
        link("Esports Teams", "/portfolio"), link("Press Kit", "/about"),
        link("Affiliates", "/services"),
    ], gap="10px", width="200px", align="flex-start")

    top = D.row([brand, products, support, company], gap="48px",
                align="flex-start", justify="space-between", width="1360px")
    bottom = D.row([
        D.text("© 2026 IPI — Infinite Play Innovation. All rights reserved.",
               size="12px", color="#6F6489"),
        D.row([D.badge("TRUE 8K"), D.badge("RAPID TRIGGER"), D.badge("CNC ALUMINIUM"),
               D.badge("2-YEAR WARRANTY")], gap="8px", wrap=False),
    ], gap="16px", align="center", justify="space-between", width="1360px", wrap=False)

    return {
        "settings": {"layout": {"mode": "full"}},
        "root": [D.section([D.col([top, D.divider(), bottom], gap="40px")],
                           tag="footer", bg="#05040A", pad="72px 40px 40px")],
    }


def upsert_block(pid: str, name: str, kind: str, document: dict, existing: list) -> str:
    hit = next((b for b in existing if b.get("name") == name), None)
    if hit:
        C.ok("/api/block/update", {"id": hit["id"], "name": name, "kind": kind,
                                   "document": document})
        return hit["id"]
    return C.ok("/api/block/create", {"projectId": pid, "name": name, "kind": kind,
                                      "category": "structure", "reuseMode": "global",
                                      "document": document})["id"]


def main() -> None:
    C.login()
    st = load_state()
    pid, tid = st["projectId"], st["themeId"]
    existing = C.ok(f"/api/block/list?projectId={pid}", method="GET") or []
    hid = upsert_block(pid, "Site Header", "header", header_doc(), existing)
    fid = upsert_block(pid, "Site Footer", "footer", footer_doc(), existing)
    print("header", hid, "footer", fid)

    themes = C.ok(f"/api/theme/list?projectId={pid}", method="GET") or []
    theme = next(t for t in themes if t["id"] == tid)
    settings = theme.get("settings") or {}
    settings["headerBlockId"] = hid
    settings["footerBlockId"] = fid
    C.ok("/api/theme/update", {"id": tid, "name": theme["name"], "settings": settings})
    st["headerBlockId"], st["footerBlockId"] = hid, fid
    save_state(st)
    print("主题已绑定结构槽位")


if __name__ == "__main__":
    main()
