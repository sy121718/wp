"""IPI 首页复刻（逐段对应 wp_test1 首页）。

段落顺序与原文案一一对应：hero → 跑马灯 → AURORA → 分类卡 → New Arrivals →
指标条 → Technology 三段 → 品牌段 → 订阅。页眉页脚走主题结构槽位，不在本文档里。
"""
from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402
import ipi_doc as D  # noqa: E402

STATE = os.path.join(C.ROOT, "tmp", "ipi", "state.json")
W = "1280px"


def hero() -> dict:
    eyebrow = D.text("MAGNETIC SWITCH ECOSYSTEM", size="12px", color="var(--sky-c-accent)",
                     rich=False)
    line1 = D.heading("OPEN THE DOOR", level=1, size="60px", size_mobile="32px",
                      color="#FFFFFF", lh="0.98", weight="800")
    line2 = D.heading("TO THE", level=1, size="60px", size_mobile="32px",
                      color="#FFFFFF", lh="0.98", weight="800")
    line3 = D.heading("ART OF", level=1, size="60px", size_mobile="32px", lh="0.98",
                      weight="800",
                      gradient="linear-gradient(92deg, #C084FC 0%, #E879F9 55%, #7C3AED 100%)")
    line4 = D.heading("LIGHTSPEED", level=1, size="60px", size_mobile="32px", lh="0.98",
                      weight="800",
                      gradient="linear-gradient(92deg, #E879F9 0%, #C084FC 60%, #8B5CF6 100%)")
    lead = D.text("IPI engineers true 8K magnetic-switch keyboards and esports mice "
                  "down to 0.03 ms. CNC aluminium, Rapid Trigger, and 32K scan — "
                  "built for players who win in the gap between frames.",
                  size="16px", color="#A99FC6")
    ctas = D.row([D.button("Explore The Lineup", "/catalog"),
                  D.button("Explore Tech", "#technology", variant="ghost")],
                 gap="14px", wrap=False)
    chips = D.row([D.badge("Official IPI Brand Site"), D.badge("8K Polling"),
                   D.badge("0.03 ms Latency"), D.badge("32K Scan Rate")],
                  gap="10px", wrap=True)

    left = D.col([eyebrow, D.col([line1, line2, line3, line4], gap="0"),
                  lead, ctas, chips], gap="22px", width="700px", align="flex-start",
                 entrance="fade-in-up", delay=0.1)

    shot = D.image("hero1.jpg", "IPI HAZEX keyboard and mouse under neon light",
                   ratio="4:3", radius="20px", width="100%")
    float_card = D.col([
        D.text("AURORA SERIES", size="11px", color="#C9BDF5", rich=False),
        D.text("IPI · Infinite Play Innovation", size="13px", color="#8A7FA8", rich=False),
    ], gap="6px", bg="rgba(18,16,27,0.88)", pad="16px 22px", radius="14px",
        border=("1px", "rgba(168,85,247,0.35)"), loop="float")
    right = D.col([shot, float_card], gap="14px", width="560px", align="flex-start")

    return D.section([D.row([left, right], gap="56px", align="center",
                            justify="space-between", width=W, wrap=False)],
                     tag="section",
                     gradient="radial-gradient(120% 90% at 78% 18%, #2A1150 0%, #0A0714 45%, #05040A 100%)",
                     pad="72px 40px 88px", min_h="640px")


def ticker() -> dict:
    return D.marquee(["Rapid Trigger", "0.03 ms Latency", "CNC Aluminium", "True 8K"],
                     bg="#4C1D95", color="#FFFFFF", size="26px", speed=26)


def aurora() -> dict:
    head = D.heading("LIGHT THAT ANSWERS EVERY KEYSTROKE", level=2, size="40px",
                     color="#FFFFFF", lh="1.06")
    bullets = D.col([
        D.text("<p>Per-key RGB with 16.8M colours and 12 onboard lighting engines.</p>",
               size="15px", color="#A99FC6"),
        D.text("<p>Aluminium frame machined from a single billet — zero flex, zero rattle.</p>",
               size="15px", color="#A99FC6"),
        D.text("<p>Magnetic switches rated for 100M presses with hot-swap sockets.</p>",
               size="15px", color="#A99FC6"),
    ], gap="10px")
    return D.section([
        D.row([
            D.col([D.image("aurora_a.jpg", "AURORA keyboard lighting",
                           ratio="4:3", radius="18px", width="100%")], width="560px"),
            D.col([D.text("AURORA SERIES", size="12px", color="var(--sky-c-accent)", rich=False),
                   head, bullets, D.button("Explore The Technology", "/catalog")],
                  gap="20px", width="560px", align="flex-start"),
        ], gap="64px", align="center", justify="space-between", width=W),
    ], tag="section", bg="#05040A", pad="96px 40px")


def categories() -> dict:
    def card(name: str, label: str, img: str, href: str) -> dict:
        return D.col([
            D.image(img, label, ratio="16:9", radius="18px", width="100%",
                    hover_scale="1.04"),
            D.row([
                D.heading(name, level=3, size="26px", color="#FFFFFF"),
                D.text("SHOP →", size="12px", color="var(--sky-c-accent)", rich=False),
            ], gap="12px", align="center", justify="space-between", wrap=False),
        ], gap="16px", width="600px",
            bg="#0B0913", radius="20px", pad="18px",
            border=("1px", "rgba(255,255,255,0.06)"), hover_effect="lift")
    return D.section([
        D.heading("SHOP BY CATEGORY", level=2, size="30px", color="#FFFFFF",
                  subtitle="Pick your weapon", subtitle_color="#8A7FA8"),
        D.row([card("Keyboards", "IPI keyboard", "flash75_a.jpg", "/catalog"),
               card("Mice", "IPI mouse", "hazex_a.jpg", "/catalog")],
              gap="24px", width=W, align="stretch", wrap=False),
    ], tag="section", bg="#05040A", pad="0 40px 96px", gap="32px")


def arrivals() -> dict:
    items = [
        ("HAZEX", "Magnetic gaming mouse", "hazex_a.jpg", "58 g · 42K DPI"),
        ("QI Pro", "Wireless magnetic keyboard", "qipro_a.jpg", "75% · Rapid Trigger"),
        ("FLY", "Ultralight esports mouse", "fly_a.jpg", "50 g · 8K polling"),
        ("ESU63", "Hall-effect keyboard", "esu63_a.jpg", "63 keys · CNC alu"),
        ("AURORA + HAZEX Kit", "Full battle station", "aurora65w_a.jpg", "Bundle · save 15%"),
    ]
    cards = []
    for name, desc, img, meta in items:
        cards.append(D.col([
            D.image(img, name, ratio="1:1", radius="16px", width="100%",
                    hover_scale="1.05"),
            D.heading(name, level=3, size="18px", color="#FFFFFF"),
            D.text(desc, size="13px", color="#8A7FA8", rich=False),
            D.text(meta, size="12px", color="#6F6489", rich=False),
            D.text("DETAILS", size="12px", color="var(--sky-c-accent)", rich=False),
        ], gap="8px", width="244px",
            bg="#0B0913", radius="18px", pad="14px",
            border=("1px", "rgba(255,255,255,0.06)"), hover_effect="lift"))
    return D.section([
        D.row([
            D.heading("NEW ARRIVALS", level=2, size="30px", color="#FFFFFF"),
            D.button("View All", "/catalog", variant="ghost", size="sm"),
        ], gap="20px", align="center", justify="space-between", width=W, wrap=False),
        D.row(cards, gap="14px", width=W, align="stretch", wrap=False),
    ], tag="section", bg="#05040A", pad="0 40px 96px", gap="28px")


def stats() -> dict:
    return D.section([
        D.row([D.counter("8000Hz", "True Polling Rate"),
               D.counter("0.08ms", "Click Latency"),
               D.counter("32K", "Scan Rate"),
               D.counter("120H", "Battery Life")],
              gap="72px", width=W, justify="center", wrap=True),
    ], tag="section", bg="#0B0913", pad="56px 40px",
        border=None)


def technology() -> dict:
    def block(title: str, body: str, img: str) -> dict:
        return D.row([
            D.col([D.image(img, title, ratio="4:3", radius="16px", width="100%",
                            hover_scale="1.03", align="center")],
                  width="560px", scroll_story="rise"),
            D.col([D.heading(title, level=3, size="26px", color="#FFFFFF", lh="1.15"),
                   D.text(body, size="15px", color="#A99FC6")],
                  gap="14px", width="560px", align="flex-start"),
        ], gap="48px", align="center", width=W, justify="space-between")
    return D.section([
        D.heading("TECHNOLOGY", level=2, size="30px", color="#FFFFFF",
                  subtitle="Engineered in the open", subtitle_color="#8A7FA8"),
        block("True 8K. Zero Compromise.",
              "A dedicated 8K polling engine reports every 0.125 ms — no interpolation, "
              "no smoothing, no dropped frames between your hand and the server.",
              "hero3.jpg"),
        block("Machined, Not Moulded.",
              "Each chassis starts as a solid aluminium billet and is CNC-cut to a "
              "0.005 mm tolerance. The result is a 1.2 kg deck that refuses to flex.",
              "hero4.jpg"),
        block("Sense Every Millimetre.",
              "Hall-effect sensors read 0.01 mm of travel, so Rapid Trigger resets the "
              "keystroke the instant you release — strafing feels like thinking.",
              "bg-chrome.jpg"),
    ], tag="section", bg="#05040A", pad="96px 40px", gap="48px",
        custom_id="technology")


def brand() -> dict:
    return D.section([
        D.col([
            D.heading("WE BUILD THE GAP BETWEEN YOU AND THE WIN", level=2, size="34px",
                      color="#FFFFFF", lh="1.12"),
            D.text("IPI — Infinite Play Innovation. We design hardware for the "
                   "milliseconds that decide a round.", size="15px", color="#A99FC6"),
            D.row([D.counter("0.005mm", "TR Accuracy"),
                   D.counter("50g", "Lightest Mouse"),
                   D.counter("2YR", "Warranty")],
                  gap="56px", justify="center", width="100%"),
        ], gap="24px", width="100%", align="flex-start"),
    ], tag="section", bg="#0B0913", pad="80px 40px",
        border=("1px", "rgba(168,85,247,0.18)"))


def subscribe() -> dict:
    return D.section([
        D.row([
            D.col([D.heading("BE FIRST WHEN NEW GEAR DROPS", level=2, size="28px",
                             color="#FFFFFF"),
                   D.text("Product launches, firmware drops and esports stories. "
                          "No spam, unsubscribe anytime.", size="14px", color="#8A7FA8")],
                  gap="10px", width="520px", align="flex-start"),
            D.node("core.form", {
                "fields": [{"type": "email", "label": "Email", "name": "email",
                            "placeholder": "you@example.com", "required": True}],
                "submitLabel": "Subscribe", "action": "/", "method": "post",
            }),
        ], gap="32px", align="center", justify="space-between", width=W),
    ], tag="section", bg="#05040A", pad="0 40px 96px")


def document() -> dict:
    D.reset_ids(500)
    return D.doc([
        hero(), ticker(), aurora(), categories(), arrivals(), stats(),
        technology(), brand(), subscribe(),
    ], title="IPI — Infinite Play Innovation",
        description="True 8K magnetic-switch keyboards and esports mice, engineered "
                    "for players who win in the gap between frames.")


def main() -> None:
    C.login()
    st = json.load(open(STATE, encoding="utf-8"))
    pid = st["projectId"]
    pages = C.ok(f"/api/page/list?projectId={pid}", method="GET") or []
    home = next((p for p in pages if p.get("kind") == "home"), None)
    doc = document()
    if home:
        C.ok("/api/page/draft/save", {"id": home["id"],
                                      "expectedVersion": home["draftVersion"],
                                      "draftPath": "/", "draftDocument": doc})
        page_id = home["id"]
    else:
        page_id = C.ok("/api/page/create", {
            "projectId": pid, "kind": "home", "contentTargetType": "none",
            "draftPath": "/", "draftDocument": doc,
        })["id"]
    print("home", page_id)
    C.ok("/api/page/build", {"id": page_id})
    C.ok("/api/page/publish", {"id": page_id, "lang": "zh-CN"})
    print("built + published")


if __name__ == "__main__":
    main()
