"""IPI 字体自托管：把 Orbitron / Rajdhani 的 latin 分片搬进 /storage/fonts。

为什么不用 <link href="https://fonts.googleapis.com/...">：静态发布站的产物要能离线、
可确定性部署，外链字体意味着「打开页面时再赌一次第三方可用性」。自托管后字体是站点
自己的一份静态资源，headScripts 里只剩 @font-face 声明。
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

ROOT = C.ROOT
CSS_URL = ("https://fonts.googleapis.com/css2?"
           "family=Orbitron:wght@400..900&family=Rajdhani:wght@300;400;500;600;700&display=swap")
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/126.0 Safari/537.36")
OUT_DIR = os.path.join(ROOT, "public", "storage", "fonts")
STATE = os.path.join(ROOT, "tmp", "ipi", "state.json")
LATIN = "U+0000-00FF"


def fetch(url: str) -> bytes:
    return subprocess.run(["curl", "-sS", "-A", UA, url], capture_output=True, check=True).stdout


def main() -> None:
    css = fetch(CSS_URL).decode()
    blocks = re.findall(r"/\* (\w[\w-]*) \*/\s*@font-face\s*{(.*?)}", css, re.S)
    os.makedirs(OUT_DIR, exist_ok=True)
    faces = []
    for subset, body in blocks:
        if LATIN not in body or subset != "latin":
            continue
        family = re.search(r"font-family:\s*'([^']+)'", body).group(1)
        weight = re.search(r"font-weight:\s*([^;]+);", body).group(1).strip()
        url = re.search(r"url\((https://[^)]+)\)", body).group(1)
        name = f"{family.lower().replace(' ', '-')}-{weight.replace(' ', '-')}.woff2"
        path = os.path.join(OUT_DIR, name)
        data = fetch(url)
        with open(path, "wb") as f:
            f.write(data)
        faces.append(
            "@font-face{font-family:'%s';font-style:normal;font-weight:%s;"
            "font-display:swap;src:url(/storage/fonts/%s) format('woff2')}" % (family, weight, name)
        )
        print(f"  {name} {len(data)//1024}KB")
    head = "<style>" + "".join(faces) + "</style>"

    st = json.load(open(STATE, encoding="utf-8"))
    C.login()
    projects = C.ok("/api/project/list", method="GET") or []
    proj = next(p for p in projects if p["id"] == st["projectId"])
    settings = proj.get("settings") or {}
    settings["headScripts"] = head
    C.ok("/api/project/update", {"id": proj["id"], "name": proj["name"], "settings": settings})
    print(f"@font-face {len(faces)} 条已写入站点 headScripts")


if __name__ == "__main__":
    main()
