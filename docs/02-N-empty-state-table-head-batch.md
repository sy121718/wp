# 「空态吃掉表头」全站收口批次记录

> 判据、方法论与逐条修复的证据链。工作清单的索引在 `docs/02-L-admin-rework-worklist.md` §0.8。

## 1. 缺陷形态

列表页写成：

```jet
{{if len(.Rows) == 0}}
<div class="empty-state">…</div>
{{else}}
<form …><table class="data-table"><thead>…</thead>…
```

空数据 / 筛选无结果时**整张表连同表头一起不渲染**：用户看不到有哪些列，
也无从确认自己是不是筛错了列 —— 比看到一张只有表头的空表格更困惑。
它不会让任何既有测试变红，HTTP 恒为 200，靠人眼发现需要「恰好在那页构造出空数据」。

## 2. 正确形态（样板：`internal/templates/admin/permissions.html:55-75`）

`<table>` / `<thead>` 留在 `{{if}}` **外面**，只有 `<tbody>` 内容分档；
空态进 `<tbody>` 的一个整行：

```jet
<tbody>
{{if len(.Rows) == 0}}
<tr><td colspan="{{if canDelete}}9{{else}}8{{end}}" class="cell-wrap">
    <div class="empty-state">…原样搬进来，文案与 data-* 一字不改…</div>
</td></tr>
{{else}}
{{range _, r := .Rows}} …正常行… {{end}}
{{end}}
</tbody>
```

三条易错点：

1. **`colspan` 必须等于该表实际渲染的列数**（含勾选列等条件列）。写错不会报错，
   只会让空态那一行与表头对不齐 —— 视觉错位，评审极易放过，所以有专门的测试钉它（§4）。
2. **空态行里不要渲染 `name="ids"` 控件**（表头 `data-check-all` 保留，与样板一致）；
   批量条加 `{{if len(...) > 0}}` 守卫，空表不渲染无可勾选对象的控件。
3. **批量 form 的 hidden 与批量条若原本在 `{{else}}` 里，必须一起提出并包上 `len>0` 条件** ——
   否则空态路径会去求值 `.Page` / `.Limit` 这类只由有数据分支提供的键，
   而 Jet 缺 key 会让**整页后续 HTML 消失**（HTTP 仍是 200）。

## 3. 范围与收口读数

静态门禁首次扫描命中 **57 处 / 36 个文件**，按域分四路修复，每路的读数都由父 agent
用独立 Chrome 实例复测（不采信子代理自报）：

| 批 | 文件数 | 复测读数（空态 `thead` / 列数 / `colspan`） |
|---|---|---|
| 管理域 | 5 | departments（真 0 行）与 i18n（零写库构造）`thead=1`、`colspan` 与列数精确相等（7/7） |
| 商品库存域 | 11 | sources 空态与筛选态、inventory 空态均 `thead=1` |
| 跨域补漏 | 6 | content_templates `cols=8 colspan=8`、site_slots `cols=4 colspan=4`、orders `cols=8 colspan=8` |
| 其它模块域 | 17 | analytics 5 表 rows 5/14/2/2/2、cols 3/3/3/3/3；mail rows 1/4；masterdata rows 20/50 |

收尾：`bash scripts/check-empty-state-table-head.sh` → `✓ 未发现「空态吃掉表头」的列表页
（已扫描 74 个后台模板；8 个已登记的豁免仍待接手）`，exit 0。

## 4. 两层守卫（分工，别互相替代）

| 层 | 位置 | 管什么 |
|---|---|---|
| 静态门禁 | `scripts/check-empty-state-table-head.sh` | 全部后台模板，抓「`<table>` 还在 `{{else}}` 里」这个**形状**；管不到 colspan 对不对 |
| 渲染级守卫 | `internal/templates/admin_list_empty_state_test.go` | 覆盖运行期构造不到空态的页面（menus / navigations / dashboard），并钉住 **colspan 精确等于列数**（含 `canDelete` 两种形态） |

渲染级守卫的**反向验证**：把 `departments.html` 的 `colspan` 从 7 改成 8 →
立即报 `期望 colspan="7" 未出现（空态行会与表头对不齐）`；md5 确认还原无误。

## 5. 门禁脚本自身的一个 bug（已修）

判据靠栈解析 Jet 的 `{{if}}/{{range}}/{{block}}` … `{{else}}/{{end}}`。
**注释里的字面 token 会把栈带偏**：某代理在注释里写了「表头在 `{{range}}` 里提不出来」，
`TOKEN` 正则把注释里的 `{{range}}` 当成真 token 压栈，此后每个 `{{end}}` 都少配一层、
`{{else}}` 配到伪节点上 —— 实测结果是**同一文件先被误报、另一文件反而侥幸漏报**。

修法：`strip_comments()` 在配对前把 `{* … *}` 换成**等长空白并保留换行**（行号与原文一一对应，
报错行号仍可直接定位），注释里的 HTML 关键字也不再参与判定。
修完命中 12 → 10 处，且 `mail_marketing.html` 的报错行从 `:97` 变成 `:173`（配平修正后的真实位置）。

**结论**：门禁绿不等于没有缺陷，门禁红也要先看它是不是读错了地方。

## 6. 八条豁免的类别（判据对它们不适用，不是「放过」）

| 类别 | 文件 | 为什么表头不该出现 |
|---|---|---|
| 整页前置引导（无站点工程） | settings / navigations / blocks / theme | 没有工程时整页只剩一张引导卡（theme 连 `page-head` 都不渲染），页面此时没有列表语义 |
| 整页异常 / 缺参引导 | product_edit、product_detail（商品不存在）、product_detail_template（能力未装配） | 主对象取不到，页内所有列表都无从渲染 |
| `{{else}}` 段不是列表 | product_pricing（调价留痕） | else 段是**逐批 `<details>` 披露块**，其中的 `<table>` 是某一批的下钻内容 |

每条都写明了「本文件真正的列表空态已按样板修复」，避免豁免把该文件的列表缺陷一起藏起来。
反面标准写在清单头部：**空数据时用户仍需要知道有哪些列、能按什么筛的，表头就必须在**。

## 7. 本轮顺带纠正的两处不可靠结论

1. **迁移 402 并未落库**（子代理报「已随服务重启落库、页面显示库值」）。
   本地 `config.yaml` 是 `run_migrations: false`，迁移不会自动跑；按 7 个精确 key 回查为空 ——
   该代理很可能是用 `LIKE '%inventory%'` 查库，把 191 等旧 seed 的行当成了自己的。
   已按迁移器同一份语句手动应用（`INSERT 0 14`、幂等重放 `INSERT 0 0`、判定返回 1）。
   **教训**：子代理报「已落库 / 已生效」时必须用**精确 key**回查。
2. **`location` 断言成立 ≠ 页面存在**（我自己的验证失误）。`/admin/dashboard` 是 404，
   而浏览器把 JSON 响应也包成文档、`location.pathname` 仍是那条路径，于是「到位判定」通过了，
   三条读数被当成「这页没有表格」的证据用。已给验证台加 `document.contentType` 断言，
   并写入 `docs/02-J-admin-ui-structure-checklist.md` **H13**。

## 8. 相关文件

- 门禁：`scripts/check-empty-state-table-head.sh`、豁免 `scripts/empty-state-table-head-allow.txt`
- 守卫：`internal/templates/admin_list_empty_state_test.go`
- 迁移：`public/migrations/402_i18n_product_inventory_empty.sql`（+ 注册文件，已手动应用）
- 记录：`docs/02-L-admin-rework-worklist.md` §0.6–0.8、`docs/02-M-product-inventory-audit.md`
