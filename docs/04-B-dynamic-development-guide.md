# 04-B · 动态能力开发指南（How-To）

> 本文是面向**开发者**的操作手册：拿到一个「页面要动态」的需求时，怎么选路径、怎么落地、怎么验收。
> 设计依据与协议规范见 [04-runtime-and-delivery.md](./04-runtime-and-delivery.md)（协议权威）与
> [04-A-dynamic-capabilities.md](./04-A-dynamic-capabilities.md)（能力规划）；
> 与复用资产的分工（区块能吃什么动态）见 [02-D-reusable-assets.md](./02-D-reusable-assets.md) §6。

## 0. 三十秒决策树

```
这个交互能不能在构建期把结果算好？
├── 能（固定分类列表、静态文章正文）
│     → 【路径 A】构建期集合/字段绑定（编译进静态产物，零运行时成本）
├── 不能，需要服务端状态/认证/实时查询/写操作
│     → 【路径 B】HTMX Runtime Fragment（Go Handler 返回 HTML 片段）
└── 不能，但只需要浏览器本地行为（轮播、折叠、复制文本）
      → 【路径 C】Client Enhancement（组件增强属性，纯前端）
```

判断标准只有一条：**构建期算得出的，绝不放到运行时**。静态主体保证确定性、可 CDN、
零查库；Fragment 只承接「算不出」的部分，且必须走白名单。

## 1. 路径 A：构建期绑定（静态产物里的「动态数据」）

### 1.1 适用

商品/文章**固定条件**列表、CMS 单实体字段（标题/正文/价格）——发布时可确定的数据。
产物是纯静态 HTML，访客零查库。

### 1.2 两种绑定（分工，见 02-D §6）

| 绑定 | 写法 | 谁消费 | 典型产物 |
|---|---|---|---|
| 集合型 CollectionSource | source=`content:product` + filter（白名单键值） | `core.CollectionResolver`（构建期） | 商品列表、营销卡列表 |
| 单实体型 FieldBinding | `product.name` | ContentResolver（构建期派生快照） | 商品详情、文章正文（ContentTemplate/presentation 路径） |

### 1.3 开发步骤（新增一个集合消费组件为例）

1. 组件目录 `internal/builder/components/<name>/`：实现 `core.Register` 的 Component，
   PropsSpec 里声明绑定字段（source + filter 键，全部白名单枚举）。
2. 构建渲染里经 `RenderContext` 拿到 `CollectionResolver`，调
   `ResolveCollection(ctx, source, filter)` 取 `[]map[string]any`，把字段值编译进 HTML。
3. 字段只允许来自集合源声明（`core.CollectionSource.Fields`）——组件渲染未声明字段即缺陷
   （不变量 4：Binding 不是 Query DSL，Document 不能存 SQL/表达式/endpoint）。
4. 补组件包内测试：确定性构建（同输入同字节）+ fuzz（`internal/builder/components/*` 惯例）。

**注意**：集合型绑定可以放进**区块**（绑定的是「类型」不是具体实体，天然可全局复用）；
单实体型绑定**禁止**进区块，只能走 ContentTemplate → presentation 派生路径。

## 2. 路径 B：HTMX Runtime Fragment（真正的运行时动态）

### 2.1 适用

筛选/搜索/无限滚动、购物车角标、登录态、库存实时数——需要 session、查库或写操作的。

### 2.2 架构（已落地，0-D）

```
页面静态 HTML（构建产物）
  └── <div hx-get="/_fragments/{type}" hx-trigger="..." hx-swap="outerHTML">
        ↓ HX-Request
FragmentEndpoint（internal/module/runtimefragment/endpoint.go）
  → Registry 白名单 Lookup(type)
  → 认证策略（anonymous / session）
  → 参数限制（≤10 个、key ≤64、value ≤200、context 枚举）
  → Spec.Render(ctx, Request) → HTML 片段（统一 escape 边界）
        ↓
HTMX 局部替换 DOM
```

安全不变量：Handler **不读 Page Document、不执行 Jet 页面模板、不接受任意 endpoint**
（type 必须在 Registry 内）；输出的用户数据必须经 `html.EscapeString`（或 Jet 模板默认转义）。

### 2.3 开发步骤（以 productAvailability 为例）

**① 注册 capability**（`internal/module/runtimefragment/capability.go` 或新文件包 init）：

```go
func init() {
    Register(Spec{
        Type:   "productAvailability", // 能力类型（白名单 key，页面只保存这个）
        Method: "GET",
        Auth:   AuthAnonymous,          // 需要登录的用 AuthSession（endpoint 自动校验 session）
        Render: renderProductAvailability,
    })
}

func renderProductAvailability(ctx context.Context, r *Request) (string, error) {
    id := r.Params["id"] // 参数已被 endpoint 长度/数量限制；业务侧再做格式校验
    // ... 查库存（经对应模块 contract，参数化查询，传播 ctx）
    return templates.RenderFragment("product_availability",
        struct{ Stock string }{Stock: html.EscapeString(stock)})
}
```

**② 片段模板**：`internal/templates` 下注册 fragment 模板（`templates.RenderFragment`），
Jet 渲染、用户数据默认转义；复用主题 token 的样式类。

**③ 页面挂载**：组件（或区块）在构建产物里输出 HTMX 属性：
`hx-get="/_fragments/productAvailability?id=xx" hx-trigger="load" hx-swap="outerHTML"`。
Document 里只存语义化 type + 白名单参数，**绝不存完整 URL 或脚本**。

**④ 测试**（对齐 `endpoint_test.go` 惯例）：
- 已知 capability 渲染 200 + 未知 capability 404；
- session 策略未登录 401；
- 参数超限（11 个 / 超长）400；
- 输出 escape（注入载荷不落原样）。

### 2.4 认证与 CSRF

- GET（无副作用）：anonymous 或 session，无需 CSRF。
- POST（写操作）：`Method: "POST"` + `Auth: AuthSession`，前端 HTMX 请求带
  `X-CSRF-Token`（workbench 页面已全局注入；公开站点 fragment 需在页面模板 head 注入）。
- context 枚举（`validateContext`）：currentProduct / visitorSession / searchQuery，
  新增语义先扩这个白名单。

## 3. 路径 C：Client Enhancement（纯浏览器）

轮播（slider）、折叠（accordion/tabs/faq）、计数动画、复制文本——组件的增强属性
（增强 JS 挂在组件渲染产物上，无网络请求）。开发方式见各组件包
（`internal/builder/components/slider` 等）：构建期输出语义骨架 + `data-*` 属性，
公共增强脚本按类型初始化。**任何需要网络的交互不属于这条路径**。

## 4. 与复用资产的分工（衔接 02-D）

| 资产 | 可带动态 | 禁止 |
|---|---|---|
| Block（global 引用） | 集合型绑定（content:product 列表）、静态无绑定 | 单实体绑定、任意 endpoint |
| Block（template 复制） | 同上（复制时绑定随 AST 一起带走） | 同上 |
| ContentTemplate → presentation | 单实体字段绑定（product.name） | —（这是它的专属能力） |
| 任何 Page Document | 语义化 fragment type + 白名单参数 | SQL、过滤表达式、完整 URL、内联脚本 |

## 5. 验收清单（合并前自检）

- [ ] 路径选对：构建期能算的没有放到运行时（决策树 §0）
- [ ] 白名单：新增的 capability / 集合源 / 过滤键 / context 枚举全部注册且封闭
- [ ] Document 不变量：页面/区块文档里没有 SQL、表达式、任意 endpoint
- [ ] escape：fragment 输出的用户数据全部转义（有注入测试用例）
- [ ] 认证/CSRF：写操作 session + token；匿名只读
- [ ] 参数限制：超长/超多参数 400（有测试）
- [ ] 确定性：路径 A 产物有 determinism/fuzz 测试
- [ ] 性能：fragment 不查 Page Document、不执行页面 Jet 模板；查询参数化 + ctx 传播

## 6. 已落地索引（2026-09）

| 能力 | 位置 | 状态 |
|---|---|---|
| Fragment Registry + Endpoint | `internal/module/runtimefragment`（registry/endpoint/capability/router） | ✅ |
| 内置 capability：loginPanel / cartSummary | `capability.go` | ✅（cartSummary 数据占位） |
| 集合解析契约 CollectionResolver | `internal/builder/core/collection.go` | ✅ |
| 集合消费组件（list 等） | `internal/builder/components/list` | ✅ |
| 规划中：productAvailability / searchResults / 无限滚动 | 见 04-A §3 | 待做 |

> 新增动态能力后请同步更新 §6 索引与 04-A §3 能力清单（同批提交，避免文档漂移）。
