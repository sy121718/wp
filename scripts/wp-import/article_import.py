"""文章导入：WP 文章（post）→ go_wp content 实体（entityType=article）。

用法：
  python3 scripts/wp-import/article_import.py          # 全量
  python3 scripts/wp-import/article_import.py 3        # 试跑
产物：tmp/wp-import/map/articles.json  {wpPostId: {id, slug}}

已知边界：go_wp content 的字段白名单只有
title / body / excerpt / featuredImage / seoTitle / seoDescription / focusKeyword，
**没有分类与标签**，所以 WP 的 category / post_tag 无处落库（导入时只登记不写入）。
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


def call(path, payload):
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


def main():
    limit = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    C.login()
    posts = C.load("posts.ndjson")
    meta = C.load("postmeta.ndjson")
    media = C.load_map("media.json")
    by_id = media.get("byId", {})
    out = C.load_map("articles.json")
    M = {}
    for m in meta:
        M.setdefault(m["post_id"], {})[m["meta_key"]] = m["meta_value"]

    items = [p for p in posts if p["post_type"] == "post" and p["post_status"] == "publish"]
    items.sort(key=lambda p: p["ID"])
    if limit:
        items = items[:limit]

    okc = failc = 0
    for p in items:
        pid = str(p["ID"])
        if pid in out:
            continue
        m = M.get(p["ID"], {})
        title = (p["post_title"] or "").strip()
        slug = (p["post_name"] or "").strip()
        if not title or not slug:
            continue
        thumb = m.get("_thumbnail_id")
        feat = by_id.get(str(thumb), {}).get("url", "") if thumb else ""
        data = {
            "title": title,
            "body": p["post_content"] or "",
            "excerpt": p["post_excerpt"] or "",
            "seoTitle": m.get("_yoast_wpseo_title") or title,
            "seoDescription": m.get("_yoast_wpseo_metadesc") or "",
            "focusKeyword": m.get("_yoast_wpseo_focuskw") or "",
        }
        if feat:
            data["featuredImage"] = feat
        try:
            res = call("/api/content/create", {"entityType": "article", "slug": slug, "data": data})
        except Exception as e:  # noqa: BLE001
            print("  FAIL", slug, str(e)[:160])
            failc += 1
            continue
        out[pid] = {"id": res["id"], "slug": slug}
        okc += 1
        if okc % 10 == 0:
            C.save_map("articles.json", out)
            print(f"  已导入 {okc}（失败 {failc}）", flush=True)
    C.save_map("articles.json", out)
    print(f"完成：成功 {okc}，失败 {failc}，总计 {len(out)}")


if __name__ == "__main__":
    main()
