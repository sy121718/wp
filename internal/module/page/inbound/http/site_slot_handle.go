// site_slot_handle.go — 后台系统页面槽位页（BIZ-1）。
//
// 系统页面槽位把「结算页是哪一页」这类事实固定下来：购物车片段的「去结算」、
// 访客订单列表的「查看订单」、登录页与注册页的互跳，都从这里取路径。
// 后端接口（/api/page/site-slot/{list,bind,unbind}）早已就绪，后台此前没有界面 ——
// 菜单点进来是 404，本文件补齐这个入口。
//
// 四条与 page 模块的约定：
//
//  1. 跨模块只依赖 pagecontract（它已把槽位 DTO 重导出为契约别名）与不可变 pageenums，
//     不 import 其它模块的 dto / model / service。
//
//  2. pageenums 的错误常量值是**常量名**（"ErrInvalidSlot"），不是中文文案，而且 page
//     模块没有 order / cart 那样的 UserFacingMessages 白名单切片。所以本页自带一份
//     「错误常量名 → 中文面客文案」映射（siteSlotFacingMessages）：未命中的多半是数据库
//     原文（可能带表名甚至 SQL 片段），一律落到 shell.PageInternalText 的统一提示。
//     ?err= / ?ok= 是用户可编辑的查询参数，回显时同样过这份白名单 —— 与订单页同口径。
//
//  3. 槽位绑的是**页面 id**（uuid，不可变）而不是 URL：改 URL 是页面的常规操作，
//     绑 id 之后链接自动跟着走。页面没有「标题」这个概念，草稿路径是它唯一稳定的身份，
//     所以下拉的 value 是页面 id、显示文本是草稿路径。
//
//  4. 「绑定存在」与「访问面真的有产物」是两件事：Bound=true 但 Published=false 时
//     Path 为空，链接生成方据此降级（不输出链接）。页面上必须把这种状态显式标出来，
//     否则运营会以为配好了；PageDeleted 是绑定指向了已删页面，属于「需要立刻修」的状态
//     （红色提示 + 解绑入口）。**未绑定不是错误** —— 这个站没有博客、没有结算页是正常状态，
//     用中性徽章而不是红色。
package pagehttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

const (
	// siteSlotPageTitle 页面标题（字面量走 shell.Prepare 的 fallback 链路；
	// 不改动 page enums 那个文件的既有集合，与订单页同口径）。
	siteSlotPageTitle = "系统页面"
	// siteSlotSitePrefix 访问面静态站点的公开前缀（routes.go 把 ActiveRoot 挂在 /site）。
	// 页面发布记录的 active_path 是站点内逻辑路径（含语言前缀，以 "/" 开头），
	// 拼上这个前缀才是浏览器能打开的地址。
	siteSlotSitePrefix = "/site"
	// siteSlotEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
	siteSlotEmptyField = "—"
	// 绑定 / 解绑的成功回执（会进 ?ok=，因此也登记在 siteSlotFacingMessages 里）。
	siteSlotBoundText   = "已绑定。该工程的页面已标记待重建，重新构建那些页面后新链接才会生效。"
	siteSlotUnboundText = "已解绑。该工程的页面已标记待重建，重新构建那些页面后访问面才会去掉旧链接。"
	// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记就会在回显时被自己吞掉）。
	siteSlotNoProjectText = "没有可用的站点工程：先去页面管理里建一个工程，槽位是挂在工程下的。"
	siteSlotNoPageText    = "请先在下拉里选一个页面再提交。"
)

// siteSlotFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// 键分两类：
//   - page 模块的错误常量名（pageenums.ErrXxx 的值就是常量名本身），值是面向运营的中文；
//   - 本页自造的成功 / 参数级文案，值等于自身（它们会进 ?ok= / ?err=）。
//
// 取值时**键与值都算命中**（见 siteSlotFacingText）：写侧回填 ?err= 的既有常量名形态、
// 也有已经转好的中文形态，两种都要能过。
//
// 用常量做键而不是手抄字符串：page 模块调整常量值时这里跟着一起变，不会静默失配。
var siteSlotFacingMessages = map[string]string{
	pageenums.ErrInvalidParam:    "提交的信息不完整或格式不对，请回到列表页重新操作。",
	pageenums.ErrProjectNotFound: "站点工程不存在，请回到列表页重新选择工程。",
	pageenums.ErrPageNotFound:    "要绑定的页面不存在，可能已被删除。",
	pageenums.ErrInvalidSlot:     "槽位键不在白名单内，请回到列表页重新选择。",
	pageenums.ErrSlotPageMiss:    "要绑定的页面不存在、已被删除，或不属于当前工程。",
	siteSlotBoundText:            siteSlotBoundText,
	siteSlotUnboundText:          siteSlotUnboundText,
	siteSlotNoProjectText:        siteSlotNoProjectText,
	siteSlotNoPageText:           siteSlotNoPageText,
}

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
func (h *siteSlotPageHandle) SiteSlotsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, siteSlotInternalText(c))
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	// 回显文案：?err= / ?ok= 都过白名单，查不到的一律收口（查询参数是用户可编辑的，
	// 不能拿它当「业务提示」直接显示）。
	pageErr := siteSlotQueryText(c, c.Query("err"), siteSlotInternalText(c))
	pageOk := siteSlotQueryText(c, c.Query("ok"), "")

	candidates := []pagecontract.PageResp{}
	var slots *pagecontract.SiteSlotListResp

	if selected != "" {
		// 页面下拉候选：themeID 传空串 = 列全部页面（与页面管理页按激活主题过滤不同 ——
		// 槽位要指向任何一页，包括还没挂主题的）。
		list, lerr := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: selected})
		if lerr != nil {
			pageErr = siteSlotFirstNonEmpty(pageErr, siteSlotFacingError(c, lerr))
		} else {
			candidates = list
		}

		// Lang 留空 = 站点默认语言：这里要的是「访问面上这一页在哪」，
		// 与后台界面语言（zh-CN / en-US）是两件事，不能把界面语言当站点语言传进去。
		resp, serr := h.pages.ListSiteSlots(ctx, &pagecontract.SiteSlotListReq{ProjectID: selected})
		if serr != nil {
			pageErr = siteSlotFirstNonEmpty(pageErr, siteSlotFacingError(c, serr))
		} else {
			slots = resp
		}
	}

	c.HTML(http.StatusOK, "admin/site_slots.html",
		shell.Prepare(c, siteSlotPageData(projects, selected, candidates, slots, pageErr, pageOk)))
}

// siteSlotPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// 为什么抽出来：渲染键名与计数口径只在这里定义一次，真实渲染测试可以直接喂数据走
// 同一条组装路径，而不是在测试里手抄一份键名 —— 手抄的那一份会随模板演进静默失配，
// 而那正是「页面上少了一块、断言却通过」的成因。
func siteSlotPageData(projects []projectcontract.ProjectResp, selected string,
	candidates []pagecontract.PageResp, slots *pagecontract.SiteSlotListResp,
	pageErr, pageOk string) gin.H {
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
		"Err":              pageErr,
		"Ok":               pageOk,
	}
}

// SiteSlotBind 绑定 / 换绑（POST /admin/site-slots/bind）。
//
// 绑定与换绑是同一个动作（服务端是 upsert），因此只有一条路由：
// 表单只表达「这个槽位绑到哪一页」，绑第二次就是把绑定改成另一个页面。
func (h *siteSlotPageHandle) SiteSlotBind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		siteSlotRedirect(c, projectID, "", siteSlotNoPageText)
		return
	}
	err := h.pages.BindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotBindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
		PageID:    pageID,
	})
	if err != nil {
		siteSlotRedirect(c, projectID, "", siteSlotFacingError(c, err))
		return
	}
	siteSlotRedirect(c, projectID, siteSlotBoundText, "")
}

// SiteSlotUnbind 解绑（POST /admin/site-slots/unbind）。
//
// 解绑是幂等的（本来没绑也返回成功），也是页面已删那种悬空绑定唯一的收场方式。
func (h *siteSlotPageHandle) SiteSlotUnbind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	err := h.pages.UnbindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotUnbindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
	})
	if err != nil {
		siteSlotRedirect(c, projectID, "", siteSlotFacingError(c, err))
		return
	}
	siteSlotRedirect(c, projectID, siteSlotUnboundText, "")
}

// —— 页面取数（视图组装：模板不做判断与算术）——

// siteSlotRow 一个槽位 → 模板视图。
//
// 四种状态在这里定型，模板只做分支渲染：
//
//	unbound      未绑定（正常状态，中性徽章）；
//	published    已绑定且已发布（链接生成方会输出链接）；
//	unpublished  已绑定但未发布（**降级状态**：链接生成方跳过它，必须显眼）；
//	deleted      绑定的页面已被删除（**必须立刻修**，红色 + 解绑入口）。
func siteSlotRow(it pagecontract.SiteSlotItem, candidates []pagecontract.PageResp) gin.H {
	row := gin.H{
		"Slot":        it.Slot,
		"SlotName":    it.SlotName,
		"Usage":       it.Usage,
		"Bound":       it.Bound,
		"Unbound":     !it.Bound,
		"PageDeleted": it.PageDeleted,
		"PageID":      it.PageID,
		"DraftPath":   siteSlotTextOrEmpty(it.DraftPath),
		"Path":        it.Path,
		"PublicURL":   siteSlotPublicURL(it.Path),
		"Published":   it.Published,
		// Unpublished 单独给一个布尔：它就是「运营以为配好了、实际链接生成方会跳过」的那一类。
		"Unpublished": it.Bound && !it.PageDeleted && !it.Published,
		"BindLabel":   "绑定",
		"PageOptions": siteSlotPageOptions(candidates, it.PageID),
	}
	switch {
	case !it.Bound:
		row["Badge"], row["StateLabel"] = "badge-mute", "未绑定"
	case it.PageDeleted:
		row["Badge"], row["StateLabel"] = "badge-danger", "绑定已失效"
	case it.Published:
		row["Badge"], row["StateLabel"] = "badge-success", "已发布"
	default:
		row["Badge"], row["StateLabel"] = "badge-warning", "已绑定但未发布"
	}
	if it.Bound {
		// 已绑定（含悬空绑定）时按钮话术是「换绑」：动作与服务端完全一致，
		// 只是运营看到的话术应当说明「这会改掉现有绑定」。
		row["BindLabel"] = "换绑"
	}
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

// siteSlotRedirect 回列表页并把结论经查询参数回显（成功 ?ok=、失败 ?err=）。
//
// 只带工程参数：本页没有分页与筛选，回跳就是「同一个工程的那一屏」。
func siteSlotRedirect(c *gin.Context, projectID, okText, errText string) {
	q := url.Values{}
	if strings.TrimSpace(projectID) != "" {
		q.Set("project", projectID)
	}
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	c.Redirect(http.StatusFound, "/admin/site-slots?"+q.Encode())
}

// siteSlotFacingError 把 page 模块的错误转成可展示文案。
//
// 命中白名单的（本页知道怎么解释的业务错误）返回中文原文，其余一律落到统一提示：
// 未命中的通常是数据库错误的 Error()，带表名甚至 SQL 片段，那是给运维看的。
func siteSlotFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := siteSlotFacingText(err.Error()); msg != "" {
		return msg
	}
	// 未命中：原文只进日志（场景 + user_id + 原始错误），对外给归口文案。
	// 少了这一条，未归类的失败在日志与页面上**同时消失** —— 页面看不到、日志也查不到。
	logger.Scene(pageErrScene).
		With("user_id", shell.CurrentUserID(c)).
		Error(err, "站点槽位页操作失败（非业务错误，只对外给归口文案）")
	return siteSlotInternalText(c)
}

// siteSlotFacingValues siteSlotFacingMessages 的**值集合**（由上面那张 map 派生，不手抄一份）。
//
// 用途见 siteSlotFacingText：写侧回填 ?err= 时有「常量名」与「中文译文」两种形态，
// 后者是 map 的值而不是键。派生而不是手写，是为了让「加一条白名单」只改一个地方 ——
// 手抄两份的下场是新增文案只加进 map、值集合忘了加，于是那条提示又被自己吞掉。
var siteSlotFacingValues = func() map[string]bool {
	m := make(map[string]bool, len(siteSlotFacingMessages))
	for _, v := range siteSlotFacingMessages {
		m[v] = true
	}
	return m
}()

// siteSlotFacingText 白名单校验：命中返回可展示文案，未命中返回空串。
//
// **键与值两种形态都要认**（原来的实现只认键，是个真缺陷）：
//
//	· 写侧把 page 模块错误常量放进 ?err= 时，串是常量的**值**（= 常量名，如 "ErrSlotPageMiss"）
//	  → 命中 map 的键，取其中文译文；
//	· 写侧把 siteSlotFacingError 已经转好的**中文文案**放进 ?err= 时，串是 map 的**值**。
//
// 只认键会让第二种一律落空，于是该页**所有业务错误都显示归口文案**「系统内部错误，请稍后重试」——
// 文案明明准备好了却永远不出现，运营看到的是「系统出错了，重试吧」，而实际是「请先选页面」。
//
// 值形态必须**整体相等**才算命中，不能用 Contains：否则任何人手拼一个 ?err=、
// 只要串里包含某条已知文案就能绕过白名单，把任意前缀 / 后缀显示到页面上。
func siteSlotFacingText(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	if msg, ok := siteSlotFacingMessages[key]; ok {
		return msg
	}
	if siteSlotFacingValues[key] {
		// 已经是白名单里的成品文案：原样放行（返回 key 即该文案本身）。
		return key
	}
	return ""
}

// siteSlotQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，未命中时用 fallback
// （错误提示落统一文案，成功提示落空串）—— 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func siteSlotQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := siteSlotFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// siteSlotInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func siteSlotInternalText(c *gin.Context) string {
	return shell.PageInternalText(c)
}

// siteSlotPublicURL 站点内逻辑路径 → 浏览器可打开的访问面地址。
//
// active_path 理论上恒带前导 "/"，这里仍做一次归一：拼出 "/sitecheckout"
// 这种地址的错法是静默的（链接能渲染、点了才 404），不值得押注上游格式。
func siteSlotPublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return siteSlotSitePrefix + "/" + strings.TrimPrefix(p, "/")
}

// siteSlotTextOrEmpty 空值统一显示成「—」。
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
