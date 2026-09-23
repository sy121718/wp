"""媒体导入：把 WP 附件逐个上传到 go_wp 媒体库，建立 URL 映射。

用法：python3 scripts/wp-import/media_upload.py
产物：tmp/wp-import/map/media.json  {byId: {wpAttachmentId: {...}}, byUrl: {wpUrl: goWpUrl}}

注意：go_wp 的 authorizedAPI 组挂了限流（config.yaml server.rate_limit_limit，默认 120/分钟），
批量上传必须自己节流 + 对 429 退避重试，否则会成批丢文件。
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

MEDIA_DIR = os.path.join(C.ROOT, "tmp", "wp-import", "media")
TSV = os.path.join(C.ROOT, "tmp", "wp-import", "media.tsv")
INTERVAL = 60.0 / 90.0
_last = [0.0]


def throttle():
    dt = time.time() - _last[0]
    if dt < INTERVAL:
        time.sleep(INTERVAL - dt)
    _last[0] = time.time()


def rows():
    out = []
    with open(TSV, encoding="utf-8") as f:
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) >= 5:
                out.append({"id": parts[0], "rel": parts[1], "title": parts[2], "alt": parts[3], "guid": parts[4]})
    return out


def upload(path: str, tries: int = 4):
    for i in range(tries):
        throttle()
        cmd = ["curl", "-s", "-b", C.COOKIE, "-H", f"X-CSRF-Token: {C.token()}",
               "-F", f"file=@{path}", C.BASE + "/api/media/upload"]
        out = subprocess.run(cmd, capture_output=True, text=True).stdout
        try:
            body = json.loads(out)
        except Exception:
            raise RuntimeError("非 JSON 响应: " + out[:150])
        if body.get("code") == 200:
            return body["data"]
        if "频繁" in str(body.get("message", "")):
            time.sleep(8 * (i + 1))
            continue
        raise RuntimeError(str(body.get("message"))[:150])
    raise RuntimeError("重试耗尽（限流）")


def main():
    C.login()
    m = C.load_map("media.json")
    by_url = m.get("byUrl", {})
    by_id = m.get("byId", {})
    done = skipped = 0
    failed = []
    for r in rows():
        if not r["rel"] or r["id"] in by_id:
            skipped += 1
            continue
        p = os.path.join(MEDIA_DIR, r["rel"])
        if not os.path.exists(p) or os.path.getsize(p) == 0:
            failed.append((r["rel"], "本地缺失"))
            continue
        try:
            data = upload(p)
        except Exception as e:  # noqa: BLE001
            failed.append((r["rel"], str(e)[:120]))
            continue
        by_id[r["id"]] = {"url": data["url"], "mediaId": data["id"], "rel": r["rel"],
                          "title": r["title"], "alt": r["alt"]}
        by_url[r["guid"]] = data["url"]
        by_url["/wp-content/uploads/" + r["rel"]] = data["url"]
        done += 1
        if done % 20 == 0:
            C.save_map("media.json", {"byId": by_id, "byUrl": by_url})
            print(f"  已上传 {done}（跳过 {skipped}，失败 {len(failed)}）", flush=True)
    C.save_map("media.json", {"byId": by_id, "byUrl": by_url})
    print(f"完成：本次上传 {done}，跳过 {skipped}，失败 {len(failed)}，总计入库 {len(by_id)}")
    for f in failed[:30]:
        print("  FAIL", f[0], f[1])


if __name__ == "__main__":
    main()
