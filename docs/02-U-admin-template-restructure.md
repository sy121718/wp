# 02-U · admin 模板按后端模块分组（已执行）

> 内部过程文档（非对外使用）

> 触发：用户指出 `internal/templates/admin/` 里部门、角色、管理员、菜单、权限与各业务模块全平铺混在一起。
> 口径：**按后端模块名分组**（与 `internal/module/` 对应）；**插队在回填批次之间做**（后续每批都要碰模板，早改省事）。
> 结果：**已执行完成并验证**（模板测试全绿、31 个页面真实 HTTP 抽查通过）。

## 1. 最终结构

```text
admin/
├── layout.html  login.html  dashboard.html        壳与首页（3，留在根）
├── partials/                                       跨模块/通用片段（8）
│     bulk_bar · pagination · toolbar_create · rich_editor · sidebar · nav-nodes
│     media_field · locale_rows
├── system/      administrators roles role_permissions permissions menus departments
│                datarules datarule_edit i18n  + datarule_config_editor    (9 页 + 1 片段)
├── analytics/   analytics seo                                             (2)
├── block/       blocks                                                    (1)
├── content/     articles article_new article_edit article_translations    (4)
├── contenttemplate/ content_templates                                     (1)
├── inventory/   inventory inventory_warehouses inventory_reasons inventory_sources
│                inventory_purchases                                       (5)
├── mail/        mail mail_campaign mail_marketing mail_automation
│                mail_automation_edit mail_automation_run mail_automation_canvas (7)
├── masterdata/  masterdata_changes                                        (1)
├── media/       media                                                     (1)
├── navigation/  navigations navigation_translations                       (2)
├── order/       orders returns coupons                                    (3)
├── page/        pages page_translations page_redirects site_slots         (4)
├── plugin/      plugins                                                   (1)
├── product/     products products_new product_edit product_detail product_detail_template
│                product_attributes product_brands product_categories product_tags
│                product_translations product_pricing product_bundle       (12)
│                + pricing_rule_fields product_create_form product_attribute_{group_form,rows,values_form}
│                  product_tag_hits                                        (6 片段)
├── project/     settings theme theme_settings                             (3)
└── user/        customers customer_detail                                 (2)
```

**目录名与后端模块的对照**：`system/` = 后端 `admin` 模块（管理控制面）。
不叫 `admin/admin/` 是为了不出现同名嵌套，且导航菜单里这一组本来就叫「系统」。

片段分流的判据是**被几个模块引用**，不是名字像哪个模块：`bulk_bar`(26 页)/`pagination`(23 页) 跨全部模块，
`rich_editor` 跨 content+product，`sidebar`/`nav-nodes` 属壳层，`media_field`/`locale_rows` 是通用控件；
`datarule_config_editor` 跟 `system/`，product 的六个片段跟 `product/`。

## 2. 影响面（实测）

| 改动项 | 数量 | 说明 |
|---|---|---|
| 文件移动 | 61 页 + 7 片段 | 3 页留在根、8 片段留在 `partials/` |
| Go 模板名 | **258** | 带 `.html` 134 + 不带后缀 **124** |
| 模板内路径 | **134** | include/import 70 + `extends` 64 |
| 测试适配 | 8 个文件 | 渲染调用、读文件、glob、基线表 key、正则 |
| `//go:embed` | 0 | `all:admin` 是递归的 |

## 3. 八个判据缺口（**本批最大的教训**）

「引用点」不是一种写法，是**一个文件名的全部出现形态**。我先后漏了八次，每一次都让某条路径静默失效
（**编译期全都不报错**，只有访问那个页面才 500 或断言红）：

1. 只认 `{{include "partials/x.html"}}` → 漏了片段之间用**相对同目录裸名**的 include；
2. 只认 `include` → 漏了 `{{import "nav-nodes.html"}}`；
3. 只认带 `.html` 的模板名 → 漏了 **`c.HTML(…, "admin/settings", …)`** 这种不带后缀的（**124 处**，比带后缀的还接近一半）；
4. 只认 include/import → 漏了 **`{{extends "layout.html"}}`** —— 它同样相对当前文件目录解析，20 个页面当场 500；
5. 只认生产代码 → 漏了测试里的**文件系统路径**（`os.ReadFile("admin/x.html")`、`filepath.Glob("admin/*.html")`、基线表的 key）；
6. 正则字符类写窄 —— `[a-z_]` 漏掉含数字的 **`i18n`**（5 处渲染调用没被改）。

前六种都是「**没改到**」。补完一轮之后又冒出两种，性质不同 —— 它们是**第二轮才暴露**的：

7. **引用落点写错**（不是没改到，是改成了错的） —— `fragments/` 在**模板根**下，从 `admin/<模块>/` 出发要写
   `../../fragments/x.html`；`admin/content/article_edit.html` 被留成 `../fragments/seo_score.html`（少一层）→
   content 包 4 条渲染测试红、那个页面 500。**编译与 `go test ./internal/templates/...` 都不报**。
8. **验证范围不足**（判据没错，是没跑到） —— ① 只跑 `./internal/templates/...`，漏掉模块包里的硬编码模板路径
   （`internal/module/product/inbound/http/*_test.go` 3 条 + `content/inbound/http` 4 条）；② 抽查只扫
   「能直接打开的列表页」，带参数的编辑页（`/admin/articles/edit?id=…`）根本没进样本 —— 第 7 条正是这样逃过抽查的。

**判据层面的教训比修正本身值钱**：第 7 条说明「反查名字」这个动作**天然覆盖不到「改完之后能不能解析」**，
第 8 条说明「抽查一批页面」天然覆盖不到**参数化页面**。两条都已补上硬判据：

- `internal/templates/admin_template_resolve_test.go` 的 `TestAdminTemplateReferencesResolve`：静态检查 admin/ 下
  每个模板的 `extends` / `import` / `include` 落点是否存在（76 个模板 / 134 处引用，带坏样本自检防它退化成空转）——
  不渲染、不连库，改完当场就能报全；
- `internal/templates/CLAUDE.md` 的硬要求补上「搬模板至少跑 `go test ./internal/...`」与参数化页面的抽查义务。

**收口的三条做法**（已落进代码与规则）：
- 加包级 helper 让**调用点不必知道文件在哪**：`adminTemplateFiles`（递归枚举）、`adminTemplatePath`（短名→路径）、
  `adminTemplateSource`（读内容）、`render()` 内部解析模板名 —— 下次搬文件不用改一片调用点；
- 门禁的 glob **必须递归**：`filepath.Glob("admin/*.html")` 在分目录后只会匹配到 3 个壳页面，
  门禁会**静默缩水**（仍然绿，但不再守任何东西）—— 这比变红危险；
- 反查残留 + **走真实 HTTP 路径**：编译通过 ≠ 引用正确。

## 4. 验证

- `go build ./...` 通过；
- `go test ./internal/templates/...` **全绿**（该包会真实渲染 58 个页面，是模板名与路径的主要回归网）；
- **反查**：`"admin/<一层名>"` 形态只剩 `dashboard` / `layout` / `login` 三个壳页面；
- **真实路径抽查**（浏览器，登录后逐个 fetch）：32 个页面里 31 个 `200` 且响应含完整 `</html>`、
  无一出现「页面暂时无法显示」；唯一 404 是我猜错的路由名（`/admin/inventory-warehouses`），与模板无关。
- **第二轮修正后的复核**（`curl` + cookie，30 个 URL，**含参数化页面**）：23 个 `200` 且响应含完整 `</html>`，
  7 个 404 全是我猜错的路由名（`inventory-*` / `product-bundles` / `navigation` / `users`）。
  样本里有 `/admin/articles/edit?id=…`（第 7 条的现场，修好后 200）与 `/admin/products/new?project=…`（批 1 的落点）。
- **第二轮的缺口已全绿**：`go test ./internal/...` 除 `TestProductDetailBundlePanelEmptyAndBroken`
  之外无红 —— 那一条是**在途**改动（新测试断言「空配置不该渲染成员表」，而既有模板 `product_detail.html`
  的注释写明「空数据也渲染 `<table>/<thead>`」是刻意设计），与本批无关，未越界改。
- `public/test` 的编译红是**在途**的 `pages.CompilePreview` 签名变更（5 参数 → 6 参数），同样与本批无关。

## 5. 回滚

备份留在 `/tmp/admin_tpl_backup_<epoch>`（76 个文件）；改动集中且机械，`git` 层面也可整体回退。
