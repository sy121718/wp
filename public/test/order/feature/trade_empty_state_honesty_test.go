package feature

// trade_empty_state_honesty_test.go — 交易域后台页的**空态诚实性**回归（docs/02-O-trade-site-audit.md）
// 与后台代客建单页（docs/02-W-admin-order-create.md）的渲染 / 回填判据。
//
// 判据（都是「错了会静默」的那类）：
//
//  1. 空态的主行动**按条件渲染**：无筛选时不给「重置」—— 那个链接指向本页自己
//     （href == 当前 URL），点了 URL 与页面逐字不变，是死按钮（orders T2 / customers T1）；
//     而**建单入口**（/admin/orders/new）是另一个 URL 的活按钮，永远该给（有权限时）；
//  2. 文案与**路由表**一致：空态曾写「台前下单后（或后台代客建单）就会出现在这里」，
//     而那时 /admin/orders 下没有任何建单端点 —— 运营读完去找入口，翻遍页头 / 列表 / 行操作
//     都找不到（orders T1，P0）。建单页落地后这条判据**反转**：路由表里存在建单入口 ⇒
//     页面必须真的给出指向它的入口（比原来那条更强：原来只防文案吹牛）；
//  3. `?returnId=` 指向不存在的单时说**真实原因**（退货申请不存在），而不是归口文案
//     「系统内部错误，请稍后重试」（returns T3）。
//
// 第 2 条的「路由表」判据取自 internal/routers/testdata/routes.snapshot（运行时路由表的快照）：
// 断言的是「这一前缀下有没有建单类端点」，不是「某一行文案里没有某个词」——
// 前者是事实，后者随时可以被改一个同义词蒙过去。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	"go_wp/internal/web/shell"
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
	// 权限上下文：生产里由 shell.PermContextMiddleware 按当前用户的权限码注入。
	// 本测试不装配 authz 组件，直接给一份「有 order:create」的集合 ——
	// 页头与空态的建单入口都按它渲染（模板里 canCreate := isset(.PermSet["order:create"])）。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{"order:create": true})
		c.Next()
	})
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

// emptyActionHrefs 摘出响应体里所有空态主行动的 href（按出现顺序）。
//
// 每个 `.empty-actions` 块内的 href 都要收集：带筛选时**建单入口与「重置」同处一块**，
// 只取块内第一个会让「重置」的第二条判据永远看不到东西（假绿）。
func emptyActionHrefs(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, `class="empty-actions"`)
		if i < 0 {
			return out
		}
		rest = rest[i:]
		end := strings.Index(rest, "</div>")
		if end < 0 {
			end = len(rest)
		}
		out = append(out, hrefsIn(rest[:end])...)
		rest = rest[end:]
	}
}

// pageActionHrefs 摘出页头 `.page-actions` 块里的 href（页级主行动的判据）。
func pageActionHrefs(body string) []string {
	i := strings.Index(body, `class="page-actions"`)
	if i < 0 {
		return nil
	}
	rest := body[i:]
	end := strings.Index(rest, "</header>")
	if end < 0 {
		end = len(rest)
	}
	return hrefsIn(rest[:end])
}

// hrefsIn 取一段 HTML 里所有的 href 值（按出现顺序）。
func hrefsIn(block string) []string {
	var out []string
	for {
		j := strings.Index(block, `href="`)
		if j < 0 {
			return out
		}
		block = block[j+len(`href="`):]
		k := strings.Index(block, `"`)
		if k < 0 {
			return out
		}
		out = append(out, block[:k])
		block = block[k+1:]
	}
}

// containsHref 这段 href 列表里有没有指向某个路径的（前缀匹配即可，链接带查询串）。
func containsHref(hrefs []string, path string) bool {
	for _, h := range hrefs {
		if strings.HasPrefix(h, path) {
			return true
		}
	}
	return false
}

// emptyDescText 取空态描述那一句（用于「承诺式描述」的判据 —— 描述里不该再讲入口在哪）。
func emptyDescText(body string) string {
	i := strings.Index(body, `class="empty-desc"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	end := strings.Index(rest, "</p>")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// TestOrdersEmptyStateActionsFollowFilter orders 空态的主行动按条件给（02-O orders T2）。
//
// 无筛选时**不得给「重置」**（链接指向本页自己，点了没有任何变化），
// 但**必须给建单入口**（/admin/orders/new 是另一个 URL 的活按钮）；
// 带筛选时两个都给，顺序固定「建单 → 重置」。
func TestOrdersEmptyStateActionsFollowFilter(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	bare := env.get(t, "/admin/orders?project="+env.project)
	bareHrefs := emptyActionHrefs(bare)
	if len(bareHrefs) != 1 {
		t.Fatalf("无筛选时应有且只有建单入口一个空态主行动，实际 %v", bareHrefs)
	}
	if !containsHref(bareHrefs, "/admin/orders/new") {
		t.Errorf("无筛选时的空态主行动必须是建单入口，实际 %v", bareHrefs)
	}
	if containsHref(bareHrefs, "/admin/orders?") {
		t.Errorf("无筛选时不该给「重置」（指向本页自己的死按钮），实际 %v", bareHrefs)
	}

	filtered := env.get(t, "/admin/orders?project="+env.project+"&keyword=ZZPROBEXYZZ")
	hrefs := emptyActionHrefs(filtered)
	if len(hrefs) != 2 {
		t.Fatalf("带筛选时应有两个空态主行动（建单 + 重置），实际 %v", hrefs)
	}
	if !containsHref(hrefs, "/admin/orders/new") {
		t.Errorf("带筛选时的空态也要给建单入口，实际 %v", hrefs)
	}
	reset := hrefs[1]
	if !strings.Contains(reset, "project="+env.project) {
		t.Errorf("「重置」必须带上当前工程，否则多工程下会掉回默认工程：%s", reset)
	}
	if strings.Contains(reset, "keyword=") {
		t.Errorf("「重置」不能把筛选条件带回去（那等于没重置）：%s", reset)
	}
}

// TestOrdersEmptyCopyMatchesRoutes 空态文案与路由表一致，且**入口必须真的存在**（02-O orders T1，P0）。
//
// 判据 ① 已按 docs/02-W §6 反转：路由表里存在建单入口（GET /admin/orders/new）时，
// 页面必须真的给出指向它的入口 —— 原判据只防「文案吹牛」（路由表里没有建单端点），
// 现在入口是真的，判据升级成「**承诺了什么就必须给出什么**」，反面更强。
func TestOrdersEmptyCopyMatchesRoutes(t *testing.T) {
	env := newTradePageEnv(t)
	if env == nil {
		return
	}

	snapshot, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "routers", "testdata", "routes.snapshot"))
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}

	// ① 路由表：这一前缀下**有**建单页端点 ⇒ 页头与空态都必须给出入口。
	hasCreateRoute := false
	for _, line := range strings.Split(string(snapshot), "\n") {
		if strings.TrimSpace(line) == "GET /admin/orders/new" {
			hasCreateRoute = true
		}
	}
	if !hasCreateRoute {
		t.Fatalf("路由表里没有 GET /admin/orders/new —— 建单页的实现被回退了（本判据的前提已不成立）")
	}
	body := env.get(t, "/admin/orders?project="+env.project)
	if !containsHref(emptyActionHrefs(body), "/admin/orders/new") {
		t.Errorf("路由表里有建单页，但空态没有给出入口（文案与路由表不一致）")
	}
	if !containsHref(pageActionHrefs(body), "/admin/orders/new") {
		t.Errorf("路由表里有建单页，但页头没有给出入口（双入口缺一）")
	}

	// ② 文案：**空态描述**里不再出现「代客建单」这种承诺式说法 —— 入口已经是下面那个真按钮，
	// 描述只说「订单从哪来」。（按钮文案「＋ 代客建单」是动作式措辞，不受本判据约束。）
	if desc := emptyDescText(body); strings.Contains(desc, "代客建单") {
		t.Errorf("空态描述里仍在描述「代客建单」：%q", desc)
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
//
// 路径：模板已按后端模块分目录（admin/order/returns.html），本用例原先读的是分目录前的
// 旧落点 admin/returns.html —— 文件不存在，它不是断言失败而是直接 Fatal（读不到就无从判）。
func TestReturnsStatusNoteMovedIntoHelp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "templates", "admin", "order", "returns.html"))
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

// ——— 后台代客建单页（docs/02-W-admin-order-create.md）———

// orderCreatePageEnv 建单页的最小真实环境：真 service + 真商品目录 + 真模板 + 隔离库。
type orderCreatePageEnv struct {
	engine  *gin.Engine
	fixture *orderFixture
	project string
}

// newOrderCreatePageEnv 装配一个可 GET / POST 的建单页。
//
// 商品目录传**真**的 productservice.Service：候选 SKU 走 ListBundleSKUs，
// 用替身就得把它的语义再抄一份进测试，而抄错的那份不会报错。
func newOrderCreatePageEnv(t *testing.T) *orderCreatePageEnv {
	t.Helper()
	f := newOrderFixture(t)
	if f == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{"order:create": true})
		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	page := orderhttp.NewOrderCreatePageHandle(f.orders, projects, f.products)
	engine.GET("/admin/orders/new", page.OrderCreatePage)
	engine.POST("/admin/orders/create", page.OrderCreateSubmit)
	return &orderCreatePageEnv{engine: engine, fixture: f, project: f.projectID}
}

func (e *orderCreatePageEnv) get(t *testing.T, path string) string {
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

func (e *orderCreatePageEnv) post(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/orders/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// adminOrderCreateForm 一份「填好了」的表单（用例按需覆盖字段）。
func adminOrderCreateForm(project, variantID, email, requestID string) url.Values {
	return url.Values{
		"project":            {project},
		"requestId":          {requestID},
		"customerEmail":      {email},
		"customerName":       {"张三"},
		"customerPhone":      {"13800000000"},
		"paymentMethod":      {"paypal"},
		"paymentMethodTitle": {"PayPal（模拟）"},
		"shippingTotal":      {"1500"},
		"remark":             {"尽快发货"},
		"adminNote":          {"电话单，客户要求顺丰"},
		"candidateKeyword":   {"回填商品"},
		"shipName":           {"李四"},
		"shipPhone":          {"13900000000"},
		"shipProvince":       {"广东省"},
		"shipCity":           {"深圳市"},
		"shipDistrict":       {"南山区"},
		"shipAddress":        {"科技园 1 号"},
		"shipZip":            {"518000"},
		"billCity":           {"上海市"},
		"variantId":          {variantID},
		"quantity":           {"2"},
	}
}

// TestAdminOrderCreatePageEchoesOnFailure 长表单失败后**就地重渲 200 + 回填**（docs/02-W §6）。
//
// 判据是「用户刚打的字还在不在」：字段 20+ 的表单一旦走 303 + ?err=，303 之后是一次 GET，
// 请求里没有 PostForm，几十个字段必然全丢（admin-ui-logic §9 第 7 条）。
// 所以这里逐字段比对**提交值**与**渲染值**，而不是只看「页面里出现了某个词」。
func TestAdminOrderCreatePageEchoesOnFailure(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	_, variantID := env.fixture.addProduct(t, "回填商品", 12.5, 5)

	// ① 整页渲染：Jet 缺键会 500 并丢弃半成品内容，</html> 是最稳的判据。
	page := env.get(t, "/admin/orders/new?project="+env.project+"&keyword=回填商品&rows=3")
	if !strings.Contains(page, `action="/admin/orders/create"`) {
		t.Error("建单页缺少提交表单")
	}
	if !strings.Contains(page, `name="variantId"`) || !strings.Contains(page, `name="quantity"`) {
		t.Error("明细行缺少并行数组字段 variantId / quantity")
	}
	if got := strings.Count(page, `name="quantity"`); got != 3 {
		t.Errorf("rows=3 时应渲染 3 行明细输入框，实际 %d", got)
	}
	if !strings.Contains(page, "回填商品") {
		t.Error("候选 SKU 应带出商品名（真商品目录的 ListBundleSKUs）")
	}

	// ② 提交**非法邮箱** → 200 + 整页 + 字段级标红 + 全部字段回填。
	form := adminOrderCreateForm(env.project, variantID, "not-an-email", "req-echo-1")
	form.Set("couponCode", "WELCOME")
	form.Set("sameBilling", "1")
	rec := env.post(t, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("失败提交必须就地重渲（200），实际 %d", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "</html>") {
		t.Fatal("失败重渲的输出不是一整页（缺 </html>）")
	}

	// 逐字段比对：提交值 == 渲染值（含多值明细行）。
	for field, want := range map[string]string{
		"customerEmail":      "not-an-email",
		"customerName":       "张三",
		"customerPhone":      "13800000000",
		"paymentMethod":      "paypal",
		"paymentMethodTitle": "PayPal（模拟）",
		"couponCode":         "WELCOME",
		"shipName":           "李四",
		"shipPhone":          "13900000000",
		"shipProvince":       "广东省",
		"shipCity":           "深圳市",
		"shipDistrict":       "南山区",
		"shipAddress":        "科技园 1 号",
		"shipZip":            "518000",
		"billCity":           "上海市",
	} {
		if !strings.Contains(out, `name="`+field+`" value="`+want+`"`) {
			t.Errorf("失败回填丢了字段 %s（期望值 %q）", field, want)
		}
	}
	if !strings.Contains(out, ">尽快发货</textarea>") {
		t.Error("客户备注没有回填（textarea 的值在标签之间）")
	}
	// 运费是数字输入框（name 与 value 之间隔着 min），单独比对 —— 单位是**分**。
	if !strings.Contains(out, `name="shippingTotal" min="0" value="1500"`) {
		t.Error("运费没有回填（单位是分）")
	}
	if !strings.Contains(out, ">电话单，客户要求顺丰</textarea>") {
		t.Error("后台备注没有回填")
	}
	// 明细行：选中的变体与数量都要在（回填是整份行重渲，不是逐字段 echo）。
	if !strings.Contains(out, `value="`+variantID+`" selected`) {
		t.Error("明细行的变体选择没有回填")
	}
	if !strings.Contains(out, `name="quantity" min="1" max="100000" value="2"`) {
		t.Error("明细行的数量没有回填")
	}
	// 复选框按「这次是否被提交」回填（勾了就是勾着）。
	if !strings.Contains(out, `name="sameBilling" value="1" checked`) {
		t.Error("「同收货地址」的勾选态没有回填")
	}
	// 出错字段标红 + 行内文案（字段级错误不能只提示在页顶）。
	if !strings.Contains(out, `name="customerEmail" value="not-an-email" aria-invalid="true"`) {
		t.Error("非法邮箱所在字段没有标红")
	}
	if !strings.Contains(out, `class="form-error"`) {
		t.Error("缺少字段级错误文案")
	}
	// 幂等键原样带回：用户重试同一份表单只落一单。
	if !strings.Contains(out, `name="requestId" value="req-echo-1"`) {
		t.Error("幂等键没有回填 —— 重试会落两单")
	}
}

// TestAdminOrderCreateDefaultsToNoGuestAccount 后台代客建单**默认不开号**（docs/02-W §4）。
//
// 这是本项最关键的语义：默认档必须让 user_id 留空、且**一封信都不发**；
// 只有显式勾选 opt-in 才开号 + 发初始密码。两个方向都要钉 —— 只测「勾了会开号」
// 无法发现「不勾也开号」这个真正的风险。
func TestAdminOrderCreateDefaultsToNoGuestAccount(t *testing.T) {
	env := newOrderCreatePageEnv(t)
	if env == nil {
		return
	}
	f := env.fixture
	_, variantID := f.addProduct(t, "开号开关商品", 20, 5)

	// —— 默认档：不勾开号 ——
	rec := env.post(t, adminOrderCreateForm(env.project, variantID, "off@example.com", "req-off-1"))
	if rec.Code != http.StatusFound {
		t.Fatalf("合法提交应 302 回列表页，实际 %d", rec.Code)
	}
	orderID := orderIDFromLocation(t, rec.Header().Get("Location"))
	if orderID == 0 {
		t.Fatal("成功跳转应带上新单的 orderId（回列表页时详情是展开的）")
	}

	var uid *uint64
	if err := f.db.Raw("SELECT user_id FROM orders WHERE id = ?", orderID).Scan(&uid).Error; err != nil {
		t.Fatalf("读订单 user_id 失败: %v", err)
	}
	if uid != nil {
		t.Errorf("默认（不勾开号）时 user_id 必须留空，实际 %d", *uid)
	}
	if m := f.mail.findTemplate("guest_account"); m != nil {
		t.Errorf("默认档不该发出初始密码邮件，实际收件人 %q", m.To)
	}
	var created int64
	if err := f.db.Raw("SELECT COUNT(*) FROM users WHERE lower(email) = 'off@example.com'").Scan(&created).Error; err != nil {
		t.Fatalf("查账号失败: %v", err)
	}
	if created != 0 {
		t.Errorf("默认档不该给客户建号，实际建了 %d 个", created)
	}

	// —— opt-in：显式勾选才开号 + 发信 ——
	optIn := adminOrderCreateForm(env.project, variantID, "on@example.com", "req-on-1")
	optIn.Set("provisionGuestAccount", "1")
	rec = env.post(t, optIn)
	if rec.Code != http.StatusFound {
		t.Fatalf("勾选开号的合法提交应 302，实际 %d", rec.Code)
	}
	orderID = orderIDFromLocation(t, rec.Header().Get("Location"))
	if err := f.db.Raw("SELECT user_id FROM orders WHERE id = ?", orderID).Scan(&uid).Error; err != nil {
		t.Fatalf("读订单 user_id 失败: %v", err)
	}
	if uid == nil {
		t.Error("显式勾选开号后订单必须关联账号（否则客户在账户中心看不到这单）")
	}
	if m := f.mail.findTemplate("guest_account"); m == nil || m.To != "on@example.com" {
		t.Errorf("显式勾选后应把初始密码寄给该邮箱，实际 %+v", m)
	}

	// —— 订单本身的来源与状态也要对：created_via=admin、状态 pending、明细落库 ——
	var head struct {
		Via    string
		Status string
	}
	if err := f.db.Raw("SELECT created_via AS via, status FROM orders WHERE id = ?", orderID).Scan(&head).Error; err != nil {
		t.Fatalf("读订单来源失败: %v", err)
	}
	if head.Via != "admin" {
		t.Errorf("后台建单的 created_via 应为 admin，实际 %q", head.Via)
	}
	if head.Status != "pending" {
		t.Errorf("建单后状态应为 pending，实际 %q", head.Status)
	}
}

// orderIDFromLocation 从 302 的 Location 里取 orderId（列表页靠它展开新单）。
func orderIDFromLocation(t *testing.T, loc string) uint64 {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("解析跳转地址失败: %v", err)
	}
	id, _ := strconv.ParseUint(u.Query().Get("orderId"), 10, 64)
	return id
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
