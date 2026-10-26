"""IPI 其余页面：目录 / 博客 / 联系 / 关于 / 服务 / 案例 / 商城流程页。

首页（ipi_home.py）之外的页面都在这里生成：版式照原站（IPI 定制主题），
文案按 IPI 品牌重写（原站 about/services/portfolio 是 Woodmart 演示的 Lorem ipsum）。
"""
from __future__ import annotations

import json
import os
import sys
import uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402
import ipi_doc as D  # noqa: E402

STATE = os.path.join(C.ROOT, "tmp", "ipi", "state.json")
W = "1280px"

PRODUCTS = [
    ("AURORA + HAZEX Kit", "NEW · BUNDLE",
     "Pair the AURORA keyboard with the HAZEX mouse for a complete 8K battle station.",
     "Bundle · save 15%", "hot1.jpg"),
    ("IPI ESU63", "NEW · KEYBOARDS",
     "High-end custom triple-fold · fully detachable · magnetic to mechanical hybrid.",
     "0.03 MS", "esu63_a.jpg"),
    ("IPI FLY Series", "NEW · MICE",
     "Dual 8K esports · PAW3950 flagship sensor · Omron switches.",
     "DUAL 8K", "fly_a.jpg"),
    ("IPI HAZEX", "MAGNETIC · MICE",
     "58 g magnesium shell with a 42K DPI magnetic sensor and 8000 Hz polling.",
     "58 G · 42K DPI", "hazex_a.jpg"),
    ("IPI QI Pro Series", "WIRELESS · KEYBOARDS",
     "75% layout, 8K wireless polling and Rapid Trigger on every key.",
     "75% · RAPID TRIGGER", "qipro_a.jpg"),
]


def eyebrow(label: str) -> dict:
    return D.text(label, size="12px", color="var(--sky-c-accent)", rich=False)


def page_header(title: str, desc: str, *, crumb: str = "") -> dict:
    """页面头部：眉标 + 大标题 + 说明，所有内页共用。"""
    kids = [eyebrow(crumb)] if crumb else []
    kids += [D.heading(title, level=1, size="52px", size_mobile="34px",
                       color="#FFFFFF", lh="1.05", spacing="2px"),
             D.text(desc, size="16px", color="#A99FC6", width="720px")]
    return D.section(kids, tag="header", pad="72px 40px 40px", gap="18px",
                     width=W, align="flex-start")


def card(img: str, alt: str, top: str, title: str, body: str, foot: str,
         *, tag: str = "") -> dict:
    """商品 / 文章卡片：图 + 眉标 + 标题 + 摘要 + 底行。"""
    kids = [D.image(img, alt, ratio="4:3", radius="14px", width="100%",
                    hover_scale="1.05"),
            eyebrow(top)]
    if tag:
        kids.append(D.badge(tag))
    kids += [D.heading(title, level=3, size="20px", color="#FFFFFF", lh="1.2"),
             D.text(body, size="14px", color="#9A90B8", rich=False)]
    if foot:
        kids.append(D.text(foot, size="12px", color="#8A7FA8", rich=False))
    body_col = D.col(kids[1:], gap="12px", pad="0 20px 22px")
    return D.col([kids[0], body_col], gap="14px", bg="#0D0B15", radius="16px",
                 border=("1px", "rgba(255,255,255,0.08)"), width="400px",
                 hover_effect="lift")


def product_grid(items, cols: int = 3) -> dict:
    rows = []
    for i in range(0, len(items), cols):
        chunk = [card(it[4], it[0], it[1], it[0], it[2], it[3])
                 for it in items[i:i + cols]]
        rows.append(D.row(chunk, gap="24px", width="100%", align="stretch",
                          justify="flex-start", wrap=False))
    return D.col(rows, gap="24px", width="100%")


# ---------------------------------------------------------------- catalog

def catalog() -> list:
    banner = D.row([
        D.col([
            eyebrow("THE FULL LINEUP"),
            D.heading("5 SKUS. ZERO COMPROMISE.", level=1, size="52px",
                      color="#FFFFFF", lh="1.05"),
            D.text("Magnetic-switch keyboards, hollow-body mice and matched bundles. "
                   "Every listing is built around the same idea — remove everything "
                   "between your intent and what happens on screen.",
                   size="16px", color="#A99FC6"),
            D.row([D.button("Shop Keyboards", "/catalog"),
                   D.button("Shop Mice", "/catalog", variant="ghost")],
                  gap="14px", wrap=False, justify="flex-start"),
        ], gap="20px", width="620px", align="flex-start"),
        D.col([D.image("esu63_b.jpg", "IPI ESU63", ratio="4:3", radius="18px",
                       width="100%")], width="600px"),
    ], gap="48px", width=W, justify="space-between", align="center")

    stats = D.row([
        D.col([D.heading("5", level=3, size="34px", color="#FFFFFF", lh="1"),
               eyebrow("SHIPPING SKUS")], gap="8px", width="280px"),
        D.col([D.heading("8K", level=3, size="34px", color="#FFFFFF", lh="1"),
               eyebrow("WIRELESS POLLING")], gap="8px", width="280px"),
        D.col([D.heading("0.03 ms", level=3, size="34px", color="#FFFFFF", lh="1"),
               eyebrow("CLICK LATENCY")], gap="8px", width="280px"),
        D.col([D.heading("350 h", level=3, size="34px", color="#FFFFFF", lh="1"),
               eyebrow("BATTERY LIFE")], gap="8px", width="280px"),
    ], gap="20px", width=W, wrap=False, justify="space-between")

    intro = D.col([
        D.text("The full IPI lineup: magnetic-switch keyboards, hollow-body mice, and "
               "matched bundles. Each listing is built around the same idea — remove "
               "everything between your intent and what happens on screen.",
               size="15px", color="#9A90B8"),
        D.text("Filter by category above, or start from the catalogue below. Specs are "
               "measured on retail units, not prototypes.", size="15px", color="#9A90B8"),
    ], gap="14px", width=W)

    toolbar = D.row([
        eyebrow("SHOWING ALL 5 RESULTS"),
        D.row([D.text("Default sorting", size="13px", color="#A99FC6", rich=False)],
              gap="8px", width="200px"),
    ], gap="24px", width=W, justify="space-between", align="center")

    return [
        D.section([banner], tag="section", pad="56px 40px 20px",
                  gradient="radial-gradient(900px 520px at 78% 20%, rgba(139,92,246,0.22), transparent 70%)"),
        D.section([stats], tag="section", pad="0 40px 40px"),
        D.section([intro], tag="section", pad="0 40px 40px"),
        D.section([toolbar], tag="section", pad="0 40px 24px"),
        D.section([product_grid(PRODUCTS)], tag="section", pad="0 40px 96px"),
    ]


# ---------------------------------------------------------------- blog

POSTS = [
    ("Aurora vs Raptor: Which Line Fits You", "DESIGN",
     "A short field note from the IPI lab on aurora vs raptor: which line fits you.",
     "hero1.jpg"),
    ("Setting Up 8K Wireless Without Dropouts", "GUIDE",
     "A short field note from the IPI lab on setting up 8K wireless without dropouts.",
     "aurora_a.jpg"),
    ("Magnetic Switches: What Actually Changes", "TECH",
     "A short field note from the IPI lab on magnetic switches: what actually changes.",
     "hazex_c.jpg"),
    ("Design Is How It Works, Not How It Looks", "LAB",
     "Inside the tolerance stack of a CNC aluminium deck — and why 0.005 mm matters "
     "more than any finish.", "bg-chrome.jpg"),
]


def blog() -> list:
    def post_card(img, cat, title, body) -> dict:
        return D.col([
            D.image(img, title, ratio="16:9", radius="14px", width="100%"),
            D.text(cat, size="12px", color="var(--sky-c-accent)", rich=False),
            D.heading(title, level=2, size="26px", color="#FFFFFF", lh="1.2"),
            D.text(body, size="15px", color="#9A90B8", rich=False),
            D.button("Read More", "/blog", variant="ghost", size="sm"),
        ], gap="14px", width="50%", align="flex-start")

    cards = []
    for i in range(0, len(POSTS), 2):
        pair = [post_card(p[3], p[1], p[0], p[2]) for p in POSTS[i:i + 2]]
        cards.append(D.row(pair, gap="40px", width=W, align="flex-start", wrap=False))
    return [
        page_header("Blog", "Field notes from the IPI lab: firmware, tolerances, and "
                            "the milliseconds that decide a round.", crumb="IPI JOURNAL"),
        D.section(cards, tag="section", pad="32px 40px 96px", gap="56px"),
    ]


# ---------------------------------------------------------------- contact

CONTACT_BLOCKS = [
    ("General", "Front desk", "121 Montague St\nBrooklyn, NY 11201", "hello@ipi.game"),
    ("New Business", "Strategic Director", "+1 718 555 55 55", "sales@ipi.game"),
    ("Press & Media", "Press Officer", "+1 718 555 55 55", "press@ipi.game"),
    ("Join the Team", "Recruiting", "Remote-first · 12 countries", "jobs@ipi.game"),
]


def contact() -> list:
    info = []
    for name, role, lines, mail in CONTACT_BLOCKS:
        info.append(D.col([
            D.text(name, size="15px", weight="700", color="#FFFFFF", rich=False),
            D.text(role, size="14px", color="#8A7FA8", rich=False),
            D.text(lines.replace("\n", "<br>"), size="14px", color="#9A90B8"),
            D.text(f'<a href="mailto:{mail}">{mail}</a>', size="14px", color="#9A90B8",
                   link_color="var(--sky-link-color)"),
        ], gap="6px", width="260px", align="flex-start"))

    left = D.col([
        D.heading("Let's work together", level=1, size="46px", color="#FFFFFF",
                  lh="1.05"),
        D.text("Hardware questions, dealer enquiries, RMA — the IPI team answers every "
               "message within one business day.", size="15px", color="#9A90B8"),
        D.row(info[:2], gap="48px", width="100%", align="flex-start", wrap=False),
        D.divider(),
        D.row(info[2:], gap="48px", width="100%", align="flex-start", wrap=False),
        D.divider(),
        D.heading("Send Us a Message", level=2, size="30px", color="#FFFFFF"),
        D.node("core.form", {
            "fields": [
                {"type": "text", "label": "Name", "name": "name", "required": True},
                {"type": "email", "label": "Email", "name": "email", "required": True},
                {"type": "text", "label": "Company", "name": "company"},
                {"type": "select", "label": "Topic", "name": "topic",
                 "options": ["Product question", "Order status", "RMA / warranty",
                             "Press", "Partnership"]},
                {"type": "textarea", "label": "Message", "name": "message",
                 "required": True},
            ],
            "submitLabel": "Send Message", "action": "/", "method": "post",
        }),
    ], gap="22px", width="600px", align="flex-start", stretch=True)

    right = D.col([
        D.image("brand1.jpg", "IPI studio", ratio="4:3", radius="14px", width="100%"),
        D.heading("Headquarters", level=2, size="30px", color="#FFFFFF"),
        D.text("New York<br>121 Montague St<br>Brooklyn, NY 11201<br>"
               "Monday – Friday, 9:30 am – 5:30 pm", size="14px", color="#0F0C1A"),
        D.text('<a href="/contact">Get Directions</a>', size="14px", color="#0F0C1A",
               link_color="#2A1B4A"),
    ], gap="16px", width="480px", align="flex-start", bg="#8E7FD6", radius="16px",
        pad="26px")

    careers = [
        ("UI/UX Designer", "Remote · Full time", "Own the interaction layer of the IPI "
                                                 "web and companion tools."),
        ("Product Designer", "Brooklyn · Full time", "Turn switch feel and chassis "
                                                    "tolerances into a product language."),
        ("Senior Product Designer", "Remote · Full time", "Lead the industrial design of "
                                                         "the next flagship line."),
    ]
    jobs = [D.col([D.text(t, size="18px", weight="700", color="#FFFFFF", rich=False),
                   D.text(m, size="13px", color="#D8CFF0", rich=False),
                   D.text(b, size="14px", color="#EDE7FA", rich=False),
                   D.button("Apply", "/contact", variant="ghost", size="sm")],
                  gap="10px", width="380px", align="flex-start")
            for t, m, b in careers]

    return [
        D.section([D.row([left, right], gap="80px", width=W, align="flex-start",
                         justify="space-between")],
                  tag="section", pad="56px 40px 80px", width=W),
        D.section([D.heading("Careers at IPI", level=2, size="40px", color="#FFFFFF"),
                   D.row(jobs, gap="40px", width=W, align="flex-start", wrap=False)],
                  tag="section", pad="64px 40px 96px", gap="36px",
                  bg="#3A2C63"),
    ]


# ---------------------------------------------------------------- about

def about() -> list:
    story = D.row([
        D.col([
            eyebrow("OUR STORY"),
            D.heading("WE BUILD THE GAP BETWEEN YOU AND THE WIN", level=1, size="48px",
                      color="#FFFFFF", lh="1.06"),
            D.text("IPI started in a Brooklyn workshop with one question: why does the "
                   "hardware still lag behind the player? Six years later we machine our "
                   "own chassis, write our own firmware, and ship 8K magnetic-switch "
                   "keyboards to 42 countries.", size="16px", color="#A99FC6"),
            D.text("We keep the lineup deliberately small. Five products, one idea — "
                   "remove everything between your intent and what happens on screen.",
                   size="16px", color="#A99FC6"),
        ], gap="20px", width="620px", align="flex-start"),
        D.image("hero2.jpg", "IPI hardware", ratio="4:3", radius="18px", width="600px"),
    ], gap="56px", width=W, justify="space-between", align="center")

    numbers = D.row([
        D.counter("42", "Countries Shipped"),
        D.counter("0.005mm", "CNC Tolerance"),
        D.counter("9", "Engineers"),
        D.counter("2YR", "Warranty"),
    ], gap="56px", width=W, justify="center", wrap=False)

    approach = [
        ("Our Approach", "We prototype in aluminium, not in slides. Every switch feel is "
                         "measured on a rig before it becomes a SKU."),
        ("Strong Team", "Nine engineers across firmware, industrial design and support — "
                        "small enough that the person who designed the board answers "
                        "your email."),
        ("How We Can Help You", "Firmware updates for life, RMA within 72 hours, and a "
                                "Discord channel where engineering is present daily."),
    ]
    cols = [D.col([eyebrow(t.upper()), D.heading(t, level=2, size="24px",
                                                color="#FFFFFF"),
                   D.text(b, size="15px", color="#9A90B8")],
                  gap="12px", width="380px", align="flex-start") for t, b in approach]

    works = D.row([
        D.col([eyebrow("SELECTED WORKS"),
               D.heading("WE CREATE PRODUCTS", level=2, size="34px", color="#FFFFFF"),
               D.text("AURORA · HAZEX · FLY · ESU63 — four lines, one tolerance stack.",
                      size="15px", color="#A99FC6"),
               D.button("View Catalogue", "/catalog")],
              gap="16px", width="460px", align="flex-start"),
        D.row([D.image("aurora65w_a.jpg", "AURORA 65", ratio="1:1", radius="14px",
                       width="300px"),
               D.image("hazex_b.jpg", "HAZEX", ratio="1:1", radius="14px",
                       width="300px"),
               D.image("fly_b.jpg", "FLY", ratio="1:1", radius="14px", width="300px")],
              gap="20px", wrap=False),
    ], gap="48px", width=W, align="center", justify="space-between")

    return [
        D.section([story], tag="section", pad="56px 40px 40px"),
        D.section([numbers], tag="section", pad="0 40px 64px"),
        D.section([D.row(cols, gap="40px", width=W, align="flex-start", wrap=False)],
                  tag="section", pad="56px 40px", bg="#0B0913"),
        D.section([works], tag="section", pad="72px 40px", bg="#05040A"),
        D.section([
            D.heading("Ready to build your battle station?", level=2, size="34px",
                      color="#FFFFFF"),
            D.text("Start from the catalogue or ask us what fits your desk.",
                   size="15px", color="#A99FC6"),
            D.button("Get Started", "/catalog"),
        ], tag="section", pad="72px 40px 96px", gap="16px", width=W,
            align="center", bg="#3A2C63"),
    ]


# ---------------------------------------------------------------- services

SERVICES = [
    ("Magnetic Switches", "Hall-effect sensing down to 0.01 mm, adjustable actuation "
                          "and Rapid Trigger on every key.", "0.01 mm"),
    ("Firmware & Driver", "WebHID tuning in the browser — no local install, no admin "
                          "rights, profiles travel with the device.", "WebHID"),
    ("CNC Manufacturing", "Single-billet aluminium chassis machined to a 0.005 mm "
                          "tolerance, anodised in-house.", "0.005 mm"),
    ("Esports Support", "On-site tuning for partner teams, spare parts within 72 "
                        "hours at events.", "72 H"),
]


def services() -> list:
    cards = [D.col([
        D.text(k, size="12px", color="var(--sky-c-accent)", rich=False),
        D.heading(name, level=3, size="24px", color="#FFFFFF", lh="1.2"),
        D.text(body, size="15px", color="#9A90B8"),
        D.button("Learn More", "/contact", variant="ghost", size="sm"),
    ], gap="12px", width="300px", bg="#0D0B15", radius="16px", pad="26px",
        border=("1px", "rgba(255,255,255,0.08)"), align="flex-start",
        hover_effect="lift") for name, body, k in SERVICES]

    return [
        page_header("Services", "Hardware is only half of it — firmware, tooling and "
                                "support are where the milliseconds are won.",
                    crumb="WHAT WE OFFER"),
        D.section([D.row(cards, gap="24px", width=W, wrap=False, align="stretch")],
                  tag="section", pad="0 40px 40px"),
        D.section([
            D.row([D.counter("100+", "Partner Teams"),
                   D.counter("42", "Countries"),
                   D.counter("72 H", "RMA Turnaround")],
                  gap="72px", width=W, justify="center", wrap=False),
        ], tag="section", pad="40px 40px", bg="#0B0913"),
        D.section([
            D.heading("Built in the open", level=2, size="30px", color="#FFFFFF"),
            D.text("Every firmware build ships with a changelog and a rollback path. "
                   "If a release regresses your board, the previous profile is one click "
                   "away in the driver.", size="15px", color="#A99FC6", width="760px"),
            D.button("Talk To Engineering", "/contact"),
        ], tag="section", pad="72px 40px 96px", gap="16px", width=W, align="flex-start"),
    ]


# ---------------------------------------------------------------- portfolio

PROJECTS = [
    ("AURORA 65", "Industrial design · CNC", "aurora65w_a.jpg"),
    ("HAZEX", "Product design · 58 g shell", "hazex_a.jpg"),
    ("FLY Series", "Esports mouse · 8 g base", "fly_a.jpg"),
    ("ESU63", "Triple-fold keyboard", "esu63_a.jpg"),
    ("QI Pro", "Wireless magnetic", "qipro_a.jpg"),
    ("AURORA 75", "75% deck · Rapid Trigger", "aurora_a.jpg"),
]


def portfolio() -> list:
    cards = [D.col([
        D.image(img, name, ratio="4:3", radius="14px", width="100%",
                hover_scale="1.06"),
        D.heading(name, level=3, size="22px", color="#FFFFFF"),
        D.text(cat, size="13px", color="#8A7FA8", rich=False),
    ], gap="12px", width="400px", bg="#0D0B15", radius="16px", pad="16px",
        border=("1px", "rgba(255,255,255,0.08)"), align="flex-start",
        hover_effect="lift") for name, cat, img in PROJECTS]

    rows = [D.row(cards[i:i + 3], gap="24px", width=W, wrap=False, align="stretch")
            for i in range(0, len(cards), 3)]
    return [
        page_header("Portfolio", "Selected work: six products, one tolerance stack — "
                                 "from the first billet to the retail box.",
                    crumb="SELECTED WORKS"),
        D.section(rows, tag="section", pad="0 40px 72px", gap="24px"),
        D.section([
            D.heading("Ready to build something?", level=2, size="34px",
                      color="#FFFFFF"),
            D.text("Tell us the interface, the deadline and the tolerance you need.",
                   size="15px", color="#C9BDF5"),
            D.button("Get Started", "/contact"),
        ], tag="section", pad="72px 40px 96px", gap="16px", width=W,
            align="center", bg="#3A2C63"),
    ]


def portfolio_single() -> list:
    return [
        page_header("AURORA 65", "A 65% magnetic-switch deck machined from a single "
                                 "aluminium billet, with per-key RGB and an 8K polling "
                                 "engine.", crumb="CASE STUDY"),
        D.section([
            D.row([D.image("aurora65w_a.jpg", "AURORA 65", ratio="16:9",
                           radius="18px", width="100%")],
                  width=W, wrap=False),
            D.row([
                D.col([D.text("CLIENT", size="11px", color="#8A7FA8", rich=False),
                       D.text("IPI", size="15px", color="#FFFFFF", rich=False)],
                      gap="6px", width="260px"),
                D.col([D.text("SCOPE", size="11px", color="#8A7FA8", rich=False),
                       D.text("Industrial design, firmware", size="15px",
                              color="#FFFFFF", rich=False)],
                      gap="6px", width="320px"),
                D.col([D.text("MATERIAL", size="11px", color="#8A7FA8", rich=False),
                       D.text("6063 aluminium · PBT", size="15px", color="#FFFFFF",
                              rich=False)],
                      gap="6px", width="320px"),
                D.col([D.text("YEAR", size="11px", color="#8A7FA8", rich=False),
                       D.text("2026", size="15px", color="#FFFFFF", rich=False)],
                      gap="6px", width="160px"),
            ], gap="40px", width=W, wrap=False, align="flex-start"),
        ], tag="section", pad="0 40px 56px", gap="32px"),
        D.section([
            D.row([D.image("aurora65w_b.jpg", "AURORA 65 detail", ratio="4:3",
                           radius="14px", width="100%"),
                   D.image("aurora_b.jpg", "AURORA 65 switches", ratio="4:3",
                           radius="14px", width="100%")],
                  gap="24px", width=W, wrap=False),
        ], tag="section", pad="0 40px 72px"),
        D.section([
            D.heading("Next project", level=2, size="30px", color="#FFFFFF"),
            D.button("Back To Portfolio", "/portfolio", variant="ghost"),
        ], tag="section", pad="0 40px 96px", gap="16px", width=W, align="center"),
    ]


# ---------------------------------------------------------------- 商城流程

def cart() -> list:
    return [
        page_header("Cart", "Review your gear before checkout — pricing, shipping "
                            "and taxes are calculated on the next step.",
                    crumb="IPI STORE"),
        D.section([
            D.heading("Your cart is currently empty.", level=2, size="28px",
                      color="#FFFFFF"),
            D.text("Add a board or a mouse and it will show up here — pricing, "
                   "shipping and taxes are calculated at checkout.",
                   size="15px", color="#9A90B8", width="620px"),
            D.button("Return To Shop", "/catalog"),
        ], tag="section", pad="40px 40px 120px", gap="18px", width=W, align="center"),
    ]


def checkout() -> list:
    form = D.node("core.form", {
        "fields": [
            {"type": "text", "label": "First name", "name": "first_name",
             "required": True},
            {"type": "text", "label": "Last name", "name": "last_name",
             "required": True},
            {"type": "text", "label": "Company", "name": "company"},
            {"type": "text", "label": "Address", "name": "address",
             "required": True},
            {"type": "text", "label": "City", "name": "city", "required": True},
            {"type": "text", "label": "Postcode", "name": "postcode",
             "required": True},
            {"type": "email", "label": "Email", "name": "email", "required": True},
            {"type": "text", "label": "Phone", "name": "phone"},
            {"type": "textarea", "label": "Order notes", "name": "notes"},
        ],
        "submitLabel": "Place Order", "action": "/", "method": "post",
    })
    return [
        page_header("Checkout", "Two steps: address, then payment. Every order ships "
                                "from the Brooklyn workshop within 24 hours.",
                    crumb="IPI STORE"),
        D.section([
            D.row([
                D.col([D.heading("Shipping details", level=2, size="26px",
                                 color="#FFFFFF"), form],
                      gap="18px", width="620px", align="flex-start", stretch=True),
                D.col([
                    D.heading("Order summary", level=2, size="22px", color="#FFFFFF"),
                    D.divider(),
                    D.row([D.text("AURORA + HAZEX Kit", size="14px",
                                  color="#ECE9FB", rich=False),
                           D.text("$329", size="14px", color="#FFFFFF", rich=False)],
                          width="100%", justify="space-between", wrap=False),
                    D.row([D.text("Shipping", size="14px", color="#9A90B8",
                                  rich=False),
                           D.text("Free", size="14px", color="#22D3EE", rich=False)],
                          width="100%", justify="space-between", wrap=False),
                    D.divider(),
                    D.row([D.text("Total", size="16px", color="#FFFFFF",
                                  weight="700", rich=False),
                           D.text("$329", size="18px", color="#E879F9",
                                  weight="700", rich=False)],
                          width="100%", justify="space-between", wrap=False),
                ], gap="14px", width="420px", bg="#0D0B15", radius="16px",
                    pad="24px", border=("1px", "rgba(255,255,255,0.08)"),
                    align="flex-start", stretch=True, push_right=True),
            ], gap="56px", width=W, align="flex-start", justify="space-between",
                wrap=False),
        ], tag="section", pad="0 40px 120px", width=W),
    ]


def my_account() -> list:
    form = D.node("core.form", {
        "fields": [
            {"type": "text", "label": "Username or email address",
             "name": "username", "required": True},
            {"type": "text", "label": "Password", "name": "password",
             "required": True},
            {"type": "checkbox", "label": "Remember me", "name": "remember"},
        ],
        "submitLabel": "Log In", "action": "/", "method": "post",
    })
    return [
        page_header("My account", "Log in to track orders, manage warranties and pull "
                                  "your device profiles.", crumb="IPI ACCOUNT"),
        D.section([
            D.col([D.heading("Login", level=2, size="24px", color="#FFFFFF"), form,
                   D.text('<a href="/contact">Lost your password?</a>', size="14px",
                          color="#9A90B8", link_color="var(--sky-link-color)")],
                  gap="18px", width="620px", align="flex-start"),
        ], tag="section", pad="0 40px 120px"),
    ]


# ---------------------------------------------------------------- 支持

KEYBOARDS = [
    ("AURORA 65", "Magnetic · 8K polling · Rapid Trigger", "65 Keys", "0x105C",
     "SUPPORTED", "aurora65w_a.jpg"),
    ("AURORA 65W", "Wireless magnetic", "65 Keys", "0x105C", "SOON", ""),
    ("AURORA 75 PRO+", "Magnetic 75% layout", "75 Keys", "0x105C", "SOON", ""),
    ("AURORA65+", "Magnetic switch", "65 Keys", "0x105C", "SOON", ""),
    ("Aurora 75", "Magnetic, 75% layout", "75 Keys", "0x105C", "SOON", ""),
    ("Aurora 75 PRO", "Magnetic, 75% layout", "75 Keys", "0x105C", "SOON", ""),
    ("QBZ65", "Magnetic switch", "65 Keys", "0x105C", "SOON", ""),
    ("QBZ65+", "Magnetic switch", "65 Keys", "0x105C", "SOON", ""),
]


def _device_card(name, desc, keys, pid, badge, img) -> dict:
    kids = []
    if badge:
        kids.append(D.badge(badge))
    else:
        kids.append(D.badge("SOON"))
    if img:
        kids.append(D.image(img, name, ratio="16:9", radius="10px", width="100%"))
    kids += [D.heading(name, level=3, size="20px", color="#FFFFFF"),
             D.text(desc, size="13px", color="#8A7FA8", rich=False),
             D.row([D.badge(keys), D.badge(pid)], gap="8px", wrap=False)]
    return D.col(kids, gap="12px", width="288px", bg="#0D0B15", radius="16px",
                 pad="18px", border=("1px", "rgba(255,255,255,0.08)"),
                 align="flex-start", hover_effect="lift")


def supported_devices() -> list:
    groups = [
        D.section([
            D.row([D.heading("MAGNETIC KEYBOARDS", level=2, size="20px",
                             color="#FFFFFF"),
                   D.badge("10")], gap="12px", wrap=False),
            D.row([_device_card(*k) for k in KEYBOARDS[:4]], gap="20px",
                  width=W, wrap=False, align="stretch"),
            D.row([_device_card(*k) for k in KEYBOARDS[4:]], gap="20px",
                  width=W, wrap=False, align="stretch"),
        ], tag="section", pad="0 40px 48px", gap="20px"),
    ]
    return [
        page_header("Supported Devices", "WebHID-ready IPI devices can be tuned right "
                                         "here — lighting, key mapping and magnetic "
                                         "switch parameters. No local software required.",
                    crumb="DEVICE SUPPORT"),
        *groups,
    ]


DRIVERS = [
    ("Driver 1.4.2", "Windows 10/11 · WebHID + local fallback", "24.8 MB", "2026-09-12"),
    ("Driver 1.4.1", "Windows 10/11 · rollback build", "24.6 MB", "2026-08-02"),
    ("Firmware 2.6.0", "AURORA / HAZEX / FLY · Rapid Trigger v2", "1.2 MB", "2026-09-30"),
    ("Firmware 2.5.4", "AURORA / HAZEX · rollback build", "1.2 MB", "2026-07-18"),
]


def driver() -> list:
    rows = []
    for name, desc, size, date in DRIVERS:
        rows.append(D.row([
            D.col([D.text(name, size="16px", weight="700", color="#FFFFFF",
                          rich=False),
                   D.text(desc, size="13px", color="#8A7FA8", rich=False)],
                  gap="6px", width="520px", align="flex-start"),
            D.text(size, size="13px", color="#9A90B8", rich=False),
            D.text(date, size="13px", color="#9A90B8", rich=False),
            D.button("Download", "/driver", variant="ghost", size="sm"),
        ], gap="32px", width="100%", justify="space-between", wrap=False,
            align="center"))
    return [
        page_header("Driver", "WebHID tuning runs in the browser — the local driver is "
                              "only needed for firmware updates and offline profiles.",
                    crumb="FIRMWARE & DRIVERS"),
        D.section([
            D.col(rows, gap="0", width=W,
                  bg="#0D0B15", radius="16px", pad="8px 24px",
                  border=("1px", "rgba(255,255,255,0.08)")),
        ], tag="section", pad="0 40px 56px"),
        D.section([
            D.heading("Connect your device", level=2, size="26px", color="#FFFFFF"),
            D.text("Plug the board in, open the driver in Chrome or Edge and press "
                   "Add Device. Profiles are stored on the board, so they follow you "
                   "between machines.", size="15px", color="#A99FC6", width="720px"),
            D.button("Open WebHID Driver", "/driver"),
        ], tag="section", pad="0 40px 120px", gap="16px", width=W,
            align="flex-start"),
    ]


PAGES = [
    ("catalog", "/catalog", "Catalog — IPI",
     "The full IPI lineup: magnetic-switch keyboards, hollow-body mice and matched bundles.",
     catalog),
    ("blog", "/blog", "Blog — IPI",
     "Field notes from the IPI lab: firmware, tolerances, and the milliseconds that decide a round.",
     blog),
    ("contact", "/contact", "Contact — IPI",
     "Hardware questions, dealer enquiries, RMA — the IPI team answers every message within one business day.",
     contact),
    ("about", "/about", "About — IPI",
     "IPI engineers true 8K magnetic-switch keyboards and esports mice in Brooklyn, NY.",
     about),
    ("services", "/services", "Services — IPI",
     "Firmware, tooling and esports support — where the milliseconds are won.",
     services),
    ("portfolio", "/portfolio", "Portfolio — IPI",
     "Selected work from the IPI lab: six products, one tolerance stack.",
     portfolio),
    ("portfolio-single-page", "/portfolio-single-page", "AURORA 65 — IPI",
     "Case study: a 65% magnetic-switch deck machined from a single aluminium billet.",
     portfolio_single),
    ("cart", "/cart", "Cart — IPI", "Your IPI cart.", cart),
    ("checkout", "/checkout", "Checkout — IPI", "Checkout at IPI.", checkout),
    ("my-account", "/my-account", "My account — IPI",
     "Log in to track orders, manage warranties and pull your device profiles.",
     my_account),
    ("supported-devices", "/supported-devices", "Supported Devices — IPI",
     "WebHID-ready IPI devices can be tuned right here — lighting, key mapping and magnetic switch parameters.",
     supported_devices),
    ("driver", "/driver", "Driver — IPI",
     "WebHID tuning in the browser, plus firmware updates for every IPI board.",
     driver),
]


def main() -> None:
    C.login()
    st = json.load(open(STATE, encoding="utf-8"))
    pid = st["projectId"]
    pages = C.ok(f"/api/page/list?projectId={pid}", method="GET") or []
    by_path = {p.get("draftPath"): p for p in pages if p.get("kind") == "page"}
    only = sys.argv[1:] or [name for name, *_ in PAGES]
    for name, path, title, desc, fn in PAGES:
        if name not in only:
            continue
        doc = D.doc(fn(), title=title, description=desc)
        # contentTargetId 是 uuid 列：用路径派生稳定 uuid，重跑不变（幂等）。
        target = {"contentTargetType": "page",
                  "contentTargetId": str(uuid.uuid5(uuid.NAMESPACE_URL,
                                                    "ipi://page" + path))}
        cur = by_path.get(path)
        if cur:
            C.ok("/api/page/draft/save", {"id": cur["id"],
                                          "expectedVersion": cur["draftVersion"],
                                          "draftPath": path, "draftDocument": doc})
            pg = cur["id"]
        else:
            created = C.ok("/api/page/create", {"projectId": pid, "kind": "page",
                                                "draftPath": path, "draftDocument": doc,
                                                **target})
            pg = created["id"] if isinstance(created, dict) else created
        C.ok("/api/page/build", {"id": pg})
        C.ok("/api/page/publish", {"id": pg, "lang": "zh-CN", "allLangs": True})
        print("ok", name, path, pg)


if __name__ == "__main__":
    main()
