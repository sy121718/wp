# media 模块改造侦察报告（图片变体 + 打包下载）

> 内部过程文档（非对外使用）

> 侦察范围：go_wp 工作目录，只读，未修改任何文件。所有行号以当前工作区为准。

---

## 1. internal/module/media/ 文件结构与职责

```
internal/module/media/
├── contract/media_service.go      # 对外契约接口 MediaService（Upload/List/Detail/Delete/分类 CRUD/UpdateAttachment/CategoryTree），14-31 行
├── dto/
│   ├── media_req.go               # ListReq(page/limit/file_type/category_id/search + GetPage/GetLimit/GetOffset)、DetailReq、DeleteReq
│   ├── media_resp.go              # AttachmentResp、ListResp、CategoryTreeNode
│   └── media_category_req.go      # CategoryCreateReq/CategoryUpdateReq/CategoryDeleteReq/AttachmentUpdateReq（nil 字段不改）
├── enums/media_enums.go           # 响应消息常量（见第 10 节）
├── inbound/http/
│   ├── media_router.go            # SetupMediaRoutes：装配 model→service→handle，注册 9 条路由（见第 5 节）
│   └── media_handle.go            # 11 个 HTTP handler，统一走 pkg/response
├── model/media_model.go           # AttachmentModel(sys_attachment) + FileCategoryModel(sys_file_category) 表访问单元；status=1/0 即启用/软删除
└── service/
    ├── media_service.go           # Service 结构体装配（am+cm），19 行
    ├── media_crud.go              # Upload（核心上传链路 L21-71）、List、Detail、Delete、CategoryTree、entityToResp、classifyType、buildCategoryTree
    └── media_category.go          # 分类 CRUD（同级重名拒绝/移动防环 isDescendant/删除级联 DetachAttachments）、UpdateAttachment（ExtraInfo JSON 合并 L175-205）
```

## 2. 上传链路

**调用链**：`media_handle.go:26 Upload`（`c.FormFile("file")` + `PostForm("category_id")`）→ `media_crud.go:21 Service.Upload` → `pkg/upload.Upload` → `pkg/upload/provider/local.go:79`（默认 provider）。

**service 层关键代码**（`internal/module/media/service/media_crud.go:37-48`）：
```go
ext := strings.ToLower(filepath.Ext(file.Filename))
fileType := classifyType(ext, file.Header.Get("Content-Type"))
result, err := upload.Upload(ctx, upload.File{
    Filename:    file.Filename,
    Reader:      src,
    Size:        file.Size,
    ContentType: file.Header.Get("Content-Type"),
}, upload.Request{})          // ← 注意：Route/Directory/ObjectKey 全空，落 storage 根
```
随后组装 `AttachmentEntity`（L52-64：FilePath/StoragePath=result.Key，URL=&result.URL，StorageType=result.Provider）并 `s.am.Create`（L66）。

**pkg/upload 要点**：
- `pkg/upload/upload.go:195 Upload(ctx, File, Request)` — 统一入口；L237 `uploadWithProvider` 做大小校验、**魔数嗅探拒绝 HTML/SVG/脚本伪装**（L254-266）、流式 LimitReader 兜底（L271-274）。
- `Request{Route, Directory, ObjectKey, PreserveName}`（provider/provider.go:17-22）— **变体生成可传 `ObjectKey` 直接指定变体文件路径**。
- `Result{Provider, Key, URL, Size}`。
- local provider（`pkg/upload/provider/local.go:20-24`）：默认根目录 `public/storage`，URL 前缀 `/storage`；可用 `upload.local_dir`、`upload.base_url` 覆盖。
- 存储路径规则（local.go:146-185 `buildObjectKey`）：默认 `{unixnano}_{6字节hex}{ext}` 随机名，无子目录；`O_EXCL+O_NOFOLLOW` 防覆盖/防软链（L113）。

**/storage 静态面映射**（`internal/routers/assembly.go` 的 `buildFoundation`；装配段已按审计 CQ-008 从 routes.go 拆出）：
```go
router.StaticFS("/storage", gin.Dir("public/storage", false))  // 禁目录列表
```

**入参**：multipart `file` + form `category_id`。**出参 DTO**（`dto/media_resp.go:4-16`）：
```go
type AttachmentResp struct {
    ID uint64; CategoryID *uint64; FileName string; FileSize int64
    FileType, MimeType, StorageType, URL, MD5, ExtraInfo, CreateTime string
}
```
`MD5` 字段存在但上传链路**从未计算写入**（entity.MD5 恒 nil）。

## 3. 数据表（public/migrations/init_schema.sql）

**sys_file_category**（L204-233）：`id BIGSERIAL PK, category_name VARCHAR(100) NOT NULL, category_code VARCHAR(50) NOT NULL UNIQUE, parent_id BIGINT DEFAULT 0, sort_order INT, icon, status SMALLINT DEFAULT 1, create_by/update_by BIGINT, create_time/update_time TIMESTAMP`；索引 idx_fc_parent_id/status 等。**软删除=status 0/1**，无 deleted_at 列。

**sys_attachment**（L236-264）：
```sql
CREATE TABLE IF NOT EXISTS sys_attachment (
id BIGSERIAL PRIMARY KEY, category_id BIGINT,
file_name VARCHAR(255) NOT NULL, file_path VARCHAR(500) NOT NULL,
file_size BIGINT NOT NULL, file_type VARCHAR(50) NOT NULL, mime_type VARCHAR(100),
storage_type VARCHAR(50) NOT NULL DEFAULT 'local', storage_path VARCHAR(500),
url VARCHAR(500), md5 VARCHAR(32), extra_info JSON,
status SMALLINT NOT NULL DEFAULT 1, create_by BIGINT, update_by BIGINT,
create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, update_time TIMESTAMP,
CONSTRAINT fk_sys_attachment_category_id FOREIGN KEY (category_id)
  REFERENCES sys_file_category(id) ON UPDATE CASCADE ON DELETE SET NULL);
CREATE INDEX ... idx_att_category_id/create_time/file_size/file_type/status/(status,create_time)/storage_type/(file_type,status)/update_time
```
media 相关迁移只有 init_schema.sql 一处，无后续增量迁移；变体表需新增编号 **048**（当前最大 047_block_category.sql，经 register.go go:embed + Version 注册）。

## 4. 媒体库前端

| 文件 | 职责 |
|---|---|
| `internal/templates/admin/media.html` | Jet 页面：左分类树 + 右网格/列表 + 详情侧栏 + 上传弹窗(L62-76) + 分类弹窗；L97-99 引 media-lib.css / media-lib.js / media-admin.js |
| `internal/templates/static/js/media-lib.js` | 184 行公共库 `window.MediaLib`（后台页与 workbench 弹窗共用）：`csrfToken()`（L12-18：优先 `meta[name="csrf-token"]`，兜底 sessionStorage）、`apiHeaders()`（L21-25 附 X-CSRF-Token）、`api()`（L28-42 统一 fetch /api/media/*，写请求带 CSRF）、renderTree/filterTree/list/thumbUrl/parseExtra 等 |
| `internal/templates/static/js/media-admin.js` | 431 行页面逻辑：loadTree/loadList、详情编辑（save L259 `M.api('update',...)`、**删除 L270 `M.api('delete',{id})`**）、`uploadFiles`（L281-300：FormData 多文件 + `fetch('/api/media/upload',{headers:M.apiHeaders({})})`）、分类管理 |
| `internal/templates/static/css/media-lib.css` | 样式 |

**HTMX 交互方式**：媒体库页面实际用**原生 fetch + JSON**（非 HTMX 属性），CSRF 由 MediaLib.api/apiHeaders 封装；页面 token 由 `dashboard/inbound/http/media_handle.go:18 MediaPage` 经 `withCSRF(c, gin.H{...})` 注入。

## 5. 路由挂载

**media 模块路由表**（`internal/module/media/inbound/http/media_router.go:20-31`）：
```go
g := rg.Group("/media", builtin.SessionAuthMiddleware())
g.POST("/upload", handle.Upload);          g.GET("/list", handle.List)
g.GET("/detail", handle.Detail);           g.POST("/delete", handle.Delete)
g.POST("/update", handle.UpdateAttachment)
g.GET("/category/tree", handle.CategoryTree)
g.POST("/category/create", ...); g.POST("/category/update", ...); g.POST("/category/delete", ...)
```

**挂载点**：`internal/routers/assembly.go`（装配段 `buildAPIAndCoreCRUD`）建 `authorizedAPI := api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware())`，紧接着 `mediahttp.SetupMediaRoutes(authorizedAPI, db)`（业务 API 依赖顺序 media 最先，该段注释有记录）。装配段已按审计 CQ-008 从 routes.go 拆出，故不再标行号——行号会随拆段漂移，段落名稳定。

**页面路由**：`dashboard_router.go:61` `adminPages.GET("/media", handle.MediaPage)`（Session+CSRF 组，无 Casbin）。

**Casbin 权限点**：`public/migrations/030_business_permissions.sql:36-44` 定义 9 个 media 权限点（media:list/detail/upload/update/delete/category_*），**新增 download/变体接口必须补权限点 seed，否则非超管 403**。

## 6. 现有图片处理：**完全没有**

全仓搜索 resize/webp/thumbnail/imaging（排除图标名、CSS、组件 props）：仅 `media_crud.go:163` classifyType 按**扩展名**归类 image/video/audio/document/other（含 .webp 识别）。**无任何图片解码/缩放/转码代码**。go.mod 无 golang.org/x/image、disintegration/imaging、chai2010/webp 等图片库（go.mod 86 行，gin/gorm/viper/casbin/jet/asynq 等）。

## 7. 下载能力现状：**无下载接口，无 zip 打包**

- 无任何 `Content-Disposition` 服务端下发代码（唯一命中是测试文件构造 multipart）。
- `archive/zip` 仅插件模块**解压**用：`internal/module/plugin/service/plugin_install.go:121` `zip.NewReader(...)` 读插件包——可参考但**没有 zip.NewWriter 打包先例**。
- 浏览器侧下载靠 URL 直开（/storage 直出）。

## 8. 配置面

`config.yaml`：
```yaml
server:
  request_body_limit: 2MB        # L10
  upload_body_limit: 32MB        # L11（上传请求体上限，config/config.go:73,109 解析）
upload:                          # L92-97
  enabled: true
  default_provider: local
  max_size: 10MB                 # upload.upload.go:380 解析（支持 KB/MB/GB）
  allowed_extensions: [".jpg",".jpeg",".png",".gif",".pdf",".txt"]   # ← 不含 .webp！
  allowed_mime_types: ["image/jpeg","image/png","image/gif","application/pdf","text/plain"]
```
隐藏可用项（local.go:55-67 读取，config.yaml 未写）：`upload.local_dir`（存储根目录）、`upload.base_url`（站点域名前缀）。组件注册：`config/register.go:98-104`（upload.Init/Ready/Close）。

## 9. 测试文件与跑法

- 单测：`public/test/media/unit/media_attachment_unit_test.go`（上传创建/类型分类/大小校验/物理清理/软删除/ExtraInfo 合并；initUploadForTest 用 viper 指向 local provider）、`media_category_unit_test.go`
- feature：`public/test/media/feature/media_category_test.go`（真实 PG，`support.NewPGTestDB` 每 schema 隔离，PG 不可用 t.Skip；测试内自建两表 DDL——**加变体表后此文件 DDL 需同步**）
- 跑法：项目根 `go test ./public/test/media/...`；并发敏感加 `-race`。

## 10. enums / response 模式（照此新增）

`internal/module/media/enums/media_enums.go`（全 13 行）：
```go
package mediaenums
const (
    ErrAttachmentNotFound = "附件不存在"
    ErrUploadFailed       = "文件上传失败"
    ErrUploadEmpty        = "上传文件不能为空"
)
const (
    MsgSuccess    = "操作成功"
    MsgBadRequest = "请求参数错误"
)
```
handler 统一 `pkg/response`：`response.Success(c, data)` / `SuccessWithMessage(c, mediaenums.MsgSuccess, att)` / `ErrorWithMessage(c, code, msg)` / `ParamError(c)`；响应壳 `Response{Code,Message,Data}`（response.go:14-18）。新消息（如「生成变体失败」「打包下载失败」）直接往 mediaenums 追加常量。pkg 层（如 upload）直用中文 err.Error()，不建 enums。

---

## 改造施工建议

### A. 变体表（推荐独立表 048 迁移）

新增 `public/migrations/048_media_variants.sql`（go:embed + register.go Version 注册，照 047 抄）：
```sql
CREATE TABLE IF NOT EXISTS sys_media_variant (
  id            BIGSERIAL PRIMARY KEY,
  attachment_id BIGINT NOT NULL,
  variant_type  VARCHAR(20) NOT NULL,   -- thumb / medium / webp
  file_path     VARCHAR(500) NOT NULL,  -- storage 相对 key
  file_size     BIGINT NOT NULL DEFAULT 0,
  width         INT, height INT,
  status        SMALLINT NOT NULL DEFAULT 1,
  create_time   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT fk_variant_attachment FOREIGN KEY (attachment_id)
    REFERENCES sys_attachment(id) ON DELETE CASCADE,
  CONSTRAINT uq_variant UNIQUE (attachment_id, variant_type, status));
CREATE INDEX IF NOT EXISTS idx_variant_attachment ON sys_media_variant(attachment_id);
```
备选轻方案：变体写进 sys_attachment.extra_info JSON（已有 UpdateAttachment 的 JSON 合并机制，media_category.go:175-205）——零迁移，但 zip 打包/按类型查询/统计存储量都别扭，**不推荐**。

配套：model 加 `MediaVariantModel`（单表 CRUD，遵循 model=Repository 定位）；service.Delete 里同步软删变体记录；物理文件清理策略需决策（当前主文件软删也不清理磁盘，变体保持一致即可）。

### B. 上传流切入点（3 个函数）

1. **主切入**：`service/media_crud.go:21 Service.Upload`——在 `upload.Upload` 返回 result 后、`s.am.Create` 后追加：
   - 条件：`classifyType=="image"` 且 ext 不在 {.svg,.gif}（svg 矢量无需位图变体；gif 动图转码丢帧）；
   - 用标准库 `image.Decode`（需 import _ image/jpeg png gif）+`golang.org/x/image/draw`（纯 Go 缩放，CatmullRom）生成 thumb(≈300px)/medium(≈1024px)；webp 编码需引入 `github.com/chai2010/webp`（cgo）或 `github.com/gen2brain/webp`（wazero 纯 Go）——**需新增依赖，go.mod 目前零图片库**；
   - 每个变体调 `upload.Upload(ctx, upload.File{...}, upload.Request{ObjectKey: "variants/<原图key去ext>/<type>.<ext>"})` 复用现有魔数嗅探/大小限制/落盘防覆盖；
   - **变体失败降级不回滚主上传**（log 记录，返回原图正常），枚举加 `ErrVariantFailed`；
   - 同步生成会拖慢上传响应，图片大时考虑 goroutine 异步 + 变体 status pending（表已留 status 字段）。
2. **DTO**：AttachmentResp 加 `variants []VariantResp{variant_type,url,file_size,width,height}`（entityToResp 扩展或 Detail/List join）。
3. **存量回填**：`Service` 加 `GenerateVariants(ctx, attachmentID)` 导出为契约方法，既供上传复用，也供详情页「重新生成」按钮与后台批量回填旧图片。

### C. 下载接口（挂在 media_router.go，遵守「仅 GET/POST + Query 参数」）

- 单图：`g.GET("/download", handle.Download)`，Query `id`；handler 里 `c.Header("Content-Disposition", `attachment; filename="..."`)` + `c.File(localPath)`。可选 `?variant=webp`（无变体回退原图）。
- 批量：`g.GET("/download/batch", handle.DownloadBatch)`，Query `ids=1,2,3`（URL 长度可控，GET 利于浏览器直接触发下载且免 CSRF）；POST 亦可但 fetch 下载麻烦。zip 用 `archive/zip.NewWriter(c.Writer)` 流式写（内存友好），目录结构 `original/`、`webp/`、`thumb/`、`medium/`，重名加 id 前缀；`zip.FileHeader{Name, Modified}`。多图时 Casbin 中间件在 authorizedAPI 组已生效，**必须在 030 补 seed：`('media:download','下载媒体','media','/api/media/download','GET')` 与 batch 两条**（或复用 media:detail 路径映射——但 enforce 按 path 精确匹配，新路径必须新权限点）。
- local provider 之外的 qiniu 存储：下载需经 `cfg.BaseURL` 拉流或 302，一期先限定 storage_type=local（当前默认即 local），qiniu 报「暂不支持打包」。

### D. 前端按钮位置

- `internal/templates/admin/media.html:24-32` 工具栏（搜索框与视图切换之间）加「批量下载」按钮（对勾选卡片打包）+ 卡片复选框；`media.html:53-59` 详情侧栏容器由 media-admin.js 渲染按钮。
- `media-admin.js:265-277` 详情侧栏 saveBtn/delBtn 旁加「下载」（`window.open('/api/media/download?id='+item.id)`）与「生成/重新生成变体」按钮（`M.api('variants/generate',{method:'POST',body:{id}})`）。
- 网格卡片（renderList 渲染处）加选择态；workbench.js:2669+ 媒体弹窗复用 MediaLib，自动获得 thumbUrl 变体能力（可改为优先取 thumb URL 渲染网格，减带宽）。
- 旧图无变体：列表 thumbUrl 已直接用原图 URL（media-lib.js:157-160），无需前端兼容处理。

### 风险与注意

- `upload.allowed_extensions` 不含 .webp：**变体是服务端生成、不经上传校验入口之外**仍会走 `validateFile`（upload.go:410 ext 白名单）——变体走 `Request.ObjectKey` 也要过扩展名白名单，需把生成变体的扩展名加入白名单或为变体单独放行（建议：变体生成直接写 local provider 语义外的独立写入函数，或 config 白名单补 .webp + image/webp）。
- 变体文件名必须可从原图 key 推导（zip 打包时不查库也能定位），`variants/<原key>/` 目录约定即可。
- feature 测试 media_category_test.go 内嵌 DDL 需补 sys_media_variant 建表，否则新 model 测试跑不起来。
