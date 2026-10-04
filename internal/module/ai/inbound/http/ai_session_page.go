// ai_session_page.go — 后台「AI 会话」页：会话列表 + 详情（投影 + 事件日志）+ 折叠操作。
//
// 与配置面的 ai_page.go 分开：命名独立（SessionPageHandle），共用同包的小工具
// （facingQuery / aiErrKey / redirectWhere / parseInt64 / userID），不引入配置面的任何状态。
//
// 写操作全部走 PRG：POST → 303 → GET（?id=/?done=/?err=）。回执里的 done/err 放的是
// **i18n key**（不是译文），渲染时再由 facingQuery 按请求语言翻译 —— 译文不能进 query，
// 否则换语言重放同一 URL 会得到另一种语言的文字，缓存与日志也跟着脏。
package aihttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/web/shell"
)

const (
	sessionTemplate = "admin/ai/sessions"
	sessionPath     = "/admin/ai/sessions"
	sessionPageSize = 20
	sessionEventTop = 50
	// sessionTrendDays 趋势图窗口（天）。写死而不是做成控件：时间维度已经有 from/to 两个
	// 日期在筛，再加一个「粒度」下拉等于让人自己算区间；窗口跟随筛选区间即可。
	sessionTrendDays = 30
	// 页内两个标签：会话看板与模型配置。
	sessionTabSessions = "sessions"
	sessionTabModels   = "models"
	// 两个标签各自的「进入」权限点（菜单可见性也挂这两个，任一命中即显示菜单）。
	permProviderList = "ai:provider_list"
	permSessionList  = "ai:session_list"
)

// providerLister 会话页需要的最小供应商读取能力（发消息区的 provider 下拉 / model 候选）。
//
// 用窄接口而不是 aiservice.Service：会话页对配置面保持「只读、单向、可替换」，
// 用例测试塞一个假列表即可，不必建出整条配置面。
type providerLister interface {
	ListProviders(ctx context.Context) ([]aidto.Provider, error)
}

// SessionPageHandle 会话管理页处理器。
type SessionPageHandle struct {
	svc       *aiservice.SessionService
	providers providerLister
}

// NewSessionPageHandle 构造；providers 为 nil 时不渲染发消息区的供应商候选。
func NewSessionPageHandle(svc *aiservice.SessionService, providers providerLister) *SessionPageHandle {
	return &SessionPageHandle{svc: svc, providers: providers}
}

// SessionsPage GET /admin/ai/sessions：AI 会话页 —— 用量看板（指标卡 + 趋势图）
// + 多维筛选 + 会话明细表，带 ?id= 时同时渲染详情；页内第二个标签是模型配置。
//
// 两个标签页在同一个页面里由服务端一次渲染完（不跳转、不 htmx 二次请求）：
// 「模型」那一块就是配置页同一套供应商卡片，切标签不该让整页重新走一遍请求。
func (h *SessionPageHandle) SessionsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	if page < 1 {
		page = 1
	}
	filter := aidto.SessionQuery{
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		Status:      sessionIntOr(c.DefaultQuery("status", "-1"), -1),
		ProviderKey: strings.TrimSpace(c.Query("provider")),
		ModelID:     strings.TrimSpace(c.Query("model")),
		CreateBy:    parseInt64(c.Query("user")),
		From:        strings.TrimSpace(c.Query("from")),
		To:          strings.TrimSpace(c.Query("to")),
	}

	rows, total, err := h.svc.ListSessionsFiltered(ctx, filter, page, sessionPageSize)
	if err != nil {
		shell.PageError(c, "ai", err)
		return
	}
	tab := sessionTabOf(c.Query("tab"))
	// 停在没权限的标签上等于给用户一个空面板：切到另一个。两个都没有时保持原样 ——
	// 页面渲染成「没有可用的 AI 功能」空态，比白屏或 403 都清楚。
	canModels := sessionCan(c, permProviderList)
	canSessions := sessionCan(c, permSessionList)
	if tab == sessionTabModels && !canModels && canSessions {
		tab = sessionTabSessions
	} else if tab == sessionTabSessions && !canSessions && canModels {
		tab = sessionTabModels
	}
	data := shell.Prepare(c, gin.H{
		"title": shell.TranslateFor(c)(aienums.AdminLLMTitle, "大模型管理"),
		// Rows 是 []gin.H（每项 {Session, Usage}），不是 []aidto.Session：
		// 每行要带上它自己的按 (供应商, 模型) 拆分，而模板对 map 做动态索引
		// （.ModelUsage[r.ID]）依赖 Jet 的索引表达式；拼成同序的嵌套结构没这层不确定性。
		"Rows":     h.sessionRows(ctx, rows),
		"Total":    total,
		"Page":     page,
		"PageSize": sessionPageSize,
		"Filter":   filter,
		"Keyword":  filter.Keyword,
		"Status":   filter.Status,
		"Tab":      tab,
		// IsModels 是给模板用的布尔：在模板里比较字符串（.Tab == "models"）
		// 要依赖 Jet 的比较表达式求值，多一处能出错的地方。
		"IsModels": tab == sessionTabModels,
		// 标签按权限裁剪：没权限的标签连按钮都不渲染（模板据此包 {{if}}）。
		"CanViewModels":   canModels,
		"CanViewSessions": canSessions,
		"Err":             facingQuery(c, "err"),
		"Done":            facingQuery(c, "done"),
	})

	h.attachSessionUsage(c, ctx, data, filter)
	h.attachProviderCards(c, ctx, data)

	// 分页链接在服务端拼好带着全部筛选条件：让模板自己拼 query 就得在每个翻页链接里
	// 把七八个参数各写一遍，漏一个就变成「翻页把筛选清空」。
	totalPages := int((total + int64(sessionPageSize) - 1) / int64(sessionPageSize))
	data["TotalPages"] = totalPages
	data["HasPrev"] = page > 1
	data["HasNext"] = page < totalPages
	if page > 1 {
		data["PrevURL"] = sessionPageURL(c, page-1)
	}
	if page < totalPages {
		data["NextURL"] = sessionPageURL(c, page+1)
	}

	c.HTML(http.StatusOK, sessionTemplate, data)
}

// sessionRows 把会话列表拼成模板行数据：每行带上它自己的按 (供应商, 模型) 拆分。
//
// 拆分一次查完整页（见 SessionService.SessionModelUsageOf），拉取失败不阻断整页 ——
// 行照常渲染，只是悬浮卡里没有明细（给空切片，模板不必再写一层 isset）。
func (h *SessionPageHandle) sessionRows(ctx context.Context, rows []aidto.Session) []gin.H {
	out := make([]gin.H, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	usage, err := h.svc.SessionModelUsageOf(ctx, ids)
	if err != nil {
		usage = map[int64][]aidto.SessionModelUsage{}
	}
	// 调用流水预览（最近几次调用各自多久、多少 token、成没成）：同样一次查完整页。
	// 失败也不阻断整页 —— 悬浮卡少一段「最近调用」，聚合那段照常显示。
	calls, cerr := h.svc.SessionCallsOf(ctx, ids)
	if cerr != nil {
		calls = map[int64]aidto.SessionCalls{}
	}
	for i := range rows {
		u := usage[rows[i].ID]
		if u == nil {
			u = []aidto.SessionModelUsage{}
		}
		out = append(out, gin.H{"Session": rows[i], "Usage": u, "Calls": calls[rows[i].ID]})
	}
	return out
}

// attachSessionUsage 装配用量看板：指标卡 + 趋势图 + 三个筛选下拉的候选值。
//
// 统计失败不阻断整页：列表与详情照常可用，只是看板那几张卡显示不出来 ——
// 一个聚合查询超时把整页打成 500 不成比例。失败信息交给模板在看板区提示。
func (h *SessionPageHandle) attachSessionUsage(c *gin.Context, ctx context.Context, data gin.H, filter aidto.SessionQuery) {
	usage, err := h.svc.SessionUsageOf(ctx, filter)
	if err != nil {
		data["UsageErr"] = aiErrText(c, err)
		// 仍然给零值：模板不必为「这个键可能不存在」再写一遍 isset 分支。
		usage = aidto.SessionUsage{}
	}
	data["Usage"] = usage

	// 趋势单独取：它按 (供应商, 模型) 分组，比指标卡多一次聚合；两者任一个失败都只
	// 影响自己那一块，不把整页打成 500。窗口两端与峰值由 service 一并给出（见 SessionTrend），
	// 模板不再自己遍历取首尾 —— Jet 里取切片首尾要判空索引。
	trend, terr := h.svc.SessionTrend(ctx, filter, sessionTrendDays)
	if terr != nil {
		if _, has := data["UsageErr"]; !has {
			data["UsageErr"] = aiErrText(c, terr)
		}
		trend = aidto.SessionTrend{}
	}
	data["Trend"] = trend

	opts, oerr := h.svc.SessionFilterOptions(ctx)
	if oerr != nil {
		data["OptionsErr"] = aiErrText(c, oerr)
		// 候选拉不到时给空集合而不是 nil：下拉里只剩「全部」，筛选仍可用 URL 手填。
		opts = aidto.SessionFilterOptions{Providers: []string{}, Models: []string{}, Creators: []int64{}}
	}
	data["FilterOptions"] = opts
}

// attachProviderCards 装配第二个标签（模型配置）要用的供应商卡片。
//
// 复用配置面的 cardData：两块页面说的是同一批供应商，各拼一份数据必然漂移
// （加了字段只改一处）。数据仍从 providerLister 这个只读窄接口来。
func (h *SessionPageHandle) attachProviderCards(c *gin.Context, ctx context.Context, data gin.H) {
	data["Presets"] = aiservice.BuiltinPresets()
	data["BuiltinKeys"] = aiservice.BuiltinProviderKeys()
	data["ProtocolOptions"] = aienums.ProtocolOptions
	data["ProviderCards"] = []gin.H{}
	h.attachProviders(c, ctx, data, true)
}

// attachProviders 拉一次供应商列表，写进 data 的 Providers；withCards 时顺带拼好卡片数据。
//
// 会话列表页、模型标签页、详情抽屉都读这一份 —— 发消息区的候选与模型标签页要的是同一个列表，
// 各拉一次就是三次查询加三处漂移点。
// 拉取失败不阻断整页（或整个抽屉）：只置 ProviderErr，其余部分照常渲染。
func (h *SessionPageHandle) attachProviders(c *gin.Context, ctx context.Context, data gin.H, withCards bool) {
	data["Providers"] = []aidto.Provider{}
	if h.providers == nil {
		return
	}
	list, err := h.providers.ListProviders(ctx)
	if err != nil {
		data["ProviderErr"] = aiErrText(c, err)
		return
	}
	data["Providers"] = list
	if !withCards {
		return
	}
	cards := make([]gin.H, 0, len(list))
	for i := range list {
		cards = append(cards, cardData(data, list[i], "", false, aiservice.BuiltinBaseURL(list[i].ProviderKey)))
	}
	data["ProviderCards"] = cards
}

// sessionTabOf 归一标签参数：只认 "models"，其余一律回会话标签。
//
// 不做严格校验：手改错一个字母时落回默认标签，比回 400 有用。
func sessionTabOf(raw string) string {
	// 默认停在模型标签：合并成一个「大模型管理」入口之后，模型是第一个标签（m00210），
	// 默认标签必须与它一致，否则点菜单进来会落在第二个标签上。
	if strings.EqualFold(strings.TrimSpace(raw), sessionTabSessions) {
		return sessionTabSessions
	}
	return sessionTabModels
}

// sessionCan 读本请求的权限码集合（shell.PermSetKey 由 Casbin 中间件注入）。
//
// 页面路由只挂了「进入页面」的那一个 obj，标签内部各自的动作权限点要在这里判 ——
// 没权限的标签不渲染，用户不会看到一个点进去全是 403 的入口。
func sessionCan(c *gin.Context, code string) bool {
	v, ok := c.Get(shell.PermSetKey)
	if !ok {
		return false
	}
	set, ok := v.(map[string]bool)
	if !ok {
		return false
	}
	return set[code]
}

// sessionPageURL 拼一个「只改页码、其余筛选照旧」的列表页 URL。
//
// 刻意不带 ?id=：翻页时那条会话的详情已经不在当前页上，把它留在 URL 里会让
// 每一次翻页都多一次详情查询，而页面根本不显示它。
func sessionPageURL(c *gin.Context, page int) string {
	q := url.Values{}
	for _, name := range []string{"keyword", "status", "provider", "model", "user", "from", "to", "tab"} {
		if v := strings.TrimSpace(c.Query(name)); v != "" {
			q.Set(name, v)
		}
	}
	q.Set("page", strconv.Itoa(page))
	return sessionPath + "?" + q.Encode()
}

// SessionAppend POST /admin/ai/sessions/append：给指定会话追加一条事件。
func (h *SessionPageHandle) SessionAppend(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.AppendEvent(c.Request.Context(), aidto.AppendEventReq{
		SessionID: id,
		Kind:      strings.TrimSpace(c.PostForm("kind")),
		Content:   strings.TrimSpace(c.PostForm("content")),
		UserID:    userID(c),
	})
	if err != nil {
		h.redirectSession(c, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, "done", aienums.MsgSessionAppended)
}

// SessionRename POST /admin/ai/sessions/rename：只改标题（乐观锁版本随表单带回）。
func (h *SessionPageHandle) SessionRename(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Title:   strings.TrimSpace(c.PostForm("title")),
		Status:  -1,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.redirectSession(c, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, "done", aienums.MsgSessionRenamed)
}

// SessionArchive POST /admin/ai/sessions/archive：归档或恢复（表单 status=1 表示恢复）。
func (h *SessionPageHandle) SessionArchive(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	status := int16(aienums.SessionArchived)
	if c.PostForm("status") == "1" {
		status = int16(aienums.SessionActive)
	}
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Status:  status,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.redirectSession(c, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, "done", aienums.MsgSessionRenamed)
}

// SessionFold POST /admin/ai/sessions/fold：提交一次折叠。
func (h *SessionPageHandle) SessionFold(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.Fold(c.Request.Context(), aidto.FoldReq{
		SessionID: id,
		FromSeq:   parseInt64(c.PostForm("fromSeq")),
		ToSeq:     parseInt64(c.PostForm("toSeq")),
		Summary:   strings.TrimSpace(c.PostForm("summary")),
		UserID:    userID(c),
	})
	if err != nil {
		h.redirectSession(c, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, "done", aienums.MsgSessionFolded)
}

// SessionSend POST /admin/ai/sessions/send：发一条消息 —— 写 user 事件 → 把当前投影拼成
// 一段文本打一次模型 → 把回复写成 assistant 事件 → 回会话页。
//
// 权限借 /api/ai/chat 的 casbin obj：发消息本质就是一次对话，不新开权限点。
func (h *SessionPageHandle) SessionSend(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.SendMessage(c.Request.Context(), aidto.SendMessageReq{
		SessionID:       id,
		ProviderKey:     strings.TrimSpace(c.PostForm("providerKey")),
		Model:           strings.TrimSpace(c.PostForm("model")),
		Input:           strings.TrimSpace(c.PostForm("input")),
		MaxOutputTokens: parseInt64(c.PostForm("maxOutputTokens")),
		UserID:          userID(c),
	})
	if err != nil {
		h.redirectSession(c, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, "done", aienums.MsgSessionSent)
}

// redirectSession 写操作做完后回列表页（带上表单里带回的 back 与回执槽）。
//
// 不再带 ?id=：详情已经是抽屉，回跳时那句 id 只会让页面白查一次详情而页面并不渲染它。
// HTMX 与原生走 redirectWhere 的两条口径（HX-Redirect / 303）。
func (h *SessionPageHandle) redirectSession(c *gin.Context, slot, text string) {
	target := sessionBackURL(c.PostForm("back"))
	if strings.TrimSpace(text) != "" {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + url.Values{slot: []string{text}}.Encode()
	}
	redirectWhere(c, target)
}

// sessionBackURL 校验抽屉表单带回的返回地址。
//
// 只接受本页路径（前缀白名单），其余一律回列表根 —— 这个值会原样变成 302 的 Location，
// 透传就是一个开放重定向。前缀校验足以挡住 `//evil.com`、`https://evil.com` 与
// `/admin/ai/sessions/rename` 这类动作端点，而它要表达的意图本来也只有
// 「回到这个列表的某个筛选组合」。
func sessionBackURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == sessionPath || strings.HasPrefix(raw, sessionPath+"?") {
		return raw
	}
	return sessionPath
}
