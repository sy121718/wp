"""WP → go_wp 导入公共层（复刻 vapecentralau.com 用）。

一次性迁移工具：登录后台、读写 JSON API、维护 WP↔go_wp 的 id 映射。
所有写操作走真实 HTTP 接口（不是直接写库），这样导入过程本身就是在验证接口。
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys

BASE = os.environ.get("GOWP_BASE", "http://127.0.0.1:8080")
COOKIE = "/tmp/gowp-import-cookie.txt"
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
NDJSON = os.path.join(ROOT, "tmp", "wp-import", "ndjson")
MAPDIR = os.path.join(ROOT, "tmp", "wp-import", "map")

_token = None


def login() -> str:
    """dev-login 建会话并取 CSRF token。"""
    global _token
    subprocess.run(["curl", "-s", "-c", COOKIE, f"{BASE}/admin/dev-login?to=/admin", "-o", "/dev/null"], check=True)
    html = subprocess.run(["curl", "-s", "-b", COOKIE, f"{BASE}/admin/media"],
                          capture_output=True, text=True, check=True).stdout
    m = re.search(r'"X-CSRF-Token":"([0-9a-f]{64})"', html)
    if not m:
        raise RuntimeError("取不到 CSRF token（dev-login 失败或页面结构变了）")
    _token = m.group(1)
    return _token


def token() -> str:
    return _token or login()


def api(path: str, payload=None, method: str = "POST", raw: bytes | None = None,
        content_type: str = "application/json"):
    """调用 JSON API，返回 (http_code, parsed_body)。"""
    cmd = ["curl", "-s", "-b", COOKIE, "-w", "\n%{http_code}", "-X", method]
    if method != "GET":
        cmd += ["-H", f"X-CSRF-Token: {token()}"]
    if raw is not None:
        cmd += ["-H", f"Content-Type: {content_type}", "--data-binary", "@-"]
        proc = subprocess.run(cmd + [BASE + path], input=raw, capture_output=True)
    elif payload is not None:
        cmd += ["-H", "Content-Type: application/json", "--data-binary", "@-"]
        proc = subprocess.run(cmd + [BASE + path], input=json.dumps(payload, ensure_ascii=False).encode(),
                              capture_output=True)
    else:
        proc = subprocess.run(cmd + [BASE + path], capture_output=True)
    out = proc.stdout.decode("utf-8", "replace")
    body, _, code = out.rpartition("\n")
    try:
        parsed = json.loads(body)
    except Exception:
        parsed = {"_raw": body[:400]}
    return int(code or 0), parsed


def ok(path, payload=None, method="POST", raw=None, content_type="application/json"):
    """调用并要求 code==200，返回 data。"""
    code, body = api(path, payload, method, raw, content_type)
    if code != 200 or body.get("code") != 200:
        raise RuntimeError(f"{path} 失败 http={code} body={json.dumps(body, ensure_ascii=False)[:300]}")
    return body.get("data")


def load(name: str):
    """读 NDJSON。"""
    rows = []
    with open(os.path.join(NDJSON, name), encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def load_map(name: str):
    p = os.path.join(MAPDIR, name)
    if os.path.exists(p):
        with open(p, encoding="utf-8") as f:
            return json.load(f)
    return {}


def save_map(name: str, data):
    os.makedirs(MAPDIR, exist_ok=True)
    with open(os.path.join(MAPDIR, name), "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=1)


def project_id() -> str:
    return os.environ.get("GOWP_PROJECT", "52935790-31b4-4bcd-8eb6-ccdc46b342c7")
