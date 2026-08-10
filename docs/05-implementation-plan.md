# 05 · 全阶段实施与验收计划

> 本文是 go_wp 从当前状态到完整交付的执行路线。每个阶段有明确的验收门禁，未通过不得进入下一阶段。

## 冻结决策

| 决策项 | 结论 |
|--------|------|
| 管理后台 | Go + Jet SSR 渲染页面/片段，HTMX 负责交互；不再维护 Vue SPA |
| 认证 | Session + Cookie（gin-contrib/sessions + Cookie 存储），不用 JWT |
| 鉴权 | Casbin（Enforce(user_id, path, method)） |
| CSRF | 所有 POST 写操作强制 CSRF Token 校验 |
| 数据库 | MySQL（唯一领域 SQL 基线） |
| 缓存 | 不使用 Redis；Session 走 Cookie 存储 |
| 公共前台 | Jet 只在 Preview/Publish 构建阶段执行，访客读取静态 Artifact |
| 富文本 | TinyMCE（先 CDN 跑通，后期按需自托管） |
| 设计系统 | Stripe 风格，亮暗双主题，详见 [DESIGN.md](../DESIGN.md) |

## 数据流总览

```text
管理后台：
  Admin Jet 页面 → HTMX 请求 → Gin inbound/http → 模块 Service → MySQL
                                                              ↓
                                                     Casbin 鉴权
                                                              ↓
                                                     Jet 渲染 HTML 片段返回

公开站点（构建期）：
  Page Document + CMS Content + BuildContext
    → Publish Compiler（Jet 构建期渲染）
    → Immutable Artifact
    → ArtifactStore.put()
    → PublicationStore.activate()
    → Static Server / CDN

公开站点（访客）：
  HTTP Request → Static Server → index.html + hash assets
    → Browser
    → 可选 HTMX → Runtime Fragment Endpoint
```

## 执行规则

1. 每个阶段固定执行：`go test ./...`、`go vet ./...`、`go build ./...`
2. 涉及数据库的阶段额外验证：空库迁移成功 → 第二次迁移幂等 → 集成测试通过
3. 每个阶段完成后提交 Git（中文注明改动文件路径和修改内容）
4. 任一门禁失败立即停止，修复后重新执行

## 当前状态

| 阶段 | 状态 | 说明 |
|------|------|------|
| 阶段 0 | 待开始 | 基线搭建 |
| 阶段 1 | 待开始 | 后台壳 + Session + 安全 |
| 阶段 2 | 待开始 | 现有模块迁移到 HTMX |
| 阶段 3 | 待开始 | 0-A1 Page 静态发布主链 |
| 阶段 4 | 待开始 | 0-A2 CMS 内容 + 自动发布 |
| 阶段 5 | 待开始 | 0-B Blueprint + Component + Media |
| 阶段 6 | 待开始 | 0-C Navigation |
| 阶段 7 | 待开始 | 0-D Runtime Fragment |

---

## 阶段 0：基线、依赖与测试底座

**目标**：搭好 Jet 渲染骨架，迁移系统就绪。

**任务**：

- [ ] `go.mod` 确认 Jet v6 依赖已引入
- [ ] 加入 HTML sanitize 依赖（ bluemonday 或类似），用于 TinyMCE 内容白名单清洗
- [ ] 加入 CSRF 中间件依赖
- [ ] 建立 `internal/templates/admin/` 目录结构和 Jet 渲染 facade
  - 实现 `gin.HTMLRender` 接口封装 Jet `*jet.Set`
  - 开发模式 `SetDevelopmentMode(true)`
- [ ] 建立 `internal/templates/publish/` 目录（构建期模板，与 Admin 模板隔离）
- [ ] 修复 `public/migrations/`：当前只有 runner 无迁移文件
  - 增加独立 migration 命令
  - 确保 runner 接入启动流程
- [ ] 加入 `gin-contrib/sessions` 依赖

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| `go build ./...` | 编译通过 |
| `go test ./...` | 现有测试全绿 |
| `go vet ./...` | 无警告 |
| Jet 渲染 | 最小模板渲染 + 自动转义测试通过 |
| MySQL 迁移 | 空库迁移成功，第二次幂等 |

---

## 阶段 1：Jet + HTMX 后台壳、Session 与安全边界

**目标**：无 JavaScript 也能登录、退出、导航后台。

**任务**：

- [ ] 在 `routes.go` 注册 `/admin/*` 页面路由和 `/admin/fragments/*` 片段路由
- [ ] 实现 Session 认证中间件（`SessionAuthMiddleware`）
  - gin-contrib/sessions + Cookie 存储
  - Cookie 属性：`HttpOnly`、`Secure`（生产）、`SameSite=Lax`
  - Session ID 随机不透明，Cookie 不保存用户资料
- [ ] 实现 CSRF 中间件
  - 所有 POST 请求强制 CSRF Token
  - Token 通过 Jet 模板注入到表单隐藏域
- [ ] 登录页 Jet 模板（`admin/login.jet`）
- [ ] 后台布局 Jet 模板（`admin/layout.jet`）
  - 侧栏导航、顶栏、主题切换按钮
  - HTMX CDN 引入
  - 主题初始化脚本（防 FOUC）
- [ ] HTMX 请求区分：`HX-Request: true` 返回片段，否则返回完整页面
- [ ] 安全头中间件复用现有 `security_headers.go`

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 登录流程 | 无 JS 也能登录、退出、导航 |
| Cookie 属性 | HttpOnly + SameSite 验证通过 |
| CSRF | POST 缺 Token 被拒、带 Token 通过 |
| 未登录 | 自动重定向到登录页 |
| HTMX 片段 | `HX-Request` 返回 HTML 片段，非 HTMX 返回完整页面 |
| Casbin 拒绝 | 无权限返回 403 |
| XSS 转义 | Jet 自动转义测试通过 |
| `go test ./...` | 全绿 |

---

## 阶段 2：现有后台模块逐个迁移并移除 SPA

**目标**：管理控制面所有功能无需 Vue/Node 即可操作。

**迁移顺序**：`admin → permission → menu → role → dept → datarule`

**每个模块的任务**：

- [ ] 列表页：Jet 表格 + HTMX 分页（`hx-get` + Query 参数）
- [ ] 搜索筛选：`hx-trigger="keyup delay:500ms"`
- [ ] 新增/编辑：弹窗式 fragment（`hx-get` 加载表单 → `hx-post` 提交）
- [ ] 删除：`hx-confirm` 确认 → `hx-post` 提交
- [ ] 权限相关：Casbin 鉴权保持不变，复用现有 service/contract/model
- [ ] 响应模式：HTMX 请求返回 HTML 片段，非 HTMX 返回完整页面

**迁移完成后清理**：

- [ ] 删除 `internal/embed/dist/` 及 `setupEmbeddedFrontend`
- [ ] 清理旧 JWT 相关代码（`pkg/auth/jwt.go`、`internal/middleware/builtin/auth.go` 中的 JWT 逻辑）
- [ ] 移除 `internal/embed/embed.go` 的 SPA 入口

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 各模块 CRUD | 列表/新增/编辑/删除/分页/筛选功能正常 |
| 权限 | 无越权跨模块访问 |
| 禁用 JS 冒烟 | 关闭 JavaScript 后基本功能可用（登录、导航、表单提交） |
| HTMX 局部更新 | 列表刷新、弹窗编辑、删除确认正常工作 |
| 无 SPA 残留 | 不存在 Vue 构建产物、旧业务资源或浏览器 token 存储 |
| `go test ./...` | 全绿 |

---

## 阶段 3：Phase 0-A1 — 手工 Page 静态发布主链

**目标**：Page Document → Publish Compiler → Artifact → Publication 全链路打通。

### 3.1 Project 与 Page

- [ ] 创建 `project` 模块：Project / SiteSettings / 构建所需 Project 快照
- [ ] 创建 `page` 模块：Page Draft / Page Document / 版本与乐观锁
- [ ] 实现 `PageKind + ContentTarget` 封闭枚举、ThemeNode AST、Binding 协议
- [ ] MySQL 迁移：`projects`、`pages`、`page_documents` 表

**验收**：合法 Draft 可往返序列化；非法 kind/target、重复 Node ID、旧版本写入被拒绝。

### 3.2 Publish Compiler

- [ ] 创建 `build` 模块：BuildContext Resolver → Migrate → Normalize → Validate → Lowering → Fragment → Style → Asset → Diagnostics → Jet Build-time Render
- [ ] BuildContext 固定后禁止读数据库、网络和当前时间
- [ ] map/集合/资源路径必须稳定排序与规范化

**验收**：同一输入重复构建字节完全相同；最终 HTML 不含 Jet 表达式、Binding 或编辑属性。

### 3.3 Artifact 与 Publication

- [ ] 创建 `artifact` 模块：不可变写入、内容对象闭包、ArtifactStore 契约与本地实现
- [ ] 创建 `publication` 模块：URL 占用、stage、activate、rollback、receipt 恢复
- [ ] 静态访问面只读取 PublicationStore 文件状态

**验收**：Page 可发布、二次发布、单页回滚；任一步失败旧页面仍可访问。

---

## 阶段 4：Phase 0-A2 — CMS 内容 + 自动发布

**目标**：Article/Product/Category 内容变更自动触发发布。

**任务**：

- [ ] `content` 模块：固定 CMS 内容（Article/Product/Category/Tag）与单调 revision
- [ ] `contenttemplate` 模块：ContentTemplate 草稿、不可变版本、Binding 约束
- [ ] `presentation` 模块：PresentationInstance / DocumentSnapshot / 内容驱动发布入口
- [ ] CMS 实体变更 → 自动派生 DocumentSnapshot → 经同一 Publish Compiler → ArtifactStore → PublicationStore
- [ ] TinyMCE 集成到文章编辑页（CDN 引入 + 服务端白名单清洗）

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 内容 CRUD | Article/Product/Category 创建/编辑/删除正常 |
| 自动发布 | 内容变更触发 PresentationInstance 重建并发布 |
| ContentTemplate 版本 | 版本变化触发所有关联 PresentationInstance 重建 |
| TinyMCE | 编辑器加载正常，提交内容经白名单清洗 |
| `go test ./...` | 全绿 |

---

## 阶段 5：Phase 0-B — Blueprint + Component + Media

**目标**：页面初始化工具、全局组件版本管理和媒体资源闭环。

**任务**：

- [ ] `blueprint` 模块：Blueprint 草稿、不可变版本、Page 初始化（用完即弃）
- [ ] `component` 模块：Global Component、版本、更新策略（immutable/auto-update/pinned）、Registry manifest
- [ ] `media` 模块：媒体元数据、变体、内容 hash、稳定 assetId

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| Blueprint | 初始化 Page Document 后不再参与构建；修改不传播 |
| Component 版本 | 更新策略生效（immutable 冻结 / auto-update 自动升级 / pinned 固定） |
| Media | 上传→变体→引用链路闭环，assetId 稳定 |
| `go test ./...` | 全绿 |

---

## 阶段 6：Phase 0-C — Navigation

**目标**：公开站点导航独立于后台权限菜单。

**任务**：

- [ ] `navigation` 模块：公开站点 Header/Footer 导航、菜单位置、revision
- [ ] 导航构建期编译进静态 Artifact

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 导航管理 | 后台可增删改公开站点导航项 |
| 与 menu 隔离 | `navigation` 不复用 `menu` 表和逻辑 |
| 构建输出 | 导航出现在发布的静态 HTML 中 |
| `go test ./...` | 全绿 |

---

## 阶段 7：Phase 0-D — Runtime Fragment

**目标**：白名单动态片段为静态页面提供实时交互能力。

**任务**：

- [ ] `runtimefragment` 模块：capability 白名单、受控 HTML Fragment handler
- [ ] HTMX 请求 → Go Handler → 返回受控 HTML 片段
- [ ] 分页列表：构建时只输出第一页静态 HTML，后续页码 HTMX 按需加载
- [ ] 搜索 Shell：规范化 Search Shell Page，搜索结果由 HTMX Fragment 按需返回

**验收门禁**：

| 检查项 | 标准 |
|--------|------|
| 白名单 | 只允许 Registry 内 capability，拒绝任意 endpoint |
| 分页 | 第一页静态，后续页 HTMX 片段正常 |
| 搜索 | 搜索结果 HTMX 按需返回 |
| 安全 | Fragment 不读取 Page Document、不执行 Jet、不接受任意 endpoint |
| `go test ./...` | 全绿 |