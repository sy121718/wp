package orderhttp

// order_shared.go — 订单模块后台页的共用件：请求取值、错误文案白名单、分页窗口解析。
//
// 这里只放「两个以上页面都要用、且与具体页面无关」的东西。判据是**改一处会不会牵动多个页面**：
// 错误文案白名单、分页窗口解析都属于这一类；而各页的视图装配已经搬进模板，
// 各页的写动作出口已经整批搬到 JSON 接口（前端直调，见 order_write.go / return_bulk_write.go）。
//
// 历史上这里还有一套「批量结论文案（key + 中文原文 → Sprintf 整句 → 提示页）」的机关。
// 它是**提示页形态**的产物：结论由 Go 拼成整句写进响应体。页面改成前端直调之后，
// 结论文案改由模板词条 + 计数承载（见 public/migrations/{603,604,605}_*.sql 的
// data-msg-* 词条），Go 侧不再拼句 —— 于是 bulkSummary / orderBulkTextOf 与整族
// orderBulk* 常量一并退役（0 消费者）。

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderenums "go_wp/internal/module/order/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
)

// countsFilled 把状态计数表**补齐零值**：白名单里的每个状态都保证有键。
//
// 为什么必须补：模板会拿计数做数值比较（`{{if pendingCount > 0}}`），而 Jet 对
// 「map 里不存在的键」索引出的是 nil —— 直接参与比较会抛
// `a non numeric value in numeric comparative expression`，整个响应 500。
// 于是「某个状态一条都没有」这种**最常见**的情形反而打不开页面（实测：有工程、无退货单即 500）。
//
// 补零而不是改模板：缺键在展示层的语义就是 0（这个状态没有单），补齐比让每个使用点
// 各自记得判空更可靠 —— 后者漏一处就是一次 500。
func countsFilled(counts map[string]int64, keys []string) map[string]int64 {
	out := make(map[string]int64, len(keys))
	for _, k := range keys {
		out[k] = counts[k]
	}
	return out
}

// orderStatusTabs 订单状态筛选行的「取值 + 分档」。
//
// 取值与分档一起给模板：原先模板自己拿 sv 逐个判状态选 badge 类，
// 同一套判据在筛选行、列表行、详情头各写一遍（见 orderenums.OrderStatusTone 的说明）。
// 顺序仍是 orderStatusValues 的顺序 —— 徽章行从左到右就是这个顺序。
func orderStatusTabs() []gin.H {
	tabs := make([]gin.H, 0, len(orderStatusValues))
	for _, v := range orderStatusValues {
		tabs = append(tabs, gin.H{"Value": v, "Tone": orderenums.OrderStatusTone(v)})
	}
	return tabs
}

// returnStatusTabs 退货状态筛选行（同上，取值白名单是 returnStatusValues）。
func returnStatusTabs() []gin.H {
	tabs := make([]gin.H, 0, len(returnStatusValues))
	for _, v := range returnStatusValues {
		tabs = append(tabs, gin.H{"Value": v, "Tone": orderenums.ReturnStatusTone(v)})
	}
	return tabs
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// —— 订单管理页的展示文案映射 ——
//
// 状态、操作人、来源、支付、金额、地址、时间的标签：一律「key + 中文兜底」，见上面的 orderLabel。

// translate 展示层的取词函数：`func(key, fallback string) string`。
//
// 类型别名（不是定义新类型）：展示标签一律经它取词，**中文原文留在代码里当兜底** ——
// 词条缺失时页面显示中文，而不是裸 key（`admin.orders.operator_type.admin`）
// 或者英文界面恒中文。
//
// 构造入口只有一个：orderTr。

type translate = func(key, fallback string) string

// orderTr 当前请求语言的取词函数（与 adminui.Prepare 注入模板的 t 同源）。
//
// 本模块 Go 侧的取词一律经它，**不再直接调 shell.TranslateFor** —— shell 那层是
// 「跳转页 + 取词」的历史合体，取词这一半已经归口到 adminui
// （语言是请求级的，请求级的东西只该有一个入口）。

func orderTr(c *gin.Context) translate {
	return func(key, fallback string) string {
		return adminui.TranslateArgs(c, key, fallback, nil)
	}
}

// orderLabel 一条展示标签（i18n key + 中文兜底）。
//
// 形态是「key + 中文兜底」：**key 与中文原文只有这一份**，取词只有一个入口（orderLabelOf）。
// 直接在函数体里写中文的后果是「模板直接渲染的文本永远中文」——
// orders.html 的 {{o.StatusLabel}} / {{detail.Head.StatusLabel}} 都是**直接渲染**，
// 不经过 pkg/response 的 translate，所以在 Go 侧硬写中文等于英文界面恒中文。

type orderLabel struct{ key, fallback string }

// orderLabelOf 取一条标签的当前语言文本；tr 为 nil（纯函数测试路径）时回落中文兜底。

func orderLabelOf(tr translate, l orderLabel) string {
	if tr == nil {
		return l.fallback
	}
	return tr(l.key, l.fallback)
}

// orderPlaceholderRE 已删除：命名占位符（`{name}`）的检测与填充复用 pkg/i18n
//（FillNamedPlaceholders / FillTranslate），本模块不再自造一份替换器。

// 订单页的展示标签词条：状态 / 操作人类型 / 下单入口的展示标签都在模板里按值取词
// （`tr("site.fragment.order.status." + status, ...)`），Go 侧不保留各自的 label 常量。
// 原来那唯一的参数级提示（订单编号不合法）随列表页写动作迁 JSON 一并退役 —— 单档的
// orderId 现在由 JSON 绑定判形状，失败给的是 pkg/response 的「参数不合法」。
// orderStatusLabel 状态 → 展示标签（未知值原样返回：宁可显示生值，也不显示空白）。
//
// 映射的真源在 orderenums.OrderStatusLabel（后台客户页经 ordercontract 引用**同一份**）：
// 三处各写一张中文表的结果是「改一处、另两处静默留在旧说法上」。
// key 为空表示这一档没有词条可查（空状态 / 认不出的取值），直接用兜底值 / 原值。

func orderStatusLabel(tr translate, status string) string {
	key, fallback := orderenums.OrderStatusLabel(status)
	if key == "" {
		if strings.TrimSpace(status) == "" {
			return orderFieldEmpty
		}
		return fallback
	}
	return orderLabelOf(tr, orderLabel{key, fallback})
}

// countryLabelFn 生成「国家/地区代码 → 当前界面语言显示名」的解析闭包。
//
// 返回 nil 表示**没有解析能力**（字典未注入 / 装配退化）：调用方经 applyCountryLabel
// 原样显示 code —— 「未接入」与「接入但查不到」刻意走同一条回落路径，这样页面在
// 两种情形下的表现一致（都是显示代码），不会出现「装配漏了」只在某个页面变成空白。
//
// 这里刻意不返回 error：展示标签的读取失败不该让订单详情整页 500 或变成一行报错。
// 字典读不到时记日志在 sysconfig 侧（那是它知道失败原因的地方），页面照常渲染。
//
// lang 取当前请求语言（response.RequestLanguage，与 sysconfig 后台页同源）。

func countryLabelFn(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	if c == nil || dict == nil {
		return nil
	}
	ctx := c.Request.Context()
	lang := response.RequestLanguage(c)
	return func(code string) string {
		code = strings.TrimSpace(code)
		if code == "" {
			return ""
		}
		if label := strings.TrimSpace(dict.CountryLabel(ctx, lang, code)); label != "" {
			return label
		}
		return code
	}
}

// —— 订单管理页的查询参数解析与对外文案出口 ——

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。

func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = reqparams.PageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

// orderQueryID 解析 orderId 查询参数（非法即 0 = 不渲染详情块）。

func orderQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// orderFacingError 把订单模块的错误转成可展示文案（本模块页面路径的**错误文案三件套**出口）。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
//
// 三件套在这里的落法：
//
//	① 白名单 —— orderFacingText（enums.UserFacingMessages，与 API 出口共用同一份）；
//	② 归口文案 —— adminui.PageInternalText(c)：**当前语言的译文**，不是裸 key
//	   （列表页装载失败时它进的是模板 {{.Err}}，那是直接渲染的文本，不经过 response 的 translate；
//	   原先那条路径是 `c.String(500, orderenums.ErrInternal)` —— 硬编码中文，英文界面照旧显示中文）；
//	   命中白名单的那一支同样取译文（orderPageFacingText），否则页面显示的是裸 key；
//	③ 结构化日志 —— **只有落到归口文案那一支才记**：命中白名单的是业务错误（预期内的用户输入问题），
//	   记 ERROR 只会淹没真正的故障；未命中的原文（表名 / SQL 片段 / 驱动前缀）只进日志、绝不进响应。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	// 命中白名单后按当前语言取词（白名单里存的是中文原文 / item_key 两种形态，
	// `tr(msg, msg)` 对两者都安全：是 key 就翻译，不是就原样返回）。
	if msg := orderFacingText(err.Error()); msg != "" {
		return i18n.TranslateFunc(response.RequestLanguage(c))(msg, msg)
	}
	logger.Scene("order-page").With("path", c.Request.URL.Path).Error(err, "订单后台页处理失败")
	return adminui.PageInternalText(c)
}

// orderFacingText 白名单校验：命中返回原文，未命中返回空串。

func orderFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range orderenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// firstNonEmpty 取第一个非空字符串（回显拼接用：业务错误优先、内部提示兜底）。
//
// 页面不能把两处错误都摊开 —— ?err= 只有一个位置，先到的那条才是用户当下要看的。

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// —— 订单管理页的视图取数（列表/详情、状态计数与筛选选项）——

// —— 表单与文案工具 ——

// salesCardView 之外，本页只有一条趋势图 —— 详情见 salesTrendView。

// ── 月度趋势（折线图）────────────────────────────────────────────────────
//
// 坐标一律在这里算完，模板只贴字符串 —— 与 AI 会话页的 token 趋势同一套规矩，
// 两处共用 .chart-trend-* 的样式（viewBox 1000×200 + preserveAspectRatio="none"）。

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// —— 退货入库页的表单与查询取值、对外文案出口 ——

// —— 退货入库页的视图取数（列表/详情、状态计数、筛选选项与待处理提示）——

// returnDetailView 退货单详情（单头 + 逐行明细 + 订单摘要 + 按状态决定的操作）→ 模板视图。
//
// countryLabel 与订单详情页同一条解析（当前界面语言的国家名，nil = 未接入时显示代码）。

// —— 表单与文案工具 ——

// warehouseOptions 某工程的仓库下拉项（默认仓在最前，由 inventory 侧排序保证）。
//
// 取不到时返回空表：页面据此只渲染「默认仓」一个选项，而不是让整页报错 ——
