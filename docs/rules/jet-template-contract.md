# Jet 模板契约（取词 / 缺键 / 存在性判定）

> 本文是 **task-155（P0-2）的实测结果 + 定稿契约**。判据一句话版在 `AGENTS.md` §「模板渲染（Jet v6）」与
> `internal/templates/CLAUDE.md`；本文给论证、实测矩阵与迁移分档。
> 探针：`internal/templates/jet_template_contract_probe_test.go`（11 个用例，36 个子测，全绿）。

## 0. 结论速览

1. **不新增 `hasVal`。** `isset` 与 dd.txt 4.2 的 `hasVal` 在**全部取值格上逐格一致**（17 格，见 §2），
   而 `isset` 额外覆盖「键不存在」这一档 —— 那一档 `hasVal` **求值即报错**，根本守卫不了。
   复用 `isset` + 生产 `isNil`（`internal/templates/funcs.go:314-324`），禁止再写第二份判据。
2. **语言/翻译器只能走 data 注入**（`adminui.Prepare` / `adminui.PrepareBare`）。`Set.AddGlobal` 是
   **Set 级（进程级单例）**，请求级语言放那里必然串词。片段侧的注入已由 `PrepareBare` 收口（§4.6）。
3. **三种 `.["t"]` 调用形态全部不动**（直调 3462 处 / `{{tr := .["t"]}}` 声明 116 处 /
   `map("t", .["t"], …)` 传参 11 处）。它们不是「待迁移的存量写法」，而是**本仓 Jet 版本下各自唯一可行的形态**。

## 1. 全站规模（实测，`internal/templates` 下 `*.html`）

| 指标 | 数量 | 命令 |
|---|---|---|
| `isset(` | **488** 处 / 85 文件 | `rg -o 'isset\(' -g '*.html' . \| wc -l` |
| `["t"]` / `["tf"]` 索引 | **3600** 处 / 138 文件 | `rg -o '\["t(f)?"\]' -g '*.html' . \| wc -l` |
| 　└ 直调形态 `.["t"](…)` | 3462 | `rg -o '\.\["t(f)?"\]\s*\(' …` |
| 　└ 声明形态 `:= .["t"]` | 116 | `rg -o ':\s*=\s*\.\["t(f)?"\]' …` |
| 　└ `map(...)` 参数位传参 `["t"]` | 11（11 个文件） | `rg -o '"t(f)?", \.\["t(f)?"\]' …` |
| 　└ 模板注释文本里 | 11 | 余额 |
| `tr(` 调用 / `tf(` 调用 | 1985 / 30 | —— |
| 模板文件总数 | 147 | —— |
| 其中 `admin/product/` | 24 个文件，837 处索引 + 150 处 isset | —— |

**对 dd.txt 的一处纠正**：dd.txt 说「24 个 Jet 模板」是**全站量级的严重低估** —— 24 是
`admin/product/` 一个目录的文件数（实测 24 个 `.html`），全站是 **147 个模板 / 138 个含取词调用 / 3600 处索引**。
同理 dd.txt 的「迁移 24 个模板的 `.["t"]`」实际要面对 3600 处索引 + 1985 处 `tr(` 调用。

按模块分布（`["t"]`/`["tf"]` 索引数 / `isset(` 数 / 文件数）：

    目录                     t/tf  isset   文件
    admin/（整棵子树）        3454    477    125
      ├ admin/product          837    150     24
      ├ admin/inventory        427     69      9
      ├ admin/mail             416     27     10
      ├ admin/system           320     89     17
      ├ admin/user             210      4      6
      ├ admin/content          199      8      5
      ├ admin/project          161     24      3
      ├ admin/page             149     27      5
      ├ admin/order            101     10      7
      ├ admin/membership        96      7      2
      ├ admin/analytics         83      7      3
      ├ admin/masterdata        68      6      1
      ├ admin/block             55      4      2
      ├ admin/media             44      0      1
      ├ admin/partials          38      3     12
      ├ admin/plugin            38      2      1
      ├ admin/contenttemplate   34      1      1
      ├ admin/ai                 8     24      8
      └ admin/comment            1      4      1
    fragments/                  23      0      9
    partials/                   20      7      3
    user/                       99      0      8
    workbench/                   4      4      2

## 2. A：`isset` 语义矩阵（实测，全格一致）

命令与用例：

    go test ./internal/templates/ -run TestJetContractIssetVsHasValMatrix -count=1 -v

探针模板（两种取值形态各钉一次；`jcHasVal` = 生产 `isNil` 取反，**不另写判据**）：

    field=[{{if isset(.X)}}T{{else}}F{{end}}] index=[{{if isset(.["X"])}}T{{else}}F{{end}}] hasVal=[{{jcHasVal(.X)}}]

| 格 | isset(.X) | isset(.["X"]) | hasVal | dd.txt 4.2 期望 | 一致 |
|---|---|---|---|---|---|
| 裸 nil（键存在、值为 nil 接口） | F | F | false | false | 是 |
| typed nil `*float64` | F | F | false | false | 是 |
| typed nil `*struct` | F | F | false | false | 是 |
| 指向 0 的指针 `*float64(&0.0)` | T | T | true | true | 是 |
| 数字 int `0` | T | T | true | true | 是 |
| 数字 float64 `0.0` | T | T | true | true | 是 |
| 布尔 `false` | T | T | true | true | 是 |
| 空字符串（空串） | T | T | true | true | 是 |
| 非 nil 空 map | T | T | true | true | 是 |
| nil map（`var m map[string]any`） | F | F | false | false | 是 |
| 非空 map | T | T | true | true | 是 |
| 非 nil 空 slice | T | T | true | true | 是 |
| nil slice（`var s []string`） | F | F | false | false | 是 |
| struct 零值 | T | T | true | true | 是 |
| 非 nil 函数值 | T | T | true | true | 是 |
| typed nil func | F | F | false | false | 是 |
| typed nil chan | F | F | false | false | 是 |

**17 格全一致**，且 Go 侧 `!isNil(v)` 与每一格同值（用例同时断言三列，任一侧漂移即红）。

守卫**形态**矩阵（`TestJetContractGuardShapeMatrix`，结构体字段 vs map 键）：

| 形态 | isset | hasVal | 说明 |
|---|---|---|---|
| `.Row.ComparePrice`（nil 指针字段） | F | false | 结构体上唯一有意义的一档 |
| `.Row.ComparePrice`（指向 0） | T | true | —— |
| `.Row.Name`（空串字段） | T | true | **isset 不是「非空」判据** |
| `.Row.Opts`（nil slice 字段） | F | false | —— |
| `.Row.Opts`（空非 nil slice） | T | true | —— |
| `.Row.NilPtr`（嵌套 nil 指针） | F | false | 嵌套字段同样成立 |
| `.["K"]`（键存在，空串） | T | true | map：问「键在不在」 |
| `.["K"]`（键存在，nil 指针） | F | false | map：键在但值为 nil 也算「没有」 |

语义的准确说法：`isset` = 「取值路径可达 **且** 拿到的值不是可 nil 种类的 nil」
（Jet `notNil`，GOMODCACHE `…/jet/v6@v6.3.2/eval.go:731-741`，Kind 名单与 `funcs.go:314` 的 `isNil` 逐一对应）。

## 3. B：裁定 —— 不新增 `hasVal`

### 3.1 依据

- **取值格全一致**（§2 两张表）→ `hasVal` 在能求值的输入上**没有任何新增信息量**。
- **唯一差异格对 isset 有利**（`TestJetContractMissingKeyIsTheDifferenceCell`，实测原文）：

      isset(.Missing)      → issetField=[F] issetIndex=[F]，err=nil
      hasVal(.Missing)     → Jet Runtime Error ("/probe/hasval_field.html":1): there is no field or method 'Missing' in map[string]interface {} (.Missing)
      hasVal(.["Missing"]) → Jet Runtime Error ("/probe/hasval_index.html":1): call expression: argument for position 0 in func(interface {}) bool is not a valid value

  即：`isset` 是**语法层**守卫（Jet `Runtime.isSet` 直接查节点、不求值，`eval.go:741-785`），
  缺键给 F；`hasVal` 的实参先求值，缺键直接 500。
  这正是全站 488 处 isset 的**主要用途**，仓库里已有两处同款记录：
  `admin/partials/ai_fab_result.html:19-20`（原文：「Jet 对 map 上缺席的键求值会抛运行时错误
  （there is no field or method 'FabError'，整段 500），而 isset 对缺席安全」）、
  `admin/product/products.html:100-102`（原文：「gojet 对 map 缺键会在取值时报错，所以先 isset 再取」）。

### 3.2 会被这两张表影响的具体调用点

- **缺键守卫档（不动，488 处的主体）**：`isset(.ListQuery)` 37 处、`isset(.Err)` 32 处、
  `isset(.LoadFailed)` 11 处、`isset(.FormEcho)` 11 处、`isset(.Buttons["product.create"])` 之类。
  这些点在数据集里**整键省略**（判据见 `internal/module/product/service/product_row_options_test.go`、
  `product_bundle_page_test.go:101-104`），换成 `hasVal` 会从「静默降级」变成 500。
- **map 行的缺键守卫（有效，不动）**：`admin/product/products.html:103`、`:200`、
  `admin/product/product_bundle.html:99`、`:117`、`admin/product/product_edit.html:136`/`:225`/`:232`
  —— 这些行是 `map[string]any`（`internal/module/product/service/product_row.go:373`），isset 问的正是「键在不在」。
- **结构体字段上的 isset（部分恒真，登记为清理项，见 §6.2）**：全站 63 处形如 `isset(x.Field)`
  （`admin/inventory/inventory.html` 8、`admin/product/products.html` 7、`admin/inventory/inventory_warehouses.html` 7、
  `admin/system/menus.html` 6、`admin/project/theme_settings.html` 5 …）。在结构体行上，除指针/slice/map
  字段外**恒为真**（如 `theme_settings.html:84` 的 `{{if isset(f.Invalid)}}` 之后紧跟 `{{if f.Invalid}}`，
  真正起作用的是后者）。这类守卫**不是错误**（多数外层已显式判空），只是冗余。

### 3.3 硬约束：不得出现第二份判据

- 生产判据只有 `internal/templates/funcs.go:314-324` 的 `isNil`（消费者 3 处：`stringify` `:267`、
  `percentChange` `:284`、`percentChangeDir` `:300`；code-review-graph `callers_of isNil` 实测 3 条）。
- 前向闸门 `TestJetContractHasValIfEverRegisteredMustEqualNotIsNil`：**将来若有人注册 `hasVal`，
  它必须逐格等于 `!isNil`**，否则该用例变红（实测输出：`生产未注册 hasVal（本次定稿：复用 isset + isNil）—— 本用例作为前向闸门待命`）。
- `hasVal` 覆盖不到、真正需要新增的只剩 **dd.txt 4.1 的另一行 `isNonBlank`**（「非空白」那一档）：
  `isset` 对空串 / `0` / `false` 恒为真（§2），所以「只在该值非空时才渲染」必须显式写
  `{{if p.BrandName != ""}}` —— 现网已有这种写法（`products.html:242`/`:245` 的 `{{if isset(p.BrandName)}}{{if p.BrandName != ""}}`）。
  **本批不动 `funcs.go`**；要加 `isNonBlank` 属另一个话题（它不替代 isset，只在需要「有可见内容」处使用）。

## 4. C：取词通道（Set 级 / per-render / Runtime）

模型版本：`go.mod:6` `github.com/CloudyKit/jet/v6 v6.3.2`（源码在 GOMODCACHE，行号按该版本）。

### 4.1 `AddGlobal` 是 **Set 级**，只有数据通道能带请求状态

- `set.go:20` `globals VarMap // global scope for this template set`；`set.go:214-222` `AddGlobal` 写入该表；
- `eval.go:179-215` `resolve()` 的查找顺序：**当前/父作用域 variables → Set globals → Jet 内置 defaultVariables**；
  数据 map（`.` 的键）不在标识符作用域里，只能经 `.` 取。
- `Set` 由 `internal/templates/jet_render.go:42-49`（`NewJetHTMLRender`）在启动时创建，**进程级共享**；
  既有实测 `i18n_jet_test.go:TestJetSetGlobalIsProcessShared` 已钉住「改全局 = 改所有渲染」。

### 4.2 **per-render 变量通道存在**，且被 include / import / yield 继承

- `exec.go:55` `func (t *Template) Execute(w io.Writer, variables VarMap, data interface{})` ——
  第一个参数就是每渲染变量表；
- `eval.go:88-93` `newScope()` 以 parent 链挂作用域，`eval.go:604-631` `executeInclude` 也走 `newScope()`；
- 实测 `TestJetContractPerRenderVarsChannelReachesInclude`：父模板 `{{include "child.html"}}` + 子模板
  `{{t("k","兜底")}}`，以 `variables` 传 `t` → 输出 `P[L:k]`，且该 Set 的全局表里**没有** `t`。

### 4.3 但**当前驱动不暴露这条通道**，而它带来的能力已被 data 通道完全覆盖

- 驱动把第一个参数传 nil：`pkg/view/driver/jet/jet.go:62` `t.Execute(&buf, nil, vars)`；
  `driver.Driver.Render(name string, vars map[string]any)` 只有一个数据通道，两个通道不可兼得。
- 三种形态在**现有 data 通道**下全部可用（`TestJetContractMapArgAndAssignmentShapes` 实测）：

      ① {{tr := .["t"]}}{{x := tr("k", "兜底")}}                    → out="[L:k]"  err=nil
      ② {{include "child_map.html" map("t", .["t"], "p", "P")}}     → out="L:k|P" err=nil（片段内 {{ .["t"](…) }}）
      ③ per-render vars + map("t", t, …)                            → out="L:k|P" err=nil（需先改驱动）

- 已知限制的**边界**（`TestJetContractAssignmentFormDependsOnChannel` 实测）：

      {{x := .["t"]("k", "兜底")}}  → Jet Runtime Error: unexpected node type .["t"] in unary expression evaluating
      {{ .["t"]("k", "兜底") }}     → 正常（直调形态）
      {{x := .["t"]}}               → 正常（**不带调用的**赋值形态）
      {{x := t("k", "兜底")}}        → per-render 变量通道下正常（out="[L:k]" err=nil）

  也就是说：限制只落在「`.["t"]` 直接作为**赋值右侧的调用基**」这一种写法上，而
  `{{tr := .["t"]}}` 收口一次就绕开了它 —— 现网 **0 处**该禁止写法（实测
  `rg -o ':\s*=\s*\.\["t(f)?"\]\s*\(' ` → 0），116 处声明 + 1985 处 `tr(` 调用就是这么来的。

### 4.4 Runtime 是**池化共享**的

`eval.go:38-42` `pool_State = sync.Pool{…}`；`exec.go:57` `st := pool_State.Get()`；
`eval.go:232-237` `pool_State.Put(st)`。每次 `Execute` 都会重设 `st.variables / st.set / st.Writer /
st.blocks / st.context`，但**任何跨 Execute 留在 Runtime 上的自定义字段都会串请求**。
全局函数里唯一能看见「本次渲染数据」的口子是 `Runtime.Context()`，而它会被 `include/exec` 的 context 参数覆盖
（`i18n_jet_test.go:203-215` 已实测并据此否决）——**所以不准用「全局函数 + 可变 currentLocale」这条路**。

### 4.5 方案对比与结论

| 方案 | 可行性 | 结论 |
|---|---|---|
| `Set.AddGlobal("t", …)` | 可行但**进程级** | **禁止**（请求级语言必然串词；§4.1、既有实测用例） |
| data 注入（`adminui.Prepare` / `PrepareBare` 注入 `data["t"]`/`["tf"]`） | **现状，可用** | **保留**（本批定稿） |
| per-render `variables` 通道（需改驱动） | 可行（§4.2） | **不推荐**：①②③ 实测证明 data 通道已覆盖全部写法，收益仅「省掉 116 行声明」，代价是改驱动 + 双形态并存 |
| 全局函数 + `Runtime.Context()` 取语言 | 脆弱（会被覆盖） | **禁止** |

### 4.6 片段（fragment）路径：task-153 已收口，本批跳过

核对结论：**全仓没有 `func PrepareFragment`**（brief 里的名字是计划名）。等价能力由
`internal/adminui/prepare.go:168-173` `PrepareBare` 提供（`injectI18n` 的**全部**调用者只有 `Prepare` `:44` 与
`PrepareBare` 两处，code-review-graph `callers_of injectI18n` 实测 2 条），配套出口
`render.go:51-65` 的 `RenderBare` / `PrepareBareView`。片段侧调用点：`adminui.PrepareBare(` 21 处、
`PrepareBareView(` 2 处；典型：`product/inbound/http/product_attributes.go:219`/`:432`/`:462`、
`page/inbound/http/page_schedule.go:192`、`content/inbound/http/content_page.go:546`、
`workbench/inbound/http/workbench_page.go:1233`、`ai/inbound/http/ai_page.go:194`。**这部分已完成，本批不再动。**

### 4.7 并发不串语言怎么验

    go test ./internal/templates/ -run TestJetContractConcurrentPerRequestTranslatorIsolated -race -count=1

判据写法（关键）：每个「请求」持自己的 `data{"lang":…, "t":闭包}`，渲染结果必须**逐字节等于**自己语言的
期望串（含语言标记前缀）——**不要**断言「至少出现过两种语言」，那种断言在串用时照样绿。
实测：`并发渲染 1280 次（4 语言 × 8 协程 × 40 轮）无串用`，`-race` 下 exit 0。

## 5. 定稿契约（下一批照此执行）

1. **取词三种形态按用途选，禁止自创第四种**：
   - 直接输出：`{{ .["t"]("key", "中文兜底") }}`（3462 处现状）；
   - 需要赋值 / 循环内复用 / 传参：先 `{{tr := .["t"]}}`，再用 `tr(...)`（116 声明 + 1985 调用现状）；
     `tf` 同理（`{{tf := .["tf"]}}`，签名 `func(key, fallback string, args []string) string`）；
   - 组件（`include` 到片段）传取词函数：`map("t", .["t"], …)`（11 处现状）。
2. **禁止** `{{x := .["t"](…)}}`（赋值右侧直调，Jet 语法层拒绝）；禁止把 `.["t"]` 放进 `map(...)` 的
   参数值位置（同源限制，见 `admin/partials/date_filter.html:11-14` 的实测记录）。
3. **存在性判定一律用 `isset`**，不用 `hasVal`（不存在），不用裸 `{{if .Key}}`（缺键整页 500）。
   结构体字段上 `isset(x.Field)` 只对「可为 nil 的字段」有意义；判「非空」必须显式
   `!= ""` / `len(...) > 0`。
4. **请求级能力（语言、用户、权限）只走 data 注入**：整页 `adminui.Prepare`、片段/无壳 `adminui.PrepareBare`。
   新增渲染出口时先问「这次渲染经 `injectI18n` 吗」，漏注入的后果是**静默空文案**（`internal/templates/CLAUDE.md` 已记）。
5. **模板里不查库、不算业务**（`docs/rules/template-boundary.md`）；取词只做「key → 当前语言文案」。

## 6. 下一批迁移批次建议

### 6.1 明确**不动**（占 3600 处索引的 99.7%）

| 形态 | 数量 | 不动的理由（实测） |
|---|---|---|
| 直调 `{{ .["t"](…) }}` / `{{ .["tf"](…) }}` | 3462 | 现网全绿；是唯一「不声明即可用」的形态 |
| `{{tr := .["t"]}}` + `tr(...)` | 116 + 1985 | 绕开赋值限制的**唯一**可行写法（§4.3） |
| `map("t", .["t"], …)` 组件传参 | 11 | 组件取词的标准形态（`date_filter.html` / `bulk_bar.html` 注释已记录） |
| 注释文本里的 `.["t"]` | 11 | 文档性文字，改它没有收益 |
| 全站 `isset(` | 488 | 缺键守卫**不可替代**（§3.1）；其中 map 键守卫必须原样保留 |

### 6.2 需要动的小项（按收益排序，均非本批范围）

1. **加一条静态闸门**（成本最低、防的是下一个代理）：扫 `{{ *:= *\.["t(f)?"]\s*\(` 命中即失败 ——
   该形态 Jet 直接报错，写下来必然踩。现网 0 处，属「终局零容忍」型检查。
2. **结构体字段恒真守卫清理**（63 处形如 `isset(x.Field)`）：先按「x 是 map 行还是结构体行」分类
   （判据：数据由 `product_row.go` 这类 map 装配处产出 → map；由 dto 结构体产出 → 结构体），
   结构体行上除可 nil 字段外删掉 `isset`，只留显式判空。
3. **新增调用点**：按 §5 契约写，不引入 `hasVal`；若将来确实需要「非空白」，先加 `isNonBlank`
   并同时加它与 `isset` 的分工判据（不要合并成一份）。

### 6.3 若 lead 决定统一取词形态（需先改驱动）

补丁骨架（**本批未落**，故意只到「能力上限 + 升级路径」）：

    // pkg/view/driver/jet/jet.go —— 新增能力接口，不改现有 Render（否则 3462 处直调全废）
    // perRenderFuncs 把「每渲染函数表」交给 Jet 的 variables 通道（exec.go:55 的第一个参数）。
    func (d *jetDriver) RenderWithFuncs(name string, funcs map[string]any, data any) ([]byte, error) {
        t, err := d.set.GetTemplate(name)
        if err != nil {
            return nil, err
        }
        vars := jet.VarMap{}
        for k, fn := range funcs {
            vars.Set(k, fn)
        }
        var buf bytes.Buffer
        if err := t.Execute(&buf, vars, data); err != nil {
            return nil, err
        }
        return buf.Bytes(), nil
    }

成本：`pkg/view/driver` 接口 + `internal/templates/jet_render.go` 装配 + 调用点；收益：模板可写
`{{t("k","兜底")}}` / `{{x := t(...)}}` / `map("t", t, …)`。**按 §4.3 的实测，这三件事今天都能用等价写法做到**，
所以建议：不落，除非同时决定把 3600 处索引一起换形态（那是独立的一批，与本次契约无关）。

## 7. 验证命令与实测输出

    cd /home/sky/project/go/wp-p155

    go test ./internal/templates/... -count=1
    # → 718 passed，exit 0（含本次新增的 11 个 TestJetContract* 用例）

    go build ./pkg/... ./internal/templates/...
    # → 无输出，build_exit=0

    go test ./internal/templates/ -run TestJetContract -count=1 -race
    # → 36 passed，race_exit=0

    go test ./internal/templates/ -run TestJetContract -count=1 -v
    # 关键输出（原文）：
    #   格 "裸 nil（键存在、值为 nil 接口）"       → field=[F] index=[F] hasVal=[false]
    #   格 "指向 0 的指针 *float64(&0.0)"         → field=[T] index=[T] hasVal=[true]
    #   格 "nil map（var m map[string]any）"      → field=[F] index=[F] hasVal=[false]
    #   格 "struct 零值 / 非 nil 函数值"           → field=[T] index=[T] hasVal=[true]
    #   hasVal(.Missing)      → err=Jet Runtime Error ("/probe/hasval_field.html":1): there is no field or method 'Missing' in map[string]interface {} (.Missing)
    #   hasVal(.["Missing"])  → err=Jet Runtime Error ("/probe/hasval_index.html":1): call expression: argument for position 0 in func(interface {}) bool is not a valid value
    #   生产未注册 hasVal（本次定稿：复用 isset + isNil）—— 本用例作为前向闸门待命
    #   data 通道赋值形态：Jet Runtime Error: unexpected node type .["t"] in unary expression evaluating
    #   per-render vars → include 继承："P[L:k]"
    #   并发渲染 1280 次（4 语言 × 8 协程 × 40 轮）无串用

**变红性（负向对照，已实测）**：把探针里的 `hasVal` 语义改坏（等价于「动了 isNil」）后重跑矩阵，
6 格立刻 FAIL（`typed nil *struct` / `nil map` / `nil slice` / `typed nil func` / `typed nil chan` …），
还原后 18 个子测全绿 —— 矩阵量的是**两个独立来源**（Jet `isset` vs Go 侧 `isNil`），不是自证。

## 8. 变更记录

- 2026-10 task-155：本文新建。定稿「不新增 `hasVal`、语言走 data 注入、三种 `.["t"]` 形态不动」，
  附 11 个常驻探针（`internal/templates/jet_template_contract_probe_test.go`）。
- 待办（清单外，需 lead 决策）：`docs/rules/README.md` 的「什么时候读哪份」表加一行指向本文；
  §6.2 两项清理与静态门禁尚未落。
