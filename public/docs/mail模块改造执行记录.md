# mail 模块改造执行记录

> 任务书：`public/docs/模块改造任务书-mail.md`；方法：`public/docs/模块改造通用规范.md`。
> 分支 `refactor/mail-taskbook`，工作树 `.worktrees/mail-refactor`，基线 main = 26ea31e9。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| M0-1 | 75576ddb | 十条后台页面路由由 `adminui.PageSlot` 改为 `pageview.UseRenderer(adminui.RenderPage)`，模板名移到 handler 的 `RenderTemplate`；同时把 PageSlot 下限守卫 50→48 |
| M0-2 | 599ee94f | 六处 `adminui.AttachPaginationView` 反射挂载改为 `view.Pagination = …`；联系人页 Base 用 `reqparams.FilterBaseURL` 在 handler 拼 |
| M1 | b666c201 | `dto/mail_page_resp.go` → `inbound/http/mail_view_handle.go`（14 个页面视图类型，package mailhttp） |
| M2 | 9ae8d1d1 | `dto/mail_option_resp.go` → `inbound/http/mail_option_view_handle.go`（下拉选项形状，含展示字段 Label） |
| M3 | 57f7c128 | 三处 offset 计算收敛到 `pkg/utils.PageOffset` |
| M4 | 核查结项 | 29 个 Err* 常量与 errFallbacks 逐条对应（脚本 missing=0、多余=0），service 内界面 key 字面量 0 处 |

## 2 结果

- 页面出口：`mail_page_router.go` 内 `adminui.PageSlot` 0 处、`mail_page.go` 内 `pageview.Render(` 0 处；十条路由统一走 `pageview.UseRenderer(adminui.RenderPage)`，模板名在 handler 声明。
- 分页：反射挂载 0 处；`view.Pagination` 直接赋值；模块内 `(page - 1) * ` 与 `(current - 1) * ` 命中 0 处。
- dto：不再有展示字段（Label/Title/Text/Tone/Err/ErrMsg 零命中）、不再有页面响应类型。

## 3 判据踩坑（下一模块直接复用）

1. **反射挂载里有一步 Base 计算会丢**。`adminui.AttachPaginationView(view, path, filters, p)` 在 `path != ""` 时用
   `filterBaseURL(path, filters)` **覆盖** `p.Base`（`internal/adminui/page_pagination.go:77-79`）。直接改成
   `view.Pagination = p` 后 Base 变空，`partials/pagination.html` 生成的页码链接全部没有 href
   （症状：`[{Text:上一页 Href:} {Text:1 Href:} …]` 五条全空）。修法：在 handler 用
   `reqparams.FilterBaseURL(path, params)` 显式拼 Base。order 的三处没踩坑是因为它们的 Base 已由 backpath 拼好、path 传的是空串。
2. **控制器方法 60 行上限**。补 Base 的三行把 `MailContactsPage` 顶到 62 行，`TestControllerMethodSize` 直接红。
   把分页事实抽成小函数（`mailContactsPagination`）比就地展开更省行，也更像「一屏一段数据构造」。
3. **控制器文件名只认 `<模块>_router.go` / `<域>_handle.go`**。把 `mail_option_view.go` 搬进 inbound/http 后
   `TestControllerFileNames` 立刻红，改名 `mail_option_view_handle.go` 才过（order 的 `order_view_handle.go` 同理）。
4. **PageSlot 下限守卫是「防扫描失效」不是覆盖率预算**。迁完一个模块就跌破阈值，按实际值下调并写清理由
   （50→48），只减不增；不要为了让它保持高位而保留死 PageSlot。
5. **搬包后要回查 import**。`mail_view_handle.go` 搬走后对 maildto 的引用只剩 `OptionResp`，二次搬走即归零，
   必须同时删 import；未使用 import 是编译错，不是 warning。

## 4 未做与待决

- `dto` 之外仍有 3 处 `gin.H`（非测试），属页面视图的临时形状；改成结构体要与模板契约同批，未在本轮动。
- 3 项 DEC 仍待决（见任务书 §5），未擅自决定。
- `check-write-fields.sh` 的 `order/order_ship_form.html` 红属 main 基线自带，mail 本轮未触碰。

## 5 验收

```bash
go build ./...
go test ./internal/module/mail/... ./internal/architecture/ ./public/test/mail/...
bash scripts/check-all.sh          # 29/30，唯一红属 main 基线
```
