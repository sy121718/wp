"""IPI 媒体导入：手稿 assets + WP 站点 logo → go_wp 媒体库。

产物：tmp/ipi/state.json 的 media 映射（本地文件名 → 站点 URL）。
页面构建脚本只认这份映射 —— 图片地址硬编码在文档里会让换站点时整页 404。
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

HANDOFF = os.path.join(C.ROOT, "tmp", "ipi", "handoff", "ipi-assets")
LOCAL = os.path.join(C.ROOT, "tmp", "ipi", "media")
STATE = os.path.join(C.ROOT, "tmp", "ipi", "state.json")
WP = "http://127.0.0.1:8089"
EXTRA = [
    (f"{WP}/wp-content/uploads/2026/10/logo-ipi-white.png", "logo-ipi-white.png"),
    (f"{WP}/wp-content/uploads/2026/10/ipimark-512.png", "ipimark-512.png"),
]

INTERVAL = 0.7
_last = [0.0]


def throttle() -> None:
    dt = time.time() - _last[0]
    if dt < INTERVAL:
        time.sleep(INTERVAL - dt)
    _last[0] = time.time()


def upload(path: str) -> str:
    cmd = ["curl", "-s", "-b", C.COOKIE, "-H", f"X-CSRF-Token: {C.token()}",
           "-F", f"file=@{path}", C.BASE + "/api/media/upload"]
    out = subprocess.run(cmd, capture_output=True, text=True).stdout
    try:
        body = json.loads(out)
    except Exception:
        raise RuntimeError("非 JSON 响应: " + out[:200])
    if body.get("code") != 200:
        raise RuntimeError("上传失败: " + json.dumps(body, ensure_ascii=False)[:200])
    data = body["data"]
    if isinstance(data, list):
        data = data[0]
    return data["url"]


def main() -> None:
    C.login()
    os.makedirs(LOCAL, exist_ok=True)
    for url, name in EXTRA:
        dst = os.path.join(LOCAL, name)
        if not os.path.exists(dst) or os.path.getsize(dst) == 0:
            subprocess.run(["curl", "-sS", "--max-time", "30", "-o", dst, url], check=False)

    files = []
    if os.path.isdir(HANDOFF):
        files += [os.path.join(HANDOFF, n) for n in sorted(os.listdir(HANDOFF)) if n.lower().endswith((".jpg", ".png"))]
    if os.path.isdir(LOCAL):
        files += [os.path.join(LOCAL, n) for n in sorted(os.listdir(LOCAL)) if n.lower().endswith((".jpg", ".png"))]

    with open(STATE, encoding="utf-8") as f:
        st = json.load(f)
    media = st.get("media", {})
    for i, path in enumerate(files, 1):
        name = os.path.basename(path)
        if name in media:
            continue
        throttle()
        try:
            try:
                media[name] = upload(path)
            except RuntimeError as e:
                # 会话 TTL 到期会让整批尾部 401：重登一次再试，别把整批标记成失败。
                if "401" not in str(e):
                    raise
                C.login()
                throttle()
                media[name] = upload(path)
            print(f"  [{i}/{len(files)}] {name} → {media[name]}")
        except Exception as e:  # noqa: BLE001
            print(f"  [{i}/{len(files)}] {name} 失败: {str(e)[:160]}")
        st["media"] = media
        with open(STATE, "w", encoding="utf-8") as f:
            json.dump(st, f, ensure_ascii=False, indent=1)
    print(f"媒体 {len(media)} 项")


if __name__ == "__main__":
    main()
