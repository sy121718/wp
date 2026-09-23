"""商品导入：WP WooCommerce 商品（含变体）→ go_wp 商品。

用法：
  python3 scripts/wp-import/product_import.py            # 全量
  python3 scripts/wp-import/product_import.py 3          # 只导前 3 个（试跑）
产物：tmp/wp-import/map/products.json  {wpPostId: {id, slug, variants: [...]}}
"""
from __future__ import annotations

import json
import os
import re
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


def call(path, payload):
    """带限流退避的 POST。"""
    for i in range(5):
        throttle()
        code, body = C.api(path, payload)
        if body.get("code") == 200:
            return body.get("data")
        msg = str(body.get("message", ""))
        if "频繁" in msg:
            time.sleep(6 * (i + 1))
            continue
        raise RuntimeError(f"{path} → {msg[:200]}")
    raise RuntimeError(f"{path} → 限流重试耗尽")


# ---------- WP 数据装载 ----------

class WP:
    def __init__(self):
        self.posts = C.load("posts.ndjson")
        self.meta = C.load("postmeta.ndjson")
        self.tts = C.load("term_taxonomy.ndjson")
        self.terms = {t["term_id"]: t for t in C.load("terms.ndjson")}
        self.rel = C.load("term_relationships.ndjson")
        self.M = {}
        for m in self.meta:
            self.M.setdefault(m["post_id"], {})[m["meta_key"]] = m["meta_value"]
        self.tt_by_id = {str(t["term_taxonomy_id"]): t for t in self.tts}
        # object_id -> [term_taxonomy_id]
        self.obj_terms = {}
        for r in self.rel:
            self.obj_terms.setdefault(r["object_id"], []).append(str(r["term_taxonomy_id"]))

    def tax_terms(self, post_id, taxonomy):
        out = []
        for ttid in self.obj_terms.get(post_id, []):
            t = self.tt_by_id.get(ttid)
            if t and t["taxonomy"] == taxonomy:
                out.append(ttid)
        return out


def num(v):
    try:
        f = float(v)
        return f if f > 0 else None
    except (TypeError, ValueError):
        return None


def main():
    limit = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    C.login()
    wp = WP()
    terms_map = C.load_map("terms.json")
    media = C.load_map("media.json")
    by_url = media.get("byUrl", {})
    by_id = media.get("byId", {})
    out = C.load_map("products.json")

    def media_url(att_id):
        e = by_id.get(str(att_id))
        return e["url"] if e else ""

    products = [p for p in wp.posts if p["post_type"] == "product" and p["post_status"] == "publish"]
    products.sort(key=lambda p: p["ID"])
    if limit:
        products = products[:limit]
    variations = [p for p in wp.posts if p["post_type"] == "product_variation"]
    var_by_parent = {}
    for v in variations:
        var_by_parent.setdefault(v["post_parent"], []).append(v)

    okc = failc = 0
    for post in products:
        pid = post["ID"]
        if str(pid) in out:
            continue
        m = wp.M.get(pid, {})
        title = (post["post_title"] or "").strip()
        slug = (post["post_name"] or "").strip()
        if not title:
            continue
        # 图片：主图 + 相册
        images, alts = [], []
        thumb = m.get("_thumbnail_id")
        if thumb and media_url(thumb):
            images.append(media_url(thumb))
            alts.append((by_id.get(str(thumb)) or {}).get("alt", "") or title)
        gal = m.get("_product_image_gallery") or ""
        for gid in [x for x in str(gal).split(",") if x.strip()]:
            u = media_url(gid.strip())
            if u and u not in images:
                images.append(u)
                alts.append((by_id.get(gid.strip()) or {}).get("alt", "") or "")
        # 分类 / 品牌 / 标签 / 属性
        cat_ids, primary = [], ""
        for ttid in wp.tax_terms(pid, "product_cat"):
            e = terms_map.get(ttid)
            if e and e.get("kind") == "category":
                cat_ids.append(e["id"])
        if cat_ids:
            primary = cat_ids[0]
        brand = ""
        for ttid in wp.tax_terms(pid, "product_brand"):
            e = terms_map.get(ttid)
            if e and e.get("kind") == "brand":
                brand = e["id"]
                break
        tag_ids = [terms_map[t]["id"] for t in wp.tax_terms(pid, "product_tag")
                   if terms_map.get(t, {}).get("kind") == "tag"]
        attr_ids = []
        for ttid in wp.tax_terms(pid, "pa_flavour") + wp.tax_terms(pid, "pa_puff-count") + wp.tax_terms(pid, "pa_bundle-size"):
            t = wp.tt_by_id.get(ttid)
            if not t:
                continue
            holder = terms_map.get("attr:" + t["taxonomy"])
            if holder and holder["id"] not in attr_ids:
                attr_ids.append(holder["id"])
        price = num(m.get("_price"))
        regular = num(m.get("_regular_price"))
        content = post["post_content"] or ""
        payload = {
            "projectId": P,
            "name": title,
            "slug": slug,
            "description": {"html": content},
            "seoTitle": m.get("_yoast_wpseo_title") or title,
            "seoDescription": m.get("_yoast_wpseo_metadesc") or "",
            "images": images,
            "imageAlts": alts,
            "categoryIds": cat_ids,
            "primaryCategoryId": primary,
            "brandId": brand,
            "tagIds": tag_ids,
            "attributeIds": attr_ids,
            "defaultPrice": price or 0,
            "defaultImage": images[0] if images else "",
        }
        sku = (m.get("_sku") or "").strip()
        if sku:
            payload["sku"] = sku
        try:
            data = call("/api/product/create", payload)
        except Exception as e:  # noqa: BLE001
            print("  FAIL create", slug, str(e)[:160])
            failc += 1
            continue
        goid = data["id"]
        rec = {"id": goid, "slug": slug, "variants": []}
        # 首个变体：补划线价 / 库存 / 图片
        try:
            full = C.api(f"/api/product/get?projectId={P}&id={goid}", method="GET")[1].get("data") or {}
            vs = full.get("variants") or []
        except Exception:  # noqa: BLE001
            vs = []
        vlist = var_by_parent.get(pid, [])
        if not vlist:
            # 简单商品：首个变体就是它自己
            if vs:
                vid = vs[0]["id"]
                upd = {"id": vid, "projectId": P, "price": price or 0}
                if regular and price and regular > price:
                    upd["comparePrice"] = regular
                elif regular and not price:
                    upd["price"] = regular
                if images:
                    upd["image"] = images[0]
                if sku:
                    upd["skuCode"] = sku
                try:
                    call("/api/product/variant/update", upd)
                except Exception as e:  # noqa: BLE001
                    print("  WARN variant update", slug, str(e)[:120])
                rec["variants"].append(vid)
        else:
            # 可变商品：删掉自动生成的首个占位变体，逐个建真实变体
            if vs:
                try:
                    call("/api/product/variant/delete", {"id": vs[0]["id"], "projectId": P})
                except Exception as e:  # noqa: BLE001
                    print("  WARN variant delete", slug, str(e)[:120])
            for i, v in enumerate(sorted(vlist, key=lambda x: x["menu_order"])):
                vm = wp.M.get(v["ID"], {})
                ov = {}
                for k, val in vm.items():
                    if k.startswith("attribute_"):
                        key = k[len("attribute_"):]
                        key = key[3:] if key.startswith("pa_") else key
                        ov[key] = val
                vp = num(vm.get("_price")) or price or 0
                vr = num(vm.get("_regular_price"))
                vimg = media_url(vm.get("_thumbnail_id")) if vm.get("_thumbnail_id") else (images[0] if images else "")
                vpayload = {"productId": goid, "projectId": P, "price": vp, "optionValues": ov,
                            "enabled": True, "sort": i}
                if vr and vr > vp:
                    vpayload["comparePrice"] = vr
                if vimg:
                    vpayload["image"] = vimg
                vsku = (vm.get("_sku") or "").strip()
                if vsku:
                    vpayload["skuCode"] = vsku
                try:
                    vd = call("/api/product/variant/create", vpayload)
                    rec["variants"].append(vd["id"])
                except Exception as e:  # noqa: BLE001
                    print("  FAIL variant", slug, ov, str(e)[:140])
        # 上架
        try:
            call("/api/product/update", {"id": goid, "projectId": P, "status": "published"})
        except Exception as e:  # noqa: BLE001
            print("  WARN publish", slug, str(e)[:120])
        out[str(pid)] = rec
        okc += 1
        if okc % 10 == 0:
            C.save_map("products.json", out)
            print(f"  已导入 {okc}（失败 {failc}）", flush=True)
    C.save_map("products.json", out)
    print(f"完成：成功 {okc}，失败 {failc}，总计 {len(out)}")


if __name__ == "__main__":
    main()
