package producthttp

import (
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	inventoryenums "go_wp/internal/module/inventory/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_jump.go — product 后台页的**共享输出原语**：筛选上下文、批量回执文案，
// 以及**错误事实与文案出口**（原先的 product_err.go 并进来的那一半）。
//
// 提示页（shell.RenderJump）与回跳地址（shell.BackPath / backpath.WithParams）已经
// **不在这一层**：写动作全部走 ui/json_write.js 直调 /api/product/*（结论进 flash 行），
// 页面 GET 的失败也改成页内失败槽（LoadError）或 302 回列表 —— 控制器不再拼「回哪去」
// 与「怎么回去」，那是壳层的事。剩下的是两件仍被生产代码用的东西：
//
//  1. 各页的筛选上下文键表与读回（productQueryFromRequest / withListQuery）；
//  2. 批量结论的文案事实（productBulkText 族）与超限出口（productBulkIDs）。
//
// 文件名保留 jump：错误文案那一半还没迁到 pkg/facing，迁完后本文件会只剩上面两条，
// 届时并进 product_subject.go 或 util 即可。

// 两个页面的路径常量（GET 失败时的 302 落点与页面自身地址）。
// 其余页面的路径常量随各自的 Back 助手一起删除 —— 删掉引用它们的写通道之后，
// 留一个没人读的路径字面量只会让下一个人以为那条通道还活着。
const (
	productListPath         = "/admin/products"
	productTranslationsPath = "/admin/products/translations"
)

// 页面上下文的键表（每页一份）。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// productQueryFromRequest 读回（页面 POST 的预览/评分端点靠它保住上下文）。
// 两处分叉的表现是「改完筛选再操作，回来静默丢了筛选」—— 页面不报错、日志也干净，
// 所以键表必须是同一份（不要在两处各写一遍字面量）。
//
// 写动作迁到 JSON 之后，Tag / Categories / Brands / Bundle / Pricing 几组的键表只剩
// 「渲染时拼进表单 action / 片段重渲时再拼一次」这一条用途，读回那一半随各自的
// 页面写通道一起没了 —— 但**不能删**：删了表单 action 就不再带当前筛选，
// 页面 POST 的预览端点回来会落到没有上下文的一页。
var (
	productListBackKeys         = []string{"project", "keyword", "status", "page", "limit"}
	productTagsBackKeys         = []string{"project", "keyword", "page", "limit"}
	productCategoriesBackKeys   = []string{"project", "keyword", "page", "limit"}
	productBrandsBackKeys       = []string{"project", "keyword", "page", "limit"}
	productBundleBackKeys       = []string{"project", "product"}
	productPricingBackKeys      = []string{"project"}
	productTranslationsBackKeys = []string{"project", "product", "lang"}
)

// productQueryFromRequest 本次请求 query 里的页面上下文（表单 action 上带回来的那一段）。
//
// 同一份实现同时服务「渲染时拼进表单 action」与「失败重渲片段时再拼一次」：
// 两者产出的形状必须一致，否则会出现「首屏表单 action 带 A、失败重渲后带 B」。
// 空值丢弃、编码走 url.Values（键有序、产物稳定）。
func productQueryFromRequest(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	out := url.Values{}
	for _, k := range keys {
		if v := strings.TrimSpace(in.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

// withListQuery 把页面上下文拼到写动作地址后面（空上下文则原样返回）。
func withListQuery(action, listQuery string) string {
	if strings.TrimSpace(listQuery) == "" {
		return action
	}
	if strings.Contains(action, "?") {
		return action + "&" + listQuery
	}
	return action + "?" + listQuery
}

// —— 一句结论的事实 ——

// productMsg 一句结论的**事实** —— 就是共享形状 adminui.Msg（key + 中文兜底 + `%s` 参数）。
//
// 这里用别名而不是新造 struct：各模块各造一份同形类型就是多份契约，字段名与参数编码
// 迟早分叉，而症状是「某个页面的提示偶尔少一个数字」，既不报错也不记日志。
type productMsg = adminui.Msg

// productMsgText 把一句结论取成当前语言文本（片段槽位 / JSON 出口没有模板取词时用）。
func productMsgText(c *gin.Context, m productMsg) string {
	return adminui.MsgText(c, m)
}

// productBulkIDs 批量 id 的归一 + 超限出口（商品批量删除 / 批量改价 / 四套归类删除共用）。
//
// 超限**必须**把「上限」与「本次条数」两个数字给出来：只说「请求参数无效」时操作者不知道
// 该拆成几批，而这正是这条拒绝唯一可行动的信息。词条与模板都归 pkg/bulkids 所有（它的两个
// %s 依次是上限与本次条数），这里只做「按请求语言取成品串 + 注入数字」。
//
// 返回 false 表示已写出响应、调用方直接 return。空列表**不在这里拦**：没勾选是各端点自己的
// 语义（批量改价要一句专门的话，批量删除只要参数无效）。
func productBulkIDs(c *gin.Context, raw []string) ([]string, bool) {
	ids, err := bulkids.Normalize(raw)
	if err == nil {
		return ids, true
	}
	var limit *bulkids.IDsError
	if errors.As(err, &limit) {
		template := i18n.Translate(bulkids.MsgTooMany, bulkids.TooManyTemplate, response.RequestLanguage(c))
		response.ErrorWithMessage(c, http.StatusBadRequest, fmt.Sprintf(template, strconv.Itoa(limit.Max), strconv.Itoa(limit.Count)))
		return nil, false
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
	return nil, false
}

// —— 成功回执文案（key + 中文兜底；词条缺失时页面上仍是中文）——

// productActionDoneFallback 单条写动作的通用成功回执的中文兜底（key 在
// productenums.ProductsActionDone）。
//
// 不复用 enums 的 MsgCreateSuccess / MsgUpdateSuccess：那几个 key 的值在词条表里是
// 「Blueprint 创建成功」这类**蓝图域**的句子（058/430 早先 seed），商品页用它会显示成
// 「Blueprint 创建成功」。这里另起一条商品域 key（登记前走中文兜底）。
const productActionDoneFallback = "操作已完成"

// productActionDoneMsg 单条写动作（新建 / 修改 / 保存）的成功回执事实。
var productActionDoneMsg = productMsg{Key: productenums.ProductsActionDone, Fallback: productActionDoneFallback}

// ==========================================================================
// 商品后台页**回执事实**的收口。
//
// 传输通道已改为响应体：写动作的结论走 JSON 回执（ui/json_write.js 提交 + flash 结论行），
// **不再**经 302/303 + `?err=` / `?done=` / `?applied=` / `?saved=` 回带列表页；
// 也不再渲染整页提示页 —— 那个出口随写通道一起删了（页面 GET 的失败改走页内失败槽，
// 缺参走 302 回列表，见 product_detail_template.go）。
//
// **读侧（?err= / ?done= 的受控文案集合与判定）已整批删除**：那条通道的代价是本模块要维护
// 一份「受控文案 + 数字归一模板」来证明提示出自本仓（productPageErr / productPageDone /
// productNoticeTexts / productFacingNotice / productVariantNoticeMatches / productSkipListMatches /
// productReasonTexts / productOwnPageTexts / productAppliedToken / productBulkResultTemplates
// 就是那套），而查询参数不是可信边界（任何人手拼一个 /admin/products?err=任意文案 就能往
// 页面上塞一条伪造消息）。文案走响应体之后，那套判定随之不需要了。
//
// 本文件剩下的都是**回执事实**：把 service 的错误 / 计数结论变成「一句结论的事实」
// （key + 中文兜底 + `%s` 参数）或可展示的成品文案（productMsgText）。
//
// **错误事实只有一条判据**：pkg/facing.FactOf（三档：自带文案 → 单段 key → `key: 明细`），
// 与 JSON 出口（response.ErrorAuto）、页面出口（adminui）同一份。原先本模块自带的第二份真相 ——
// `productErrSentinels` 白名单 + `productErrKey` 逐条比对常量 —— 已整个删除：
// service 换了措辞、或某个 key 改由别的模块提供（如 inventory / presentation / contenttemplate
// 的 enums），白名单就静默失配，症状是把业务文案吞成归口、或把内部错误当业务文案透出，
// 两种都不报错。
//
// **兜底绑在声明处**：本模块 service 一律 `productenums.Err(key)` 返回自带中文兜底的错误，
// key 与那句文案同处声明（internal/module/product/enums）；出口经 facing 拿到 Fallback，
// 不再查本模块的表。只剩「对端模块 enums 还没有兜底表」的那批 key 走
// productErrForeignFallbacks（只减不增，对方补齐后连表一起删）。
// ==========================================================================
type productBulkText struct{ key, fallback string }

// fact 把一条结论文案变成**事实**（key + 中文兜底 + `%s` 参数）：模板在渲染期取词，
// JSON 出口用 productMsgText 取成品串。
//
// 参数一律 strconv 成字符串（模板侧的占位符是 `%s`，传 int 会渲染成 `%!s(int=3)`）；
// 没有占位符的词条（如 productSaved）**不要**传参数 —— TranslateArgs 对多余参数会 Sprintf 出
// `%!(EXTRA string=…)`。
func (t productBulkText) fact(args ...string) productMsg {
	m := productMsg{Key: t.key, Fallback: t.fallback}
	if len(args) > 0 {
		m = m.WithArgs(args...)
	}
	return m
}

// productBulkTextOf 取一条批量结论文案的当前语言模板（**文本**形态，给「多段拼一句」的合成句用）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经 strconv.Itoa）——
// 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看（与 shell.BulkIDsFacingText 同理）。
func productBulkTextOf(c *gin.Context, t productBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 变体清单保存的结论文案（详情页编辑表单的回执）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// 商品编辑页保存成功的回执已随页面 POST 一起消失：写动作交给 ui/json_write.js 直调
// /api/product/update，成功后基座 reload 编辑页（字段值就是刚落库的那一份）。

// —— 整页 GET 的失败出口：文案归口 ——
//
// 契约：docs/rules/template-boundary.md §整页 GET 的失败出口 —— 取数失败的整页 GET
// **也必须是「这一页」**（同一份模板 + 页壳 + 侧边菜单 + `Cache-Control: no-store`），
// 状态码按档位说实话（主数据没读到 = 500；请求参数不合法 = 400；资源不存在 = 404）。
//
// 此前本模块三个整页 GET（/admin/products、/admin/products/new、/admin/products/detail）
// 的取数失败一律走 shell.PageError(c, "product"|"products_new", err)：浏览器收到的是
// `{"code":500,…}` —— DOM 里没有标题栏与左侧菜单，地址栏停在原路径，用户只能手改地址栏回去。
//
// **本文件只做取词，不做出口**：渲染留在各 handler 末尾那唯一一次 c.HTML 里
//（失败页与成功页共用同一份模板与同一份页数据，差别只有状态码 / no-store / 失败槽）。
// 这里若再放一个「渲染失败页」的函数，模块就又多了一条出口形态 —— 那正是要收敛的东西。

// productErrUnregisteredFallback 「有业务形态、但本仓没登记展示原文」时的兜底（task-158 §7.3 ⑥）。
//
// 两个「不能」：页面不能显示空串（错误槽空着，用户以为没出错），也不能显示裸 key
// （ErrSomethingNew 这种串运营只会以为后台坏了）。也不能借共享归口文案说「系统内部错误」
// —— 那是**非业务**错误的口径，把一次业务拒绝说成系统故障同样误导。
//
// 它本该永不出现：productenums.ProductErrFallbacks 的覆盖面由 AST 守卫钉住，
// 未登记的 key 只可能来自「新加常量又没跑测试」。出现时记一条 warn（带 err_key），
// 所以「多了一句稳定文案、少了一句该有的话」在日志里看得见。
const productErrUnregisteredFallback = "操作失败，请稍后重试（细节只进日志）"

// productErrPageText 页面出口的成品文案（片段槽位 / 整页失败槽用）。
//
// 判定与日志都在 productErrMsg（见 product_page.go 的 productErrFact）：命中业务错误取
// 当前语言（缺词条走 productErrFallbackText 查到的中文兜底）；未命中记一条日志并给归口文案。
func productErrPageText(c *gin.Context, err error) string {
	return productMsgText(c, productErrMsg(c, err))
}

// productPageScene 页面装载失败的日志场景名（与 shell.PageError 的 page 场景同源，便于检索）。
const productPageScene = "page"

// productErrKeyOf err → 业务错误 key（非业务错误 / nil 一律空串）。
//
// 页面出口用它分流「不存在」(404) 与「不合法」(400)：两者都是**业务事实**，
// 由 service 的 enums 常量判定，不靠匹配驱动报错原文（那会随 PG 版本改字面量）。
func productErrKeyOf(err error) string {
	key, _, _ := productErrFactOf(err)
	return key
}

// productErrForeignFallbacks 别模块业务 key 的展示兜底（key 的归属不在本模块，表暂由本层带着）。
//
// 为什么留在这层而不是搬进各自的 enums：那两个模块**还没有**通用兜底表
// （internal/module/inventory/enums/inventory_enums.go 只有 ErrDetailFallbacks，
// presentation 同理）。写进 product 的 enums 更糟 —— key 的归属一旦跨模块，
// 对方改名就没有编译错误兜着。所以本表只装「展示原文」，且**只减不增**：
// 对方模块补上 ErrFallbacks 后就把对应条目删掉。
var productErrForeignFallbacks = map[string]string{
	inventoryenums.ErrSKUSourceInvalid:             "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	inventoryenums.ErrWarehouseSKURequired:         "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	inventoryenums.ErrWarehouseSKUNotFound:         "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	inventoryenums.ErrWarehouseSKUBundleNotAllowed: "捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码",
	inventoryenums.ErrWarehouseSKUCodeTaken:        "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	inventoryenums.ErrExternalSKUInvalid:           "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrStockQuantityInvalid:         "数量必须是非负整数",
	inventoryenums.ErrExternalSKUProductConflict:   "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	inventoryenums.ErrWarehouseNotFound:            "仓库不存在",
	inventoryenums.ErrWarehouseDisabled:            "已停用的仓库不能作为归属仓",
	inventoryenums.ErrWarehouseProjectMismatch:     "仓库不属于该工程",
	inventoryenums.ErrWarehouseDefaultMissing:      "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	presentationenums.ErrDetachConfirmRequired:     "该操作会放弃模板同步（商品页转为独立文档），需要先确认",
	presentationenums.ErrRollbackTargetMiss:        "回滚目标不存在：该版本不属于这个商品，或产物文件已缺失",
	presentationenums.ErrRollbackFailed:            "回滚失败：线上版本保持不变",
	presentationenums.ErrSnapshotMismatch:          "该版本不属于这个商品，不能用来回滚",
}

// productErrForeignFallbackText 别模块 key → 展示兜底（本模块 key 的兜底由 pkg/facing 随错误带出）。
//
// 本模块 service 一律用 productenums.Err(key) 返回**自带中文兜底**的错误（声明处见
// productenums.ProductErrFallbacks），出口经 facing.FactOf 直接拿到 Fallback，不再查表。
// 这条函数只剩「对端 enums 还没有兜底表」的那一批 key（inventory / presentation）——
// 对方补上 ErrFallbacks 后本表清空、本函数与 productErrForeignFallbacks 一起删。
func productErrForeignFallbackText(key string) string {
	if key == "" {
		return ""
	}
	return productErrForeignFallbacks[key]
}

// productErrFactOf 业务错误的判定与拆分（**纯事实**，不记日志）。
//
// 判据只有一条：pkg/facing.FactOf（adminui.BusinessErrFact 是它的转发）——三档入参形态
// 都在那里判：自带文案（facing.New）→ 单段 key → `key: 明细`（%w 包装）。
//
// service 侧只用前两档与 `fmt.Errorf("%w: %s", productenums.Err(key), 明细)`：
// 全角冒号的复合形态已在本批全部改掉（facing 只按 ASCII 冒号切口），所以这里不再补切。
//
// 明细段（tail）原样带出，由 productErrDetailText 解码成当前语言。
func productErrFactOf(err error) (key, tail string, business bool) {
	if err == nil {
		return "", "", false
	}
	if f, ok := adminui.BusinessErrFact(err); ok {
		return f.Key, f.Tail, true
	}
	return "", "", false
}

// productErrFact 判定 + 归口（**记日志**）：给「调用点没有更具体的日志」的地方用。
func productErrFact(c *gin.Context, err error) adminui.ErrFact {
	return productErrFactWith(c, err, true)
}

// productErrFactQuiet 同 productErrFact，但**不记日志** —— 给「调用点已经记过一条更具体的日志
// （带 productId / project 等）」的地方用，免得同一个错误在日志里出现两遍。
func productErrFactQuiet(c *gin.Context, err error) adminui.ErrFact {
	return productErrFactWith(c, err, false)
}

func productErrFactWith(c *gin.Context, err error, logIt bool) adminui.ErrFact {
	if err == nil {
		return adminui.ErrFact{}
	}
	if fact, business := adminui.BusinessErrFact(err); business {
		key, tail := fact.Key, fact.Tail
		fb := productErrFallbackOf(key, fact.Fallback)
		if fb == productErrUnregisteredFallback {
			// 「有业务形态、但没有展示原文」的 key：对外给稳定的一句兜底（绝不显示裸 key），
			// 对内记一条 warn 带上 key —— 否则「页面上少了一句该有的话」没有任何信号。
			// 本模块 key 已由 productenums.Err 随错误带出兜底，走到这里的只有别模块的新 key。
			if logIt {
				logger.Scene(productErrScene).
					With("path", c.Request.URL.Path).
					With("user_id", builtin.CurrentUserID(c)).
					With("err_key", key).
					Warn("业务错误 key 未登记中文兜底（补进 productenums.ProductErrFallbacks）")
			}
			fb = productErrUnregisteredFallback
		}
		return adminui.ErrFact{Key: key, Fallback: fb, Tail: tail, Business: true}
	}
	if logIt {
		logger.Scene(productErrScene).
			With("path", c.Request.URL.Path).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return adminui.InternalErrorFact()
}

// productErrMsg 业务错误 → 一句结论事实（判定见 productErrFact）。
func productErrMsg(c *gin.Context, err error) productMsg {
	return productErrMsgOf(c, productErrFact(c, err))
}

// productErrMsgQuiet 同 productErrMsg，但不记日志。
func productErrMsgQuiet(c *gin.Context, err error) productMsg {
	return productErrMsgOf(c, productErrFactQuiet(c, err))
}

// productErrMsgOf 事实 → 一句结论（key + 中文兜底；带明细时给成品串）。
func productErrMsgOf(c *gin.Context, f adminui.ErrFact) productMsg {
	if f.Key == "" {
		return productMsg{}
	}
	fb := productErrFallbackOf(f.Key, f.Fallback)
	if f.Tail != "" {
		if detail := productErrDetailText(shell.TranslateFor(c), f.Tail); detail != "" {
			// 明细是「词条 + 具名参数」的多段结构，一段 key 表达不了它 ——
			// 这一档给成品串（与迁入前逐字一致；明细本身仍随语言变化）。
			return productMsg{Fallback: adminui.TranslateArgs(c, f.Key, fb, nil) + "：" + detail}
		}
	}
	return productMsg{Key: f.Key, Fallback: fb}
}

// productErrFallbackOf key → 中文兜底（缺词条时的展示原文，不是可透出性判据）。
//
// 顺序：**事实自带的兜底优先**（本模块 service 经 productenums.Err 声明，facing 随错误带出），
// 再查别模块那张表（对端 enums 还没有兜底表），都取不到时给**归口文案** ——
// 不回落到裸 key：页面上出现 ErrBundlePriceRequired 这种串，运营只会以为后台坏了。
//
// def == key 说明「事实没有自带文案」（facing 对单段 key 会把 Fallback 填成 key 本身）。
func productErrFallbackOf(key, def string) string {
	if fb := strings.TrimSpace(def); fb != "" && fb != key {
		return fb
	}
	if fb := productErrForeignFallbackText(key); fb != "" {
		return fb
	}
	if strings.TrimSpace(def) != "" && def == key {
		return productErrUnregisteredFallback
	}
	return adminui.InternalErrorFact().Fallback
}

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// productInternalText 未命中业务判据时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外只给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 既看不懂，
// 也把库表结构泄了出去 —— 响应（含提示页文案与模板数据）不是可信边界。
func productInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(productErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return adminui.PageInternalText(c)
}

// productErrText 业务错误 → 当前语言文案（**片段槽位 / JSON 出口**用：那边模板不取词）。
//
// 写侧与整页失败出口不再用它 —— 那些地方传**事实**（productErrMsg），取词在渲染期发生。
// 与 block 模块的 blockErrText 同一形状：页面上的错误必须是能读的一句话，
// 而不是 ErrBundlePriceRequired 这样的裸 key，也不是 GORM / PG 的原始报错。
func productErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	return productMsgText(c, productErrMsg(c, err))
}

// productErrDetailText 业务错误的**补充说明** → 当前语言文案。
//
// 补充说明有两种形态：
//
//  1. i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，可多段）——
//     service 用它把「为什么被拒」的上下文（哪个属性组 / 超了多少 / 被哪个商品占用）
//     也词条化，于是整句按当前语言取词并填 {name} 占位符；
//  2. 其余（受控提示、历史形态的纯文本）—— 原样透出，行为与本通道引入前一致。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
func productErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := productenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(productErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}
