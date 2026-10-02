# /storage 交给 nginx 直出：部署参考

> **状态：参考文档。写进文档 ≠ 生效。**
> 仓库里**没有**任何 nginx 配置文件，也**没有**任何自动化（Makefile / 脚本 / CI）会应用本文片段；
> 落地由部署环境按「落地与自检」一节人工执行。
>
> 本文的规则不是自创的第二套：**Cache-Control 两条字符串与指纹正则逐字来自**
> [`internal/middleware/builtin/storage_cache.go`](../internal/middleware/builtin/storage_cache.go)
> 的 `storageCacheImmutable` / `storageCacheRevalidate` / `storageImmutableVariant`。
> 改动那边**必须同批改本文**（见「维护约定」）。

## 1. 现状与收益

`/storage` 现在由 Go 进程直出（`internal/routers/assembly.go`）：

```go
storage := router.Group("/storage", nosniff 匿名中间件, builtin.StorageCacheMiddleware())
storage.StaticFS("/", gin.Dir("public/storage", false))
```

两条已核实的代价：

1. **内核级零拷贝失效**：`gin.Dir(dir, false)` 返回 `*gin.OnlyFilesFS`，其 `Open` 把 `*os.File`
   包成 `neutralizedReaddirFile{f}`（**值**包装，见 `gin@v1.12.0/fs.go`）；`http.ServeContent` 的
   `io.CopyN` 因此拿不到源侧 `WriteTo`，而 gin v1.12.0 的 `responseWriter` 也没有 `ReadFrom`
   → **sendfile / splice 双双不可用**，每个响应退化为 32KB 用户态缓冲循环 + 双份内存拷贝。
2. **每个媒体请求占用 Go 进程**：图片字节的读盘与写 socket 都在业务进程内完成，与页面 / 接口请求
   争抢同一批 goroutine 与连接。

交给 nginx 后：`sendfile(2)` 在内核里搬字节（`tcp_nopush` 聚合小包），文件元数据由
`open_file_cache` 兜住，Go 进程只服务动态请求。

## 2. 可直接抄的配置

```nginx
# ── ① http { } 块内：缓存分档变量（map 只能放在 http 层）────────────────────────
# 正则与 storage_cache.go 的 storageImmutableVariant **逐字对应**：
#   · 用 "~"（大小写敏感）而不是 "~*" —— Go 侧 regexp 默认大小写敏感；
#   · 只认小写 hex [0-9a-f]，与 Go 侧一致。
# 两个引号都不能省：正则含 { } 不加引号会被 nginx 当成块语法；值含逗号不加引号会被拆词。
#
# **判据是形状 + 状态码两段**（与 Go 侧一致，见 storage_cache.go 文件头）：
# 形状只说明「这个 URL 与内容绑定」，还要它真的命中了一个 2xx 才配 immutable。
# 只按形状分发时，「形状合法但文件不存在」会拿到 immutable 404 —— 浏览器同样把 404
# 钉住 max-age，而且这条路径在换图后清理旧变体、变体生成失败等场景下真实存在。
# 所以 map 的输入串里带上 $status，只放 2xx 进 immutable 档。
map "$uri:$status" $gowp_storage_cache_control {
    default                                                                "public, max-age=0, must-revalidate";
    "~_(thumb|small|medium|full)-[0-9]+-[0-9a-f]{8}\.jpg:2[0-9][0-9]$"     "public, max-age=31536000, immutable";
}
```

```nginx
# ── ② server { } 块内 ──────────────────────────────────────────────────────
location /storage/ {
    # 直出文件。root 指向**应用工作目录**（其下才是 public/storage）；
    # 若应用用 upload.local_dir 改过存储根（pkg/upload/provider/local.go），这里必须同步改。
    root /srv/gowp/public;

    # 内核级零拷贝：文件字节不进用户态 —— 这正是接管 /storage 的目的。
    sendfile on;
    tcp_nopush on;

    # 与 Go 侧 gin.Dir(dir, false) 对齐：不给目录列表。
    autoindex off;

    # 文件句柄 + 元数据缓存，降低 stat/open 次数。
    # 代价：open_file_cache_valid 同时是「原图换图后 nginx 仍可能按旧元数据/旧 inode 应答」的
    # 最坏窗口（见「已知差异」第 2 条）。要严格对齐 Go 的「每次使用前回源验证」，
    # 把这四行整组注释掉即可 —— 代价是每个请求多一次 stat+open。
    open_file_cache max=1000 inactive=30s;
    open_file_cache_valid 5s;
    open_file_cache_min_uses 2;
    open_file_cache_errors on;

    # always：4xx（含 404）也下发 —— Go 侧这两条头由中间件无条件设置，404 也带（已实测）。
    # 注意 nginx 的 add_header 继承规则：本 location 内出现 add_header 会让 http/server 层的
    # add_header 全部失效（HSTS 等）。若上层有安全头，需一并写进本 location 或 include 同一片段。
    add_header Cache-Control $gowp_storage_cache_control always;
    add_header X-Content-Type-Options nosniff always;

    # 不要在本 location 用 expires：它会另发一份 Cache-Control，与上面的 add_header 冲突。
    # 不要为 /storage 开 gzip：Go 侧只有 /static 挂 StaticGzipMiddleware，/storage 从不压缩；
    # 且开 gzip 会让该响应失去 sendfile，而图片本身已是压缩格式、没有收益。
}

# 无尾斜杠的 /storage：明确拒掉（Go 侧是 301 → /storage/ → 404，见「已知差异」第 5 条）。
location = /storage { return 404; }
```

## 3. nginx 规则 ↔ Go 规则逐条对照

| # | 语义 | Go 侧（真源 `storage_cache.go` / `assembly.go`） | nginx 侧（本文片段） |
|---|---|---|---|
| 1 | 带指纹变体 → 缓存头 | `storageImmutableVariant` 命中 **且状态码为 2xx** → `public, max-age=31536000, immutable` | map 同正则 + `$status` 2xx 档（`~`）→ **同一字符串** |
| 2 | 其余（原图 / 无指纹旧变体 / 未知路径 / **任何非 2xx**）→ 缓存头 | `storageCacheRevalidate` = `public, max-age=0, must-revalidate` | map `default` → **同一字符串** |
| 3 | 匹配对象 | `path.Base(c.Request.URL.Path)`：只取 URL 路径最后一段（已解码） | `$uri`：全路径（已解码，不含查询串），正则锚定结尾，语义等价 |
| 4 | 404 / 目录请求 | 形状命中但**非 2xx**（含 `/storage/123_thumb-9-fedcba98.jpg` 这种形状合法而文件不存在的路径）→ 保持 `must-revalidate`；`/storage/`、`/storage/nope.jpg` 同理（状态码覆盖缓存头，不改形状判据） | map 输入串带 `$status`、immutable 档只认 `2xx`；`add_header … always`（缺 `always` 则 4xx 不带）；目录请求由 `autoindex off` 给 **403**，见差异第 7 条 |
| 5 | nosniff | 匿名中间件无条件 `X-Content-Type-Options: nosniff`（SEC-014） | 同一头 + `always` |
| 6 | 目录列表 | `gin.Dir(dir, false)` → `OnlyFilesFS`，列表请求 404 | `autoindex off` + `location = /storage { return 404; }` |
| 7 | 传输压缩 | `/storage` 未挂 gzip 中间件（挂 gzip 的是 `/static` 的 `StaticGzipMiddleware` 与访问面的 `PrecompressedAssetMiddleware` + `StaticGzipMiddleware`，见第 7 节） | 不配 gzip |
| 8 | 条件请求验证器 | `Last-Modified`（`http.ServeContent`），**无** ETag | `Last-Modified` + ETag（nginx 默认生成），见差异第 1 条 |

指纹正则当前值（**四档**）：Go 侧 `_(?:thumb|small|medium|full)-\d+-[0-9a-f]{8}\.jpg$`；
nginx 片段写 `_(thumb|small|medium|full)-[0-9]+-[0-9a-f]{8}\.jpg`（`\d` 换成 `[0-9]` 是
第 6 条差异里的可选加固，本片段直接采用，避免 PCRE 的 UCP 语义漂移）。
两侧引擎（Go RE2 / nginx PCRE）在代表性文件名上逐一比对过，结果一致，例如
`100_small-1-0af15413.jpg`、`100_thumb-1-ab12cd34.jpg`、`photo_thumb-1-92ca61da.jpg` 命中；
`100.jpg`、`100_medium.jpg`（无指纹旧变体）、`100_thumb-1-AB12CD34.jpg`（大写 hex）、
`100_thumb-1-ab12cd3.jpg`（7 位）、`100_thumb-ab12cd34.jpg`（缺 generation）、
`100_thumb-1-ab12cd34.jpeg`（非 .jpg）不命中。

**状态码维度**（本批新增，两侧同判据）：形状命中只决定「能不能长缓存」，
还要这一条响应真的是 2xx。所以 Go 侧把缓存头延迟到状态码已知、nginx 侧把 `$status`
拼进 map 输入串 —— 否则形状合法而文件不存在的路径会拿到 **immutable 404**，
浏览器把 404 钉一年（`/storage/123_thumb-9-fedcba98.jpg` 就是这种形态：
换成旧变体已清理、或变体生成失败只留下 DB 行的场景）。

## 4. 为什么两边规则必须同源

- **Go 直出不会消失**：本地开发、内网直连、nginx 未接管或临时回退时，同一份字节仍由 Go 下发。
- **两处不一致 = 同一份字节在不同入口有不同缓存语义**：经 nginx 的访客拿 immutable、直连 Go 的人拿
  must-revalidate。而 must-revalidate 不是性能偏好，是**正确性属性** —— 原图 URL 与内容解耦
  （`media_replace` 原地 rename 覆盖、URL 不变），一旦原图被误标 immutable，换图对老访客在
  `max-age` 内不可见，且没有任何 URL 能取到新图。反过来把指纹变体标成 must-revalidate 不致命，
  但会让 CDN / 浏览器每次都回源验证，白丢长缓存收益。
- 这条规则在两侧**各有一次落点**（Go 常量 + 本片段）；只改一处，就是静默分叉。

## 5. 已知差异与边界（诚实清单）

1. **ETag**：nginx 默认下发 ETag 并优先按 `If-None-Match` 判 304；Go 侧没有 ETag，只按
   `If-Modified-Since` 判。两边都会 304，但验证器不同 —— 排查缓存问题时要意识到这一点。
2. **open_file_cache 与「换图立即可见」**：原图换图是 `os.Rename` **原子覆盖**（inode 变、
   mtime 变）。`open_file_cache` 缓存句柄与元数据，在 `open_file_cache_valid`（本文给 5s）窗口内
   nginx 可能按旧元数据应答 304、或沿旧 inode 读到旧字节 —— 换算下来，Go 侧「每次使用前必须回源
   验证」在 nginx 侧被放宽为「最多 5 秒后才必然可见」。要严格同源就把那四行注释掉；保留 5s 是
   刻意的性能取舍，**但它是取舍，不是对齐**。
3. **安全头族不完整**：Go 侧 `/storage` 还带全局 `SecurityHeadersMiddleware` 的
   `X-Frame-Options: SAMEORIGIN`、`Referrer-Policy: strict-origin-when-cross-origin` 与 CSP。
   本文片段只保留 nosniff + Cache-Control（接管时**必须**保留的两条）。若要求两入口响应头逐条
   一致，把这些头也用同一 `include` 片段引入本 location（注意第 2 节注释里的 add_header 继承陷阱）。
4. **Content-Type 推断**：Go 侧按扩展名（`mime.TypeByExtension`）、失败时嗅探内容；nginx 按
   `mime.types` 查表、未知扩展名落 `default_type`（通常 `application/octet-stream`）。上传产物恒带
   扩展名，实际不会触发；若 nginx 版本较老、`mime.types` 缺 `.webp` / `.avif`，需补映射。
5. **尾斜杠形态（实测）**：
   · `/storage`：Go 侧 301 → `/storage/`（该 301 **不带**缓存头与 nosniff —— 重定向发生在路由层、
     中间件未参与），再 `/storage/` → 404 + must-revalidate；nginx 侧按本文片段直接 404。
   · `/storage/x_thumb-1-ab12cd34.jpg/`（文件路径带尾斜杠）：Go 侧 301 到去掉尾斜杠的规范路径
     （`Location: ../x_thumb-1-ab12cd34.jpg`，这个 301 **带** must-revalidate + nosniff）；
     nginx 侧直接 404 + must-revalidate + nosniff。两者最终都指向同一份字节，差别只在「是否给 301」。
6. **`\d` 的引擎差异（可选加固）**：Go RE2 的 `\d` 等价 `[0-9]`；PCRE 在启用 UCP 时 `\d` 会匹配
   Unicode 数字。要绝对对齐可把片段里的 `\d` 写成 `[0-9]`（语义相同，零漂移风险）。
7. **目录请求的状态码**：`/storage/` 在 Go 侧是 404（`gin.Dir(dir, false)` → `OnlyFilesFS`），在
   nginx 侧是 **403**（`autoindex off` 的默认行为）—— 两者都不给目录列表，但状态码不同。若巡检 /
   客户端把 403 当异常，需知道这是**预期**（实测：403 + must-revalidate + nosniff），不是配置错误。

## 6. 落地与自检

1. **对齐存储根**：`upload.local_dir` 覆盖默认值 `public/storage`（`pkg/upload/provider/local.go`）；
   nginx 的 `root` 必须指到该目录的**上层**（默认即 `<应用工作目录>/public`）。
2. **只切 `/storage`**：`/site`（`must-revalidate`）与 `/static`（`no-cache`）各有自己的缓存语义，
   保持 Go 直出，不要顺手搬进同一个 location。
3. **生效前自检**（下列期望值已用 nginx/1.31.4 + 本仓库真实 `public/storage` 实测过；文件名换成真实存在的）：

   ```bash
   curl -sI http://HOST/storage/100_thumb-1-ab12cd34.jpg | rg -i '^(HTTP|cache-control|x-content-type-options)'
   # 期望：200 + public, max-age=31536000, immutable + nosniff
   curl -sI http://HOST/storage/100_thumb-9-fedcba98.jpg | rg -i '^(HTTP|cache-control|x-content-type-options)'
   # 期望：404 + public, max-age=0, must-revalidate + nosniff
   # （形状合法但文件不存在：必须落协商缓存，不能是 immutable 404 —— 见第 3 节的「状态码维度」）
   curl -sI http://HOST/storage/100.jpg | rg -i '^(HTTP|cache-control|x-content-type-options)'
   # 期望：200 + public, max-age=0, must-revalidate + nosniff
   curl -sI http://HOST/storage/ | rg -i '^(HTTP|cache-control|x-content-type-options)'
   # 期望：403 + public, max-age=0, must-revalidate + nosniff
   # （403 是 autoindex off 的预期行为，不是配置错误；Go 侧对应 404，见差异第 7 条）
   ```

4. **回滚**：删掉该 location 即回到 Go 直出；Go 侧规则未动，无需改代码、无需重新构建。

## 7. `/site` 与 `/static`：产物里已经有 `.gz`，但**不要**顺手加 `gzip_static`

> 状态同上：**参考文档，写进文档 ≠ 生效**。仓库里没有 nginx 配置文件，
> 本节也没有任何自动化会去应用它。

### 7.1 构建期已经压好了

`internal/pipeline/precompress.go` 在产物组装时，对文本类条目多落一份同名 `.gz`
（`index.html` → `index.html.gz`），与明文**同一个 `artifacts/{hash}` 目录** ——
跟着 `{hash}` 一起不可变、一起激活、一起回滚，不需要在产物之外再维护一份预压缩缓存。

| 维度 | 取值 | 说明 |
|---|---|---|
| 类型白名单 | `.html .htm .css .js .mjs .json .xml .svg .txt .webmanifest .map` | 图片 / 字体 / 音视频不压：它们本身已是压缩格式，再压一次往往比原文还大 |
| 阈值 | 明文 < 1024 B 不生成 | 与传输中间件的 `gzipMinSize` 同值，有测试（`TestPrecompressMinSizeAlignedWithGzipMiddleware`）钉住 |
| 级别 | `gzip.BestCompression` | 构建是离线一次性成本，换访问面每次传输的体积 |
| 确定性 | 只由明文决定 | gzip header 的 MTIME / OS 被写死（`pipeline.GzipDeterministic`），同一份文档两次构建逐字节相同 |
| 进产物 hash？ | **不进** | `.gz` 不登记在 `manifest.json` 的 `files` 里。登记会让产物 hash 依赖压缩库实现（Go 升版换了 flate 输出 ⇒ 全站 hash 变 ⇒ 全量重建）。推论：**`.gz` 缺失不算产物损坏**，删掉即回落到实时压缩 |

访问面命中 `.gz` 时由 `middleware/builtin` 的 `PrecompressedAssetMiddleware` 直接下发该文件
（`Content-Encoding: gzip` + `Content-Length` + `Vary: Accept-Encoding`）；未命中由链上后一个
`StaticGzipMiddleware` **实时压缩**兜底 —— 检测这一段最简单的方式是看响应有没有 `Content-Length`：
预压缩路径有，实时压缩被迫走 chunked、没有。

### 7.2 为什么这两个面仍然 Go 直出（三条语义搬不动）

不是偏好，是这三条各自都会在 `location` 那一层断掉：

| 语义 | 真源 | 搬进 nginx 会丢什么 |
|---|---|---|
| AccessGuard（登录可见 / 密码保护） | `internal/middleware/builtin/access_guard.go` | 判定必须发生在**产出字节之前**：它读 cookie 签名与 Redis 里的访客会话，命中受限时用产物内的 `guard.html` 兜底。nginx 的 `auth_request` 只能模仿前半段 —— 密码解锁后的签名 cookie 由 Go 签发，拆开就是两套密钥、两套过期规则 |
| SiteRedirect（改 URL 后旧路径 301） | 产物目录里的 `redirect.json` | 它是**每次发布可变**的产物指令，不是配置项。搬进 nginx 等于要求每次改 URL 都重新生成并 reload 一份配置 |
| 站点根映射（`/` → `<active>/index/index.html`、`/en/` 语言根） | `pipeline.ResolveActiveEntry` | 这是「URL → 文件」的**唯一权威**。在 nginx 里重写一份 = 第二次实现；两份分叉的真实后果是「守卫判 `/about` 受限，而访问面对 `/about/index.html` 原样直出」 |

`/static`（后台静态资源）另有理由：生产模式走 `go:embed`，磁盘上**没有目录**能 `root` 指过去。

### 7.3 结论：当前装配下 `gzip_static` 没有安全落点

`ngx_http_gzip_static_module` 只作用于**它自己服务的静态文件**，因此：

- 给 `/site` 加一个 `location` 直出产物 → 上面三条语义全丢（**绕过访问守卫**，这不是性能取舍）；
- 让 nginx 只做 `proxy_pass` 反代 → `gzip_static` 对**上游响应**不生效，配了也是空的；
- `/storage` 不该开 gzip（图片已压缩，且开 gzip 会让该响应失去 `sendfile`，见第 2 节）。

**所以默认不要加。** 唯一允许加的前提是「这个站不用任何访问守卫、不用 redirect 产物」，
落地前先核对产物目录（两条都为空才成立）：

```bash
# 有任何一份产物带守卫 → 不能把 /site 交给 nginx
find /srv/gowp/public/runtime/artifacts -name guard.json | head
# 有任何一份重定向产物 → 同理
find /srv/gowp/public/runtime/artifacts/redirects -name redirect.json | head
```

### 7.4 若确实要加：三处必须注意（官方文档原文依据）

```nginx
# server { } 内，接管 /site 静态产物的前提是 7.3 的两条核对都为空。
location /site/ {
    alias /srv/gowp/public/runtime/artifacts/public/active/;   # 指向激活目录
    gzip_static on;          # 只认与请求文件同目录同名的 .gz，与本仓产物布局一致
    gzip_vary on;            # ← 必须显式打开，理由见下
    autoindex off;
    # 不要写 gzip_static always;
}
```

1. **`gzip_static always` 绝不能用**。官方文档对 `always` 的定义是「在任何情况下都使用
   gz 文件，不检查客户端是否支持」（[nginx 文档](http://nginx.org/en/docs/http/ngx_http_gzip_static_module.html)），
   它针对的是「磁盘上原本就没有未压缩文件」的场景。而产物目录里**明文和 `.gz` 都在** ——
   用了 `always`，不支持 gzip 的客户端会收到无法解码的字节流。
2. **`gzip_vary on;` 必须显式打开**：`gzip_vary` 默认 `off`，命中 `gzip_static` 时
   **不会**下发 `Vary: Accept-Encoding`；而 Go 侧两条路径（预压缩与实时压缩）都是无条件下发的。
   少了这个头，CDN / 共享缓存会拿 gzip 的响应去满足一个不支持 gzip 的客户端。
3. **判据不是同一套**：`gzip_static` 还受 `gzip_http_version`（默认 1.1）、`gzip_proxied`、
   `gzip_disable` 影响；Go 侧只看 `Accept-Encoding` 与其中的 q 值。排查「为什么这个客户端拿到
   明文」时要知道差异在哪一侧。另外该模块**默认不编译**，先确认：

   ```bash
   nginx -V 2>&1 | tr ' ' '\n' | rg gzip_static || echo "缺 --with-http_gzip_static_module"
   ```

### 7.5 推荐形态：只把 nginx 当缓存层（不改路由语义）

Go 已经下发 `Vary: Accept-Encoding`，nginx 在这一层只需按它分档缓存：

```nginx
proxy_cache_path /var/cache/nginx/gowp levels=1:2 keys_zone=gowp_site:64m inactive=1h;

location /site/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_cache gowp_site;
    proxy_cache_valid 200 10m;
    proxy_cache_key "$scheme$host$request_uri$http_accept_encoding";  # 按 Accept-Encoding 分档
    add_header X-Cache-Status $upstream_cache_status always;
    # 不要在这里配 gzip / gzip_static：上游已经是 gzip，再压一层是嵌套 Content-Encoding。
}
```

自检：

```bash
# ① 预压缩路径：有 Content-Length 且 Content-Encoding 为 gzip
curl -sI -H 'Accept-Encoding: gzip' http://HOST/site/ | rg -i '^(HTTP|content-encoding|content-length|vary)'
# ② 协商不满足：必须是明文、且没有 Content-Encoding
curl -sI http://HOST/site/ | rg -i '^(HTTP|content-encoding|vary)'
# ③ 回落（.gz 缺失时仍应有 gzip，但无 Content-Length）
```

## 8. 维护约定

改 `internal/middleware/builtin/storage_cache.go` 的**任何**规则 —— 正则的类型词白名单、
两条常量字符串、匹配对象 —— 都要**同批改本文**第 2 节片段与第 3 节对照表。
本文不是可以独立演进的第二份缓存策略。
