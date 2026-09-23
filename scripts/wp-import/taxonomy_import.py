"""分类法导入：商品分类 / 品牌 / 标签 / 属性组。

用法：python3 scripts/wp-import/taxonomy_import.py
产物：tmp/wp-import/map/terms.json  {wpTermTaxonomyId: {kind, id, name, slug}}
"""
from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

P = C.project_id()


def build():
    terms = {t["term_id"]: t for t in C.load("terms.ndjson")}
    tts = C.load("term_taxonomy.ndjson")
    out = []
    for t in tts:
        term = terms.get(t["term_id"])
        if not term:
            continue
        out.append({
            "ttid": str(t["term_taxonomy_id"]),
            "termId": str(t["term_id"]),
            "taxonomy": t["taxonomy"],
            "name": term["name"],
            "slug": term["slug"],
            "parent": str(t["parent"] or 0),
            "description": t.get("description") or "",
            "count": int(t["count"] or 0),
        })
    return out


def main():
    C.login()
    all_terms = build()
    m = C.load_map("terms.json")

    # --- 商品分类（支持父子） ---
    cats = [t for t in all_terms if t["taxonomy"] == "product_cat"]
    by_ttid = {t["ttid"]: t for t in cats}
    pending = [t for t in cats if t["ttid"] not in m]
    guard = 0
    while pending and guard < 10:
        guard += 1
        rest = []
        for t in pending:
            parent = t["parent"]
            if parent != "0" and parent in by_ttid and by_ttid[parent]["ttid"] not in m:
                rest.append(t)
                continue
            payload = {"projectId": P, "name": t["name"], "slug": t["slug"],
                       "description": t["description"], "sort": 0}
            if parent != "0" and parent in by_ttid:
                payload["parentId"] = m[by_ttid[parent]["ttid"]]["id"]
            data = C.ok("/api/product/category/create", payload)
            m[t["ttid"]] = {"kind": "category", "id": data["id"], "name": t["name"], "slug": t["slug"]}
            print("  分类", t["name"], "→", data["id"])
        pending = rest

    # --- 品牌 ---
    for t in all_terms:
        if t["taxonomy"] != "product_brand" or t["ttid"] in m:
            continue
        data = C.ok("/api/product/brand/create",
                    {"projectId": P, "name": t["name"], "slug": t["slug"], "sort": 0})
        m[t["ttid"]] = {"kind": "brand", "id": data["id"], "name": t["name"], "slug": t["slug"]}
        print("  品牌", t["name"], "→", data["id"])

    # --- 商品标签 ---
    for t in all_terms:
        if t["taxonomy"] != "product_tag" or t["ttid"] in m:
            continue
        data = C.ok("/api/product/tag/create",
                    {"projectId": P, "name": t["name"], "slug": t["slug"], "kind": "manual", "sort": 0})
        m[t["ttid"]] = {"kind": "tag", "id": data["id"], "name": t["name"], "slug": t["slug"]}
        print("  标签", t["name"], "→", data["id"])

    # --- 属性组（pa_* 全局属性，值是同一分类法下的术语） ---
    attrs = {}
    for t in all_terms:
        if t["taxonomy"].startswith("pa_"):
            attrs.setdefault(t["taxonomy"], []).append(t)
    for tax, values in attrs.items():
        key = tax[3:]
        holder = f"attr:{tax}"
        if holder in m:
            continue
        vals = [{"key": v["slug"] or v["name"], "label": v["name"], "sort": i, "enabled": True}
                for i, v in enumerate(sorted(values, key=lambda x: x["name"]))]
        data = C.ok("/api/product/attribute/create", {
            "projectId": P, "key": key, "name": key.replace("-", " ").title(),
            "isVariation": True, "sort": 0, "values": vals,
        })
        m[holder] = {"kind": "attribute", "id": data["id"], "key": key, "taxonomy": tax}
        for v in values:
            m[f"attrval:{tax}:{v['termId']}"] = {"kind": "attrvalue", "attrId": data["id"],
                                                 "key": v["slug"] or v["name"], "label": v["name"]}
        print(f"  属性组 {key} → {data['id']}（{len(vals)} 值）")

    # --- 文章分类 / 标签：go_wp content 模块没有分类法字段，仅登记备查 ---
    for t in all_terms:
        if t["taxonomy"] in ("category", "post_tag") and t["ttid"] not in m:
            m[t["ttid"]] = {"kind": "wp-only:" + t["taxonomy"], "name": t["name"], "slug": t["slug"]}

    C.save_map("terms.json", m)
    print(f"完成：{len(m)} 条映射")


if __name__ == "__main__":
    main()
