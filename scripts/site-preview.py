#!/usr/bin/env python3
"""站点预览服务器：把激活产物当作**站点根**来服务。

为什么需要它（而不是直接用 /site）：
  产物里的站内链接是**站点根相对路径**（href="/shop"、"/about"、"/product_category/kuz"）——
  构建期的前提是「站点独占域名根」，生产部署靠反代把站点域名的 "/" 映射到激活目录。
  而开发环境下 "/" 是控制面（后台）。
  /site 只是把激活目录挂在控制面**子路径**上，页面打得开、点链接就离开站点了
  （/shop 落到后台 → 401）。这个脚本把激活目录挂到独立端口的 "/"，
  让站内链接按设计语义生效。

与 Go 侧访问面的两点一致：
  · 激活目录里每个条目的名字就是它的 URL 路径（无扩展名的目录符号链接；
    "/" → index、"/about" → about），index.html 在目录里面 ——
    所以 / 与 /<路径>/ 要映射到 <条目>/index.html，而不是找根下的 index.html；
  · /storage 指向 pkg/upload local provider 的落盘目录（默认 public/storage）。

用法：
    python3 scripts/site-preview.py                 # 默认 127.0.0.1:8081
    python3 scripts/site-preview.py --port 9000
    python3 scripts/site-preview.py --open          # 顺带打印首页地址
"""
from __future__ import annotations

import argparse
import http.server
import mimetypes
import os
import posixpath
import socketserver
import sys
import urllib.parse

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ACTIVE = os.path.join(REPO, "public", "runtime", "artifacts", "public", "active")
STORAGE = os.path.join(REPO, "public", "storage")

# 控制面后端：动态片段与打点要回源到这里。
#
# 为什么必须代理：产物里的交互接线是**同域相对请求**（hx-get="/_fragments/productList..."）
# —— 构建期的前提是「站点独占域名根、片段端点与站点同域」。生产靠反代满足这个前提，
# 而本脚本把站点挂在独立端口上，/_fragments 会打到脚本自己身上、返回 404，
# 表现为**筛选与分页点了没反应**（404 被 htmx 静默吞掉，控制台之外没有任何提示）。
BACKEND = os.environ.get("SITE_PREVIEW_BACKEND", "http://127.0.0.1:8080")

# 需要回源的控制面路径前缀（与 routes.go 挂载的公开面一致）：
#   片段端点 /_fragments/{type}、访问统计 /analytics/collect、支付回调 /payment/callback。
PROXY_PREFIXES = ("/_fragments/", "/analytics/", "/payment/")


def safe_join(root: str, rel: str) -> str | None:
    """把 URL 相对路径拼到 root 下；越界（..）返回 None。"""
    rel = posixpath.normpath("/" + rel.replace("\\", "/")).lstrip("/")
    if rel.startswith("../") or rel == "..":
        return None
    target = os.path.normpath(os.path.join(root, rel))
    if target != root and not target.startswith(root + os.sep):
        return None
    return target


def resolve_page(rel: str) -> str | None:
    """URL 路径 → 激活目录里的 index.html。

    先按「目录根 → <rel>/index/index.html」（本站的条目命名规则）找，
    再回落到 <rel>/index.html（普通静态站写法，兼容未来变更）。
    """
    base = safe_join(ACTIVE, rel)
    if base is None:
        return None
    for candidate in (os.path.join(base, "index", "index.html"),
                      os.path.join(base, "index.html"),
                      base):
        if os.path.isfile(candidate):
            return candidate
    return None


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "go_wp-site-preview"

    def log_message(self, fmt, *args):  # 静音：预览服务器不需要访问日志噪声
        pass

    def do_GET(self):
        self.serve(head_only=False)

    def do_HEAD(self):
        self.serve(head_only=True)

    def do_POST(self):
        self.serve(head_only=False)

    def proxy(self, head_only: bool, body: bytes) -> None:
        """把控制面请求原样转发给后端，并把状态码 / 内容类型 / 响应体原样写回。

        htmx 靠**响应体**做 DOM 替换、靠状态码决定是否触发 htmx:responseError ——
        所以不能只在成功时转发：后端的 400/500 也必须透传。只转成功那一支的话，
        前端会把失败当成功（替换成错误页）或把成功当失败（静默丢弃），
        两种误判都只在真实点击时才暴露。
        """
        import urllib.error
        import urllib.request

        url = BACKEND.rstrip("/") + self.path
        headers = {}
        for k in ("Accept", "Content-Type", "HX-Request", "HX-Target", "HX-Trigger",
                  "HX-Trigger-Name", "HX-Current-URL", "X-CSRF-Token", "Cookie"):
            v = self.headers.get(k)
            if v:
                headers[k] = v
        req = urllib.request.Request(url, data=body or None, headers=headers,
                                     method=self.command)
        status, resp_body, ctype = 502, b"", "text/plain; charset=utf-8"
        try:
            with urllib.request.urlopen(req, timeout=15) as r:
                status = r.status
                resp_body = r.read()
                ctype = r.headers.get("Content-Type", ctype)
        except urllib.error.HTTPError as e:
            status = e.code
            resp_body = e.read()
            ctype = e.headers.get("Content-Type", ctype)
        except OSError as e:
            resp_body = ("预览服务器无法连接控制面 " + BACKEND + "：" + str(e)).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(resp_body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if not head_only:
            self.wfile.write(resp_body)

    def serve(self, head_only: bool):
        parsed = urllib.parse.urlparse(self.path)
        raw = urllib.parse.unquote(parsed.path)

        # 控制面路径（动态片段 / 打点 / 支付回调）回源到后端。
        if raw.startswith(PROXY_PREFIXES):
            length = int(self.headers.get("Content-Length") or 0)
            body = self.rfile.read(length) if length > 0 else b""
            self.proxy(head_only, body)
            return

        # 媒体：/storage/* → public/storage/*
        if raw == "/storage" or raw.startswith("/storage/"):
            rel = raw[len("/storage"):].lstrip("/")
            target = safe_join(STORAGE, rel)
            if target and os.path.isfile(target):
                self.send_file(target, head_only)
                return
            self.not_found(head_only, parsed.path)
            return

        rel = raw.strip("/")
        hit = resolve_page(rel)
        if hit:
            self.send_file(hit, head_only)
            return

        # URL 不带尾斜杠但目录存在 → 301 补斜杠（与标准静态服务器一致）
        base = safe_join(ACTIVE, rel)
        if base and os.path.isdir(base) and not raw.endswith("/"):
            self.send_response(301)
            self.send_header("Location", raw + "/")
            self.end_headers()
            return
        self.not_found(head_only, parsed.path)

    def send_file(self, path: str, head_only: bool):
        ctype, _ = mimetypes.guess_type(path)
        if path.endswith(".html"):
            ctype = "text/html; charset=utf-8"
        try:
            with open(path, "rb") as f:
                body = f.read()
        except OSError:
            self.send_error(500, "read failed")
            return
        self.send_response(200)
        self.send_header("Content-Type", ctype or "application/octet-stream")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "public, max-age=0, must-revalidate")
        self.end_headers()
        if not head_only:
            self.wfile.write(body)

    def not_found(self, head_only: bool, original: str):
        """站点自定义 404 页（激活根下的 404.html）；没有就回纯文本。"""
        custom = os.path.join(ACTIVE, "404.html")
        if os.path.isfile(custom):
            try:
                with open(custom, "rb") as f:
                    body = f.read()
                self.send_response(404)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                if not head_only:
                    self.wfile.write(body)
                return
            except OSError:
                pass
        self.send_error(404, "Not Found", explain=f"{original} 没有对应的激活产物")


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


def main():
    ap = argparse.ArgumentParser(description="把激活产物当作站点根预览（独立端口）")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8081)
    ap.add_argument("--open", action="store_true", help="启动后打印首页地址")
    args = ap.parse_args()

    if not os.path.isdir(ACTIVE):
        print(f"激活产物目录不存在：{ACTIVE}\n先去后台发布至少一个页面。", file=sys.stderr)
        return 1

    with Server((args.host, args.port), Handler) as httpd:
        base = f"http://{args.host}:{args.port}"
        print(f"站点预览：{base}/  （激活目录 {os.path.relpath(ACTIVE, REPO)}）", flush=True)
        print(f"媒体目录：{base}/storage/  ← {os.path.relpath(STORAGE, REPO)}", flush=True)
        if args.open:
            print(f"打开：{base}/", flush=True)
        try:
            httpd.serve_forever()
        except KeyboardInterrupt:
            pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
