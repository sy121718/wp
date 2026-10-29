package navigationhttp

// 自 dashboard 迁回本模块。与后台权限菜单（admin/menus）严格隔离：本页管理的是
// 公开站点导航（navigations 表），构建期由 core.nav 组件绑定位置后编译进静态产物。
// 交互遵循后台规范：GET 渲染整页，写操作按承载形态分两档：
//   · 仍在页面这一侧的写动作（update / move / panel / panel-create）走
//     POST + shell.RenderJump 整页提示（成功 1 秒后自动回列表）；
//   · 新建、来源批量加入与删除（单个、批量）已迁到真实 JSON 端点
//     （POST /api/navigation/create：新建档 / action=addSource 档，见
//     navigation_add_source_handle.go；POST /api/navigation/delete，见 navigation_delete_handle.go）：
//     页面只把端点与文案写进
//     data-write-*，请求由基座直接发出，成功整页刷新 —— 不再有无 JS 的页面 POST 兜底，
//     相应的按钮因此是 [data-requires-js] hidden。
// 迁移的理由与 block / 词条页同源：页面 POST 会为同一个动作维护第二条（只服务页面的）
// 契约，而前端早已能直调同一份 JSON。

// 面板内容存**全局块**（一处改、多处复用），展示形态存菜单项（同一个块可被多项复用）。
// 两个写动作刻意分开，不在一次请求里做两处持久化：
//   · PanelSet：把已有块挂到菜单项 / 清除面板（单条 navigation 更新）；
//   · PanelCreate：只建块并跳块编辑器（单条 block 写入）。
// 「新建块并自动挂上」需要跨模块两次写（block 建 + navigation 改），按硬规则必须同事务
// 透传，而两个模块当前都没有可用的 Tx 变体 —— 本批不提供这个组合动作：先建块（跳编辑器
// 设计面板），保存后回列表在面板下拉里选它。两步各自都是单写，不需要跨模块事务。

// 自 dashboard 迁回本模块。
//
// 为什么导航标签需要**自己**的维护入口：它的 label 不在页面文档里，而在 navigation
// 模块的节点上 —— 页面翻译工作台看不到它（Props 里根本没有这个值），组件侧声明的
// Translatable 白名单对它也无效。构建期由 pipeline.NavigationAdapter 用
// navigation.label 语境回填译文（I18N-018 的定案），本页是那个语境的唯一维护入口。
//
// 两项与其它工作台不同的地方：
//
//	· 语境恒为 navigation.label，不需要从字段推导 —— 菜单项只有一个可见文本；
//	· 节点按位置（header / footer）组织，标题取自节点自身（与菜单管理页同一视图）。

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
)

// navEditEcho is the submitted edit state, separate from the current database row.
type navEditEcho struct {
	ID, ProjectID, Kind, Title, Path, Target, UpdatedAt string
	PanelBlockID, PanelWidth                            string
}

func (h *navigationPageHandle) NavigationEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("id"))
	projectID := strings.TrimSpace(c.Query("project"))
	kind := strings.TrimSpace(c.Query("kind"))
	if id == "" || projectID == "" || kind != normalizeNavKind(kind) {
		c.String(http.StatusBadRequest, "%s", adminui.MsgText(c, navInvalidParamMsg))
		return
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: id})
	if err != nil {
		if err.Error() == navigationenums.ErrNotFound {
			c.String(http.StatusNotFound, "%s", adminui.MsgText(c, navigationErrMsg(c, err)))
			return
		}
		logger.Scene("page").With("id", id).Error(err, "加载导航编辑片段失败")
		c.String(http.StatusInternalServerError, "%s", adminui.PageInternalText(c))
		return
	}
	if item == nil || item.ProjectID != projectID || item.Kind != kind {
		c.String(http.StatusNotFound, "%s", adminui.MsgText(c, navNoticeItemMissing))
		return
	}
	panelID := ""
	if item.PanelBlockID != nil {
		panelID = *item.PanelBlockID
	}
	h.renderNavigationEdit(c, navEditEcho{
		ID: item.ID, ProjectID: projectID, Kind: kind, Title: item.Title,
		Path: item.Path, Target: item.Target, UpdatedAt: item.UpdatedAt,
		PanelBlockID: panelID, PanelWidth: item.PanelWidth,
	}, navText{})
}

func (h *navigationPageHandle) renderNavigationEdit(c *gin.Context, echo navEditEcho, errMsg navText) {
	c.Header("Cache-Control", "no-store")
	blocks := make([]panelBlockOption, 0)
	panelAvail := h.blocks != nil
	if panelAvail {
		list, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: echo.ProjectID})
		if err != nil {
			logger.Scene("page").With("project", echo.ProjectID).Error(err, "加载导航面板块候选失败")
			panelAvail = false
		} else {
			for _, b := range list {
				blocks = append(blocks, panelBlockOption{ID: b.ID, Name: b.Name})
			}
		}
	}
	// NavEditError 是**片段槽位**：navigation_edit_form.html 直接渲染这段文本
	//（`{{if .NavEditError != ""}}…{{.NavEditError}}`），不取词 —— 所以这里必须取成文本。
	// 「这个槽位还没取词化」已登记在报告里；取词入口是共享的 adminui.MsgText，
	// 不是 shell.TranslateFor。
	// 片段出口的视图（强类型：字段名即模板契约）。
	data := &navigationEditFormView{
		NavEditEcho:        echo,
		NavEditError:       adminui.MsgText(c, errMsg),
		NavEditPanelAvail:  panelAvail,
		NavEditPanelBlocks: blocks,
	}
	// 片段出口没有页壳：t / tf 由路由的 BareSlot 注入，csrf_token 由视图字段给
	// （模板要它来拼写请求的 X-CSRF-Token 头）。
	//
	// 从前这里手写 data["csrf_token"]，那是「页壳关切混进视图」的旧形态 ——
	// 换成显式字段后，"这一屏要不要 csrf" 在类型上看得见。
	token, err := builtin.GetCSRFToken(c)
	if err != nil || token == "" {
		logger.Scene("page").With("id", echo.ID).Error(err, "加载导航编辑 CSRF token 失败")
		c.String(http.StatusInternalServerError, "%s", adminui.PageInternalText(c))
		return
	}
	data.CSRFToken = token
	pageview.Render(c, data)
}

func navEditHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

func (h *navigationPageHandle) navEditFailure(c *gin.Context, err error, echo navEditEcho) {
	if !navEditHTMX(c) {
		// 原生提交：整页提示（结论走响应体，不再是 303 + ?err=）。
		navJumpListMenu(c, false, navigationErrMsg(c, err))
		return
	}
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		// 回填上下文不完整，抽屉无法重渲：整页跳回列表（htmx 分档看不到文案，与现状一致）。
		c.Header("HX-Redirect", navListBack(c))
		c.Status(http.StatusOK)
		return
	}
	// Empty submitted values are intentional; a stale database row must never
	// overwrite user input after a failed optimistic-lock update.
	h.renderNavigationEdit(c, echo, navigationErrMsg(c, err))
}

// navEditOwnedRow checks the submitted context before a write addressed by id alone.
func (h *navigationPageHandle) navEditOwnedRow(c *gin.Context, echo navEditEcho) bool {
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		c.String(http.StatusBadRequest, "%s", adminui.MsgText(c, navInvalidParamMsg))
		return false
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: echo.ID})
	if err != nil && err.Error() != navigationenums.ErrNotFound {
		logger.Scene("page").With("id", echo.ID).Error(err, "核对导航编辑归属失败")
		c.String(http.StatusInternalServerError, "%s", adminui.PageInternalText(c))
		return false
	}
	if item == nil || item.ProjectID != echo.ProjectID || item.Kind != echo.Kind {
		c.String(http.StatusNotFound, "%s", adminui.MsgText(c, navNoticeItemMissing))
		return false
	}
	return true
}

// 页面标题（i18n key，与原 dashboard 枚举同值）。
//
// 参数级提示不再用 MsgFieldRequired 这个通用 key 直出（原先 c.String(400, key) 把 key
// 本身当响应体）：统一走 navigation_error_handle.go 的 navInvalidParamText / navNoticeNoSourcePicked。
const (
	navigationsPageTitle = "MsgNavigationsTitle"
)

// 导航位置（与 navigation 模块 kind 白名单对齐）。
const (
	navKindHeader       = "header"
	navKindHeaderMobile = "header_mobile"
	navKindFooter       = "footer"
	navKindFooterMobile = "footer_mobile"
)

// navigationPageHandle 导航菜单管理页处理器。
type navigationPageHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService
	// blocks 面板所需的块能力（超级菜单，迁移 285）：未注入时面板入口降级为不可用。
	blocks BlockPanelPort
}

// NewNavigationPageHandle 创建导航菜单管理页处理器。
func NewNavigationPageHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationPageHandle {
	return &navigationPageHandle{navigations: navigations, projects: projects}
}

// navMenuRow 菜单结构行：展平树 + 层级 + 同级首末标记（控制上移/下移按钮禁用态）。
//
// 行上只放**事实**（标题 / 链接 / 层级 / 打开方式取值 / 同级首末），不放展示加工：
// · 打开方式的文案由模板按 Target 取词 —— 与「添加菜单项」抽屉的 target 下拉同一批 key；
// · 层级缩进由模板把 Depth 塞进 CSS 变量 `--tree-depth`、CSS 的 `.tree-cell` 算间距。
// Go 侧既不产 px 也不产标签（判据见 AGENTS.md：语义归 Go、
// 取词与外观归模板/CSS）。
type navMenuRow struct {
	ID    string
	Title string
	Path  string
	// Depth 层级（0 = 顶级）：语义值，模板只把它写进 CSS 变量。
	Depth int
	// Target 打开方式取值 self / blank；未知取值由模板按 self 显示（与下拉默认项一致）。
	Target     string
	SourceType string
	First      bool
	Last       bool
	// PanelBlockID / PanelWidth 悬浮面板（超级菜单，迁移 285）：内容存块、展示存菜单项。
	PanelBlockID string
	PanelWidth   string
	// UpdatedAt 乐观锁 token（精确到微秒的 RFC3339 串）：编辑抽屉原样回带，
	// 服务端据此判断「打开抽屉之后这一项有没有被别处改过」。
	UpdatedAt string
}

// navSourceGroup 来源候选分组（页面渲染用）：在 contract 的 SourceGroup 上补一个
// 「本组是否至少有一项可加入」。
//
// 为什么在页面侧派生而不是加进 contract：它是**展示判据**（按钮禁用态），跨模块契约
// 不该为了一个后台按钮多一个字段。判据只有一份 —— 候选 URL 非空才可加入，与 service
// 「无公开路径的候选不提供添加」同源（见 navigation_page_handle.go 的 NavigationAddSource）。
//
// 为什么要有它：逐项 disabled 只挡住单个复选框，整组都不可用时按钮仍可点，用户提交后
// 才拿到「请至少勾选一项要加入菜单的内容」—— 而 33 个项目里一个都勾不动，那句提示
// 只会让人更困惑。按钮在源头置灰，配合每项的「（暂无公开路径，先发布后再添加）」说明。
type navSourceGroup struct {
	// Type 来源类型（page / article / product / category）：分组的**事实**。
	// 分组标题由模板按 Title 取词（与 enums 的 Source* 是同一批 key）——
	// contract 的 SourceGroup.Title 只承载 key（适配器在 service 层之下、拿不到请求语言），
	// 取词这一步落在展示层，但不在 Go 侧预制成品文案。
	Type string
	// Title 分组标题的 i18n key，从 contract 原样透传（不是文案本身）。
	// 模板对四种已知来源按类型取词并自带中文兜底，**未登记的类型**用这里的 key 兜住：
	// 新来源类型不改模板也能拿到已翻好的译文，而不是永远显示类型名。
	Title string
	Items []navigationcontract.SourceCandidate
	// Usable 本组至少有一项可加入（URL 非空）。
	Usable bool
}

// panelBlockOption 面板块下拉选项（超菜单面板选择）。
//
// 只服务编辑抽屉（navigation_edit_form.html 的 NavEditPanelBlocks）：列表页没有面板列，
// 不在页面数据里预列一份块清单（那会为一份从不渲染的下拉做一次跨模块查询）。
type panelBlockOption struct {
	ID   string
	Name string
}

// navigationsPageData 导航菜单管理页数据。
type navigationsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Kind            string
	Rows            []navMenuRow
	// ParentOptions 可作为父级的项（两级上限：仅顶级项可选）。
	ParentOptions []navMenuRow
	// SourceGroups 可加入菜单的来源候选（页面/文章/产品/分类，按来源分组）。
	SourceGroups []navSourceGroup
	// MenuID 要自动展开编辑抽屉的菜单项 id（?menu=）。
	//
	// 用途：面板块「新建并编辑」跳到工作台，块保存后带着这个参数回到菜单编辑器 ——
	// 用户回来时抽屉已经开着，能看到刚挂上面板的那一项（否则要在几十行里自己找）。
	// 落在列表里的项才生效；非列表值不渲染任何东西（它也从不进 HTML，只用作行比对）。
	MenuID string
	// LoadError 取数失败时的归口文案（当前语言）。非空时模板渲染错误槽、不渲染列表 ——
	// 但页壳 / 菜单 / 权限上下文照旧（失败仍是页面，不是 JSON 报文）。
	//
	// 与「还没有站点工程」（模板的 len(.Projects) == 0 分支）是两件事：那个是**读到了**、
	// 工程列表为空；这个是**没读到**。判据先判本字段，否则运营会以为工程被删了。
	LoadError string
}

// NavigationsPage 导航菜单管理页。
func (h *navigationPageHandle) NavigationsPage(c *gin.Context) {
	data, err := h.buildNavigationsData(c)
	if err != nil {
		// 取数失败仍是**页面请求**：出口是页面，不是 JSON 报文。
		//
		// 此前这里是 response.ErrorWithMessage(c, 500, adminui.MsgInternalError)：浏览器收到
		// `{"code":500,…}`，DOM 里没有标题栏与左侧菜单，地址栏停在 /admin/navigations，
		// 用户只能手改地址栏回去 —— 与「参数级失败返回 JSON」是同一类缺陷
		//（navigation_error_handle.go 的文件头记过那一类）。
		//
		// 现在渲染**同一个列表模板** + 可选错误槽（.LoadError）：页壳 / 菜单 / 权限上下文
		// 照旧，状态码仍是 500 —— 页面确实没取到数据，200 会让失败看起来像成功、监控也
		// 看不见。no-store：失败页不该被缓存，重试必须真的回到服务端。
		// 与 page.RedirectPage 的取数失败同形（同页模板 + 空数据 + 归口文案 + 500）。
		//
		// 零值数据只用于这条路径：工程 / 位置筛选一律为空，模板据 .LoadError 先判，
		// 渲染错误槽而不是「还没有站点工程」空态。
		//
		// 详情只进日志：文案由 navigationErrMsg 归口（命中白名单给业务文案，
		// 未命中给归口事实）；这里取成文本是因为 LoadError 槽位直接渲染
		//（navigations.html 的 `{{loadError}}`，不取词）—— 「这个槽位还没取词化」
		// 已登记在报告里。
		logger.Scene("page").With("path", c.Request.URL.Path).Error(err, "加载导航菜单页失败")
		// 失败一律走 JSON 错误体（pageview.Fail）：页面 GET 失败不渲染页壳。
		// 从前这里渲染「完整页壳 + 500」的失败档 —— 同一张模板要维护两套必需键，
		// 而失败时页壳给的信息（侧栏、菜单树）没有一条是这次失败需要的：
		// 菜单树本来就是这次没读到的东西（用户裁定：所有提示只需要一个提示）。
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, data)
}

// buildNavigationsData 组装页面数据（工程/位置筛选 + 菜单树展平）。
func (h *navigationPageHandle) buildNavigationsData(c *gin.Context) (*navigationsPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	kind := normalizeNavKind(c.Query("kind"))
	rows := make([]navMenuRow, 0, 8)
	var groups []navigationcontract.SourceGroup
	if selected != "" {
		nodes, terr := h.navigations.Tree(ctx, selected, kind)
		if terr != nil {
			return nil, terr
		}
		flattenNavRows(nodes, 0, &rows)
		// 来源候选：页面/文章/产品/分类（依赖模块不可用时对应分组为空）。
		if groups, terr = h.navigations.SourceGroups(ctx, selected); terr != nil {
			logger.Scene("page").With("project", selected).Warn("导航来源候选加载失败，管理页仅显示自定义链接")
		}
	}
	// 视图模型：把「本组是否有可加入项」一次算清（模板做不了遍历统计）。
	// 分组标题不在这里取词 —— 模板按 group.Type 取，key 与 enums 的 Source* 同源。
	sourceGroups := make([]navSourceGroup, 0, len(groups))
	for _, g := range groups {
		usable := false
		for _, it := range g.Items {
			if strings.TrimSpace(it.URL) != "" {
				usable = true
				break
			}
		}
		sourceGroups = append(sourceGroups, navSourceGroup{Type: g.Type, Title: g.Title, Items: g.Items, Usable: usable})
	}
	// 面板块下拉不在本页：悬浮面板的挂载（PanelSet / PanelCreate）与块清单都在编辑抽屉里，
	// renderNavigationEdit 自己列块。列表页不为一份从不渲染的下拉做一次跨模块查询。
	parents := make([]navMenuRow, 0, len(rows))
	for _, r := range rows {
		if r.Depth == 0 {
			parents = append(parents, r)
		}
	}
	return &navigationsPageData{
		Title: navigationsPageTitle, Menu: "navigations",
		Projects: projects, SelectedProject: selected, Kind: kind,
		Rows: rows, ParentOptions: parents, SourceGroups: sourceGroups,
		// 操作结论不再经 query 回带（PRG）：写动作的结论由 shell.RenderJump 渲染成
		// 整页提示（见 navigation_page_handle.go），列表页不再从 query 读任何结论。
		// 面板块保存后回菜单编辑器时要把对应那一项的抽屉重新打开（?menu=）。
		MenuID: normalizeMenuFocus(c.Query("menu")),
	}, nil
}

// normalizeMenuFocus 收敛 ?menu= 的菜单项 id：只认 uuid 形状（本表主键是 uuid），
// 其余一律空串。它只参与「哪一行自动展开抽屉」的行比对、从不进 HTML；
// 收死形状是为了将来有人把它渲染出去时也是安全的（查询参数不是可信边界）。
func normalizeMenuFocus(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || len(v) > 64 {
		return ""
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return ""
		}
	}
	return v
}

// flattenNavRows 深度优先展平菜单树（同级首末标记用于按钮禁用态）。
//
// 只搬运事实：层级、打开方式取值、链接。标签与缩进由模板/CSS 产出 ——
// 这里不做目标文案映射（模板按 Target 取词）、不算缩进像素（模板把 Depth 写进
// `--tree-depth`，CSS 的 .tree-cell 算间距）。
func flattenNavRows(nodes []*navigationdto.NavigationNode, depth int, out *[]navMenuRow) {
	for i, n := range nodes {
		*out = append(*out, navMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Depth: depth,
			Target: n.Target, SourceType: n.SourceType,
			First: i == 0, Last: i == len(nodes)-1,
			PanelBlockID: panelBlockIDOf(n), PanelWidth: n.PanelWidth,
			UpdatedAt: n.UpdatedAt,
		})
		flattenNavRows(n.Children, depth+1, out)
	}
}

// panelBlockIDOf 读取菜单项的面板块 id（nil 安全）。
func panelBlockIDOf(n *navigationdto.NavigationNode) string {
	if n == nil || n.PanelBlockID == nil {
		return ""
	}
	return strings.TrimSpace(*n.PanelBlockID)
}

// normalizeNavKind 位置参数规范化（非法值回落 header）。
func normalizeNavKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile:
		return strings.TrimSpace(kind)
	}
	return navKindHeader
}

// navListURL / navListURLWith / navListURLMenu 三个「列表页回跳地址构造点」已删除：
// 回跳地址改由 shell.BackPath 从**表单 action 的 query** 按白名单读回（见 navigation_page_handle.go
// 的 navListBack / navListBackMenu），结论文案不再进 URL。菜单项的 `?menu=` 也走同一条读回链。

// NavigationAddSource 已退役：来源批量加入菜单迁到 POST /api/navigation/create 的
// action=addSource 档（见 navigation_add_source_handle.go）—— 页面只留 data-write-* 声明，
// 请求由基座直接发出。这条页面 POST 曾借 /api/navigation/create 的权限对象，
// 退役后「加一个菜单项」只剩一个接口出口，不会出现「能加普通的、加不了来源的」两套锚点。

// NavigationUpdate 更新菜单项（标题/链接/打开方式，空字段保持不变）。
func (h *navigationPageHandle) NavigationUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	// 「冲突 / 失败后仍要展开的那一项」（?menu=）随表单 action 的 query 回带，
	// 回跳地址由 navListBackMenu 从**本次请求**读回 —— 不再从 POST 隐藏域解析。
	echo := navEditEcho{
		ID: id, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind")),
		Title: c.PostForm("title"), Path: c.PostForm("path"),
		Target: c.PostForm("target"), UpdatedAt: c.PostForm("expectedUpdatedAt"),
		PanelBlockID: c.PostForm("panelBlockId"), PanelWidth: c.PostForm("panelWidth"),
	}
	if id == "" {
		if navEditHTMX(c) {
			h.renderNavigationEdit(c, echo, navInvalidParamMsg)
		} else {
			navJumpListMenu(c, false, navInvalidParamMsg)
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", adminui.MsgText(c, navInvalidParamMsg))
		return
	}
	if !h.navEditOwnedRow(c, echo) {
		return
	}
	req := &navigationdto.UpdateReq{ID: id}
	if v := strings.TrimSpace(c.PostForm("title")); v != "" {
		req.Title = &v
	}
	if v := strings.TrimSpace(c.PostForm("path")); v != "" {
		req.Path = &v
	}
	if v := strings.TrimSpace(c.PostForm("target")); v != "" {
		req.Target = &v
	}
	// 乐观锁：编辑抽屉把打开时的 update_time 原样回带（见 navMenuRow.UpdatedAt）。
	if v := strings.TrimSpace(c.PostForm("expectedUpdatedAt")); v != "" {
		req.ExpectedUpdatedAt = &v
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {
		// 业务文案必须回到页面上：冲突（已被别处改过）与「路径被占用」这类结论
		// 都要让操作者知道该刷新重做还是改字段 —— 通用提示会把这些区别全吞掉。
		logger.Scene("page").With("id", id).Error(err, "更新导航项失败")
		h.navEditFailure(c, err, echo)
		return
	}
	navJumpList(c, true, navDoneNotice)
}

// 同级上移 / 下移原本是本文件的 NavigationMove（连调两次 Update，历史 P1 半交换）：
// 原子交换在 task-84 落进 service.SwapMove，页面 POST 在 task-91 迁到真实 JSON 端点
// （POST /api/navigation/update，action="move"，见 navigation_move_handle.go）——
// 这里不再有它的 handler。

// errPanelUnavailable 块能力未装配（装配缺陷；对外只出通用文案）。
var errPanelUnavailable = errors.New("面板块能力未装配")

// BlockPanelPort 面板所需的块能力（消费者侧最窄接口：只列与建）。
type BlockPanelPort interface {
	List(ctx context.Context, req *blockcontract.ListReq) ([]blockcontract.BlockResp, error)
	Create(ctx context.Context, req *blockcontract.CreateReq) (*blockcontract.BlockResp, error)
}

// SetBlockPanelPort 注入块能力（装配期调用；未注入时面板入口整体降级为不可用）。
func (h *navigationPageHandle) SetBlockPanelPort(p BlockPanelPort) {
	if h == nil {
		return
	}
	h.blocks = p
}

// PanelSet POST /admin/navigations/panel：把块挂到菜单项（或清除面板）。
func (h *navigationPageHandle) PanelSet(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := strings.TrimSpace(c.PostForm("id"))
	// 「回跳后仍要展开的那一项」（?menu=）随表单 action 的 query 回带，
	// 回跳地址由 navListBackMenu 从本次请求读回 —— 不再从 POST 隐藏域解析。
	echo := navEditEcho{
		ID: id, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind")),
		PanelBlockID: c.PostForm("panelBlockId"), PanelWidth: c.PostForm("panelWidth"),
		UpdatedAt: c.PostForm("expectedUpdatedAt"),
		Title:     c.PostForm("title"), Path: c.PostForm("path"), Target: c.PostForm("target"),
	}
	if id == "" {
		if navEditHTMX(c) {
			h.renderNavigationEdit(c, echo, navInvalidParamMsg)
		} else {
			navJumpListMenu(c, false, navInvalidParamMsg)
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", adminui.MsgText(c, navInvalidParamMsg))
		return
	}
	if !h.navEditOwnedRow(c, echo) {
		return
	}
	// 空串 = 清除面板（服务层把空块 id 归一成 NULL；nil 才是「不改动」）。
	blockID := strings.TrimSpace(c.PostForm("panelBlockId"))
	req := &navigationdto.UpdateReq{ID: id, PanelBlockID: &blockID}
	if w := strings.TrimSpace(c.PostForm("panelWidth")); w != "" {
		req.PanelWidth = &w
	}
	// 乐观锁：保存面板同样是一次 navigation 更新，冲突语义与编辑菜单项一致。
	if v := strings.TrimSpace(c.PostForm("expectedUpdatedAt")); v != "" {
		req.ExpectedUpdatedAt = &v
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("id", id).Error(err, "设置菜单悬浮面板失败")
		h.navEditFailure(c, err, echo)
		return
	}
	navJumpListMenu(c, true, navDoneNotice)
}

// PanelCreate POST /admin/navigations/panel/create：新建面板块并跳块编辑器。
//
// 命名带位置与项名（如「页眉菜单·产品」）：面板块在块列表里与其它全局块混排，
// 不带来源信息的名字过几天就没人知道它是干什么的、能不能删。
func (h *navigationPageHandle) PanelCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))
	// 菜单项 id：块保存后要带着它回到菜单编辑器并重新展开这一项。
	menuID := strings.TrimSpace(c.PostForm("id"))
	if !h.navEditOwnedRow(c, navEditEcho{ID: menuID, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind"))}) {
		return
	}
	if h.blocks == nil {
		logger.Scene("page").With("project", projectID).Error(errPanelUnavailable, "新建面板块失败：块能力未装配")
		navJumpListMenu(c, false, adminui.InternalMsg())
		return
	}
	name := panelBlockName(navTr(c), kind, title)
	created, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: "block",
	})
	if err != nil {
		logger.Scene("page").With("project_id", projectID).Error(err, "新建面板块失败")
		navJumpListMenu(c, false, navigationErrMsg(c, err))
		return
	}
	// 跳块编辑器：把面板结构画出来。
	//
	// returnUrl：块保存后回到本页**并重新展开这一项**（?menu=<id>）。回跳目标由服务端
	// 从本次请求的 query 构造、消费侧（块保存路径）再校验一次（只接受站内相对路径）。
	target := "/workbench?block=" + url.QueryEscape(created.ID)
	if back := navListBackMenu(c); back != "" {
		target += "&returnUrl=" + url.QueryEscape(back)
	}
	// 成功提示页 1 秒后自动跳到块编辑器（BackText 留空：这里没有一句现成的「回哪里」）。
	navJump(c, true, navDoneNotice, target, navText{})
}

// panelBlockName 面板块的默认名（可辨认来源）。
//
// **这个名字会落库**（blockcontract.CreateReq.Name）：它是块的初始名、用户随后可以在
// 块编辑器里改。取词只影响「新建那一刻用哪种语言拼出这个名字」，落库之后就是普通用户数据、
// 不再随后台语言变化 —— 彻底的解法是给 Block 表加「默认名 key」字段、展示时按 key 取词，
// 那要动 block 模块的 contract/model/迁移与全局块列表页，超出本批范围（见报告）。
//
// 位置词用当前请求语言取词：英文后台里新建的块名至少与界面语言一致，
// 而不是让一张中文表格里突然出现一个纯中文名。
func panelBlockName(tr func(key, fallback string) string, kind, title string) string {
	pos := tr(navigationenums.PanelBlockNameMenu, "菜单")
	switch kind {
	case "header":
		pos = tr(navigationenums.PanelBlockNameHeader, "页眉菜单")
	case "header_mobile":
		pos = tr(navigationenums.PanelBlockNameHeaderMobile, "页眉移动菜单")
	case "footer":
		pos = tr(navigationenums.PanelBlockNameFooter, "页脚菜单")
	case "footer_mobile":
		pos = tr(navigationenums.PanelBlockNameFooterMobile, "页脚移动菜单")
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = tr(navigationenums.PanelBlockNamePanel, "面板")
	}
	return pos + "·" + t
}

// translationLangOption 工作台语言下拉项（跨包引私有符号不成立，各页面包各自持有一份）。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

// navigationTranslationRow 一个菜单项的标签翻译行。
type navigationTranslationRow struct {
	Context    string
	NodeID     string
	Kind       string
	Path       string
	Source     string
	SourceHash string
	Target     string
	Translated bool
}

// navigationTranslationGroup 按位置（header / footer）分组。
type navigationTranslationGroup struct {
	Kind  string
	Title string
	Rows  []navigationTranslationRow
}

// navigationTranslationHandle 导航译文工作台。
type navigationTranslationHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService
	writer      *i18n.ContentWriter
	// pages 译文变更后标记手工页面待重建（与商品 / 文章工作台同一动作）。
	// 菜单文字出现在**每个页面**上，所以这里没有「精确到某页」的选项 ——
	// 按 i18n:content 依赖条目做全站标记，与页面翻译工作台同一链路。
	pages navTranslationPageMarker
	// menus 译文变更后派发导航依赖失效（自动发布实例侧）。
	//
	// 只标页面是不够的：菜单文字同样烘在 presentation 实例的产物里，那些实例
	// 没有 i18n:content 全站标记的落点，会永远停在旧标签上（与刚修好的 menu 依赖
	// 缺口同源）。派发口径与导航项增删改完全一致（menu:{projectID}:{kind}），
	// 这里只按位置发起，键构造与扇出都在发布内核，不自创第三套。
	menus navTranslationMenuInvalidator
}

// navTranslationPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这条）。
type navTranslationPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// navTranslationMenuInvalidator 导航依赖的最小失效端口（消费者侧定义）。
//
// 实现方是 navigation 模块自己的 Service —— 它持有 SetMenuStaleDispatcher 注入的
// 派发端口，所以「改菜单项」与「改菜单文字」走的是同一段失效逻辑、同一条装配链。
type navTranslationMenuInvalidator interface {
	InvalidateMenuLabels(ctx context.Context, projectID string, kinds []string)
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *navigationTranslationHandle) SetPageMarker(m navTranslationPageMarker) { h.pages = m }

// SetMenuInvalidator 注入导航依赖失效端口（装配期）。
func (h *navigationTranslationHandle) SetMenuInvalidator(m navTranslationMenuInvalidator) {
	h.menus = m
}

// NewNavigationTranslationHandle 构造。
func NewNavigationTranslationHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationTranslationHandle {
	return &navigationTranslationHandle{navigations: navigations, projects: projects}
}

// SetContentWriter 注入译文写入端口（装配期）。
func (h *navigationTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

// navigationTranslationContext 菜单标签的固定语境（与构建期 resolver 逐字一致）。
func navigationTranslationContext() string {
	return i18n.ContentContext("navigation", "label")
}

// NavigationTranslations GET /admin/navigations/translations。
func (h *navigationTranslationHandle) NavigationTranslations(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	lang := strings.TrimSpace(c.Query("lang"))
	data := h.build(c.Request.Context(), projectID, lang, navTr(c))
	data.filter(strings.TrimSpace(c.Query("keyword")))
	pageview.Render(c, data)
}

// 译文保存已迁到真实 JSON 写接口 POST /api/navigation/update（action="translations"，见
// navigation_translations_handle.go）：页面只发一次 JSON 请求，成功由前端跳回带筛选的 GET。
// 页面侧的写 handler（整表 form POST）与它的提示页（shell.RenderJump）随之退役。

// navigationTranslationsData 页面数据。
type navigationTranslationsData struct {
	Title        string
	Menu         string
	ProjectID    string
	Keyword      string
	NoProject    bool
	HasData      bool
	VisibleCount int
	Lang         string
	Langs        []translationLangOption
	Groups       []navigationTranslationGroup
	RowCount     int
	Done         int
	Saved        bool
	SavedNote    string
	Errors       []string
	// SuccessURL 保存成功后的回跳地址：由 Go 侧拼好并转义。
	//
	// 为什么是字段而不是模板里拼：keyword 的转义规则只该有一份，
	// 模板自己拼 query 就会出现第二份转义规则。
	SuccessURL string
	// kindsByHash 本次渲染中「原文哈希 → 出现过该文字的全部菜单位置」。
	//
	// 与 Groups 的差别：Groups 按原文去重（同一段文字在页眉与页脚都出现时只列一行，
	// 因为译文按 (原文, 语境) 寻址，列两行反而不知道改哪一行才算数），这份记录**不去重** ——
	// 保存译文后要按它派发每个位置的失效，只看被提交那一行会漏掉另一个位置。
	kindsByHash map[string][]string
}

// filter 在完整导航树去重后筛选展示行；kindsByHash 保留完整树以供保存后失效派发。
func (d *navigationTranslationsData) filter(keyword string) {
	d.Keyword = keyword
	d.VisibleCount = d.RowCount
	if keyword == "" {
		return
	}
	needle := strings.ToLower(keyword)
	for i := range d.Groups {
		rows := d.Groups[i].Rows
		selected := make([]navigationTranslationRow, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Source+" "+row.Path), needle) {
				selected = append(selected, row)
			}
		}
		d.Groups[i].Rows = selected
		d.VisibleCount -= len(rows) - len(selected)
	}
	// RowCount/Done/HasData 均描述本次完整加载结果，空态据此识别筛选无匹配。
}

// noteHashKind 记下「某个原文出现在某个菜单位置」（不受列表去重影响）。
func (d *navigationTranslationsData) noteHashKind(hash, kind string) {
	if d == nil || hash == "" || kind == "" {
		return
	}
	if d.kindsByHash == nil {
		d.kindsByHash = map[string][]string{}
	}
	for _, k := range d.kindsByHash[hash] {
		if k == kind {
			return
		}
	}
	d.kindsByHash[hash] = append(d.kindsByHash[hash], kind)
}

// kindsForHashes 本次变更的译文覆盖了哪些菜单位置（失效派发用）。
//
// 按原文哈希回查而不是取被提交那一行的 Kind：跨位置的同名文字只列一行，
// 提交的行只带其中一个位置 —— 照它派发会漏掉另一个位置，表现为「页脚实例的菜单标签
// 没被重建」（与 NAV 批的失效不完全同源，但同样是静默不收敛）。
// 回查不到时（理论上不会：能提交的行一定来自 build 出来的行）回落被提交行的位置：
// 宁可只标已知的位置，也不静默不标。
func (d *navigationTranslationsData) kindsForHashes(hashes map[string]bool, fallback []string) []string {
	if d == nil || len(hashes) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for h := range hashes {
		for _, kind := range d.kindsByHash[h] {
			if strings.TrimSpace(kind) == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			out = append(out, kind)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

// sourceOf 按原文哈希反查本次提交对应的菜单项（防表单被裁剪）。
func (d *navigationTranslationsData) sourceOf(hash string) (navigationTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.SourceHash == hash {
				return r, true
			}
		}
	}
	return navigationTranslationRow{}, false
}

// scopeProjectID 失效派发用的工程作用域：以 build 解析出的为准。
//
// build 在表单没带 project 时会回落到第一个工程（与菜单管理页同一默认规则），
// 而表单变量仍是空串 —— 拿空串去打 menu:{pid}:{kind} 只会得到一条不匹配任何依赖的键，
// 表现为「保存成功、实例侧却没有任何失效」（静默失效比报错更难查）。
func scopeProjectID(data *navigationTranslationsData, formProjectID string) string {
	if data != nil && strings.TrimSpace(data.ProjectID) != "" {
		return strings.TrimSpace(data.ProjectID)
	}
	return strings.TrimSpace(formProjectID)
}

// navigationTranslationLocation 译文工作台的回跳地址：只带筛选，不带结论文案。
func navigationTranslationLocation(projectID, lang string) string {
	return navigationTranslationFilteredLocation(projectID, lang, "")
}

func navigationTranslationFilteredLocation(projectID, lang, keyword string) string {
	q := url.Values{"lang": {lang}}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	return "/admin/navigations/translations?" + q.Encode()
}

// build 组装工作台数据：两个位置的全部菜单项 → 现有译文。
func (h *navigationTranslationHandle) build(ctx context.Context, projectID, lang string,
	tr func(key, fallback string) string) *navigationTranslationsData {
	data := &navigationTranslationsData{
		// 标题写 i18n key：adminui.Prepare 会对 "title" 取词，词条见 sys_i18n。
		Title: navigationenums.TranslationHeading, Menu: "navigation-translations",
		ProjectID: projectID, Lang: lang,
		Groups:    []navigationTranslationGroup{},
		NoProject: projectID == "" && h.projects == nil,
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if h.projects != nil {
		projects, perr := h.projects.List(ctx)
		if perr != nil {
			logger.Scene("navigation").Error(perr, "读取导航译文工程列表失败")
			data.Errors = []string{tr(navigationenums.ErrProjectListFailed, "读取工程列表失败，请稍后重试")}
			return data
		}
		data.NoProject = len(projects) == 0
		if projectID == "" && len(projects) > 0 {
			projectID = projects[0].ID
			data.ProjectID = projectID
		}
	}
	if lang == "" || h.navigations == nil {
		return data
	}
	if projectID == "" {
		return data
	}
	seen := map[string]bool{}
	hashes := make([]string, 0, 8)
	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
	}
	var refs []rowRef
	for _, kind := range []string{"header", "footer"} {
		nodes, terr := h.navigations.Tree(ctx, projectID, kind)
		if terr != nil {
			logger.Scene("navigation").With("kind", kind).Error(terr, "读取导航树失败")
			continue
		}
		group := navigationTranslationGroup{Kind: kind, Title: navKindTitle(tr, kind), Rows: []navigationTranslationRow{}}
		var walk func(list []*navigationdto.NavigationNode)
		walk = func(list []*navigationdto.NavigationNode) {
			for _, n := range list {
				if n == nil {
					continue
				}
				src := strings.TrimSpace(n.Title)
				if src != "" && i18n.ShouldTranslateContent(src) {
					h := i18n.ContentHash(src)
					// 位置记录**先于去重**：同一段文字同时出现在页眉与页脚时只列一行，
					// 但两个位置都要能被失效派发命中（见 kindsForHashes）。
					data.noteHashKind(h, kind)
					// 同一段文字在多个菜单项里出现时只保留一行（译文按原文寻址，
					// 列出多行会让「改哪一行才算数」变得不清楚）。
					if !seen[h] {
						seen[h] = true
						hashes = append(hashes, h)
						refs = append(refs, rowRef{groupIdx: len(data.Groups), rowIdx: len(group.Rows), hash: h})
						group.Rows = append(group.Rows, navigationTranslationRow{
							Context: navigationTranslationContext(), NodeID: n.ID, Kind: kind,
							Path: n.Path, Source: src, SourceHash: h,
						})
					}
				}
				walk(n.Children)
			}
		}
		walk(nodes)
		data.RowCount += len(group.Rows)
		data.Groups = append(data.Groups, group)
	}
	data.HasData = data.RowCount > 0
	data.VisibleCount = data.RowCount
	if h.writer != nil && len(hashes) > 0 {
		targets, lerr := h.writer.LoadTargets(ctx, lang, hashes)
		if lerr != nil {
			logger.Scene("navigation").With("lang", lang).Error(lerr, "读取现有导航译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, navigationTranslationContext())]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}
	// 回跳地址在 build 末尾统一算一次（依赖最终的 ProjectID / Lang / Keyword）。
	data.SuccessURL = navigationTranslationFilteredLocation(data.ProjectID, data.Lang, data.Keyword)
	return data
}

// navKindTitle 菜单位置的当前语言名（key 与导航页的 kind 下拉同源：admin.navigations.kind.*）。
func navKindTitle(tr func(key, fallback string) string, kind string) string {
	if kind == "footer" {
		return tr(navigationenums.KindFooter, "页脚导航")
	}
	return tr(navigationenums.KindHeader, "页眉导航")
}

// navigation_page_handle.go — 导航菜单页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=` / `?done=` 回列表页：那条通道要求读侧再判一次「这条提示是不是
// 本仓给的」（navigationNoticeTexts / navigationPageErr / navigationPageDone 就是那套），
// 而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除（见 navigation_error_handle.go）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词，
//     取词由提示页注入的 tf 做（见 adminui.Prepare）。事实的形状是本文件的 `navText`；
//     错误事实的判定统一在 navigation_error_handle.go 的 navigationErrMsg（pkg/facing 判据 + 归口）；
//   · 回跳地址由 backref.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。
//
// 注意这条**不适用于删除链**：行内删除与批量删除已迁到 JSON 端点
// （navigation_delete_handle.go），那里的原因是 navigationErrText 的成品句、超限是
// `key|max|count` 协议；来源批量加入同理（navigation_add_source_handle.go，逐条原因随回执
// 返回）。同级上移 / 下移也已迁走（同一条 JSON 端点，action="move"）。页面这一侧只剩
// update / panel。

// navigationListPath 导航菜单列表页（回跳目标）。
const navigationListPath = "/admin/navigations"

// navListBack 列表页回跳地址：从**本次请求的 query** 读回筛选上下文。
//
// 上下文随表单 action 的 query 一起提交（`action="/admin/navigations/update?project=…&kind=…`），
// 服务端按调用点显式列出的键读回来 —— 不再由 Go 拼 ?err= / ?done=。
func navListBack(c *gin.Context) string {
	return backref.BackPath(c, navigationListPath, "project", "kind")
}

// navListBackMenu 同 navListBack，额外保留 `?menu=`（编辑 / 面板抽屉保存后要重新展开的那一项）。
func navListBackMenu(c *gin.Context) string {
	return backref.BackPath(c, navigationListPath, "project", "kind", "menu")
}

// —— 回跳链接文字与成功回执（key + 中文兜底，**不需要 gin.Context**）——
//
// 「这段文案属于哪个语言」由提示页取词时决定，与控制器无关 —— 这正是本批的形状要求：
// 控制器曾经在这里调 shell.TranslateFor 把 key 提前翻成一句当前语言的成品串，而语言是
// **请求级**的，每多一处就多一次「这句话属于哪个语言」的判断。
var (
	// navListBackTitle 列表页回跳链接文字（复用页面标题词条，不新增全站词条）。
	navListBackTitle = navText{Key: navigationenums.Heading, Fallback: "导航菜单"}

	// navDoneNotice 写动作成功回执（复用同一句话，不按动作细分；词条见迁移 590）。
	navDoneNotice = navText{Key: navigationenums.ActionDone, Fallback: "操作已完成"}
)

// navJump 提示页出口：成功 1 秒后自动跳，失败不自动跳（运营要看清楚原因）。
func navJump(c *gin.Context, ok bool, msg navText, back string, backText navText) {
	j := shell.Jump{
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
	}
	if ok {
		j.OK = true
		j.Seconds = 1
	}
	shell.RenderJump(c, j)
}

// navJumpList 回列表页的提示页（保留工程 / 位置筛选）。
func navJumpList(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBack(c), navListBackTitle)
}

// navJumpListMenu 回列表页并重新展开某项抽屉的提示页（编辑 / 面板设置共用）。
func navJumpListMenu(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBackMenu(c), navListBackTitle)
}

// navigationEditFormView 导航编辑抽屉片段的视图（**片段出口**，不是整页）。
type navigationEditFormView struct {
	// CSRFToken 写请求的 CSRF 令牌（模板拼 X-CSRF-Token 头）。
	CSRFToken          string
	NavEditEcho        any
	NavEditError       string
	NavEditPanelAvail  bool
	NavEditPanelBlocks any
}
