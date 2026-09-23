# 02-T · 写失败不丢输入 · 批 1（商品新建整页）

> 清单来源：`docs/02-M-product-inventory-audit.md` 的 **D4**（P1）。
> 本批同时确立了后续批次（批 2~7）复用的**分档契约**，所以先落文档再往下做。

## 1. 问题（实测复现）

`POST /admin/products/create` 失败时是 `302 + ?err=` 回**列表页**，而表单在 `/admin/products/new`：
用户为这一张表填了 13 个字段（含属性组多选、多仓多选、数量开关），一个业务错误（如捆绑商品没填套餐价）
就把整屏输入全部蒸发，还要重新找一遍新建入口。

## 2. 契约：写表单失败的「分档出口」

表单保留原生 `method`/`action`（无 JS 时也能提交），另加 `hx-post` 让 htmx 优先拦住 submit，
服务端按 `HX-Request` 分档：

| 请求 | 失败 | 成功 |
|---|---|---|
| htmx（`HX-Request: true`） | **200 + 片段自身**（错误槽 + 回填后的表单） | `HX-Redirect`（`redirectWhere`） |
| 原生（无该头） | `302 + ?err=` 回**本页** | 同左（`302` 到目标页） |

四条不可省的实现约束（每条都有实测代价）：

1. **成功路径也必须分档** —— htmx 的 XHR 会自己跟随 302，最终响应里读不到 `Location`，
   于是整页 HTML 被塞进表单/目标位置。实测：改前成功提交后页面结构错乱。
2. **回填键名带前缀**（`FormEcho` / `FormEchoChecked` / `FormEchoMulti`）——
   片段与页面共用同一份渲染 data，而页面键空间里早有 `Form`（列表页批量改价的 `pricingForm` **结构体**），
   撞名时片段里的 `{{.Form.name}}` 命中结构体、Jet 报错、整个响应失败。
3. **多选回填按值命中**（`FormEchoMulti` + 逐项比对），不能用「该字段提交过」判 ——
   后者会把整组选项一次勾满（用户只勾了一个，回填后变成四个）。
4. **帧内错误槽放在表单之外、host 之内** —— 放进 `<form>` 会被下一次提交一起换掉。

## 3. 改动清单

| 文件 | 改动 |
|---|---|
| `internal/templates/admin/partials/product_create_form.html` | 顶层包 `<div data-product-create-host>`；表单加 `hx-post` / `hx-target="closest [data-product-create-host]"` / `hx-swap="outerHTML"`；错误槽（`role="alert"` + `data-create-err`）；13 个字段回填；局部变量改由片段自己从页面数据键取（不再依赖调用点声明） |
| `internal/module/product/inbound/http/product_new_page.go` | 新增 `productCreateFormFields` / `productCreateFormData` / `productCreateFail` / `productNewURL` |
| `internal/module/product/inbound/http/product_page_handle.go` | `ProductsCreate` 三个失败出口 → `productCreateFail`；两个成功出口 → `redirectWhere` |
| `internal/module/product/inbound/http/product_page_util.go` | `formEchoData` 键名加 `FormEcho` 前缀（撞名判据见上） |
| `internal/module/product/inventory/inbound/http/inventory_page_util.go` | 同签名同步改键名 |
| `internal/templates/admin/products.html` | **删除** `tpl-product-create` 死 template（见 §5） |
| `internal/templates/static/js/product-create-form.js` | `dirty` 初值改为按输入框现值判（见 §5 P1-1） |

测试：`product_create_form_fail_test.go`（3 例，含**双向**字段清单一致性）、
`product_create_form_fail_render_test.go`（2 例：失败片段回填 / 首屏无回填）。

## 4. 验收（浏览器实测，非推断）

服务：`tmp/gowp-dev`（新构建）→ `http://127.0.0.1:8080/admin/products/new`，一键登录。

| 场景 | 实测结果 |
|---|---|
| bundle + 无套餐价提交 | URL 停在 `/admin/products/new`；错误槽「捆绑商品必须自定价：容器价需大于 0」在表单**上方**；host 与 form 各 1 个 |
| 回填保真 | name / type=bundle / externalSku / quantity=7（且未 disabled，跟踪数量勾选态还原）/ 属性组**恰好勾中我勾的那 1 个** / 仓 1 个 |
| **P1-1 复验** | 手填 SKU `MYCODE_B` → 提交失败 → 增强脚本跑完后 SKU **仍是 `MYCODE_B`**（修复前会被覆盖成派生值 `1P1_B`） |
| 成功路径 | 跳转 `/admin/products/edit?product=…&project=…`，页面壳完整、无片段残留（`HX-Redirect` 生效） |
| 测试数据 | 已删除（列表页删除表单 + 空态确认） |

## 5. 对抗式审查发现的三个 P1（均已修复）

**P1-1 · bundle 失败后增强脚本覆盖用户手填的 SKU（本批目标反向失效）**
`product-create-form.js` 的 `var dirty = false` 是 `enhance()` 的闭包局部变量，每次增强重建；
失败回填后 `apply(true)` 见 `dirty=false` 就把回填值改写成派生建议值 —— 用户改了编码、只忘填价格，
补一次价格再提交，落库的是建议值，页面上不留痕迹。
修法：`var dirty = trim(input.value) !== ''`（首屏值为空，预填行为不变）。实测已复验。

**P1-2 · 抽屉路径已无入口，真实入口的原生回退落点却是错的**
`tpl-product-create` 全仓库没有任何 `data-drawer-open` 指向它（两个入口都已是整页链接），
是「渲染了却永远打不开」的死块；而 `products_new.html` 的错误槽（读 `?err=`）**没有任何生产者**
（全仓库没有第二处重定向到 `/admin/products/new?err=`）。
修法：删死 template + 新增 `productNewURL`，原生档改用 `302 + ?err=` 回**本页**。

**P1-3 · 「Jet 缺键 → 200 + 截断」是过时论断**
真实行为：`internal/templates/jet_render.go` 先渲到 buffer，`Execute` 失败走 `renderError`
→ `http.Error(500, "页面暂时无法显示，请稍后重试")`，半截内容被丢弃；htmx 2.0.4 默认
`responseHandling` 把 5xx 判成 `swap:false` —— **片段根本不换，用户看不到任何反应**。
影响：字段清单漏列时不是「能看出来」，而是「点了保存没动静」。修复三件：
① 补**反向**一致性测试（模板读的每个 `FormEcho*` 字段都必须在清单里，反向也钉）；
② 片段头注释、`formEchoData` 注释（product + inventory 两处）改写成实测形态；
③ 规则文件收口 —— `internal/templates/CLAUDE.md` 三处 + `AGENTS.md` 一处
（仓库里仍有若干 handler / 测试注释沿用旧说法，后续碰到顺手改）。

**未采纳的两条**（记录判断依据）：
· P3-5「工程无仓库时失败回填会吞掉数量/属性组」—— 那些块本身依附于 `len(cfWarehouses) > 0`，
  无仓库的工程页面上**根本没有这些控件**，用户无从填写，不构成丢失。
· P2「分档工具在两个包整份复制」—— 属既有结构（批 0 的地基就如此），本轮记在此处待收敛，
  不在本批扩面。

## 6. 剩余入口（批 2~7）

- **批 2**（进行中）：商品属性域的属性组 / 属性值抽屉
- 批 3：分类 / 品牌 / 标签抽屉
- 批 4+：库存（仓库 / 原因）、货源、采购
- 每批照本批契约；**字段清单要正反双向断言**，迁移号段从 **430** 起（429 已被裸 key 批次占用）

## 7. 相关：裸 key 同病批次（本会话并行完成，一并记录）

白名单值是 i18n key、而**页面出口直接渲染**（不走 `pkg/response` 的 translate）→ 页面显示裸 key。
判据只能**读值**（同一 enums 包里 key 形态与中文常量形态并存）。
修 3 处共 31 个渲染点：`user`（前台访客页 19 处）、`runtimefragment`（11 处）、`analytics`（1 处）；
另确认 14 份白名单已合规（`cart` 只有 API 出口，无需取词）。
补迁移 **429**（4 个「已进白名单、却从未登记词条」的 key × 2 语言 = 8 行，已落库核对）。
`analytics` 的 `ErrInvalidRange` 文案里的「366 天」已与 `maxRangeDays = 366` 对齐（不凭记忆写数字）。

## 8. 顺带扫出的一类静默缺陷：「同名隐藏域打底 + 复选框」

批 2 在属性页发现真 bug：模板是 `<input type="hidden" name="isVariation" value="0">` **在前**、
`<input type="checkbox" name="isVariation" value="1">` 在后，而 handler 用 `c.PostForm("isVariation")`
——它**只返回第一个值**（`url.Values.Get` 语义），于是恒取到 `"0"`：**用户的勾选被静默丢弃**
（页面 200、值照常落库、不报错）。`values[n].enabled` 更糟（`!= ""` 恒真，禁用永不生效）。

修法：判「同名多值里是否存在 1」（`formValueHas`）；「字段完全没给」要返回 **nil** 而不是 false
（缺省即启用的字段，service 兜底为 true）。

**同病扫描结论**（`rg 'type="hidden" name="X" value="0"'` 全模板）：全仓只有 4 处 ——
`product_attribute_group_form.html`（isVariation）、`product_attribute_rows.html`（values[n].enabled）
两处是真问题（批 2 已修）；`mail.html` / `mail_marketing.html` 的 `id value="0"` 是各自 form 里
**唯一**的 id 字段（表示新建），无害。

判据值得记住：**「打底 hidden + 同名 checkbox」这种让字段「总是存在」的写法，必须配「遍历多值」的读取**，
用 `PostForm` / `Get`（取第一个）读就一定会踩 —— 而且踩了不报错。
