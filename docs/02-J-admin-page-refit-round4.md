# 后台页面改造（第四轮）：四维评审与落地

> 规则源：`~/.dsh/skills/admin-ui-logic`（四维判据 + 列表页标准骨架）。
> 本文只记录**本项目实际做了什么、在哪做的**，判据本身不在这里重复。

## 1. 为什么有这一轮

前三轮改的是「好看不好看」（对比度、间距、动效）。这一轮改的是「**能不能干活**」：
菜单层级、功能归属、CRUD 操作模式、列表可用性 —— 这四件事没有任何视觉审查会覆盖，
一份无障碍满分的后台仍然可能让人找不到功能、改不了一条数据。

## 2. 改造范围

55 个后台页面（`internal/templates/admin/*.html`，不含 layout）全部逐页过了一遍。
按域拆成 5 个分片并行执行，共享文件（`theme.css` / `admin.js` / 迁移 / 技能）由主会话独占，
避免并发写冲突。

| 分片 | 覆盖 |
|---|---|
| 商品域 | products / product_attributes / product_tags / product_pricing / product_bundle / product_detail_template / product_translations |
| 内容域 | articles / article_edit / article_translations / blocks / pages / content_templates / navigations / navigation_translations / media / page_translations / page_redirects / site_slots / i18n |
| 交易域 | orders / returns / coupons / customers / customer_detail |
| 系统域 | administrators / roles / permissions / menus / departments / datarules / datarule_edit / settings / theme / theme_settings / plugins / seo / analytics |
| 邮件域 | mail / mail_marketing / mail_campaign / mail_automation / mail_automation_edit / mail_automation_run / mail_automation_canvas |
| 库存域 | inventory / inventory_warehouses / inventory_sources / inventory_purchases / inventory_reasons / masterdata_changes |

> **覆盖口径核实（主会话实测，不靠记忆）**：`internal/templates/admin/` 下实测 **61 个模板**，
> 上表分片清单只列了 **51 个页面名** —— 差额 10 个**不是漏审**，全部在本轮之后的逐域深审里覆盖：
> `dashboard`（`02-O` §6）、`role_permissions`（`02-L` §0.3 的 M-4/M-6 与 P0-2/P1-16，含实测）、
> `products_new`（`02-M` D4/D7）、`product_edit`（`02-M`、`02-L` P2-10）、`product_detail`（`02-M` D6）、
> `product_brands` / `product_categories`（`02-M` D12、`02-L` P1-22）、`article_new`（`02-L` §0.9）、
> `layout` / `login`（非业务页，无操作逻辑可审）。
> **核对手法**：`comm -23 <(目录清单) <(分片清单)` 逐个对账，**不要按分片表反推「哪些页没审」** ——
> 那张表是首过的分片派工表，不是覆盖清单。以后新增审核报告请同时更新本核实段。

## 3. 每个页面统一的改造动作

1. **说明文字清零** —— 正文里的 `intro` / `.hint` 长句 / 口径说明全部进 `.help` 悬浮（默认 0 高度）；
   空状态统一成 `.empty-state`（标题 + 一句话 + 主行动）。
2. **页头合并** —— 标题 + `?` 在左，工程选择器与主行动在右同一行；
   删掉「一张卡里只有一个新建按钮」的空卡。
3. **工程下拉条件渲染** —— `{{if len(.Projects) > 1}}`；只有一个工程时不再渲染唯一选项的下拉。
4. **列表改标准表格** —— `<details>` 展开式列表全部改 `.data-table`；
   操作放末列 `.col-actions`；行内表单移出表格、用 `form="<id>"` 关联（HTML 不允许 form 嵌套）。
5. **筛选紧凑** —— `.filter-bar` + `.filter-fields`（label 在左、控件在右、一行 4~5 组）+ `.filter-actions`。
6. **描述类字段换控件** —— 多段描述改 Trix 富文本；SEO 标题/描述改 `textarea` + `data-counter` 字数提示
   （meta 必须纯文本，富文本标签会原样进 meta）。
7. **导航层级** —— 库存独立成一级目录（库存管理 / 仓库管理 / 货源管理 / 采购入库 / 变动原因字典）。

## 4. 新增的通用组件与基建

| 组件 | 位置 | 说明 |
|---|---|---|
| 页签 tabs | `ui.css` 基座 + `admin.js` | WAI-ARIA tabs 模式，方向键 / Home / End；面板服务端渲染，切换零请求。按审计 UIK-011 归纳进基座，与 workbench 的 `.wb-tabs` 划清边界 |
| 批量选择 | `theme.css` + `admin.js` | `.col-check` / `.is-selected` / `.bulk-bar`；全选、半选态、选中计数；批量动作与勾选框同属一个 `<form>` |
| 客户端筛选 | `admin.js` | `[data-filter-input]` + `[data-filter-text]`：输入即过滤，不发请求（此前模板注释声称有此实现，实际不存在，是死控件） |
| 字数提示 | `admin.js` | `data-counter` + `data-counter-out`；超出建议长度转警告色**不拦截**输入 |
| 操作列固定 | `ui.css` | 列多的表格横向滚动时，操作列 sticky 在右侧 |
| 表格不折行 | `ui.css` | 单元格默认 `nowrap`（中文在窄列里会被逐字竖排），需要折行的加 `.cell-wrap` |

## 5. 数据与迁移

| 迁移 | 内容 |
|---|---|
| 228 / 229 | 页面标题词条、页壳改造配套词条 |
| 230 | 列表页标准骨架第一轮（仪表盘真实概览 + 商品品牌/分类） |
| 231 | 骨架第二轮（通用件 + 变更记录页签） |
| 232 | 本轮 55 个页面的全部新增文案位（223 key） |
| 233 | 商品详情拆页配套（25 key） |
| 234 | 内容模板删除权限点 + 超管策略 |

## 6. 验证方式

- `go test ./internal/templates/...`：模板结构契约（Jet 配平、公共类契约、i18n 双向校验、页签基座一致性）
- `go test ./internal/module/{product,mail,masterdata,admin}/...` 与 `public/test/product/feature/`：链路与渲染
- **运行时回归**：42 个页面逐页导航，检查渲染完整（无 Jet 中断、无登录跳转、正文长度合理）
- **交互实测**（DOM 级，不只看代码）：批量勾选与半选态、页签切换与 aria 同步、sticky 操作列在滚动前后的右边界、客户端筛选的命中行数
- `code-review-graph update`：结构图谱同步

## 7. 批量操作：32 个端点

列表页标准骨架要求「首列勾选 + 批量动作」。全部按同一个模式实现：

- **复用对应单条动作的权限点**（`builtin.CasbinMiddlewareForPath("/api/xxx")`），不新增权限点；
- **逐条走同一条单条业务路径**（不另抄一套 where），单条失败**只计跳过、不整批回滚** ——
  整批回滚会让用户以为"一条都没做"，然后反复重试；
- 结果按「成功 N 个 / 跳过 M 个」回带（`?done=` / `?err=`，中文 `url.QueryEscape`）。

| 域 | 端点 |
|---|---|
| 商品 | products / product-brands / product-tags / product-attributes / product-categories 的 `bulk-delete` |
| 库存 | inventory/warehouses、inventory/sources 的 `bulk-delete`（变动原因没有删除能力，未加） |
| 交易 | orders 的 `bulk-status`/`bulk-cancel`、returns 的 `bulk-approve`/`bulk-reject`、coupons 的 `bulk-delete`/`bulk-toggle` |
| 客户 | customers 的 `bulk-status`/`bulk-unlock` |
| 管理面 | administrators / roles / permissions / menus / departments / datarules 的 `bulk-delete` |
| 内容 | articles / blocks / content-templates 的 `bulk-delete`（后者配套新增 `contenttemplate:delete` 权限点与迁移 234） |
| 其他列表 | pages / navigations / i18n / page-redirects 的 `bulk-delete` |
| 邮件 | mail 的 accounts / templates `bulk-delete`、contacts `bulk-status`、campaigns `bulk-delete` |

**语义上不做"无脑批量删除"**：订单不能删（批量流转状态 / 批量取消），优惠码有核销记录的不能删
（批量停用），退货批量同意时 `AutoReceive` 固定 false —— 不批量做「同意 → 入库 → 退款」一步到底。

## 8. 已知取舍

- **批量动作按域补齐**，但每页只加**有业务意义**的动作：订单是批量流转/取消而不是批量删除（订单不能删）；
  优惠码是批量停用而不是删除（有核销记录的必须留痕）。
- 商品详情页的写操作提交后回到详情页；**删除商品**仍回列表页。
- 变体与评分的入口按钮放在各自区块的标题行（它们是**子资源行动**），不是页头的主行动。

## 8. 权限点与 `check-permission-gaps.sh` 的正确读法

本轮踩过一次：跑审计看到「库中存在但代码未声明的权限点：`contenttemplate:delete`」，
就断定"历史 seed 早有了、不该再写迁移" —— 其实那条记录**正是本次新写的迁移刚灌进去的**，
脚本读的是**运行库的当前状态**，不是历史来源。核对方式是比对 `create_time` 与 `git log -S`。

机制（读脚本之前先记住这条）：

- `DECLARED` 清单只在 **`permission.RouteGroup.GET/POST`（声明式注册）**时采集；
- 后台**页面路由**走 `adminPages.POST(..., builtin.CasbinMiddlewareForPath("/api/xxx"), handler)`，
  **不经过声明式注册**，因此永远进不了 declared 清单；
- 所以输出里「库中存在但代码未声明的权限点」那一节 = **页面路由入口 / 历史 seed 的保留清单**，
  **不是缺口信号**（先例：`i18n:manage`）；
- 真正的缺口判定只有一条：「路由 − 权限点」的集合差（`comm -23`）。当前 **✓ 没有缺口**。

**新增页面级端点如果需要新权限点，必须同批写迁移**（权限点 + 超管策略，幂等），
否则干净库 / CI 上这条路由没有任何策略可匹配 —— **含超管在内全员 403**
（072 / 077 / 078 / 079 / 151 / 213 都踩过同一个坑）。

## 9. 两条容易重犯的坑

1. **CSS 选择器与模板脱节**：页签最初 CSS 写 `.tab`、模板只写了 `role="tab"`，样式整段落空，
   页签退化成浏览器默认的描边方块按钮。现在基座选择器用 `[role="tab"]`（role 是必需属性，模板不会漏写），
   并有测试双向守卫（有用法没基座、有基座没用法都会失败）。
2. **增强脚本的过早返回**：`if (!fields.length) return;` 这类写法会在「初始化时元素还不存在」（抽屉里的字段）
   的情况下把后面的**事件委托注册**一起跳过，表现是「功能永远不生效，刷新也不会好」。
   正确做法：委托 + 在 `wbui:drawer-open` 时补一次初始化。
