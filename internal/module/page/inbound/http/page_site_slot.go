package pagehttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

const (
	// siteSlotPageTitle 页面标题（**i18n key**：adminui.Prepare 会对 "title" 取词；
	// 词条 pageenums.SiteSlotsTitle 已存在，值即原来的「系统页面」）。
	siteSlotPageTitle = pageenums.SiteSlotsTitle
	// siteSlotSitePrefix 访问面静态站点的公开前缀（routes.go 把 ActiveRoot 挂在 /site）。
	// 页面发布记录的 active_path 是站点内逻辑路径（含语言前缀，以 "/" 开头），
	// 拼上这个前缀才是浏览器能打开的地址。
	siteSlotSitePrefix = "/site"
	// siteSlotEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
	siteSlotEmptyField = "—"
	// 绑定 / 解绑的成功回执（经 shell.RenderJump 渲染进提示页，也登记在
	// siteSlotFacingMessages 里供取词兜底）。
	// 常量值是 **i18n key**，中文兜底在同文件的 siteSlotFacingMessages —— 两者必须成对，
	// 取词统一走模板里的 tr(key, 中文兜底)：Go 侧只给事实，不产出成品串。
	siteSlotBoundText   = pageenums.SiteSlotBoundOk
	siteSlotUnboundText = pageenums.SiteSlotUnboundOk
	// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记取词就没有中文兜底）。
	siteSlotNoProjectText = pageenums.SiteSlotNoProject
	siteSlotNoPageText    = pageenums.SiteSlotNoPage
	// 绑定 / 换绑的话术：动作与服务端完全一致（bind 是 upsert），
	// 差别只在「这会改掉现有绑定」要不要说出来。两处取词同源（行内操作列与绑定面板）。
)

// siteSlotFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// **键是 i18n key**（两类都是）：page 模块错误常量的值就是常量名、而常量名即词条 key；
// 本页自造的成功 / 参数级文案的常量值也已经是 key（见上面的常量块）。值是**中文兜底**，
// 与词条缺失时页面上显示的那句话逐字相同。
//
// 用常量做键而不是手抄字符串：page 模块调整常量值时这里跟着一起变，不会静默失配。
var siteSlotFacingMessages = map[string]string{
	pageenums.ErrInvalidParam:    "提交的信息不完整或格式不对，请回到列表页重新操作。",
	pageenums.ErrProjectNotFound: "站点工程不存在，请回到列表页重新选择工程。",
	pageenums.ErrPageNotFound:    "要绑定的页面不存在，可能已被删除。",
	pageenums.ErrInvalidSlot:     "槽位键不在白名单内，请回到列表页重新选择。",
	pageenums.ErrSlotPageMiss:    "要绑定的页面不存在、已被删除，或不属于当前工程。",
	siteSlotBoundText:            "已绑定。该工程的页面已标记待重建，重新构建那些页面后新链接才会生效。",
	siteSlotUnboundText:          "已解绑。该工程的页面已标记待重建，重新构建那些页面后访问面才会去掉旧链接。",
	siteSlotNoProjectText:        "没有可用的站点工程：先去页面管理里建一个工程，槽位是挂在工程下的。",
	siteSlotNoPageText:           "请先在下拉里选一个页面再提交。",
}

// 槽位状态的徽章档与标签已移交模板（internal/templates/admin/page/site_slots.html）：
// Go 只给事实（Bound / PageDeleted / Published），选档与取词是展示判断，见
// docs/rules/template-boundary.md「Go 给事实 + Jet 决定怎么显示」。
//
// 词条 key 仍是 admin.site_slots.state.{unbound,deleted,published,unpublished}
//（public/migrations/451_i18n_go_hardcoded_texts.sql 已种 zh-CN / en-US 两语）；
// 模板侧的 key 集由 internal/templates/site_structure_i18n_test.go 对账钉住。

// siteSlotPageHandle 系统页面槽位页处理器。
type siteSlotPageHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
}

// NewSiteSlotPageHandle 构造。
func NewSiteSlotPageHandle(pages pagecontract.PageService, projects projectcontract.ProjectService) *siteSlotPageHandle {
	return &siteSlotPageHandle{pages: pages, projects: projects}
}

// SiteSlotsPage 系统页面槽位页（GET /admin/site-slots）。
//
// 工程列表装载失败**降级渲染**（空列表 + 归口提示，HTTP 200，与 project 域主题页、
// admin 六页同一判据）：侧栏、页头、概览、菜单全部保留，运营看得出「是这一页没读出来」，
// 还能换菜单、刷新重试。原先这里是 `c.String(500, …)` —— 浏览器里只剩一块纯文本，
// 用户既改不了也退不回（AGENTS.md 形态 ①）。原文只进日志（siteSlotFacingError）。
func (h *siteSlotPageHandle) SiteSlotsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.HTML(http.StatusOK, "admin/page/site_slots.html",
			adminui.Prepare(c, siteSlotPageData(nil, "", nil, nil, pageErrMsgQuiet(c, err))))
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	// 写动作的结论不在本页回显（走 shell.RenderJump，见 page_jump.go）：Err 只剩
	// **取数失败**这一条来源（工程列表 / 页面候选 / 槽位清单读不到）。
	var pageErr pageMsg

	// 绑定面板的槽位参数：行内那条「绑定 / 换绑」链接带过来的（GET 回本页 + #slot-bind-panel 锚点）。
	// 这里只取值，渲染与否由 siteSlotApplyBindPanel 对照槽位清单判定 —— 手拼的 slot 一律不渲染。
	bindSlot := strings.TrimSpace(c.Query("slot"))

	candidates := []pagecontract.PageResp{}
	var slots *pagecontract.SiteSlotListResp

	if selected != "" {
		// 页面下拉候选：themeID 传空串 = 列全部页面（与页面管理页按激活主题过滤不同 ——
		// 槽位要指向任何一页，包括还没挂主题的）。
		list, lerr := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: selected})
		if lerr != nil {
			pageErr = pageErrMsgQuiet(c, lerr)
		} else {
			candidates = list
		}

		// Lang 留空 = 站点默认语言：这里要的是「访问面上这一页在哪」，
		// 与后台界面语言（zh-CN / en-US）是两件事，不能把界面语言当站点语言传进去。
		resp, serr := h.pages.ListSiteSlots(ctx, &pagecontract.SiteSlotListReq{ProjectID: selected})
		if serr != nil {
			pageErr = pageErrMsgQuiet(c, serr)
		} else {
			slots = resp
		}
	}

	data := siteSlotPageData(projects, selected, candidates, slots, pageErr)
	siteSlotApplyBindPanel(data, candidates, slots, bindSlot)
	c.HTML(http.StatusOK, "admin/page/site_slots.html", adminui.Prepare(c, data))
}

// siteSlotPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// pageErr 交的是**事实**（key + 中文兜底），取词在模板里做 —— 键名与计数口径只在这里定义一次，
// 真实渲染测试可以直接喂数据走同一条组装路径，而不必构造 gin.Context 去抽语言。
//
// 为什么抽出来：渲染键名与计数口径只在这里定义一次，真实渲染测试可以直接喂数据走
// 同一条组装路径，而不是在测试里手抄一份键名 —— 手抄的那一份会随模板演进静默失配，
// 而那正是「页面上少了一块、断言却通过」的成因。
func siteSlotPageData(projects []projectcontract.ProjectResp, selected string,
	candidates []pagecontract.PageResp, slots *pagecontract.SiteSlotListResp,
	pageErr pageMsg) gin.H {
	rows := make([]gin.H, 0)
	boundCount, total, unpublished, deleted := 0, 0, 0, 0
	if slots != nil {
		total, boundCount = slots.Total, slots.BoundCount
		for _, it := range slots.Items {
			// 计数口径：绑了但页面已删（悬空）与绑了但没发布（降级）分开数，
			// 前者要换绑或解绑，后者要发布或换绑 —— 两件事，不能合成一个数字。
			switch {
			case it.PageDeleted:
				deleted++
			case it.Bound && !it.Published:
				unpublished++
			}
			rows = append(rows, siteSlotRow(it, candidates))
		}
	}
	return gin.H{
		"title":           siteSlotPageTitle,
		"menu":            "site-slots",
		"Projects":        projects,
		"SelectedProject": selected,
		"Rows":            rows,
		// 工程里一个页面都没有时，绑定表单不渲染空下拉（空下拉只会让运营点一个必然失败的提交）。
		"HasPages":         len(candidates) > 0,
		"NoPages":          len(candidates) == 0,
		"BoundCount":       boundCount,
		"Total":            total,
		"UnpublishedCount": unpublished,
		"DeletedCount":     deleted,
		// 页头两个徽标的档位不再由 Go 产出：模板按上面两个计数自己选档
		//（判据与旧 helper 逐字一致：`n != 0`，**含负数非零**）。
		"Err": pageErr,
		// NoProjectEmpty 「还没有站点工程」空态：工程列表为空 **且** 本次没有出错。
		//
		// 不能只判 len(Projects) == 0：装载失败时工程列表同样是空的，但那时该显示的是
		// 错误条，而不是「先去页面管理建一个工程」—— 用户明明有工程，是这一页没读出来，
		// 引导他去建一个已存在的工程是比没有提示更坏的结果（同一判据见 project 域主题页的
		// themeManageData.NoProjectEmpty）。
		//
		// 判据在 handler 算好、模板只读一个布尔：模板是磁盘热读文件，而 Go 侧改动要等
		// air 重编译，两边短暂不同步 —— 缺键直接参与判断会让**整页中断**
		//（HTTP 200 + 后面整块 HTML 消失），所以模板侧一律 isset 包裹。
		"NoProjectEmpty": len(projects) == 0 && pageErr.Empty(),
	}
}

// siteSlotApplyBindPanel 把「绑定面板」需要的键补进渲染数据（就地改 data）。
//
// 行内不再内嵌页面下拉（每行一份 13 项候选 → N 行 N×13 个 option 节点，操作列还被下拉撑宽），
// 改成「点这一行 → GET 回本页带 slot 参数 → 表格下方的面板里选页面」：候选只渲染一份，
// 面板本身是原生 form + 原生 select，无 JS 也成立，鼠标 / 滚轮与触摸板 / 触屏 / 键盘都走原生路径。
//
// 为什么不并进 siteSlotPageData：那个函数被同包的渲染测试直接调用（page_page_err_test.go），
// 改签名会波及清单外的文件 —— 所以面板单独一步装配，既有调用点一个都不动。
//
// 三种情况一律不渲染面板（fail closed，不给一个点了必然失败的下拉）：
//   - 没带 slot 参数（常态浏览列表）；
//   - slot 不在这个工程的槽位清单里（手拼 URL 猜槽位键）；
//   - 这个工程一个页面都没有（没有候选可绑 —— 与列表页「不给空下拉」同一判据）。
func siteSlotApplyBindPanel(data gin.H, candidates []pagecontract.PageResp,
	slots *pagecontract.SiteSlotListResp, bindSlot string) {
	if bindSlot == "" || slots == nil || len(candidates) == 0 {
		return
	}
	for _, it := range slots.Items {
		if it.Slot != bindSlot {
			continue
		}
		data["BindSlot"] = it.Slot
		data["BindSlotNameKey"] = it.SlotName
		data["BindSlotNameFallback"] = pageenums.SiteSlotName(it.SlotName)
		data["BindBound"] = it.Bound
		data["BindDeleted"] = it.PageDeleted
		data["BindPageOptions"] = siteSlotPageOptions(candidates, it.PageID)
		// 行内那条链接据此标 aria-current：读屏用户能听出「面板正开着的是这一行」。
		if rows, ok := data["Rows"].([]gin.H); ok {
			for _, row := range rows {
				if slot, _ := row["Slot"].(string); slot == it.Slot {
					row["Active"] = true
				}
			}
		}
		return
	}
}

// SiteSlotBind 绑定 / 换绑（POST /admin/site-slots/bind）。
//
// 绑定与换绑是同一个动作（服务端是 upsert），因此只有一条路由：
// 表单只表达「这个槽位绑到哪一页」，绑第二次就是把绑定改成另一个页面。
// 结论走整页提示（成功 1 秒后回本页）—— 不再经 302 + ?ok= / ?err= 回显。
func (h *siteSlotPageHandle) SiteSlotBind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		siteSlotJump(c, false, siteSlotMsg(siteSlotNoPageText))
		return
	}
	err := h.pages.BindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotBindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
		PageID:    pageID,
	})
	if err != nil {
		siteSlotJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	siteSlotJump(c, true, siteSlotMsg(siteSlotBoundText))
}

// SiteSlotUnbind 解绑（POST /admin/site-slots/unbind）。
//
// 解绑是幂等的（本来没绑也返回成功），也是页面已删那种悬空绑定唯一的收场方式。
// 结论走整页提示。
func (h *siteSlotPageHandle) SiteSlotUnbind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	err := h.pages.UnbindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotUnbindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
	})
	if err != nil {
		siteSlotJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	siteSlotJump(c, true, siteSlotMsg(siteSlotUnboundText))
}

// === 接口面（JSON API：/api/page/site-slot/{list,bind,unbind}）===
//
// 与上面的页面面是同一个功能的两条通道（internal/module/CLAUDE.md §inbound/http
// 「一个功能一个文件，接口面与页面面都在里面」）：同一条 service 路径、同一份错误映射
// （pageErrorStatus / pageErrorMessage 在 pages_handle.go 的接口面一节），差别只在出口 ——
// 页面面走 shell.RenderJump 的整页提示，接口面走 pkg/response 的 JSON。
//
// 绑定用的是 ShouldBind 而不是 ShouldBindJSON：后台页是原生表单 POST，
// 同时 API 调用方发 JSON 也走同一入口（DTO 的 json / form tag 同名同义）。

// ListSiteSlots 列出系统页面槽位及其绑定状态（含未绑定的槽位）。
func (h *Handle) ListSiteSlots(c *gin.Context) {
	req, ok := binding.Query[pagedto.SiteSlotListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListSiteSlots(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

// BindSiteSlot 把系统页面槽位绑到某个页面。
//
// 用 ShouldBind 而不是 ShouldBindJSON：后台页是原生表单 POST（与其它后台页一致），
// 同时 API 调用方发 JSON 也走同一入口 —— DTO 的 json / form tag 同名同义。
func (h *Handle) BindSiteSlot(c *gin.Context) {
	req, ok := binding.Form[pagedto.SiteSlotBindReq](c)
	if !ok {
		return
	}
	if err := h.svc.BindSiteSlot(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgSiteSlotBound, nil)
}

// UnbindSiteSlot 解绑系统页面槽位（幂等：本来没绑也返回成功）。
func (h *Handle) UnbindSiteSlot(c *gin.Context) {
	req, ok := binding.Form[pagedto.SiteSlotUnbindReq](c)
	if !ok {
		return
	}
	if err := h.svc.UnbindSiteSlot(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgSiteSlotUnbound, nil)
}

// —— 页面取数（视图组装：模板不做判断与算术）——

// siteSlotRow 一个槽位 → 模板视图。
//
// 行里只有事实：模板按 Bound / PageDeleted / Published 三个布尔选徽章档与文案
// （判据顺序与原先的 switch 一致：未绑定优先，其次绑定已失效，再其次已发布，
// 最后是降级未发布）。四种状态的语义是：
//
//	unbound      未绑定（正常状态，中性徽章）；
//	published    已绑定且已发布（链接生成方会输出链接）；
//	unpublished  已绑定但未发布（**降级状态**：链接生成方跳过它，必须显眼）；
//	deleted      绑定的页面已被删除（**必须立刻修**，红色 + 解绑入口）。
func siteSlotRow(it pagecontract.SiteSlotItemResp, candidates []pagecontract.PageResp) gin.H {
	row := gin.H{
		"Slot": it.Slot,
		// 槽位名与用途的取值是**词条 key**（pageenums 的两张表就是词条键，值即中文兜底）：
		// 交 key + 兜底，取词交给模板 —— 模板才能在英文界面下换成英文。
		"SlotNameKey":      it.SlotName,
		"SlotNameFallback": pageenums.SiteSlotName(it.SlotName),
		"UsageKey":         it.Usage,
		"UsageFallback":    pageenums.SiteSlotUsage(it.Usage),
		"Bound":            it.Bound,
		"Unbound":          !it.Bound,
		"PageDeleted":      it.PageDeleted,
		"PageID":           it.PageID,
		"DraftPath":        siteSlotTextOrEmpty(it.DraftPath),
		"Path":             it.Path,
		"PublicURL":        siteSlotPublicURL(it.Path),
		"Published":        it.Published,
		// Unpublished 单独给一个布尔：它就是「运营以为配好了、实际链接生成方会跳过」的那一类。
		"Unpublished": it.Bound && !it.PageDeleted && !it.Published,
		"PageOptions": siteSlotPageOptions(candidates, it.PageID),
	}
	// 徽章（档位 + 文案）不在这里产出：模板按上面这些事实选档、取词并写类名
	//（badge-{{tone}}）—— 见 internal/templates/admin/page/site_slots.html。
	return row
}

// siteSlotPageOptions 页面下拉候选（value = 页面 id，显示文本 = 草稿路径，当前绑定预先选中）。
func siteSlotPageOptions(pages []pagecontract.PageResp, selectedID string) []gin.H {
	out := make([]gin.H, 0, len(pages))
	for _, p := range pages {
		out = append(out, gin.H{
			"ID":       p.ID,
			"Label":    siteSlotTextOrEmpty(p.DraftPath),
			"Selected": p.ID == selectedID,
		})
	}
	return out
}

// —— 表单与文案工具 ——

// siteSlotPublicURL 站点内逻辑路径 → 访问面可打开的地址（拼上 /site 前缀）。
func siteSlotPublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return siteSlotSitePrefix + "/" + strings.TrimPrefix(p, "/")
}

// siteSlotTextOrEmpty 空值统一显示成「—」（表格里的空白单元格读不出「没有值」）。
func siteSlotTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return siteSlotEmptyField
	}
	return value
}

// siteSlotFirstNonEmpty 取第一个非空文案（本页多处「错误提示只留第一条」的收口）。
func siteSlotFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// —— 本功能自己的回跳与回执（不借 page_jump.go 的共享出口）——

// siteSlotBackKeys 回跳筛选键：渲染时拼进表单 action 的 query、POST 回来时由
// shell.BackPath 读回 —— 两处必须同一份，否则「写完跳回去筛选静默丢了」。
var siteSlotBackKeys = []string{"project"}

const pageSiteSlotsPath = "/admin/site-slots"

func siteSlotBack(c *gin.Context) string {
	return shell.BackPath(c, pageSiteSlotsPath, siteSlotBackKeys...)
}

// siteSlotBackMsg 回跳链接文字（复用本页标题词条，不新增全站词条）。
var siteSlotBackMsg = pageMsg{Key: pageenums.SiteSlotsTitle, Fallback: "系统页面"}

// siteSlotJump 回系统页面槽位页的提示页出口。
func siteSlotJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, siteSlotBack(c), siteSlotBackMsg)
}

// siteSlotMsg 站点槽位页的一条文案（key + 中文兜底，兜底表见 siteSlotFacingMessages）。
func siteSlotMsg(key string) pageMsg {
	return pageMsg{Key: key, Fallback: siteSlotFacingMessages[key]}
}
