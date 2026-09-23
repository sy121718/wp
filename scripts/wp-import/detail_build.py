"""详情页生成：给每个商品 / 文章建自动发布实例（presentation_instances）。

用法：
  python3 scripts/wp-import/detail_build.py            # 全量
  python3 scripts/wp-import/detail_build.py 2          # 只建 2 个试跑
  python3 scripts/wp-import/detail_build.py --kind product

背景：商品卡与文章卡的链接字段（item.url / product.url）来自 publishedLocator ——
**实体没有已上线的详情页实例时它是空的**，卡片就没有 href，表现为「点不进去」。
所以建完商品与文章之后，必须再给每个实体建一次详情页实例。

路径规则取 internal/siteurl 的默认模式：商品 /products/{slug}、文章 /blog/{slug}。
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
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    limit = int(args[0]) if args else 0
    kinds = ["product", "article"]
    for a in sys.argv[1:]:
        if a.startswith("--kind="):
            kinds = [a.split("=", 1)[1]]

    C.login()
    out = C.load_map("details.json")
    products = C.load_map("products.json")
    articles = C.load_map("articles.json")

    # 路径规则与站点设置 SiteSettings.urlPatterns 一致（这里同步一份是为了试跑时
    # 不必先读工程设置）。**必须与站点配置同源**：发布时显式传入的路径优先，
    # 传成别的模式会让「后台预填的路径」和「实际线上路径」对不上。
    #   · 文章 → /{slug}（与源站一致：文章在根下，博客列表在 /blog）
    #   · 商品 → /product/{slug}
    # 注意 /blog/{slug} 这种默认模式在本站**不可用**：列表页已经占用 /blog 这一条
    # 激活符号链接，而激活目录禁止父子路径共存（父路径被 symlink 占位后无法再建子条目）。
    jobs = []
    if "product" in kinds:
        for wp_id, rec in products.items():
            jobs.append(("product", rec["id"], rec["slug"], "/product/" + rec["slug"]))
    if "article" in kinds:
        for wp_id, rec in articles.items():
            jobs.append(("article", rec["id"], rec["slug"], "/" + rec["slug"]))
    jobs.sort(key=lambda x: (x[0], x[3]))
    if limit:
        jobs = jobs[:limit]

    ok = skip = fail = 0
    for entity_type, entity_id, slug, url_path in jobs:
        key = f"{entity_type}:{entity_id}"
        if key in out:
            skip += 1
            continue
        try:
            data = call("/api/presentation/create", {
                "projectId": P, "entityType": entity_type,
                "entityId": entity_id, "urlPath": url_path,
            })
            out[key] = {"instanceId": data.get("id"), "urlPath": url_path}
            ok += 1
        except Exception as e:  # noqa: BLE001
            msg = str(e)[:160]
            # 「路径已占用 / 已存在实例」按幂等处理，不算失败。
            if "占用" in msg or "已存在" in msg or "存在" in msg:
                out[key] = {"instanceId": "", "urlPath": url_path, "note": msg}
                skip += 1
            else:
                print("  FAIL", entity_type, slug, msg)
                fail += 1
        if (ok + skip) % 20 == 0 and (ok + skip) > 0:
            C.save_map("details.json", out)
            print(f"  进度：新建 {ok} / 跳过 {skip} / 失败 {fail}", flush=True)
    C.save_map("details.json", out)
    print(f"完成：新建 {ok}，跳过 {skip}，失败 {fail}，总计 {len(out)}")


if __name__ == "__main__":
    main()
