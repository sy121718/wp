"""IPI 文档构造器：把「一段文案 + 一张图」写成构建器能吃的 AST。

只做三件事：结构（行/列/版心）、文字（标题/正文/按钮）、图片。
所有颜色与圆角都留给主题 token，文档里出现的色值只有「与 token 不等价的那几个」
（渐变文字、霓虹描边），这样换主题时整站才会跟着走。
"""
from __future__ import annotations

import json
import os
import uuid

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
STATE = os.path.join(ROOT, "tmp", "ipi", "state.json")

_seq = [0]


def nid(prefix: str = "n") -> str:
    """节点 ID 用递增序号：确定性构建要求同一输入产生同一份字节。"""
    _seq[0] += 1
    return f"{prefix}{_seq[0]:03d}"


def reset_ids(start: int = 0) -> None:
    _seq[0] = start


def media_map() -> dict:
    with open(STATE, encoding="utf-8") as f:
        return json.load(f).get("media", {})


M = None


def url(name: str) -> str:
    global M
    if M is None:
        M = media_map()
    return M.get(name, "")


def node(t: str, props: dict, children: list | None = None) -> dict:
    n = {"id": nid(), "type": t, "props": props}
    if children:
        n["children"] = children
    return n


def interaction(*, hover: str | None = None, loop: str | None = None,
                entrance: str | None = None, delay: float = 0.0,
                scroll_story: str | None = None,
                active: str | None = None) -> dict:
    """动效组（InteractionProps）：词汇表由构建器白名单约束，写错会被校验拒掉。"""
    out: dict = {}
    if entrance:
        out["entrance"] = entrance
        if delay:
            out["entranceDelay"] = delay
    if scroll_story:
        out["scrollStory"] = scroll_story
    if hover:
        out["hoverEffect"] = hover
    if loop:
        out["loopEffect"] = loop
    if active:
        out["activeEffect"] = active
    return out


# ---------------------------------------------------------------- 布局

def box(pad: str | None = None, pad_x: str | None = None, max_width: str | None = None,
        center: bool = False, min_h: str | None = None) -> dict:
    b: dict = {}
    if pad:
        b["padding"] = {"desktop": pad, "tablet": pad, "mobile": pad}
    elif pad_x:
        b["padding"] = {"desktop": f"0 {pad_x}", "tablet": f"0 {pad_x}", "mobile": "0 20px"}
    if max_width:
        b["maxWidth"] = max_width
        b["center"] = center
    if min_h:
        b["minHeight"] = min_h
    return b


def visual(bg: str | None = None, gradient: str | None = None, bg_image: str | None = None,
           radius: str | None = None, border: tuple | None = None, shadow: str | None = None,
           overflow: str | None = None) -> dict:
    v: dict = {}
    if bg:
        v["bgColor"] = bg
    if gradient:
        v["bgGradient"] = gradient
    if bg_image:
        v["bgImage"] = bg_image
        v["bgSize"] = "cover"
        v["bgPosition"] = "center"
        v["bgRepeat"] = "no-repeat"
    if radius:
        v["radius"] = radius
    if border:
        w, c = border
        v["borderWidth"] = w
        v["borderStyle"] = "solid"
        v["borderColor"] = c
    if shadow:
        v["shadow"] = shadow
    if overflow:
        v["overflow"] = overflow
    return v


def section(children: list, *, tag: str = "section", bg: str | None = None,
            gradient: str | None = None, bg_image: str | None = None,
            pad: str | None = None, width: str | None = None, gap: str = "28px",
            align: str = "stretch", justify: str = "flex-start",
            radius: str | None = None, border: tuple | None = None,
            direction: str = "column", wrap: bool = False, min_h: str | None = None,
            max_width: str | None = None, custom_id: str | None = None) -> dict:
    """一段页面区块：外层吃满宽度（背景色/背景图），内层版心由 max_width 收窄。"""
    inner = node("core.container", {
        "tag": "div",
        "layout": {"engine": "flex", "flex": {
            "direction": direction, "gap": gap, "align": align,
            "justify": justify, "wrap": wrap}},
        "box": box(pad=pad, max_width=width or max_width, center=bool(width or max_width)),
        "visual": {},
    }, children)
    if not (width or max_width):
        return node("core.container", {
            "tag": tag,
            "layout": {"engine": "flex", "flex": {"direction": direction, "gap": gap,
                                                 "align": align, "justify": justify,
                                                 "wrap": wrap}},
            "box": box(pad=pad, min_h=min_h),
            "visual": visual(bg=bg, gradient=gradient, bg_image=bg_image, radius=radius,
                             border=border),
        }, children)
    outer_props: dict = {
        "tag": tag,
        "layout": {"engine": "flex", "flex": {"direction": "column",
                                             "align": "stretch"}},
        # 内层已经吃了 pad：外层再补一次就是双倍留白（暗底上直接看出色带）。
        "box": box(min_h=min_h),
        "visual": visual(bg=bg, gradient=gradient, bg_image=bg_image, radius=radius,
                         border=border),
    }
    if custom_id:
        outer_props["advanced"] = {"customId": custom_id}
    return node("core.container", outer_props, [inner])


def row(children: list, *, gap: str = "24px", align: str = "center",
        justify: str = "flex-start", wrap: bool = True, width: str | None = None,
        tag: str = "div", hover_effect: str | None = None,
        entrance: str | None = None, delay: float = 0.0) -> dict:
    props: dict = {
        "tag": tag,
        "layout": {"engine": "flex", "flex": {
            "direction": "row", "gap": gap, "align": align, "justify": justify,
            "wrap": wrap}},
        "box": box(max_width=width, center=bool(width)),
        "visual": {},
    }
    inter = interaction(hover=hover_effect, entrance=entrance, delay=delay)
    if inter:
        props["interaction"] = inter
    return node("core.container", props, children)


def col(children: list, *, gap: str = "16px", align: str = "flex-start",
        justify: str = "flex-start", width: str | None = None,
        bg: str | None = None, radius: str | None = None, pad: str | None = None,
        border: tuple | None = None, flex: str | None = None,
        tag: str = "div", align_self: str | None = None,
        hover_effect: str | None = None, loop: str | None = None,
        entrance: str | None = None, delay: float = 0.0,
        scroll_story: str | None = None, stretch: bool = False,
        push_right: bool = False) -> dict:
    props = {
        "tag": tag,
        "layout": {"engine": "flex", "flex": {"direction": "column", "gap": gap,
                                             "align": align, "justify": justify}},
        "box": box(pad=pad, max_width=None),
        "visual": visual(bg=bg, radius=radius, border=border),
    }
    if width:
        props["box"]["maxWidth"] = width
    if stretch:
        # 只写 maxWidth 时，列作为 flex 成员宽是 content-sized，里面的 width:100%
        # 输入框会被一起压窄。用 box.width 定宽（不带 margin-inline:auto），
        # 不用 box.center —— 后者的自动边距会吸走主轴剩余空间、破坏 space-between。
        props["box"]["width"] = width
        props["box"].pop("maxWidth", None)
    if push_right:
        props["box"]["margin"] = {"desktop": "0 0 0 auto", "tablet": "0 0 0 auto",
                                  "mobile": "0"}
    if flex:
        props["styleEx"] = {"flex": flex}
    if align_self:
        props["box"]["margin"] = {"desktop": "0", "tablet": "0", "mobile": "0"}
    # container 的动效是 props 顶层 interaction（container.go:107），不是 advanced.interaction ——
    # 写到 advanced 里会被静默丢弃，卡片的上浮/漂浮/入场全不生效。
    inter = interaction(hover=hover_effect, loop=loop, entrance=entrance,
                        delay=delay, scroll_story=scroll_story)
    if inter:
        props["interaction"] = inter
    return node("core.container", props, children)


# ---------------------------------------------------------------- 文字

def heading(text: str, *, level: int = 2, size: str = "40px", size_mobile: str | None = None,
            color: str | None = None, weight: str = "800", lh: str = "1.08",
            align: str | None = None, spacing: str | None = None,
            subtitle: str | None = None, subtitle_color: str | None = None,
            subtitle_size: str | None = None, transform: str | None = None,
            width: str | None = None, gradient: str | None = None) -> dict:
    p: dict = {
        "text": text,
        "tag": f"h{level}",
        "weight": weight,
        # heading.css 自带 margin-bottom: 0.4em；排版间距由外层 gap 统一给，
        # 保留它会让「四行 H1」被撑成四段 26px 的空档。
        "advanced": {"margin": {"desktop": {"top": "0", "bottom": "0"},
                                "tablet": {"top": "0", "bottom": "0"},
                                "mobile": {"top": "0", "bottom": "0"}}},
        "typography": {
            "desktop": {"fontSize": size, "lineHeight": lh,
                        **({"textAlign": align} if align else {})},
            "mobile": {"fontSize": size_mobile or "28px"},
        },
    }
    if color:
        p["color"] = color
    if spacing:
        p["letterSpacing"] = spacing
    if transform:
        p["transform"] = transform
    if align:
        p["align"] = {"desktop": align, "tablet": align, "mobile": align}
    if width:
        p["width"] = {"desktop": width, "tablet": width, "mobile": "100%"}
    if gradient:
        p["advanced"]["textGradient"] = gradient
    if subtitle:
        p["subtitle"] = subtitle
        p["subtitleColor"] = subtitle_color or "var(--sky-c-accent)"
        p["subtitleFontSize"] = subtitle_size or "13px"
        p["subtitleFontWeight"] = "700"
        p["subtitleSpacing"] = "14px"
    return node("core.heading", p)


def text(body: str, *, size: str = "16px", color: str | None = None,
         lh: str = "1.75", align: str | None = None, weight: str | None = None,
         width: str | None = None, rich: bool = True,
         link_color: str | None = None) -> dict:
    p: dict = {
        "mode": "richtext" if rich else "plaintext",
        "text": body,
        "advanced": {"margin": {"desktop": {"top": "0", "bottom": "0"},
                                "tablet": {"top": "0", "bottom": "0"},
                                "mobile": {"top": "0", "bottom": "0"}}},
        "typography": {
            "desktop": {"fontSize": size, "lineHeight": lh,
                        **({"textAlign": align} if align else {})},
            "mobile": {"fontSize": size},
        },
    }
    if color:
        p["color"] = color
    # 富文本里的 <a> 不吃主题链接色（渲染成浏览器默认蓝）：正文链接色必须显式给。
    if link_color:
        p["linkColor"] = link_color
    if width:
        p["advanced"] = {"widthMode": "fixed", "widthValue": width}
    return node("core.text", p)


def button(label: str, href: str = "", *, variant: str = "solid",
           size: str = "md") -> dict:
    if href.startswith("#"):
        action, value = "anchor", href[1:]
    elif href.startswith("/"):
        action, value = "internal", href
    else:
        action, value = "external", href or "/"
    if variant == "solid":
        props = {
            "text": label, "action": action,
            "value": value, "size": size, "fontSize": "13px",
            "fontWeight": "700", "transform": "uppercase", "letterSpacing": "1px",
            "bg": "var(--sky-c-primary)", "textColor": "#FFFFFF",
            "radiusTL": "999px", "radiusTR": "999px",
            "radiusBR": "999px", "radiusBL": "999px",
        }
    else:
        props = {
            "text": label, "action": action,
            "value": value, "size": size, "fontSize": "13px",
            "fontWeight": "700", "transform": "uppercase", "letterSpacing": "1px",
            "bg": "rgba(0,0,0,0)", "textColor": "var(--sky-c-text)",
            "borderWidth": "1px", "borderStyle": "solid",
            "borderColor": "rgba(255,255,255,0.22)",
            "radiusTL": "999px", "radiusTR": "999px",
            "radiusBR": "999px", "radiusBL": "999px",
        }
    props["advanced"] = {"interaction": interaction(hover="glow", active="press")}
    return node("core.button", props)


def image(name_or_url: str, alt: str, *, width: str | None = None,
          ratio: str | None = None, radius: str | None = None,
          fit: str = "cover", max_width: str | None = None,
          align: str | None = None, hover_scale: str | None = None) -> dict:
    src = name_or_url if name_or_url.startswith(("/", "http")) else url(name_or_url)
    p: dict = {"src": src, "alt": alt, "objectFit": fit}
    if width:
        p["width"] = width
    if max_width:
        p["maxWidth"] = max_width
    if ratio:
        p["aspectRatio"] = ratio
    if radius:
        p["borderRadius"] = radius
    if align:
        p["align"] = {"desktop": align, "tablet": align, "mobile": align}
    if hover_scale:
        p["hover"] = {"scale": hover_scale, "duration": "400ms"}
    return node("core.image", p)


def badge(label: str, variant: str = "outline") -> dict:
    return node("core.badge", {"text": label, "variant": variant,
                              "color": "rgba(255,255,255,0.14)"})


def marquee(items: list[str], *, bg: str = "#7C3AED", color: str = "#FFFFFF",
            size: str = "34px", speed: float = 22) -> dict:
    body = "  •  ".join(items)
    return node("core.marquee", {
        "speed": speed, "direction": "left", "background": bg, "padding": "18px 0",
        "advanced": {},
    }, [node("core.heading", {
        "text": body, "tag": "div", "weight": "800", "color": color,
        "typography": {"desktop": {"fontSize": size}, "mobile": {"fontSize": "20px"}},
        "transform": "uppercase", "letterSpacing": "1px",
    })])


def counter(value: str, label: str, *, size: str = "38px", color: str | None = None) -> dict:
    """counter 组件要数字，但 IPI 的指标是 '0.08ms' / '32K' 这种带单位的串；
    这里用 heading 直出，保留原始写法，不为了套组件把文案拆成 数字+后缀 两截。"""
    return col([
        heading(value, level=3, size=size, weight="800",
                color=color or "var(--sky-c-heading)", align="center", lh="1"),
        text(label, size="12px", align="center", color="#8A7FA8", rich=False),
    ], gap="10px", align="center", width="220px")


def divider(color: str = "rgba(255,255,255,0.08)", width: str = "100%") -> dict:
    # 宽度必须显式给：flex column（align:flex-start）里 auto 宽的 <hr> 会收缩成一个点。
    return node("core.divider", {"weight": "1px", "color": color,
                                 "width": {"desktop": width, "tablet": width,
                                           "mobile": width},
                                 "advanced": {}})


def spacer(height: str = "40px") -> dict:
    return node("core.spacer", {"height": {"desktop": height, "tablet": height,
                                           "mobile": "28px"}})


def doc(root: list, *, title: str, description: str = "",
        schema: str = "WebPage") -> dict:
    return {
        "settings": {
            "layout": {"mode": "full"},
            "seo": {"schemaType": schema, "title": title, "description": description},
        },
        "root": root,
    }
