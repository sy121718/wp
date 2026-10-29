# 其他模块 MVC 职责分离规范任务书

日期：2026-10-29。范例：本次商品模块实际改造代码。

## 0. 执行者必须先遵守的规则

**先把本任务范围内所有实现、前后端调用方、装配、权限声明和测试源码全部修改完成，再由一个负责人集中运行测试。**

- 不要改一个函数就运行一次测试，不要每完成一个子任务就构建一次，不要由多个代理重复测试同一工作树。
- 一屏或一域一个提交是提交组织方式，**不等于每个提交都运行一轮测试**。
- 实现阶段允许读取源码、查依赖、编写测试源码、`gofmt`、检查差异；构建、测试、`vet` 和门禁统一放在实现完成后的验收阶段。
- 首轮验收一次收集全部失败，按根因集中修复；随后只复验失败项及此次修复实际影响的范围。
- **没有具体的共享契约变更或跨模块失败证据，不运行 `go test ./...`、`go test -race ./...`。**“保险起见”“顺手跑一下”不是扩大测试的理由。
- 仓库要求的生产构建、相关包及 feature、最终门禁仍必须完成。集中运行不等于省略验收，不得以“代码看起来没问题”代替测试证据。
- 已通过的检查在代码及相关依赖未变化时不重复运行。修改门禁后只重跑受影响门禁；变更需要扩大检查时先记录原因和明确范围。
- PG/Redis 不可用而跳过的用例必须单独报告，不能合并计作通过。最终数据库验收优先使用 `GOWP_REQUIRE_PG=1`，避免数据库缺失变成假绿。

**本文是实施任务书，不是测试通过报告。**商品范例的验收结果由主负责人写入 `public/docs/商品模块MVC改造执行记录.md`，执行者应读取该记录的实际结果和限制。本文不预填任何“已全绿”“已合并”结论，也不宣称全系统已经完成职责分离。

## 1. 可以直接交给 AI 的任务指令

> 在指定模块及其直接前端、装配、权限和测试调用方内，参照商品模块完成 MVC 职责分离。先读取当前 `AGENTS.md` 与适用目录规则，再建立层归属表和接口迁移表；保留业务语义、数据隔离、事务和并发约束。后端业务接口返回事实 JSON，页面成功只传现成对象，界面结构和样式归模板与 JS；失败只给真实 HTTP 状态和统一 JSON，导航由前端显式声明。删除旧形状前查全实际调用方。全部实现和测试源码完成后，再统一构建、测试和门禁，不边改边跑、不无理由运行全量测试。以实际文件差异、接口行为、数据库结果和测试日志交付，不以搜索计数或门禁绿灯代替语义验收。
>
> 开工必须填写：目标模块、业务域、工作树、起始提交、允许修改的共享依赖、文件负责人、集中验收负责人、必须保留的业务行为。未指定具体目标模块时，不自行扩成全仓重构。

## 2. 目标与边界

目标是减少业务与界面之间的相互依赖，让同一份业务事实可以被后台页面、可视化组件、MCP 或其他客户端消费。不是将所有页面改成 JSON，也不是删除所有返回 HTML 的能力。

| 请求类别 | 后端允许承担 | 正确出口 |
| --- | --- | --- |
| 后台控制面页面 GET 成功 | 按授权读取现成业务对象，命名传给页面 | 处理器 `pageview.Render`；router 的 `adminui.PageSlot/BareSlot` 渲染 Jet |
| 页面 GET 失败 | 返回错误事实 | 真实状态码和 `{code,message,data}`，不渲染提示替代页 |
| CRUD、试算、候选搜索、局部刷新、校验等业务请求 | 绑定、业务用例、事实结果 | 统一 JSON；只读也要声明权限 |
| 构建与预览成功 | 在控制面编译文档并传递产物字节 | 已构建 HTML 字节；不能借机由 handler 拼 HTML |
| 构建与预览失败 | 返回失败事实 | JSON；前端在发起页面展示错误 |
| 普通访客页面 GET | 从发布状态定位静态产物 | 静态产物；不查控制面页面数据、不执行 Jet/AST |
| 明确注册的访客业务 API | 按模块契约、鉴权/显式豁免、CSRF、限流处理 | 业务数据 JSON；不能恢复运行时 HTML 片段 |
| 下载、SSE 等既有字节通道 | 按原协议传输 | 保留真实协议，逐项记录，不与业务 JSON 混淆 |

共享 SEO 的算法和其他模块仍使用的 `scoreview` 不是本任务默认清退范围。商品已经迁出的部分可以作为范例；不能因为商品不再使用某类型就删除其他模块仍依赖的共享代码。

## 3. 实施前层归属表

执行者必须给每个具体改动填写本表，不接受只写“优化架构”。一段逻辑先判断归属，再决定改哪个文件。

| 层 | 允许职责 | 应移走或删除的内容 | 商品参考 |
| --- | --- | --- | --- |
| dto/contract | 请求绑定、输入形状校验；响应中的语言无关业务事实；最小接口 | DOM id、HTML、CSS、颜色、标题菜单、控件选中态、已翻译错误句、页面专用包装 | `dto/product_seo_resp.go::SeoScoreResp` |
| service | 用例编排、CRUD、集合与聚合、事务、工程隔离、失败传播 | 页面组装、界面取词 key 字面量、HTML、模板名、导航、错误被吞成空态 | `service/product_detail_template_ops.go` |
| model | 本模块表访问和聚合内原子操作 | 他人表、跨聚合业务事务、界面规则 | 以目标模块 model 现状逐方法登记 |
| enums | 错误 key、兜底、状态码；界面 key 的声明 | 运行时取词、拼成品展示句 | `enums/product_detail_template_enums.go`、`product_err_fallbacks.go`、`product_err_status.go` |
| inbound/http | `binding.*` → 用例 → 响应；页面命名传递原对象 | 第二份业务规则、状态码白名单、拼 map/串、手读参数、视图派生 | `inbound/http/product_score_handle.go::SeoScore` |
| router/assembly | 路由、权限、渲染出口、实例依赖注入 | SQL、业务规则、页面数据计算 | `inbound/http/product_router.go::SetupProductRoutes/SetupProductPages` |
| Jet/HTML/JS | 布局、标题、菜单、空态、显示格式、交互状态、显式导航 | 业务金额求和、重复实现定价/库存/状态机规则 | `admin/product/product_seo_panel.html`、`static/js/ui/product-seo.js` |
| 测试 | 证明业务等式、失败语义、工程隔离、渲染与协议 | 手造生产表、仅计数字符串、依赖内部实现名字的无效断言 | `public/test/product/feature/product_preview_json_test.go` |

对于历史名称不准确但语义仍是事实的字段，按实际含义判定，不能机械搜索后批量删除。例如商品名称、SEO 标题、属性值 `Label`、调价对象 `TargetID` 可能是原始业务事实；SEO 抽屉的 DOM `TargetID`、隐藏控件 `Name/Value` 才是展示形状。

## 4. 商品范例：实际前后变化

| 原形态 | 当前商品实现 | 迁移其他模块时的要求 |
| --- | --- | --- |
| SEO handler 组装评分视图、SERP 标志、DOM 目标和隐藏字段 | service 返回评分事实与覆盖范围，前端生成显示等级、颜色和结构 | 把算法与展示拆开；不得将 `View` 改名 `Resp` 后原样保留展示字段 |
| 商品、分类、品牌评分通过 HTML 片段返回 | 三条独立 JSON API | 前后端同批切换，删除旧 HTML 路由和消费者 |
| SEO 数据查询失败被当作正常空态 | 已接入来源失败向上传播；缺失可选来源明确写入 coverage | 不能用“暂无数据”掩盖数据库或契约故障 |
| 重复标题只读前 100 件商品 | 按批次完整翻页，100 是页大小 | 查询上限不能冒充全量结果 |
| 文章来源无法按工程过滤仍进入索引 | 暂不纳入，`project_filter_unavailable` 明示缺口 | 没有安全范围契约就不混入其他工程数据 |
| 父分类候选返回 `<option>` HTML | 返回原分类对象，前端生成候选显示 | 业务筛选仍在 service，不把树规则复制到 JS |
| 标签命中列表通过 HTML/OOB 更新总数 | 复用 `/api/product/tag/products` 的分页事实 | 列表、总数、翻页由同一结果驱动 |
| 变体预览已经 JSON，但挂在 admin 页面路径 | 迁入 `/api/product/variant/preview` 并声明独立权限 | 不重复重构已经正确的业务计算，只修接口归属与调用方 |
| 预览故障返回 200 提示替代页面 | 成功构建字节，失败 JSON；父页面显示提示 | 客户端先检查状态及类型，再展示预览 |
| 编辑预览未区分当前实例文档与默认模板 | 优先使用绑定实例当前文档，未发布才预览默认模板 | 保留用户当前编辑/发布语义，不只验证“能返回 HTML” |
| 指定复制模板通过无工程读取入口取得 | `GetScoped(projectID, id)` | 实体、模板、实例、列表和计数都带同一工程范围 |
| handler 内空白名/路径校验、400/404 分档、包级依赖 | service 校验、enums 声明错误、处理器实例注入 | 对外错误沿声明处传递，不保留另一套状态表 |
| 认证拒绝根据 Accept/HX 导航或渲染整页 | 共用 JSON 拒绝出口，真实状态、`no-store` | 必须验证整个中间件链，不能只测绕过鉴权的 handler |

商品写入、定价、库存与变体事务已有正确实现的部分应保留。其他模块的任务也应先找出“已正确、需要保留”的部分，不能为了形式统一再造一套接口或业务规则。

## 5. 当前商品路由迁移清单

以下是当前商品代码中的实现路径，不是要求所有模块复制相同命名。实际装配参考 `internal/module/product/inbound/http/product_router.go` 的 `registerProductReadAPI`、`registerProductTaxonomyAPI`、`registerProductTagAPI`、`registerProductTemplatePages`。

| 旧入口/用法 | 当前入口/用法 | 处理器与契约 |
| --- | --- | --- |
| `POST /admin/products/seo-score`，HTML | `POST /api/product/seo-score` | `SeoScore`；JSON/FormData 请求 `projectId`、`id`；JSON 评分事实 |
| `POST /admin/product-categories/seo-score`，HTML | `POST /api/product/category/seo-score` | `CategorySeoScore`；同上 |
| `POST /admin/product-brands/seo-score`，HTML | `POST /api/product/brand/seo-score` | `BrandSeoScore`；同上 |
| `GET /admin/products/seo/drawer`，远程抽屉 HTML | 页面内前端抽屉壳 + 商品评分 API | `entity_seo_drawer.html` 与 `product_seo_panel.html`；不再需要远程抽屉接口 |
| `GET /admin/product-categories/parents`，HTML 候选 | `GET /api/product/category/parents` | `ProductCategoryParents`；`project/projectId`、`keyword` 绑定后调用 `SearchCategoryParents` |
| `GET /admin/product-tags/hits`，HTML/OOB | `GET /api/product/tag/products` | `ListTagProducts`；查询 `id/projectId/page/size`；返回 `tagId/tagName/total/page/pageSize/items`，总数和列表同源 |
| `POST /admin/products/variant/preview`，JSON | `POST /api/product/variant/preview` | `PreviewVariant`；维持原组合试算，不写库 |
| `GET /admin/products/preview-frame` 的 HTML 提示替代页 | 保留路径；成功 HTML，失败 JSON | `ProductPreviewFrame`；请求 `productId`、`project`；`PreviewSavedProduct` |
| `POST /admin/products/template/preview` 包页面槽 | 保留构建预览路径，移除页面槽 | `ProductDetailTemplatePreview`；Form 的 `projectId/productId/templateId`；`PreviewProductDetailTemplate` |

新增业务 API 各有独立权限常量：`ProductVariantPreview`、`ProductSEOScore`、`ProductCategorySEOScore`、`ProductBrandSEOScore`、`ProductCategoryParents`。标签命中复用既有 `ProductTagProducts`，不并行新建同义接口。

商品详情模板的六个写入口继续使用既有 `/api/product/detail-template/{reapply-preset,rollback-document,create,publish,apply,update-url}`。页面 GET 成功仍在 router 通过页面槽渲染；预览权限复用明确的 presentation 预览能力。不能把“只读”当作免授权理由。

## 6. 契约与实现规范

### 6.1 事实结果与页面对象

- JSON 统一 `{code,message,data}`，状态码必须与失败语义一致；禁止 HTTP 200 内塞错误页或错误态 HTML。
- `message` 可以是响应出口翻译后的受控提示。业务数据不携带 HTML/CSS、DOM selector、跳转指令或为界面预排好的句子。
- 页面的多个对象使用 `pageview.Objects(pageview.Object("名称", 原对象), ...)` 命名传递，不合并成一份页面专用 shape，不在 handler 派生总价、分档、空态、菜单。
- 业务数据自身包含用户内容、标题或富文本，不代表它是 UI 越界；但不能因此返回一个“HTML 字段”给客户端直接插入，变相恢复动态片段。
- 用事实字段表达可观测缺口，例如 `coverage.included/omitted`、稳定 reason。前端按 reason 取词，不能向后台索取某个颜色或“显示在第三个按钮旁”的状态。
- 算法事实与显示分档区分：商品评分算法保留在共享 scoring；前端只按分数展示等级与颜色，不另算评分权重或业务分数。

### 6.2 请求绑定与 FormData

- HTTP 输入统一经过 `pkg/binding`：写请求/JSON 只读计算按契约用 `binding.JSON`，查询用 `binding.Query`，已有专门表单预览用 `binding.Form`。
- `binding.JSON` 是项目统一入口，当前同时接收 JSON 与表单体；不要仅凭函数名改成只接 JSON，导致既有 FormData 保存全部失效。
- 同时服务 JSON 与表单的字段应有一致的 `json`/`form` tag。`RawMessage`、多值、带前缀或下标字段使用 DTO 的 `FormBinder/QueryBinder`，不要在 handler 重新遍历 `c.PostForm` 或手解 body。
- JSON/表单是传输形态；两条输入最终必须形成同一用例请求与业务行为，不建立两套 service。
- 单次提交只采集一次有效表单快照。保留空值、未选复选框、重复 name、多选项、数组空位、删除后的行等原语义；不能清掉空项后改变数组对齐。
- 前端负责按字段契约收集数据，service 负责业务校验。前端校验不能代替服务端规则，也不能成为第二份金额/库存/定价算法。
- 参考：`dto/product_page_req.go::VariantPreviewFormReq`、`dto/product_tag_req.go::BindForm`、`static/js/ui/product_variant_write.js`、`static/js/ui/write-base.js::buildBody`。

### 6.3 错误与显式导航

- 错误 key、中文兜底和状态码在所属 enums 声明：参数 400、未登录 401、无权限 403、不存在 404、冲突 409、内部故障 500；既有明确临时依赖故障可保留 503。
- controller 只调用 `response.ErrorStatusAuto`；页面链使用 `pageview.Fail` 并由页面出口交给同一失败协议。禁止 controller 按 key 再维护状态表。
- 包装错误必须保留原错误事实，不通过 `err.Error()` 把数据库原文写进 message。必要明细只给可定位业务事实；日志保存内部诊断。
- 缺少必填身份是参数错误，实体不存在是 404，查询失败是 500；三者不得都改成“空列表”或“尚未配置”。
- 成功操作默认显示提示。需要跳转或刷新时，模板明确声明 `data-write-nav`、`data-write-reload`，或通过已登记前端结果处理器更新界面。
- 不输出 `302`、`Location`、`HX-Redirect`、`HX-Location` 或 JSON 的“请跳转”字段。前端不能解析服务端建议地址后自动导航，也不能自动跟随接口重定向伪装成成功。
- 正常页面链接和用户主动点击的导航保留。商品 `WithRedirect` 是发布 URL 迁址的业务 301，不是本次操作响应导航，不能误删。
- 验证范围要包括 Session、CSRF、Casbin 和页面拒绝出口。商品相关共享改造参考 `builtin/auth.go`、`adminui/reject.go`、`pkg/enums/auth_errors.go`。

### 6.4 预览与字节例外

- 成功预览是构建器产物，不由 HTTP 层拼结构或样式。失败统一 JSON，不再给“失败专用页面”。
- 前端先检查 HTTP 状态和 `Content-Type`；错误 message 显示在发起页面，保留用户输入。不能把 JSON 塞进 iframe 后当作“已展示错误”。
- 未保存实体由前端展示“先保存”空态；后台仍必须拒绝缺少实体身份的请求。
- 校验真实商品/工程后再编译。当前实例已存在时，应按该业务定义使用实例当前文档或指定模板，不能静默选错默认模板。
- 预览不得创建实例、产物持久记录或快照，不得激活线上 URL。按既有架构允许的临时构建机制工作。
- 前端 HTML 预览应隔离脚本和顶层导航，处理重复请求、取消、响应顺序、弹窗关闭和资源相对路径。参考 `static/js/ui/product-preview.js`，不要为展示方便给预览不必要的后台同源能力。

### 6.5 工程范围、完整查询与分页

- 身份、工程、语言从请求和已有上下文进入用例，经所有下游契约一致传递。禁止为了找得到数据而遍历其他工程或取第一个工程。
- 复制来源、绑定实例、候选项、实体详情、分页行和总数都要验证范围；只在列表过滤工程而详情按 ID 裸读仍算失败。
- 可选能力未接入可以返回明确缺口；已接入能力执行失败必须传播，不能退化成空值。
- 完整索引或聚合必须读取完整定义范围，不能用第一页的结果推断全量结论；分页排序用唯一键作为最后排序条件，防止相同时间或序号跨页漏项。支持取消，避免不终止的翻页循环。
- URL、实例记录或草稿存在不等于已发布；SEO 技术事实应依据实际激活记录，不因拟定路径存在就加分。
- 译文等写操作在读取现状失败时必须停止；存储故障应返回内部失败，不能当作输入错误 400，不能假定全部变化后继续写入。
- SEO 商品范例明确覆盖同工程商品/分类/品牌，可选页面按工程过滤；文章因契约缺少工程过滤暂未纳入，返回 `project_filter_unavailable`。不能把这称为“全站无重复”。
- 分页使用真实 `items/total/page/size` 或已有稳定契约字段；例如标签当前字段是 `pageSize`，不要为改名重构再造兼容层。
- 明确总数定义。分类树根 `Total` 与命中分类 `MatchTotal` 不是同一指标，不能互换；列表与数量若回答同一批行应共享筛选条件。

### 6.6 实例依赖与事务

- 构造/装配时给处理器或 service 实例注入依赖。不要使用包级可变指针在多个应用或测试之间互相覆盖。
- 有装配顺序约束可以实例 setter 注入，但只在注册/启动期间使用；请求期不再改依赖，不为了构造方便重新加入全局变量。
- 窄接口只暴露当前用例所需能力。跨模块调用 contract，不直接穿透其他模块 service/model，不复制第三方契约 DTO。
- 商品参考：`ProductHandle.templateDeps/modePort`、`SetDetailTemplateWriteDeps`、`SetDetailTemplateModePort`，以及 `assembly.go/assembly_publish.go` 的实例传递。
- 一次用户写操作涉及多处持久化时，由 service 决定单事务，model 接收事务句柄。读改写使用行锁或原子 SQL，不能因为“正在拆页面”顺便放宽一致性。
- 批量要么全成，要么全败；拒绝时 `data.items` 放可定位的 ID、名称和原因，不静默跳过、不返回逐条成功回执、不自动改名。
- 试算中的统计分类事实不等同于批量写的部分成功。应按语义审查，不能看见 `SkippedCount` 就删除合法试算信息。
- 外部系统或跨库补偿继续满足幂等、留痕、可重放；无法同事务时要写明边界与实际补偿机制。
- 商品译文的既有待重建标记仍是写入后的跨模块动作，失败通过 `StaleMarkFailed` 明示；它不属于本轮已完成的共享事务重构，也不是推荐复制的事务范式。实施其他模块时仍须独立审计此类多写边界。

## 7. 分阶段执行任务

### 阶段 A：锁定范围与旧形状

交付层归属表、接口表、字段归属表、调用方清单、测试计划，尚不运行测试。

1. 读当前规则和目标模块已有文档，记录起始提交、工作树、已有未提交改动；不合入其他未验证草稿。
2. 用 `rg` 搜索目标模块的路由、HTML/模板/DOM/导航、页面 DTO、错误映射、包级可变依赖、吞错和采样上限。
3. 对命中逐处读语义。列出明确正确、必须保留的能力，避免重复重构。
4. 沿每个旧符号和路径找全部真实消费者：模板、JS、MCP、装配、权限、迁移 seed、feature、门禁、共享包。全仓定向搜符号是清理调用方，不能借机扩成全仓业务改造。
5. 对不能安全迁移的共享依赖登记：缺什么能力、谁拥有、最小改动、是否影响目标完成。不要新增空壳适配掩盖缺陷。

### 阶段 B：先完成后端契约与业务边界

1. 声明事实结果、请求形状、错误事实；删除页面专用结果形状，必要调用方在同一工作阶段迁移。
2. service 统一用例、范围、完整查询和事务；handler 缩成绑定、调用、返回。
3. 删除吞错、正常空态掩盖故障、重复算法、状态码白名单与无消费的包装错误。
4. 注入实例依赖，更新全部装配和构造夹具；只读 API 同样声明独立权限。
5. 保留构建预览成功字节出口，移除失败页面；不得同时保留旧 HTML API 和新 JSON API 作为永久兼容。

### 阶段 C：一次迁完前端与调用方

1. 将局部控件改为消费 JSON；结构在 Jet/HTML，JS 更新节点、状态和分页，不插入服务端下发 HTML。
2. 使用既有统一写基座，表单字段与 DTO 一一对齐；验证空值、多值、数组、CSRF 的设计。
3. 显式写导航/刷新策略，删除隐式错误跳页、自动 follow 和后端 DOM 指令。
4. 预览先判 JSON/HTML，失败在当前页面提示；处理并发请求与关闭行为。
5. 删除旧路由、模板、类型、函数、导出符号、死注释和重复转换。最后再用符号搜索确认无遗留调用，不只检查生产编译是否会发现它。
6. 同步直接测试源码、权限 seed、必要文案和门禁预算；禁止扩大豁免让现存错误通过。

### 阶段 D：实现完成屏障

满足以下条件后，集中验收负责人才能运行第一轮检查：

- 范围内所有文件负责人完成并停止写入，没有“先测试后等另一个代理补前端”。
- 接口、字段、权限与模板消费者已一致；旧符号的全调用方检查完成。
- 测试源码已经补齐；真实 PG 使用生产迁移，没用手写表结构代替。
- `gofmt` 与差异回读完成；已检查临时文件、无关改动、过期注释、被删文档指针。
- 集中验收命令清单、环境依赖、预计覆盖范围已经记录。未知风险写明，不能预填通过。

### 阶段 E：集中验收、集中修复与交付

按下一节执行一次统一验收；汇总失败后集中修复，禁止每修一条就重跑全仓。通过后按域整理提交，代码与文档分开，精确暂存。合并、清理工作树遵循当前授权和仓库流程，不擅自推送或删除其他任务工作树。

## 8. 多代理协作约定

仅在用户或适用规则已授权委派时使用子代理；本文不要求为了完成任务必须启用多代理。

| 角色 | 文件所有权建议 | 不得做的事 |
| --- | --- | --- |
| 主负责人 | router、assembly、权限、共享边界、最终集成、验收 | 子任务未完成就开始全量测试；未核实即合并草稿 |
| 后端代理 | 某一业务域 service、DTO、handler 与专用测试源码 | 擅改其他域或共享表；运行测试、build、vet |
| 前端代理 | 对应模板、JS、浏览器/DOM 测试源码 | 独立发明请求字段和响应字段；复制业务算法 |
| 文档/核查代理 | 任务书、接口矩阵、只读调用方核查 | 宣称未执行测试已通过；改规则文件放宽边界 |

- 派发时列出绝对工作树路径、允许文件、禁止文件、协议负责人和“不得运行测试”约束。
- 同一个文件只能有一个写入负责人；共享 enums、DTO 或 router 由一人统一改。需要修改时先交字段/符号清单，不并发覆盖。
- 先交流协议：路径、方法、字段、成功/失败状态和空态语义；不要等两边实现结束再猜接口。
- 代理完成后报告文件、已删符号、行为变化、未解决依赖和未运行的检查。主负责人统一收口后再测试。

## 9. 集中验收计划与真实验收矩阵

### 9.1 命令组织

先填写目标模块及直接依赖，再执行一组明确命令。下面商品命令是范围示例，其他模块替换包路径；不要直接照抄成全系统任务。

```bash
# 生产构建：在实施 git 工作树内，只在实现完成后运行一次。
go build -o /tmp/wp-mvc-acceptance ./cmd

# 商品模块与真实 feature 合并为一次执行；并发敏感业务使用 -race。
GOWP_REQUIRE_PG=1 go test -count=1 -race ./internal/module/product/... ./public/test/product/feature

# 仅在这次确实修改共享拒绝链时纳入相关包，可与上一命令合成一次。
go test -count=1 ./internal/adminui ./internal/middleware/builtin ./pkg/enums

# 本次直接前端消费者合并执行，不逐文件启动独立测试轮次。
node --test internal/templates/static/js/ui/product-read-controls.test.cjs internal/templates/static/js/ui/product_variant_write.test.cjs internal/templates/static/js/ui/write-base.test.cjs

# 收口门禁；先查看 check-all.sh 的实际内容，避免另跑它已经包含的相同检查。
bash scripts/check-all.sh
```

目标模块修改到的模板渲染测试和架构断言必须纳入集中计划；若最终门禁已包含相同命令，不再无理由重复。门禁不包含的必要相关测试显式补入。需要 DB 门禁时先满足环境再使用 `--with-db`，报告采用了哪组。

首次运行中出现编译阻塞，可先集中修复编译根因再执行尚未运行的检查；已经通过且未受影响的检查不用重跑。失败汇总必须区分：本次实现缺陷、旧测试仍断言旧协议、环境不可用、既有无关失败。旧协议测试要改成新的行为断言，不能直接删掉有价值覆盖。

### 9.2 验收矩阵

| 领域 | 必须制造的条件 | 必须断言的结果 |
| --- | --- | --- |
| 绑定 | JSON 与 FormData 相同业务输入；空白名、缺 ID、多值、空数组、保留空项 | 两种传输产生同一业务结果；参数错误 400；没有第二份字段解析 |
| 错误 | 资源不存在、版本冲突、真实依赖异常 | 分别真实 404/409/500；JSON 的 code 与 HTTP 相同；message 有可理解兜底且不含 SQLSTATE/表名/内部原文 |
| 鉴权 | 未登录、有会话无权限、权限能力故障；HTML Accept 与不同 HX 请求头 | 401/403/500 仍 JSON；无导航头、不缓存；handler 不被继续执行 |
| 事实边界 | SEO/候选/命中数据成功 | 数据包含算法/实体事实，不包含颜色、HTML、DOM 目标、隐藏字段、成品展示句 |
| 完整性 | 数据超过单批页大小；最后一页；空列表 | 后续页确实参与计算；总数与同范围实际行一致；不以第一页冒充全量 |
| 覆盖缺口 | 可选契约未接；契约不支持工程过滤；已接契约读失败 | 前两者明确 coverage/reason；后一种失败传播，不能报告“无重复” |
| 工程隔离 | A/B 工程的实体、模板和绑定，客户端交叉提交 | 不读取或修改另一工程；拒绝后两边持久数据不变 |
| 批量原子性 | 中间一项不合法；后续依赖写失败；并发修改 | 所有业务写和审计同成同败；冲突带可定位 items；并发用锁后当前事实判定 |
| 页面 | 成功读取与读取故障 | 成功真实完整 HTML，数据/按钮/分页正确；失败 JSON，不给 200 提示页 |
| 前端局部控件 | 查询、下一页、无结果、错误、慢请求先后反转；同表单多动作按 Enter | 同一份结果更新列表和总数；旧响应不覆盖新状态；Enter 默认保存；无直接插入后端 HTML |
| 显式导航 | 无导航声明、声明 nav、声明 reload、接口意外重定向 | 默认留在原页提示；仅声明动作导航；意外重定向作为失败处理 |
| 预览 | 未保存、缺实体、无模板、构建失败、空产物、绑定实例成功 | 错误父页可见；成功才显示 HTML；绑定模板正确；实例/产物/快照数量不变 |
| 装配 | 同进程两个独立处理器使用不同端口 | 互不覆盖依赖；未装配显式失败；没有请求期全局状态写入 |
| 删除收口 | 旧 API/模板/DTO/函数已移除 | 所有实际调用方同步迁移；无死导出、旧注释、临时兼容双轨；门禁预算真实下降 |

测试参考：

- `service/product_seo_facts_test.go`：超过首批、工程过滤、已配置来源失败、共享算法一致。
- `public/test/product/feature/product_translation_failure_test.go`：存储故障真实 500，读取失败零写入，无内部原文和隐式导航。
- `inbound/http/product_seo_score_handler_test.go`：JSON 事实与错误状态，不泄漏内部原文。
- `inbound/http/product_pagination_facts_test.go`：页码和总数事实、查询失败协议。
- `service/product_preview_boundary_test.go` 与 `public/test/product/feature/product_preview_json_test.go`：空产物、失败传播、绑定实例、工程隔离及不写入。
- `public/test/product/feature/product_pricing_atomicity_test.go`：失败回滚及锁后事实；保留既有业务正确性。
- `static/js/ui/product-read-controls.test.cjs`、`product_variant_write.test.cjs`、`write-base.test.cjs`：前端事实消费、表单和显式导航。
- `internal/adminui/reject_test.go`、`internal/middleware/builtin/auth_test.go`：完整拒绝协议；`prepare_render_test.go` 保留真实壳渲染覆盖。

以上测试文件是参考锚点，不代表本文确认它们已通过；实际执行范围、结果和日志必须单独填写。

## 10. 禁止的伪完成方式

- 只改命名、搬文件或多包一层 service，却保留原页面形状和 HTML 回包。
- 删除旧 endpoint 后不查 JS、抽屉、MCP、权限及 feature 消费者。
- 将所有错误都变成“暂无数据”，或把成功空态一律改成 500。
- 把工程安全问题改成前端过滤，或为了绕开缺失契约直接跨模块读表。
- 将 `limit=100` 变大到另一个数就宣称完整查询。
- 为避免报错重新加入旧 HTML 路由、全局依赖或兼容包装，形成两套实现。
- 删除仍被其他模块使用的共享 `scoreview`、提示模板或字节出口，宣称“所有后端 HTML 已清空”。
- 把页面显示逻辑和业务逻辑一起移到 JS，导致金额、库存、优惠或状态机有第二份实现。
- 将已跳过的数据库测试写成通过，或仅凭 `rg -c` 下降、构建成功、门禁全绿判断完成。
- 修改规则文件、增加预算/豁免、屏蔽有效断言来制造绿灯。

## 11. 交付完成标准与报告模板

必须同时满足：职责边界成立、原业务正确性未退化、全调用方已迁移、相关集中验收通过或明确报告未通过原因、旧形状实际减少。不能以“已经大部分改好”代替任务完成。

交付报告填写：

```text
目标模块/业务域：
工作树与起始/最终提交：

实际改变的行为：
- 旧入口 → 新入口、请求字段、成功/失败协议、权限点。
- 从后端移除的界面职责；保留的业务事实与构建字节出口。
- 错误传播、工程隔离、事务/并发、查询完整性改变。

删除与保留：
- 删除的路由、模板、DTO/函数、全局依赖和重复实现。
- 已保留的重要业务、迁址语义及共享调用方。
- 门禁/存量台账实际减项与证据。

集中验收：
- 首轮开始时是否所有实现和测试源码都已完成。
- 每条命令、范围、退出状态、实际失败/通过/跳过、日志位置。
- 集中修复的问题、复验范围和扩大检查的具体原因。
- 数据库/Redis环境及未覆盖范围；不能只填“全绿”。

剩余边界：
- 未覆盖的可选来源、共享包其他调用方、环境阻塞或既有无关失败。
- 各项的具体影响、所属模块和下一步；无剩余项也应基于证据说明。

提交与交付：
- 代码与文档分开提交；逐文件暂存回读。
- 是否合并/推送/清理工作树，以实际已执行结果填写。
```

完成后的模块应当更容易独立解释：后端负责规则与事实，前端负责如何显示和何时导航，失败不伪装成页面成功。这个标准需要代码与行为证据共同成立，不能只靠文档表述。
