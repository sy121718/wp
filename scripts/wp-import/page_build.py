"""页面复刻：用 go_wp 原生组件重建 vapecentralau.com 的前台页面。

用法：
  python3 scripts/wp-import/page_build.py          # 全量
  python3 scripts/wp-import/page_build.py home     # 只建某一页
产物：tmp/wp-import/map/pages.json  {key: {id, path}}

设计取舍：
  · 首页 / 商店 / 博客用**原生组件**搭（productList / cardstack / container / heading），
    不是把 WP 渲染出的 Elementor HTML 硬灌进来 —— 灌进来只会得到一堆不可编辑的壳。
  · 政策类页面是纯正文，走 core.text 富文本（编译期过全站白名单清洗）。
  · WP 页面里引用的图片有一部分是 WP 自动生成的缩放尺寸（不在附件表里），
    这里按 URL 现取现传，见 ensure_image。
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

P = C.project_id()
INTERVAL = 60.0 / 90.0
_last = [0.0]
TMP = os.path.join(C.ROOT, "tmp", "wp-import")
MEDIA = C.load_map("media.json")
BY_URL = MEDIA.get("byUrl", {})
BY_ID = MEDIA.get("byId", {})


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


def wp_media(url: str) -> str:
    """WP 图片地址 → go_wp /storage 地址；缺失时现取现传。"""
    if not url:
        return ""
    hit = BY_URL.get(url) or BY_URL.get(url.split("vapecentralau.com")[-1])
    if hit:
        return hit
    return ensure_image(url)


def ensure_image(url: str) -> str:
    """把站点上任意图片下载后上传进媒体库（覆盖 WP 自动生成的缩放尺寸）。"""
    if "vapecentralau.com" not in url:
        return url
    rel = url.split("/wp-content/uploads/")[-1]
    local = os.path.join(TMP, "media", rel)
    os.makedirs(os.path.dirname(local), exist_ok=True)
    if not os.path.exists(local) or os.path.getsize(local) == 0:
        r = subprocess.run(["curl", "-sS", "--max-time", "60", "-o", local, url], capture_output=True)
        if r.returncode != 0 or not os.path.exists(local) or os.path.getsize(local) == 0:
            return ""
    throttle()
    out = subprocess.run(["curl", "-s", "-b", C.COOKIE, "-H", f"X-CSRF-Token: {C.token()}",
                          "-F", f"file=@{local}", C.BASE + "/api/media/upload"],
                         capture_output=True, text=True).stdout
    try:
        body = json.loads(out)
    except Exception:
        return ""
    if body.get("code") != 200:
        return ""
    u = body["data"]["url"]
    BY_URL[url] = u
    BY_URL["/wp-content/uploads/" + rel] = u
    C.save_map("media.json", {"byId": BY_ID, "byUrl": BY_URL})
    return u


# ---------- 节点构造 ----------

def node(nid, ntype, props, children=None):
    n = {"id": nid, "type": ntype, "props": props}
    if children:
        n["children"] = children
    return n


def section(nid, children, padding="56px 24px", bg="", gap="24px", max_width="1280px"):
    """一个版块容器。

    版心约束走 **box.maxWidth + box.center**（容器原生能力）：
      · 不要写进 advanced —— core.container 的 Props 里**没有** Advanced 字段，
        AdvancedOf 找不到就返回 nil，那里的 widthMode/maxWidth 会被静默忽略
        （实测：模板里配了 820px，产物里一条 width 都没有）；
      · 也不要指望 StyleEx.contentWidth —— 那是「版心模式」枚举，只认 boxed/full。
    """
    box = {
        "padding": {"desktop": padding, "tablet": padding, "mobile": "32px 16px"},
    }
    if max_width:
        box["maxWidth"] = max_width
        box["center"] = True
    visual = {}
    if bg:
        visual["bgColor"] = bg
    props = {
        "tag": "section",
        "layout": {"engine": "flex", "flex": {"direction": "column", "gap": gap, "align": "stretch"}},
        "box": box,
        "visual": visual,
    }
    return node(nid, "core.container", props, children)


def heading(nid, text, tag="h2", size="30px", color="#111827", align="left", weight="700"):
    return node(nid, "core.heading", {
        "tag": tag,
        "text": text,
        "color": color,
        "weight": weight,
        "typography": {
            "desktop": {"fontSize": size, "textAlign": align},
            "tablet": {"fontSize": size, "textAlign": align},
            "mobile": {"fontSize": size, "textAlign": align},
        },
    })


def paragraph(nid, text, size="16px", color="#374151", align="left"):
    return node(nid, "core.text", {
        "mode": "plaintext",
        "text": text,
        "color": color,
        "typography": {
            "desktop": {"fontSize": size, "textAlign": align, "lineHeight": "1.8"},
            "tablet": {"fontSize": size, "textAlign": align, "lineHeight": "1.8"},
            "mobile": {"fontSize": size, "textAlign": align, "lineHeight": "1.8"},
        },
    })


def rich(nid, html):
    return node(nid, "core.text", {"mode": "richtext", "text": html})


def image(nid, src, alt="", radius="0", ratio="original"):
    return node(nid, "core.image", {
        "src": src, "alt": alt, "aspectRatio": ratio,
        "objectFit": "cover", "borderRadius": radius, "width": "100%",
    })


def product_list(nid, limit=8, columns="4", order="newest", title="", page_size=0, filters="", toolbar="", slider="off"):
    return node(nid, "core.productList", {
        "collectionSource": "content:product",
        "collectionLimit": limit,
        "columns": columns,
        "layout": "grid",
        "orderBy": order,
        "pageSize": page_size,
        "filters": filters,
        "toolbar": toolbar,
        "priceSlider": slider,
        "currency": "$",
        "imageField": "item.images",
        "titleField": "item.name",
        "priceField": "item.priceRange",
        "comparePriceField": "item.comparePrice",
        "linkField": "item.url",
        "emptyText": "暂无商品",
    })


def article_list(nid, limit=6, columns="3", layout="grid", tag="h3"):
    """文章列表（core.articleList）。

    不要用 core.cardstack 做列表：它是「卡片堆叠 / 悬停扇形展开」的展示件，
    N 张卡叠在同一个 grid cell 里，页面上只看得见最上面一张 —— 拿它当博客列表的
    结果是「文章一条都看不见、也点不进去」。文章列表是单独的组件（core.articleList）。

    链接 = linkPrefix（"/"）+ slug，经构建期站内链接本地化器处理。
    """
    return node(nid, "core.articleList", {
        "collectionSource": "content:article",
        "collectionLimit": limit,
        "layout": layout,
        "columns": columns,
        "titleTag": tag,
        "showImage": "on",
        "showExcerpt": "on",
        "linkPrefix": "/",
        "linkText": "Read more",
        "emptyText": "No articles yet",
    })


def nav_node(nid, menu="header", mobile_collapse=True):
    return node(nid, "core.nav", {
        "menu": menu,
        "orientation": "horizontal",
        "gap": "28px",
        "align": "left",
        "mobileCollapse": mobile_collapse,
        "toggleLabel": "菜单",
    })


# ---------- 页面定义 ----------

def home_doc():
    hero = wp_media("https://vapecentralau.com/wp-content/uploads/2026/09/admigo-go-3500-banner-scaled.jpg")
    return wrap([
        section("home-hero", [
            heading("home-hero-h", "Australia's Best Disposable Vapes", "h1", "44px", "#111827", "center"),
            paragraph("home-hero-p",
                      "Shop IGET, Alibarbar, Amigo Go and KUZ with fast delivery across Australia.",
                      "18px", "#4b5563", "center"),
            image("home-hero-img", hero, "Amigo Go 35000 banner", "16px"),
        ], padding="40px 24px", gap="18px"),
        section("home-new", [
            heading("home-new-h", "New Arrivals"),
            product_list("home-new-list", limit=8, columns="4", order="newest"),
        ], bg="#f9fafb"),
        section("home-best", [
            heading("home-best-h", "Best-selling products"),
            product_list("home-best-list", limit=4, columns="4", order=""),
        ]),
        section("home-blog", [
            heading("home-blog-h", "Latest Articles"),
            article_list("home-blog-list", limit=4, columns="4", tag="h4"),
        ], bg="#f9fafb"),
        section("home-cta", [
            heading("home-cta-h", "Free shipping on orders over $300", "h2", "26px", "#ffffff", "center"),
            paragraph("home-cta-p", "Discreet packaging · Australia-wide delivery · 18+ only",
                      "16px", "#d1d5db", "center"),
        ], padding="48px 24px", bg="#111827", gap="12px"),
    ], "VapeStoreOZ | Disposable Vapes Australia", "Shop IGET, Alibarbar, Amigo Go and KUZ with fast delivery across Australia.")


def shop_doc():
    return wrap([
        section("shop-head", [
            heading("shop-head-h", "Shop All Vapes", "h1", "38px"),
            paragraph("shop-head-p", "Disposable vapes, pods and accessories — filter by brand, flavour and price."),
        ], padding="40px 24px 16px", gap="12px"),
        section("shop-list", [
            product_list("shop-list-grid", limit=24, columns="4", order="",
                         page_size=12, filters="categories,brands,attributes",
                         toolbar="sort,pageSize", slider="off"),
        ], padding="8px 24px 56px"),
    ], "Shop All Vapes", "Disposable vapes, pods and accessories — filter by brand, flavour and price.")


def blog_doc():
    return wrap([
        section("blog-head", [
            heading("blog-head-h", "Vaping Guides & News", "h1", "38px"),
            paragraph("blog-head-p", "Australian vape laws, product guides and harm-reduction articles."),
        ], padding="40px 24px 16px", gap="12px"),
        section("blog-list", [
            article_list("blog-list-grid", limit=12, columns="3", tag="h3"),
        ], padding="8px 24px 56px"),
    ], "Vaping Guides & News", "Australian vape laws, product guides and harm-reduction articles.")


CONTENT_PAGES = [
    ("about", "about", "关于我们", "About VapeStoreOZ"),
    ("contact", "contact", "联系我们", "Contact Us"),
    ("privacy-policy", "privacy-policy", "隐私政策", "Privacy Policy"),
    ("returns-refunds", "returns-refunds", "退换货政策", "Returns & Refunds"),
    ("shipping-policy", "shipping-policy", "配送政策", "Shipping Policy"),
    ("terms-conditions", "terms-conditions", "条款与条件", "Terms & Conditions"),
    ("payment-methods", "payment-methods", "支付方式", "Payment Methods"),
    ("verify-product", "verify-product", "产品验证", "Verify Product"),
]


def content_doc(slug, title, html):
    body = html if len(html) <= 30000 else html[:30000]
    return wrap([
        section(slug + "-head", [heading(slug + "-h", title, "h1", "36px")],
                padding="40px 24px 8px", gap="8px"),
        section(slug + "-body", [rich(slug + "-rich", body)],
                padding="8px 24px 56px", max_width="900px"),
    ], title)


# ---------- 建页 ----------

def wp_pages():
    posts = C.load("posts.ndjson")
    return {p["post_name"]: p for p in posts if p["post_type"] == "page" and p["post_status"] == "publish"}


def wrap(root, seo_title="", seo_desc=""):
    """包成完整的 Page Document —— settings.layout.mode 是编译端必填项。"""
    seo = {"schemaType": "WebPage"}
    if seo_title:
        seo["title"] = seo_title
    if seo_desc:
        seo["description"] = seo_desc
    return {"settings": {"layout": {"mode": "full"}, "seo": seo}, "root": root}


def create_page(key, path, doc, kind="home", target_type="none", target_id=None):
    return call("/api/page/create", {
        "projectId": P, "kind": kind, "contentTargetType": target_type,
        "contentTargetId": target_id, "draftPath": path, "draftDocument": doc,
    })


def main():
    only = sys.argv[1] if len(sys.argv) > 1 else ""
    C.login()
    out = C.load_map("pages.json")
    wpp = wp_pages()

    jobs = [
        ("home", "/", home_doc),
        ("shop", "/shop", shop_doc),
        ("blog", "/blog", blog_doc),
    ]
    for key, path, fn in jobs:
        if only and only != key:
            continue
        if key in out:
            print(f"  跳过 {key}（已存在）")
            continue
        data = create_page(key, path, fn())
        out[key] = {"id": data["id"], "path": data.get("draftPath", path)}
        print(f"  页面 {key} → {data['id']} {path}")
        C.save_map("pages.json", out)

    for key, slug, cn, en in CONTENT_PAGES:
        if only and only != key:
            continue
        if key in out:
            print(f"  跳过 {key}（已存在）")
            continue
        p = wpp.get(slug)
        if not p:
            print(f"  跳过 {key}（WP 无此页）")
            continue
        title = en or p["post_title"] or cn
        data = create_page(key, "/" + slug, content_doc(key, title, p["post_content"] or ""))
        out[key] = {"id": data["id"], "path": "/" + slug}
        print(f"  页面 {key} → {data['id']} /{slug}")
        C.save_map("pages.json", out)

    C.save_map("pages.json", out)
    print(f"完成：{len(out)} 个页面")


if __name__ == "__main__":
    main()
