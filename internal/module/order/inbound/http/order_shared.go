package orderhttp

// order_shared.go — 订单模块后台页的共用件：请求取值、错误文案白名单、批量结论文案。
//
// 这里只放「两个以上页面都要用、且与具体页面无关」的东西。判据是**改一处会不会牵动多个页面**：
// 批量结论的句式、错误文案白名单、分页窗口解析都属于这一类；而各页的视图装配已经搬进模板，
// 各页的写动作出口（提示页 + 回跳白名单）留在各自的控制器文件里。
//
// 三个控制器文件的写动作出口形状一致：`xxxDone`（成功提示页）/ `xxxFail`（失败提示页），
// 内部都是 shell.RenderJump + shell.BackPath —— 回跳地址由服务端从表单 action 的 query
// 按白名单读回，不再由页面预算好塞进隐藏域。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/order/enums"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
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

// bulkSummary 批量动作的结果文案（成功 N 个 / 跳过 M 个）；订单与退货申请共用。
//
// 「跳过」必须出现在文案里：只报成功数会让「选了 10 个、实际改了 6 个」看起来像全做完了，
// 而剩下的那几个会在下次列表刷新时莫名其妙地回到原状。
// 动词与名词都由调用方以 orderBulkText 传入（key + 中文原文），文案模板与词一样经
// orderBulkTextOf 按当前语言取 —— 与读侧 orderDoneTexts 同一个取法。
func bulkSummary(c *gin.Context, verb, noun orderBulkText, done, skipped int) string {
	v, n := orderBulkTextOf(c, verb), orderBulkTextOf(c, noun)
	switch {
	case done == 0 && skipped == 0:
		return fmt.Sprintf(orderBulkTextOf(c, orderBulkNothingSelected), n)
	case skipped == 0:
		return fmt.Sprintf(orderBulkTextOf(c, orderBulkAllDone), v, strconv.Itoa(done), n)
	case done == 0:
		return fmt.Sprintf(orderBulkTextOf(c, orderBulkAllSkipped), n, v, strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(orderBulkTextOf(c, orderBulkPartial), v, strconv.Itoa(done), n, strconv.Itoa(skipped))
	}
}

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// order_page_labels.go - 订单管理页的展示文案映射（状态、操作人、来源、支付、金额、地址、时间）。

// translate 展示层的取词函数（与 shell.TranslateFor(c) 同形）。
//
// 类型别名（不是定义新类型）是为了能把 shell.TranslateFor(c) 直接传进来：
// 展示标签一律经它取词，**中文原文留在代码里当兜底** —— 词条缺失时页面显示中文，
// 而不是裸 key（`admin.orders.operator_type.admin`）或者英文界面恒中文。

// translate 展示层的取词函数（与 shell.TranslateFor(c) 同形）。
//
// 类型别名（不是定义新类型）是为了能把 shell.TranslateFor(c) 直接传进来：
// 展示标签一律经它取词，**中文原文留在代码里当兜底** —— 词条缺失时页面显示中文，
// 而不是裸 key（`admin.orders.operator_type.admin`）或者英文界面恒中文。
type translate = func(key, fallback string) string

// orderLabel 一条展示标签（i18n key + 中文兜底）。
//
// 与 orderBulkText 同形：**key 与中文原文只有这一份**，取词只有一个入口（orderLabelOf）。
// 直接在函数体里写中文的后果是「模板直接渲染的文本永远中文」——
// orders.html 的 {{o.StatusLabel}} / {{detail.Head.StatusLabel}} 都是**直接渲染**，
// 不经过 pkg/response 的 translate，所以在 Go 侧硬写中文等于英文界面恒中文。

// orderLabel 一条展示标签（i18n key + 中文兜底）。
//
// 与 orderBulkText 同形：**key 与中文原文只有这一份**，取词只有一个入口（orderLabelOf）。
// 直接在函数体里写中文的后果是「模板直接渲染的文本永远中文」——
// orders.html 的 {{o.StatusLabel}} / {{detail.Head.StatusLabel}} 都是**直接渲染**，
// 不经过 pkg/response 的 translate，所以在 Go 侧硬写中文等于英文界面恒中文。
type orderLabel struct{ key, fallback string }

// orderLabelOf 取一条标签的当前语言文本；tr 为 nil（纯函数测试路径）时回落中文兜底。

// orderLabelOf 取一条标签的当前语言文本；tr 为 nil（纯函数测试路径）时回落中文兜底。
func orderLabelOf(tr translate, l orderLabel) string {
	if tr == nil {
		return l.fallback
	}
	return tr(l.key, l.fallback)
}

// orderPlaceholderRE 已删除：命名占位符（`{name}`）的检测与填充复用 pkg/i18n
//（FillNamedPlaceholders / FillTranslate），本模块不再自造一份替换器。

// 订单页的展示标签词条。
// orderInvalidIDLabel 订单编号不合法（表单里的 orderId 缺失 / 被改坏时的参数级提示）。
//
// 只留这一条：状态 / 操作人类型 / 下单入口的展示标签现在都在模板里按值取词
// （`tr("site.fragment.order.status." + status, ...)`），Go 侧不再需要各自的 label 常量。

// 订单页的展示标签词条。
// orderInvalidIDLabel 订单编号不合法（表单里的 orderId 缺失 / 被改坏时的参数级提示）。
//
// 只留这一条：状态 / 操作人类型 / 下单入口的展示标签现在都在模板里按值取词
// （`tr("site.fragment.order.status." + status, ...)`），Go 侧不再需要各自的 label 常量。
var orderInvalidIDLabel = orderLabel{"admin.orders.form.invalid_id", "订单编号不合法，请回到列表页重新操作。"}

// orderStatusLabel 状态 → 展示标签（未知值原样返回：宁可显示生值，也不显示空白）。
//
// 映射的真源在 orderenums.OrderStatusLabel（后台客户页经 ordercontract 引用**同一份**）：
// 三处各写一张中文表的结果是「改一处、另两处静默留在旧说法上」。
// key 为空表示这一档没有词条可查（空状态 / 认不出的取值），直接用兜底值 / 原值。

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

// order_page_query.go - 订单管理页的查询参数解析与对外文案出口。

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。
func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = shell.PageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

// orderQueryID 解析 orderId 查询参数（非法即 0 = 不渲染详情块）。

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
//	② 归口文案 —— shell.PageInternalText(c)：**当前语言的译文**，不是裸 key
//	   （列表页装载失败时它进的是模板 {{.Err}}，那是直接渲染的文本，不经过 response 的 translate；
//	   原先那条路径是 `c.String(500, orderenums.ErrInternal)` —— 硬编码中文，英文界面照旧显示中文）；
//	   命中白名单的那一支同样取译文（orderPageFacingText），否则页面显示的是裸 key；
//	③ 结构化日志 —— **只有落到归口文案那一支才记**：命中白名单的是业务错误（预期内的用户输入问题），
//	   记 ERROR 只会淹没真正的故障；未命中的原文（表名 / SQL 片段 / 驱动前缀）只进日志、绝不进响应。

// orderFacingError 把订单模块的错误转成可展示文案（本模块页面路径的**错误文案三件套**出口）。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
//
// 三件套在这里的落法：
//
//	① 白名单 —— orderFacingText（enums.UserFacingMessages，与 API 出口共用同一份）；
//	② 归口文案 —— shell.PageInternalText(c)：**当前语言的译文**，不是裸 key
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
		return shell.TranslateFor(c)(msg, msg)
	}
	logger.Scene("order-page").With("path", c.Request.URL.Path).Error(err, "订单后台页处理失败")
	return shell.PageInternalText(c)
}

// orderFacingText 白名单校验：命中返回原文，未命中返回空串。

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

// orderBulkText 一条批量结论文案（i18n key + 中文兜底）。

// orderBulkText 一条批量结论文案（i18n key + 中文兜底）。
type orderBulkText struct{ key, fallback string }

// orderBulkTextOf 取一条批量结论文案的当前语言文本。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经
// strconv.Itoa）：否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。

// orderBulkTextOf 取一条批量结论文案的当前语言文本。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经
// strconv.Itoa）：否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func orderBulkTextOf(c *gin.Context, t orderBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// 批量动作结论文案的四个分支（bulkSummary 用它们 Sprintf 出整句）。
//
// 结论**在响应体里**渲染（提示页），不再经 ?done= —— 于是读侧那套「受控文案 + 数字归一比对」
// （orderDoneTexts / orderPageDone / orderBulkActions / orderBulkExtraNotices）整批消失。

// 批量动作结论文案的四个分支（bulkSummary 用它们 Sprintf 出整句）。
//
// 结论**在响应体里**渲染（提示页），不再经 ?done= —— 于是读侧那套「受控文案 + 数字归一比对」
// （orderDoneTexts / orderPageDone / orderBulkActions / orderBulkExtraNotices）整批消失。
var (
	orderBulkNothingSelected = orderBulkText{orderenums.BulkNoneSelected, "没有勾选任何%s。"}
	orderBulkAllDone         = orderBulkText{orderenums.BulkAllDone, "%s %s 个%s。"}
	orderBulkAllSkipped      = orderBulkText{orderenums.BulkAllSkipped, "0 个%s%s，%s 个被跳过（状态不允许或已不存在）。"}
	orderBulkPartial         = orderBulkText{orderenums.BulkPartial, "%s %s 个%s，跳过 %s 个（状态不允许或已不存在）。"}
)

// 批量动作的动词（i18n key + 中文原文）：**动词也 key 化**，否则英文界面上会出现
// 「Moved 3 个订单」这种中英混排 —— 语序不同，不能只翻模板。

// 批量动作的动词（i18n key + 中文原文）：**动词也 key 化**，否则英文界面上会出现
// 「Moved 3 个订单」这种中英混排 —— 语序不同，不能只翻模板。
var (
	orderBulkVerbFlowed    = orderBulkText{orderenums.BulkVerbFlowed, "已流转"}
	orderBulkVerbCancelled = orderBulkText{orderenums.BulkVerbCancelled, "已取消"}
	orderBulkVerbApproved  = orderBulkText{orderenums.BulkVerbApproved, "已同意"}
	orderBulkVerbRejected  = orderBulkText{orderenums.BulkVerbRejected, "已拒绝"}
	orderBulkVerbDeleted   = orderBulkText{orderenums.BulkVerbDeleted, "已删除"}
	orderBulkVerbDisabled  = orderBulkText{orderenums.BulkVerbDisabled, "已停用"}
	orderBulkVerbEnabled   = orderBulkText{orderenums.BulkVerbEnabled, "已启用"}
)

// 批量动作的名词（i18n key + 中文原文）。

// 批量动作的名词（i18n key + 中文原文）。
var (
	orderBulkNounOrder  = orderBulkText{orderenums.BulkNounOrder, "订单"}
	orderBulkNounReturn = orderBulkText{orderenums.BulkNounReturn, "退货申请"}
	orderBulkNounCoupon = orderBulkText{orderenums.BulkNounCoupon, "优惠码"}
)

// orderBulkIDsText shell.BulkIDs 的失败文案（单次提交的 id 超过上限）。
//
// 只是转调 shell 的受控出口：超限错误是 shell 的类型（shell.BulkIDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有 shell 知道 —— 本模块不再用 shell.MaxBulkIDs 重算一遍：
// 那是第二份真相，而且必然丢掉 Count（旧的实现正是如此）。
// 判据也不再是「文案来自哪里」而是类型：出口只认 sentinel，认不出就回落归口文案。
// 留痕（哪个页面、哪个操作人触发）由 shell 的出口统一记日志。

// orderBulkIDsText shell.BulkIDs 的失败文案（单次提交的 id 超过上限）。
//
// 只是转调 shell 的受控出口：超限错误是 shell 的类型（shell.BulkIDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有 shell 知道 —— 本模块不再用 shell.MaxBulkIDs 重算一遍：
// 那是第二份真相，而且必然丢掉 Count（旧的实现正是如此）。
// 判据也不再是「文案来自哪里」而是类型：出口只认 sentinel，认不出就回落归口文案。
// 留痕（哪个页面、哪个操作人触发）由 shell 的出口统一记日志。
func orderBulkIDsText(c *gin.Context, err error) string {
	return shell.BulkIDsFacingText(c, err)
}

// firstNonEmpty 取第一个非空字符串（回显拼接用：业务错误优先、内部提示兜底）。
//
// 页面不能把两处错误都摊开 —— ?err= 只有一个位置，先到的那条才是用户当下要看的。

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

// order_page_view.go - 订单管理页的视图构造（列表/详情、状态计数与筛选选项）。

// —— 表单与文案工具 ——

// salesCardView 之外，本页只有一条趋势图 —— 详情见 salesTrendView。

// ── 月度趋势（折线图）────────────────────────────────────────────────────
//
// 坐标一律在这里算完，模板只贴字符串 —— 与 AI 会话页的 token 趋势同一套规矩，
// 两处共用 .chart-trend-* 的样式（viewBox 1000×200 + preserveAspectRatio="none"）。

// —— 页面取数（视图组装：模板不做逻辑与算术）——

// return_page_query.go - 退货入库页的表单与查询取值、对外文案出口。

// return_page_view.go - 退货入库页的视图构造（列表/详情、状态计数、筛选选项与待处理提示）。

// returnDetailView 退货单详情（单头 + 逐行明细 + 订单摘要 + 按状态决定的操作）→ 模板视图。
//
// countryLabel 与订单详情页同一条解析（当前界面语言的国家名，nil = 未接入时显示代码）。

// —— 表单与文案工具 ——

// warehouseOptions 某工程的仓库下拉项（默认仓在最前，由 inventory 侧排序保证）。
//
// 取不到时返回空表：页面据此只渲染「默认仓」一个选项，而不是让整页报错 ——
