package feature

// trade_empty_state_honesty_test.go — 交易域后台页的**空态诚实性**回归（docs/02-O-trade-site-audit.md）。
//
// 三条判据（都是「错了会静默」的那类）：
//
//  1. 空态的主行动**按条件渲染**：无筛选时不给「重置」—— 那个链接指向本页自己
//     （href == 当前 URL），点了 URL 与页面逐字不变，是死按钮（orders T2 / customers T1）；
//  2. 文案与**路由表**一致：订单页空态曾写「台前下单后（或后台代客建单）就会出现在这里」，
//     而 /admin/orders 只有 GET + cancel / status / refund / note / bulk-*，没有任何建单端点 ——
//     运营读完去找入口，翻遍页头 / 列表 / 行操作都找不到（orders T1，P0）；
//  3. `?returnId=` 指向不存在的单时说**真实原因**（退货申请不存在），而不是归口文案
//     「系统内部错误，请稍后重试」（returns T3）。
//
// 第 2 条的「路由表」判据取自 internal/routers/testdata/routes.snapshot（运行时路由表的快照）：
// 断言的是「这一前缀下不存在建单类端点」，不是「某一行文案里没有某个词」——
// 前者是事实，后者随时可以被改一个同义词蒙过去。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	orderenums "go_wp/internal/module/order/enums"
	orderhttp "go_wp/internal/module/order/inbound/http"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// tradePageEnv 订单 / 退货两个后台页的最小真实环境（真 service + 真模板 + 隔离库）。
type tradePageEnv struct {
	engine  *gin.Engine
	db      *gorm.DB
	project string
}

// newTradePageEnv 建一个隔离库、一个站点工程，并注册两个列表页的 GET 路由。
//
// 走真 service 而不是 fake：空态分支的判据由 handler 算（FilterActive / HasDetail），
// 用 fake 就得把 handler 的判据再抄一份进测试，而抄错的那份不会报错。
func newTradePageEnv(t *testing.T) *tradePageEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	// 迁移把结构建好，种子把词条灌进去（本文件里的库值断言依赖它）。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "交易域空态测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	orders := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	pages := orderhttp.NewOrderPageHandle(orders, projects)
	returns := orderhttp.NewReturnPageHandle(orders, projects, nil)
	engine.GET("/admin/orders", pages.OrdersPage)
	engine.GET("/admin/returns", returns.ReturnsPage)

	return &tradePageEnv{engine: engine, db: db, project: project.ID}
}

// get 请求一个页面并返回响应体（状态码必须是 200：Jet 缺键会 200 + 半截页）。
func (e *tradePageEnv) get(t *testing.T, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 状态码 %d", path, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("GET %s 渲染中断（缺 </html>）—— 模板里有缺键的 if", path)
	}
	return body
}

// emptyActionHrefs 摘出响应体里所有空态主行动的 href（空态里最多一两个）。
func emptyActionHrefs(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, `class="empty-actions"`)
		if i < 0 {
			return out
		}
		rest = rest[i:]
		if j := strings.Index(rest, `href="`); j >= 0 && j < 400 {
			tail := rest[j+len(`href="`):]
			if k := strings.Index(tail, `"`); k > 0 {
				out = append(out, tail[:k])
			}
		}
		rest = rest[len(`class="empty-actions"`):]
	}
}

// TestOrdersEmptyStateActionsFollowFilter orders 空态的主行动按条件给（02-O orders T2）。
//
// 无筛选时必须**不给**：链接会指向本页自己，点了没有任何变化。
func TestOrdersEmptyStateActionsFollowFilter(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	bare := env.get(t, "/admin/orders?project="+env.project)
	if got := emptyActionHrefs(bare); len(got) != 0 {
		t.Errorf("无筛选时不该给空态主行动（指向本页自己的死按钮），实际 %v", got)
	}

	filtered := env.get(t, "/admin/orders?project="+env.project+"&keyword=ZZPROBEXYZZ")
	hrefs := emptyActionHrefs(filtered)
	if len(hrefs) != 1 {
		t.Fatalf("带筛选时应有且只有 1 个空态主行动，实际 %v", hrefs)
	}
	if !strings.Contains(hrefs[0], "project="+env.project) {
		t.Errorf("「重置」必须带上当前工程，否则多工程下会掉回默认工程：%s", hrefs[0])
	}
	if strings.Contains(hrefs[0], "keyword=") {
		t.Errorf("「重置」不能把筛选条件带回去（那等于没重置）：%s", hrefs[0])
	}
}

// TestOrdersEmptyCopyMatchesRoutes 空态文案不得承诺路由表里不存在的建单能力（02-O orders T1，P0）。
func TestOrdersEmptyCopyMatchesRoutes(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	// ① 路由表：/admin/orders 下不存在建单类端点。
	snapshot, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "routers", "testdata", "routes.snapshot"))
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}
	for _, line := range strings.Split(string(snapshot), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " /admin/orders") {
			continue
		}
		for _, createWord := range []string{"/create", "/new", "/place", "/draft"} {
			if strings.HasSuffix(line, "/admin/orders"+createWord) || strings.Contains(line, "/admin/orders"+createWord+" ") {
				t.Fatalf("路由表里出现了订单建单端点 %q —— 空态文案与路由表不再一致（要么补入口、要么改文案）", line)
			}
		}
	}

	// ② 页面输出：不再出现「后台代客建单」这类承诺。
	body := env.get(t, "/admin/orders?project="+env.project)
	if strings.Contains(body, "代客建单") {
		t.Error("订单空态仍在承诺「后台代客建单」—— 页面上没有任何建单入口")
	}
	if !strings.Contains(body, "台前下单后就会出现在这里") {
		t.Error("订单空态应说明「台前下单后就会出现在这里」")
	}

	// ③ 库值（模板兜底不是真源）：sys_i18n 的值同样不得留旧承诺。
	assertI18nValueNotContains(t, env.db, "admin.orders.list.empty_tail", "zh-CN", "代客建单")
	assertI18nValueNotContains(t, env.db, "admin.orders.list.empty_tail", "en-US", "on their behalf")
}

// TestReturnsMissingDetailSaysNotFound `?returnId=` 指向不存在的单时说真实原因（02-O returns T3）。
//
// 原先 ErrReturnNotFound 不在本页白名单里 → 落归口文案「系统内部错误，请稍后重试」：
// 有反馈，但把「单不存在」说成了「系统故障」，用户会去重试而不是回列表。
//
// 词条在这里显式注入（pkg/i18n.InjectForTest）：本测试的引擎不建 i18n 组件，
// 不注入的话取词会回落 fallback（key 本身），断言就只能停在「放行了 key」这一步。
// 「库里真的有这条词条」由 TestTradePagesI18nValuesAreActuallyUpdated 查库覆盖 ——
// 两条合起来才是完整的判据：**命中白名单 + 取当前语言译文**。
func TestReturnsMissingDetailSaysNotFound(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrReturnNotFound: {"zh-CN": "退货申请不存在", "en-US": "Return request not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	body := env.get(t, "/admin/returns?project="+env.project+"&returnId=999999")
	alert := orderPageAlert(body)
	if !strings.Contains(alert, "退货申请不存在") {
		t.Errorf("缺详情时的提示应是「退货申请不存在」，实际 %q", alert)
	}
	if strings.Contains(alert, "系统内部错误") {
		t.Errorf("缺详情被归口成了系统故障：%q", alert)
	}
	// 反面：非法 id（非数字）仍走本页自造的参数级文案，两者不能混。
	invalid := orderPageAlert(env.get(t, "/admin/returns?project="+env.project+"&returnId=abc"))
	if strings.Contains(invalid, "退货申请不存在") {
		t.Errorf("非数字 id 应说「编号不合法」，实际 %q", invalid)
	}
}

// TestTradePagesI18nValuesAreActuallyUpdated 库值断言：改的必须是 sys_i18n，不是模板兜底。
//
// 上一轮的教训：模板有兜底 ≠ 词条已登记/已更新（模板里的中文只是 t() 的 fallback，
// 词条命中时显示的是库里的值）。所以这里**直接查库**。
func TestTradePagesI18nValuesAreActuallyUpdated(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}

	// customers 空态：标题不再自相矛盾，说明把「后台不能直接新建」还回来（02-O customers T2）。
	assertI18nValueEquals(t, db, "admin.customers.list.empty_heading", "zh-CN", "还没有客户")
	assertI18nValueContains(t, db, "admin.customers.list.empty_desc", "zh-CN", "后台不能直接新建")

	// 注册时间两个 label 成对（02-O customers T5）。
	assertI18nValueEquals(t, db, "admin.customers.field.registered_at", "zh-CN", "注册时间（起）")
	assertI18nValueEquals(t, db, "admin.customers.field.registered_to", "zh-CN", "注册时间（止）")

	// 状态按钮动词已 key 化（02-O customer_detail T1：否则英文界面「Disable这个账号」）。
	assertI18nValueEquals(t, db, "admin.customers.action.disable", "zh-CN", "停用")
	assertI18nValueEquals(t, db, "admin.customers.action.enable", "en-US", "Enable")

	// 退货单状态说明已 key 化（02-O returns T2）。
	for _, key := range []string{
		"admin.returns.note.requested", "admin.returns.note.approved", "admin.returns.note.received",
		"admin.returns.note.completed", "admin.returns.note.rejected", "admin.returns.note.cancelled",
	} {
		for _, lang := range []string{"zh-CN", "en-US"} {
			if got := i18nValue(db, key, lang); got == "" {
				t.Errorf("词条 %s（%s）缺失 —— 英文 / 中文界面会回落模板兜底", key, lang)
			}
		}
	}

	// returns T3 的另一半：放行的是 key，而「退货申请不存在」这句译文必须真的在库里 ——
	// 页面那条提示走的是 shell.TranslateFor，词条缺了就只剩裸 key。
	assertI18nValueEquals(t, db, orderenums.ErrReturnNotFound, "zh-CN", "退货申请不存在")

	// 孤儿词条已退役（02-O customers T3）：两语言行都不在了。
	if got := i18nValue(db, "admin.customers.empty", "zh-CN"); got != "" {
		t.Errorf("孤儿词条 admin.customers.empty 仍在库里（zh-CN=%q）—— 删能力要连 seed 与幂等条件一起收口", got)
	}
	if got := i18nValue(db, "admin.customers.empty", "en-US"); got != "" {
		t.Errorf("孤儿词条 admin.customers.empty 仍在库里（en-US=%q）", got)
	}
}

// TestReturnsStatusNoteMovedIntoHelp 退货单的「当前状态说明」进 .help 悬浮，不再平铺在正文（returns T2）。
//
// 那段文案是操作指引（「这一单现在该做什么 / 为什么没有按钮」），不是数据：
// 平铺在「操作」标题下方时，对已经会用的人是每次访问都付的噪声，也把真正的操作表单往下推；
// 而且它此前是 Go 侧硬编码中文，英文界面整块露中文。
//
// 判据：模板里正文没有那一行、悬浮里有取词；六条词条中英成对（查库那半在
// TestTradePagesI18nValuesAreActuallyUpdated）。
func TestReturnsStatusNoteMovedIntoHelp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "templates", "admin", "returns.html"))
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	html := string(src)

	if strings.Contains(html, `class="hint">{{detail.Note}}`) {
		t.Error("状态说明仍在正文里（<p class=\"hint\">{{detail.Note}}</p>）—— 应移进「操作」标题的 .help 悬浮")
	}
	if !strings.Contains(html, "detail.NoteKey") {
		t.Error("模板没有取 detail.NoteKey —— 说明没有走词条，英文界面会露中文")
	}
	if !strings.Contains(html, `role="tooltip"`) {
		t.Error("「操作」标题的 .help 悬浮不见了")
	}
}

// TestOrdersPageErrShowsTranslationNotRawKey 页面上的 ?err= / ?ok= 显示**译文**，不是 item_key。
//
// 这一条钉的是本轮修掉的根因：白名单里存的是 item_key（order.err.orderNotFound），
// 而 API 出口把 key 交给 pkg/response 翻译、页面出口是**直接渲染**（模板里的 {{.Err}}），
// 于是同样命中白名单，页面显示的却是 `order.err.orderNotFound` 这一串裸 key ——
// 「订单不存在」这句现成的译文永远到不了运营眼前。
func TestOrdersPageErrShowsTranslationNotRawKey(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrOrderNotFound: {"zh-CN": "订单不存在", "en-US": "Order not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	body := env.get(t, "/admin/orders?project="+env.project+"&err="+orderenums.ErrOrderNotFound)
	alert := orderPageAlert(body)
	if !strings.Contains(alert, "订单不存在") {
		t.Errorf("?err= 命中白名单时应显示当前语言的译文，实际 %q", alert)
	}
	if strings.Contains(alert, orderenums.ErrOrderNotFound) {
		t.Errorf("提示条上出现了裸 key：%q", alert)
	}

	// 反面：手拼一个「看起来像业务文案」的值仍落归口文案（读侧白名单不能被绕过）。
	forged := orderPageAlert(env.get(t, "/admin/orders?project="+env.project+"&err="+url.QueryEscape("订单不存在")))
	if !strings.Contains(forged, "系统内部错误") {
		t.Errorf("伪造的 ?err= 应落归口文案，实际 %q", forged)
	}
}

// TestOrdersMissingDetailSaysNotFound `?orderId=` 指向不存在的单时说「订单不存在」。
//
// 与 returns T3 同形，但判据要自己重测：02-O 的实测用 UUID 形 URL 测这个参数，
// 而 orders.id 是 uint64（handler 走 strconv.ParseUint）—— 那个 URL 恒解析为 0、
// 恒不渲染详情，所以「没有提示」那条读数**无法区分「没注入提示」与「URL 根本走不通」**。
// 这里用**数字 id**（解析得通、查不到）重测：页面应显示「订单不存在」。
func TestOrdersMissingDetailSaysNotFound(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrOrderNotFound: {"zh-CN": "订单不存在", "en-US": "Order not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	alert := orderPageAlert(env.get(t, "/admin/orders?project="+env.project+"&orderId=999999"))
	if !strings.Contains(alert, "订单不存在") {
		t.Errorf("orderId 指向不存在的单时应说明原因，实际 %q", alert)
	}
	if strings.Contains(alert, "系统内部错误") {
		t.Errorf("缺详情被归口成了系统故障：%q", alert)
	}
}

// —— 词条查询小工具（直接读 sys_i18n：模板兜底不能当判据）——

func i18nValue(db *gorm.DB, key, lang string) string {
	var value string
	if err := db.Raw(
		"SELECT COALESCE(item_value, '') FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang,
	).Scan(&value).Error; err != nil {
		return ""
	}
	return value
}

func assertI18nValueEquals(t *testing.T, db *gorm.DB, key, lang, want string) {
	t.Helper()
	if got := i18nValue(db, key, lang); got != want {
		t.Errorf("词条 %s（%s）= %q，期望 %q", key, lang, got, want)
	}
}

func assertI18nValueContains(t *testing.T, db *gorm.DB, key, lang, want string) {
	t.Helper()
	if got := i18nValue(db, key, lang); !strings.Contains(got, want) {
		t.Errorf("词条 %s（%s）= %q，应包含 %q", key, lang, got, want)
	}
}

func assertI18nValueNotContains(t *testing.T, db *gorm.DB, key, lang, unwanted string) {
	t.Helper()
	if got := i18nValue(db, key, lang); strings.Contains(got, unwanted) {
		t.Errorf("词条 %s（%s）= %q，不应包含 %q", key, lang, got, unwanted)
	}
}
