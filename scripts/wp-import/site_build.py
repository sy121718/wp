"""站点装配：内容模板（归档）/ 站点槽位 / 导航菜单。

用法：python3 scripts/wp-import/site_build.py
产物：tmp/wp-import/map/nav.json
"""
from __future__ import annotations

import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

P = C.project_id()
INTERVAL = 60.0 / 90.0
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


def wrap(root, title="", desc=""):
    seo = {"schemaType": "WebPage"}
    if title:
        seo["title"] = title
    if desc:
        seo["description"] = desc
    return {"settings": {"layout": {"mode": "full"}, "seo": seo}, "root": root}


def archive_doc(kind, title):
    """归档模板：标题 + 按归档上下文筛选的商品列表（filterFromArchive=on）。"""
    return wrap([{
        "id": f"archive-{kind}-sec",
        "type": "core.container",
        "props": {
            "tag": "section",
            "layout": {"engine": "flex", "flex": {"direction": "column", "gap": "24px"}},
            "box": {"padding": {"desktop": "48px 24px", "tablet": "40px 20px", "mobile": "28px 16px"}},
        },
        "children": [
            {"id": f"archive-{kind}-h", "type": "core.heading",
             "props": {"tag": "h1", "text": title, "typography": {
                 "desktop": {"fontSize": "34px"}, "tablet": {"fontSize": "30px"}, "mobile": {"fontSize": "26px"}}}},
            {"id": f"archive-{kind}-list", "type": "core.productList",
             "props": {"collectionSource": "content:product", "collectionLimit": 24, "columns": "4",
                       "layout": "grid", "pageSize": 12, "filterFromArchive": "on",
                       "filters": "categories,brands,attributes", "toolbar": "sort,pageSize",
                       "priceSlider": "off", "currency": "$",
                       "imageField": "item.images", "titleField": "item.name",
                       "priceField": "item.priceRange", "comparePriceField": "item.comparePrice",
                       "linkField": "item.url", "emptyText": "暂无商品"}},
        ],
    }], title)


def main():
    C.login()
    pages = C.load_map("pages.json")
    out = C.load_map("nav.json")

    # --- 1. 站点槽位 ---
    for slot, key in (("shop", "shop"), ("blog", "blog")):
        p = pages.get(key)
        if not p:
            print(f"  槽位 {slot}: 缺页面 {key}，跳过")
            continue
        try:
            call("/api/page/site-slot/bind", {"projectId": P, "slot": slot, "pageId": p["id"]})
            print(f"  槽位 {slot} → {key}")
        except Exception as e:  # noqa: BLE001
            print(f"  槽位 {slot} 绑定失败: {str(e)[:160]}")

    # --- 2. 归档模板（分类 / 品牌 / 标签）---
    tpl_ids = out.get("templates", {})
    for et, title in (("product_category", "分类"), ("product_brand", "品牌"), ("product_tag", "标签")):
        if et in tpl_ids:
            continue
        try:
            d = call("/api/contenttemplate/create", {
                "projectId": P, "entityType": et, "name": f"{title}归档页",
                "templateRole": "archive", "draftDocument": archive_doc(et.replace("product_", ""), title),
            })
            tpl_ids[et] = d["id"]
            print(f"  归档模板 {et} → {d['id']}")
        except Exception as e:  # noqa: BLE001
            print(f"  归档模板 {et} 失败: {str(e)[:200]}")
    out["templates"] = tpl_ids
    C.save_map("nav.json", out)

    # --- 3. 触发分类归档实例（更新分类会让 archiveEnsurer 建归档页）---
    cats = C.load_map("terms.json")
    for ttid, e in cats.items():
        if e.get("kind") != "category":
            continue
        try:
            call("/api/product/category/update", {"id": e["id"], "projectId": P, "slug": e["slug"]})
        except Exception as ex:  # noqa: BLE001
            print(f"  归档实例 {e['name']} 失败: {str(ex)[:140]}")

    # --- 4. 导航 ---
    navs = out.get("nav", {})
    header = [
        ("Shop", "/shop"),
        ("Amigo", "/product_category/amigo"),
        ("Go 35000", "/product_category/go-35000"),
        ("Alibarbar", "/product_category/alibarbar"),
        ("Ingot 9000", "/product_category/ingot-9000"),
        ("Iget", "/product_category/iget"),
        ("IGET Bar Pro 10000", "/product_category/iget-bar-pro-10000"),
        ("IGET One 12000", "/product_category/iget-one-12000"),
        ("KUZ", "/product_category/kuz"),
        ("Blog", "/blog"),
    ]
    footer = [
        ("About Us", "/about"),
        ("Contact", "/contact"),
        ("Shipping Policy", "/shipping-policy"),
        ("Returns & Refunds", "/returns-refunds"),
        ("Privacy Policy", "/privacy-policy"),
        ("Terms & Conditions", "/terms-conditions"),
        ("Payment Methods", "/payment-methods"),
        ("Verify Product", "/verify-product"),
    ]
    for kind, items in (("header", header), ("footer", footer)):
        if kind in navs:
            continue
        ids = []
        for i, (title, path) in enumerate(items):
            try:
                d = call("/api/navigation/create", {
                    "projectId": P, "title": title, "path": path, "kind": kind,
                    "sortOrder": i, "sourceType": "custom", "target": "self",
                })
                ids.append(d["id"])
            except Exception as e:  # noqa: BLE001
                print(f"  导航 {kind}/{title} 失败: {str(e)[:140]}")
        navs[kind] = ids
        print(f"  导航 {kind}: {len(ids)} 项")
    out["nav"] = navs
    C.save_map("nav.json", out)
    print("完成")


if __name__ == "__main__":
    main()
