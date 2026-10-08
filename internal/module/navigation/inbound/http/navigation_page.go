package navigationhttp

// 自 dashboard 迁回本模块。与后台权限菜单（admin/menus）严格隔离：本页管理的是
// 公开站点导航（navigations 表），构建期由 core.nav 组件绑定位置后编译进静态产物。
// 交互遵循后台规范：GET 渲染整页，POST 写操作后由 shell.RenderJump 渲染整页提示
//（成功 1 秒后自动回列表），不写新 JS。

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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
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
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
		return
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: id})
	if err != nil {
		if err.Error() == navigationenums.ErrNotFound {
			c.String(http.StatusNotFound, "%s", navigationErrPageText(c, err))
			return
		}
		logger.Scene("page").With("id", id).Error(err, "加载导航编辑片段失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return
	}
	if item == nil || item.ProjectID != projectID || item.Kind != kind {
		c.String(http.StatusNotFound, "%s", shell.TranslateFor(c)(navigationenums.ErrNotFound, "菜单项不存在"))
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
	}, "")
}

func (h *navigationPageHandle) renderNavigationEdit(c *gin.Context, echo navEditEcho, errorText string) {
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
	data := gin.H{"NavEditEcho": echo, "NavEditError": errorText, "NavEditPanelAvail": panelAvail, "NavEditPanelBlocks": blocks}
	// A fragment has no page shell: supply only translation and session CSRF data.
	tr := shell.TranslateFor(c)
	data["t"] = tr
	token, err := builtin.GetCSRFToken(c)
	if err != nil || token == "" {
		logger.Scene("page").With("id", echo.ID).Error(err, "加载导航编辑 CSRF token 失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return
	}
	data["csrf_token"] = token
	c.HTML(http.StatusOK, "admin/navigation/navigation_edit_form", data)
}

func navEditHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

func (h *navigationPageHandle) navEditFailure(c *gin.Context, err error, echo navEditEcho) {
	if !navEditHTMX(c) {
		// 原生提交：整页提示（结论走响应体，不再是 303 + ?err=）。
		navJumpListMenu(c, false, navigationErrPageText(c, err))
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
	h.renderNavigationEdit(c, echo, navigationErrPageText(c, err))
}

// navEditOwnedRow checks the submitted context before a write addressed by id alone.
func (h *navigationPageHandle) navEditOwnedRow(c *gin.Context, echo navEditEcho) bool {
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
		return false
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: echo.ID})
	if err != nil && err.Error() != navigationenums.ErrNotFound {
		logger.Scene("page").With("id", echo.ID).Error(err, "核对导航编辑归属失败")
		c.String(http.StatusInternalServerError, "%s", shell.PageInternalText(c))
		return false
	}
	if item == nil || item.ProjectID != echo.ProjectID || item.Kind != echo.Kind {
		c.String(http.StatusNotFound, "%s", shell.TranslateFor(c)(navigationenums.ErrNotFound, "菜单项不存在"))
		return false
	}
	return true
}

// 页面标题（i18n key，与原 dashboard 枚举同值）。
//
// 参数级提示不再用 MsgFieldRequired 这个通用 key 直出（原先 c.String(400, key) 把 key
// 本身当响应体）：统一走 navigation_err.go 的 navInvalidParamText / navNoticeNoSourcePicked。
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

// navMenuRow 菜单结构行：展平树 + 层级缩进 + 同级首末标记（控制上移/下移按钮禁用态）。
type navMenuRow struct {
	ID     string
	Title  string
	Path   string
	Depth  int
	Target string
	// TargetLabel 打开方式中文标签（模板直出，避免模板里做条件判断）。
	TargetLabel string
	// Indent 层级缩进（内联样式值，二级项缩进一格）。
	Indent     string
	SourceType string
	First      bool
	Last       bool
	// PanelBlockID / PanelBlockName / PanelWidth 悬浮面板（超级菜单，迁移 285）：
	// 内容存块、展示存菜单项。
	PanelBlockID   string
	PanelBlockName string
	PanelWidth     string
	// UpdatedAt 乐观锁 token（精确到微秒的 RFC3339 串）：编辑抽屉原样回带，
	// 服务端据此判断「打开抽屉之后这一项有没有被别处改过」。
	UpdatedAt string
}

// navSourceGroup 来源候选分组（页面渲染用）：在 contract 的 SourceGroup 上补一个
// 「本组是否至少有一项可加入」。
//
// 为什么在页面侧派生而不是加进 contract：它是**展示判据**（按钮禁用态），跨模块契约
// 不该为了一个后台按钮多一个字段。判据只有一份 —— 候选 URL 非空才可加入，与 service
// 「无公开路径的候选不提供添加」同源（见 navigation_page.go 的 NavigationAddSource）。
//
// 为什么要有它：逐项 disabled 只挡住单个复选框，整组都不可用时按钮仍可点，用户提交后
// 才拿到「请至少勾选一项要加入菜单的内容」—— 而 33 个项目里一个都勾不动，那句提示
// 只会让人更困惑。按钮在源头置灰，配合每项的「（暂无公开路径，先发布后再添加）」说明。
type navSourceGroup struct {
	Type  string
	Title string
	Items []navigationcontract.SourceCandidate
	// Usable 本组至少有一项可加入（URL 非空）。
	Usable bool
}

// navSourceGroupTitles 来源分组标题：来源类型 → {key, 中文兜底}。
//
// contract 的 SourceGroup.Title 只承载 i18n key（见 outbound/source/resolver.go）：
// 那个适配器在 service 层之下、拿不到请求语言，中英文案必须在展示层取。
var navSourceGroupTitles = map[string]navText{
	"page":     {navigationenums.SourcePage, "页面"},
	"article":  {navigationenums.SourceArticle, "文章"},
	"product":  {navigationenums.SourceProduct, "产品"},
	"category": {navigationenums.SourceCategory, "分类"},
}

// navSourceGroupTitle 来源类型 → 当前语言分组标题；未知类型回落 contract 给的 key 原文
// （显示成 key 比显示成空标题更容易被发现是新增来源类型没登记）。
func navSourceGroupTitle(tr func(key, fallback string) string, typ string) string {
	if item, ok := navSourceGroupTitles[typ]; ok {
		return tr(item.Key, item.Fallback)
	}
	return typ
}

// navigationsPageData 导航菜单管理页数据。
// panelBlockOption 面板块下拉选项（超菜单面板选择）。
type panelBlockOption struct {
	ID   string
	Name string
}

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
	// PanelBlocks 可挂作悬浮面板的全局块（超级菜单）；块能力未装配时为空。
	PanelBlocks []panelBlockOption
	// PanelAvail 面板能力是否可用（块契约已注入）。
	PanelAvail bool
	// MenuID 要自动展开编辑抽屉的菜单项 id（?menu=）。
	//
	// 用途：面板块「新建并编辑」跳到工作台，块保存后带着这个参数回到菜单编辑器 ——
	// 用户回来时抽屉已经开着，能看到刚挂上面板的那一项（否则要在几十行里自己找）。
	// 落在列表里的项才生效；非列表值不渲染任何东西（它也从不进 HTML，只用作行比对）。
	MenuID string
}

// templateMap 转为模板所需的小写键 map（layout.html 以 {{.title}}/{{.menu}} 取值）。
func (d *navigationsPageData) templateMap() gin.H {
	return gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Kind":            d.Kind,
		"Rows":            d.Rows,
		"ParentOptions":   d.ParentOptions,
		"SourceGroups":    d.SourceGroups,
		"PanelBlocks":     d.PanelBlocks,
		"PanelAvail":      d.PanelAvail,
		"MenuID":          d.MenuID,
	}
}

// NavigationsPage 导航菜单管理页。
func (h *navigationPageHandle) NavigationsPage(c *gin.Context) {
	data, err := h.buildNavigationsData(c)
	if err != nil {
		logger.Scene("page").Error(err, "加载导航菜单页失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	c.HTML(http.StatusOK, "admin/navigation/navigations", shell.Prepare(c, data.templateMap()))
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
	tr := shell.TranslateFor(c)
	rows := make([]navMenuRow, 0, 8)
	var groups []navigationcontract.SourceGroup
	if selected != "" {
		nodes, terr := h.navigations.Tree(ctx, selected, kind)
		if terr != nil {
			return nil, terr
		}
		flattenNavRows(tr, nodes, 0, &rows)
		// 来源候选：页面/文章/产品/分类（依赖模块不可用时对应分组为空）。
		if groups, terr = h.navigations.SourceGroups(ctx, selected); terr != nil {
			logger.Scene("page").With("project", selected).Warn("导航来源候选加载失败，管理页仅显示自定义链接")
		}
	}
	// 视图模型：把「本组是否有可加入项」一次算清，模板只做渲染（模板不做查询、不做统计）。
	// 分组标题在 contract 里只有 i18n key（resolver 是 outbound 适配器、拿不到请求语言），
	// 取词落在这一层。
	sourceGroups := make([]navSourceGroup, 0, len(groups))
	for _, g := range groups {
		usable := false
		for _, it := range g.Items {
			if strings.TrimSpace(it.URL) != "" {
				usable = true
				break
			}
		}
		sourceGroups = append(sourceGroups, navSourceGroup{
			Type: g.Type, Title: navSourceGroupTitle(tr, g.Type), Items: g.Items, Usable: usable,
		})
	}
	// 悬浮面板（超级菜单）：列出可挂的块并回填行上的块名。块能力未装配时整体降级
	// （PanelAvail=false，模板隐藏面板区）——降级可见，不留一个点了没反应的下拉。
	panelBlocks := make([]panelBlockOption, 0, 8)
	blockNames := map[string]string{}
	panelAvail := h.blocks != nil
	if panelAvail && selected != "" {
		list, berr := h.blocks.List(ctx, &blockcontract.ListReq{ProjectID: selected})
		if berr != nil {
			logger.Scene("page").With("project", selected).Error(berr, "列出面板块候选失败")
			panelAvail = false
		} else {
			for _, b := range list {
				panelBlocks = append(panelBlocks, panelBlockOption{ID: b.ID, Name: b.Name})
				blockNames[b.ID] = b.Name
			}
		}
	}
	for i := range rows {
		if rows[i].PanelBlockID != "" {
			rows[i].PanelBlockName = blockNames[rows[i].PanelBlockID]
		}
	}

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
		PanelBlocks: panelBlocks, PanelAvail: panelAvail,
		// 操作结论不再经 query 回带（PRG）：写动作的结论由 shell.RenderJump 渲染成
		// 整页提示（见 navigation_jump.go），列表页不再从 query 读任何结论。
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

// navTargetLabels 打开方式标签：target → {key, 中文兜底}。
// key 与模板 admin/navigation/navigations.html 的 target 下拉同源（admin.navigations.target.*）。
var navTargetLabels = map[string]navText{
	"self":  {"admin.navigations.target.self", "当前窗口"},
	"blank": {"admin.navigations.target.blank", "新标签页"},
}

// navTargetLabel 打开方式 → 当前语言标签；未知取值按「当前窗口」（与模板下拉默认项一致）。
func navTargetLabel(tr func(key, fallback string) string, target string) string {
	item, ok := navTargetLabels[target]
	if !ok {
		item = navTargetLabels["self"]
	}
	return tr(item.Key, item.Fallback)
}

// flattenNavRows 深度优先展平菜单树（同级首末标记用于按钮禁用态）。
func flattenNavRows(tr func(key, fallback string) string, nodes []*navigationdto.NavigationNode, depth int, out *[]navMenuRow) {
	for i, n := range nodes {
		*out = append(*out, navMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Depth: depth,
			Target: n.Target, TargetLabel: navTargetLabel(tr, n.Target),
			Indent:     fmt.Sprintf("%dpx", depth*24),
			SourceType: n.SourceType,
			First:      i == 0, Last: i == len(nodes)-1,
			PanelBlockID: panelBlockIDOf(n), PanelWidth: n.PanelWidth,
			UpdatedAt: n.UpdatedAt,
		})
		flattenNavRows(tr, n.Children, depth+1, out)
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
// 回跳地址改由 shell.BackPath 从**表单 action 的 query** 按白名单读回（见 navigation_jump.go
// 的 navListBack / navListBackMenu），结论文案不再进 URL。菜单项的 `?menu=` 也走同一条读回链。

// NavigationCreate 新增菜单项（默认自定义链接；指定父级则成为二级项）。
func (h *navigationPageHandle) NavigationCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))
	path := strings.TrimSpace(c.PostForm("path"))
	target := strings.TrimSpace(c.PostForm("target"))
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	if projectID == "" || title == "" || path == "" {
		navJumpList(c, false, navInvalidParamText(c))
		return
	}
	req := &navigationdto.CreateReq{ProjectID: projectID, Title: title, Path: path, Kind: kind, Target: target}
	if parentID != "" {
		req.ParentID = &parentID
	}
	if _, err := h.navigations.Create(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("path", path).Error(err, "新增导航项失败")
		navJumpList(c, false, navigationErrPageText(c, err))
		return
	}
	navJumpList(c, true, navDoneText(c))
}

// NavigationAddSource 按来源实体批量加入菜单（管理页「从已有内容添加」）。
// 标题与链接取自来源候选（页面 SEO 标题/内容标题 + 公开路径），并写入 source_type/source_id，
// 之后来源实体改标题/路径，重新构建即同步。无公开路径的候选会被跳过。
func (h *navigationPageHandle) NavigationAddSource(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	sourceType := strings.TrimSpace(c.PostForm("sourceType"))
	ids := c.PostFormArray("sourceIds")
	if projectID == "" || sourceType == "" {
		navJumpList(c, false, navInvalidParamText(c))
		return
	}
	if len(ids) == 0 {
		// 最常见的一条：抽屉里 33 个复选框默认全不勾，用户直接点「加入菜单」。
		// 模板侧已把「本组没有可加入项」的提交按钮置灰，这里是服务端兜底 —— 两条
		// 都要有：只靠前端，手工构造的请求仍会拿到一条看不懂的响应。
		navJumpList(c, false, navTextOf(c, navNoticeNoSourcePicked))
		return
	}
	groups, err := h.navigations.SourceGroups(ctx, projectID)
	if err != nil {
		logger.Scene("page").With("project", projectID).Error(err, "加载导航来源候选失败")
		navJumpList(c, false, shell.PageInternalText(c))
		return
	}
	candidates := make(map[string]navigationcontract.SourceCandidate, len(ids))
	for _, g := range groups {
		if g.Type != sourceType {
			continue
		}
		for _, it := range g.Items {
			candidates[it.ID] = it
		}
	}
	added, skipped := 0, 0
	for _, id := range ids {
		item, ok := candidates[id]
		if !ok || item.URL == "" {
			skipped++
			continue
		}
		sourceID := item.ID
		title := item.Title
		if title == "" {
			title = item.Label
		}
		if _, cerr := h.navigations.Create(ctx, &navigationdto.CreateReq{
			ProjectID: projectID, Title: title, Path: item.URL, Kind: kind,
			SourceType: sourceType, SourceID: &sourceID,
		}); cerr != nil {
			logger.Scene("page").With("sourceType", sourceType).With("sourceId", id).Error(cerr, "来源实体加入菜单失败")
			skipped++
			continue
		}
		added++
	}
	logger.Scene("page").With("added", added).With("skipped", skipped).Info("来源实体加入导航菜单")
	navJumpList(c, true, navDoneText(c))
}

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
			h.renderNavigationEdit(c, echo, navInvalidParamText(c))
		} else {
			navJumpListMenu(c, false, navInvalidParamText(c))
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
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
	navJumpList(c, true, navDoneText(c))
}

// NavigationDelete 删除菜单项（连同其子项，避免留下孤儿节点）。
func (h *navigationPageHandle) NavigationDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		navJumpList(c, false, navInvalidParamText(c))
		return
	}
	if err := h.navigations.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("id", id).Error(err, "删除导航项失败")
		navJumpList(c, false, navigationErrPageText(c, err))
		return
	}
	navJumpList(c, true, navDoneText(c))
}

// NavigationsBulkDelete 批量删除菜单项（POST /admin/navigations/bulk-delete）。
//
// 逐条走同一条单条删除路径（NavigationDelete 用的同一个 navigations.Delete）：
// 「子项一并删除」的规则由 service 决定，handler 不复制一套。某一条失败（已不存在等）
// 只计入跳过数、整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，保留工程与位置筛选。
func (h *navigationPageHandle) NavigationsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的**受控错误**（shell.BulkIDsError：值域只有 Count/Max 两个整数），
		// 走 shell 的受控文案出口：按当前语言给出「一次最多操作 N 项，当前 M 项，请分批进行」——
		// 这是运营照着做的可操作提示，不能换成通用文案；出口只认类型，不认文本，
		// 因此将来 shell 把上层原文拼进错误也不会跟着出来。
		navJumpList(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.navigations.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); err != nil {
			logger.Scene("page").With("id", id).Error(err, "批量删除导航项失败")
			skipped++
			continue
		}
		deleted++
	}
	// 有跳过 → 失败页（不自动跳，运营要看清剩下那些）；全成功 → 成功页（1 秒后回列表）。
	msg := navigationsBulkDeleteResult(c, deleted, skipped)
	if skipped > 0 {
		navJumpList(c, false, msg)
		return
	}
	navJumpList(c, true, msg)
}

// navigationsBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func navigationsBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	// 模板取自 navigation_err.go 的 navigationsBulkResultTemplates ——
	// 那里同时是读侧候选文案的来源：写侧改措辞时读侧跟着变，不会静默失配。
	switch {
	case deleted == 0 && skipped == 0:
		return navBulkFilled(c, navigationsBulkResultTemplates[0], nil)
	case skipped == 0:
		return navBulkFilled(c, navigationsBulkResultTemplates[1], map[string]string{"count": strconv.Itoa(deleted)})
	case deleted == 0:
		return navBulkFilled(c, navigationsBulkResultTemplates[2], map[string]string{"count": strconv.Itoa(skipped)})
	default:
		return navBulkFilled(c, navigationsBulkResultTemplates[3],
			map[string]string{"deleted": strconv.Itoa(deleted), "skipped": strconv.Itoa(skipped)})
	}
}

// NavigationMove 同级上移/下移（与相邻项交换 sort_order）。
func (h *navigationPageHandle) NavigationMove(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.PostForm("id"))
	dir := strings.TrimSpace(c.PostForm("dir"))
	if id == "" || (dir != "up" && dir != "down") {
		navJumpList(c, false, navInvalidParamText(c))
		return
	}
	item, err := h.navigations.Get(ctx, &navigationdto.GetReq{ID: id})
	if err != nil {
		navJumpList(c, false, navigationErrPageText(c, err))
		return
	}
	nodes, err := h.navigations.Tree(ctx, item.ProjectID, item.Kind)
	if err != nil {
		logger.Scene("page").With("project", item.ProjectID).Error(err, "读取导航树失败")
		navJumpList(c, false, shell.PageInternalText(c))
		return
	}
	siblings := navSiblings(nodes, item.ParentID)
	idx := -1
	for i, s := range siblings {
		if s.ID == id {
			idx = i
			break
		}
	}
	next := idx - 1
	if dir == "down" {
		next = idx + 1
	}
	if idx < 0 || next < 0 || next >= len(siblings) {
		// 已到边界（按钮在首/末项上本就是禁用态，这里兜手工请求）：列表顺序未改动。
		navJumpList(c, true, navDoneText(c))
		return
	}
	a, b := siblings[idx], siblings[next]
	// 交换前带上各自读到的 update_time（乐观锁）：排序也是这份数据的一次写入，
	// 谁在这期间改过哪一条就拒绝哪一条，而不是把对方的修改顺手抹平。
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{
		ID: a.ID, SortOrder: &b.SortOrder, ExpectedUpdatedAt: &a.UpdatedAt,
	}); err != nil {
		logger.Scene("page").With("id", a.ID).Error(err, "调整导航项排序失败")
		navJumpListMenu(c, false, navigationErrPageText(c, err))
		return
	}
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{
		ID: b.ID, SortOrder: &a.SortOrder, ExpectedUpdatedAt: &b.UpdatedAt,
	}); err != nil {
		logger.Scene("page").With("id", b.ID).Error(err, "调整导航项排序失败")
		navJumpListMenu(c, false, navigationErrPageText(c, err))
		return
	}
	navJumpList(c, true, navDoneText(c))
}

// navSiblings 返回指定父级下的同级节点列表（父级为空即顶级）。
func navSiblings(nodes []*navigationdto.NavigationNode, parentID *string) []*navigationdto.NavigationNode {
	if parentID == nil || *parentID == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.ID == *parentID {
			return n.Children
		}
		if found := navSiblings(n.Children, parentID); found != nil {
			return found
		}
	}
	return nil
}

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
			h.renderNavigationEdit(c, echo, navInvalidParamText(c))
		} else {
			navJumpListMenu(c, false, navInvalidParamText(c))
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", navInvalidParamText(c))
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
	navJumpListMenu(c, true, navDoneText(c))
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
		navJumpListMenu(c, false, shell.PageInternalText(c))
		return
	}
	name := panelBlockName(shell.TranslateFor(c), kind, title)
	created, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: "block",
	})
	if err != nil {
		logger.Scene("page").With("project_id", projectID).Error(err, "新建面板块失败")
		navJumpListMenu(c, false, navigationErrPageText(c, err))
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
	navJump(c, true, navDoneText(c), target, "")
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
	data := h.build(c.Request.Context(), projectID, lang, shell.TranslateFor(c))
	data.filter(strings.TrimSpace(c.Query("keyword")))
	c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
}

// SaveNavigationTranslations POST /admin/navigations/translations/save（整表提交）。
func (h *navigationTranslationHandle) SaveNavigationTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("project"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	keyword := strings.TrimSpace(c.PostForm("keyword"))
	tr := shell.TranslateFor(c)
	data := h.build(ctx, projectID, lang, tr)
	data.filter(keyword)

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = []string{tr(navigationenums.ErrRowCountMismatch, "提交的行数不一致，请刷新后重试")}
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if h.writer == nil {
		data.Errors = []string{tr(navigationenums.ErrStorageUnavailable, "译文存储不可用")}
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	// kinds 与 items / keys 同下标：这条译文来自被提交的那一行所在的菜单位置。
	// 跨位置的同名文字在列表里只出现一行（见 build 的去重），因此它只是**回退值** ——
	// 权威的「哪些位置受影响」按原文哈希回查（见 kindsForHashes）。
	kinds := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		if contextName != navigationTranslationContext() {
			rowErrors = append(rowErrors, fmt.Sprintf(tr(navigationenums.ErrContextInvalid, "语境非法：导航标签只接受 %s"), navigationTranslationContext()))
			continue
		}
		source, ok := data.sourceOf(hashes[i])
		if !ok {
			rowErrors = append(rowErrors, tr(navigationenums.ErrSourceChanged, "菜单文字已变化，请刷新后重试"))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if !i18n.ShouldTranslateContent(source.Source) {
			rowErrors = append(rowErrors, fmt.Sprintf(tr(navigationenums.ErrNotTranslatable, "%s：该菜单文字不参与翻译（纯数字或纯符号）"), source.Source))
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.Source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		keys = append(keys, key)
		kinds = append(kinds, source.Kind)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		navigationTranslationDone(c, data.ProjectID, lang, keyword, 0)
		return
	}
	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("navigation").With("lang", lang).Error(berr, "读取现有导航译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	// 只有**真的变了**的译文才派发失效：没变化的提交不该把产物标一遍 stale
	//（那会让下一次构建白跑，且查不出是谁改的 —— 与 invalidateMenu 的注释同一理由）。
	changedHashes := map[string]bool{}
	fallbackKinds := make([]string, 0, 2)
	seenKind := map[string]bool{}
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changedHashes[item.SourceHash] = true
		if k := strings.TrimSpace(kinds[i]); k != "" && !seenKind[k] {
			seenKind[k] = true
			fallbackKinds = append(fallbackKinds, k)
		}
	}
	changedKinds := data.kindsForHashes(changedHashes, fallbackKinds)
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("navigation").With("lang", lang).Error(uerr, "写入导航译文失败")
			// 归口文案：命中 enums 白名单的业务文案原样透出，其余（数据库原文：
			// 表名 / 约束名 / SQLSTATE）只进上面那条日志，页面拿归口提示。
			data.Errors = []string{fmt.Sprintf(tr(navigationenums.ErrSaveFailed, "保存失败：%s"), navigationErrPageText(c, uerr))}
			c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
			return
		}
		// 译文已落库 → 标记待重建。失败只记日志：译文本身已经写好了，
		// 标记失败只影响「下次构建会不会主动带上这些页」，不该让运营以为保存失败。
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("navigation").With("lang", lang).Error(merr, "导航译文保存后标记页面待重建失败")
			}
		}
		// 自动发布实例侧同批派发（menu:{projectID}:{kind}）：只标手工页面会让
		// 详情页 / 产品页停在旧菜单标签上，而线上与库里都看不出任何异常。
		if h.menus != nil && len(changedKinds) > 0 {
			h.menus.InvalidateMenuLabels(ctx, scopeProjectID(data, projectID), changedKinds)
		}
	}
	navigationTranslationDone(c, data.ProjectID, lang, keyword, written)
}

// navigationTranslationDone 译文保存的结论：提示页，回跳只带筛选。
func navigationTranslationDone(c *gin.Context, projectID, lang, keyword string, written int) {
	tr := shell.TranslateFor(c)
	var msg string
	if written > 0 {
		msg = fmt.Sprintf(tr(navigationenums.Saved, "已保存 %d 条译文；已标记受影响页面待重建（下次构建生效）。"), written)
	} else {
		msg = tr(navigationenums.SavedNone, "没有需要写入的变化。")
	}
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      msg,
		Back:     navigationTranslationFilteredLocation(projectID, lang, keyword),
		BackText: tr("admin.navigation_translations.heading", "导航译文"),
		Seconds:  1,
	})
}

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

// templateMap 转模板键 map。
func (d *navigationTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"ProjectID": d.ProjectID, "Keyword": d.Keyword, "NoProject": d.NoProject, "HasData": d.HasData, "VisibleCount": d.VisibleCount,
		"Lang": d.Lang, "Langs": d.Langs,
		"Groups": d.Groups, "RowCount": d.RowCount, "Done": d.Done,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
	}
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
		// 标题写 i18n key：shell.Prepare 会对 "title" 取词，词条见 sys_i18n。
		Title: "admin.navigation_translations.heading", Menu: "navigation-translations",
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
	return data
}

// navKindTitle 菜单位置的当前语言名（key 与导航页的 kind 下拉同源：admin.navigations.kind.*）。
func navKindTitle(tr func(key, fallback string) string, kind string) string {
	if kind == "footer" {
		return tr(navigationenums.KindFooter, "页脚导航")
	}
	return tr(navigationenums.KindHeader, "页眉导航")
}
