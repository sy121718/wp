package navigationhttp

// navigation_page_handle.go — 前台导航菜单管理页（/admin/navigations）。
// 自 dashboard 迁回本模块。与后台权限菜单（admin/menus）严格隔离：本页管理的是
// 公开站点导航（navigations 表），构建期由 core.nav 组件绑定位置后编译进静态产物。
// 交互遵循后台规范：GET 渲染整页，POST 写操作后 303 回列表页，不写新 JS。

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

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
// 「无公开路径的候选不提供添加」同源（见 navigation_page_handle.go 的 NavigationAddSource）。
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
	// Err / Done 是列表页回带的操作结论（?err= / ?done=）：批量删除按
	// 「已删除 N 个 / 跳过 M 个」写进 Done（有跳过时写 Err，警告条更显眼）。
	Err  string
	Done string
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
		"Err":             d.Err,
		"Done":            d.Done,
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
		// 操作结论走 query 回带（PRG）：批量删除的结果条。
		// 读侧一律经 navigation_err.go 的白名单出口（查询参数不是可信边界）。
		Err:  navigationPageErr(c),
		Done: navigationPageDone(c),
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

// navListURL 列表页回跳地址（保留工程与位置筛选）。
func navListURL(projectID, kind string) string {
	return navListURLWith(projectID, kind, "", "")
}

// navListURLWith 带操作结论的回跳地址（批量删除用）。
func navListURLWith(projectID, kind, errText, doneText string) string {
	return navListURLMenu(projectID, kind, "", errText, doneText)
}

// navListURLMenu 列表页回跳地址的**唯一构造点**：保留工程/位置筛选，可带操作结论文案与
// 「保存块后要重新展开的那一项」（?menu=）。
//
// 两条文案都由服务端拼装（受控文本 + 计数），经 QueryEscape 回带；模板侧 Jet 默认 HTML
// 转义，不构成注入面。menuID 只认 uuid 形状（normalizeMenuFocus），非法值丢掉。
func navListURLMenu(projectID, kind, menuID, errText, doneText string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	q.Set("kind", normalizeNavKind(kind))
	if id := normalizeMenuFocus(menuID); id != "" {
		q.Set("menu", id)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	if doneText != "" {
		q.Set("done", doneText)
	}
	return "/admin/navigations?" + q.Encode()
}

// NavigationCreate 新增菜单项（默认自定义链接；指定父级则成为二级项）。
func (h *navigationPageHandle) NavigationCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))
	path := strings.TrimSpace(c.PostForm("path"))
	target := strings.TrimSpace(c.PostForm("target"))
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	if projectID == "" || title == "" || path == "" {
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, "", navInvalidParamText(c), ""))
		return
	}
	req := &navigationdto.CreateReq{ProjectID: projectID, Title: title, Path: path, Kind: kind, Target: target}
	if parentID != "" {
		req.ParentID = &parentID
	}
	if _, err := h.navigations.Create(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("path", path).Error(err, "新增导航项失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
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
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, "", navInvalidParamText(c), ""))
		return
	}
	if len(ids) == 0 {
		// 最常见的一条：抽屉里 33 个复选框默认全不勾，用户直接点「加入菜单」。
		// 模板侧已把「本组没有可加入项」的提交按钮置灰，这里是服务端兜底 —— 两条
		// 都要有：只靠前端，手工构造的请求仍会拿到一条看不懂的响应。
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, "", navTextOf(c, navNoticeNoSourcePicked), ""))
		return
	}
	groups, err := h.navigations.SourceGroups(ctx, projectID)
	if err != nil {
		logger.Scene("page").With("project", projectID).Error(err, "加载导航来源候选失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
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
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationUpdate 更新菜单项（标题/链接/打开方式，空字段保持不变）。
func (h *navigationPageHandle) NavigationUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	// menu 是「冲突/失败后仍要展开的那一项」：带着它回跳，用户刷新后抽屉还开着，
	// 能立刻看到库里的当前值并决定怎么改。解析放在 id 判定之前 —— 参数级失败同样要
	// 带着它回列表页，否则用户回来还得在几十行里重新找那一项。
	menuID := strings.TrimSpace(c.PostForm("menu"))
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
			c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, menuID, navInvalidParamText(c), ""))
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
	navEditRedirect(c, navListURL(projectID, kind))
}

// NavigationDelete 删除菜单项（连同其子项，避免留下孤儿节点）。
func (h *navigationPageHandle) NavigationDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	if id == "" {
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, "", navInvalidParamText(c), ""))
		return
	}
	if err := h.navigations.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("id", id).Error(err, "删除导航项失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationsBulkDelete 批量删除菜单项（POST /admin/navigations/bulk-delete）。
//
// 逐条走同一条单条删除路径（NavigationDelete 用的同一个 navigations.Delete）：
// 「子项一并删除」的规则由 service 决定，handler 不复制一套。某一条失败（已不存在等）
// 只计入跳过数、整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，保留工程与位置筛选。
func (h *navigationPageHandle) NavigationsBulkDelete(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的**受控错误**（shell.BulkIDsError：值域只有 Count/Max 两个整数），
		// 走 shell 的受控文案出口：按当前语言给出「一次最多操作 N 项，当前 M 项，请分批进行」——
		// 这是运营照着做的可操作提示，不能换成通用文案；出口只认类型，不认文本，
		// 因此将来 shell 把上层原文拼进错误也不会跟着出来。
		c.Redirect(http.StatusSeeOther, navListURLWith(projectID, kind, shell.BulkIDsFacingText(c, berr), ""))
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
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	msg := navigationsBulkDeleteResult(c, deleted, skipped)
	if skipped > 0 {
		c.Redirect(http.StatusSeeOther, navListURLWith(projectID, kind, msg, ""))
		return
	}
	c.Redirect(http.StatusSeeOther, navListURLWith(projectID, kind, "", msg))
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
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	if id == "" || (dir != "up" && dir != "down") {
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, "", navInvalidParamText(c), ""))
		return
	}
	item, err := h.navigations.Get(ctx, &navigationdto.GetReq{ID: id})
	if err != nil {
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	nodes, err := h.navigations.Tree(ctx, item.ProjectID, item.Kind)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
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
		c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
		return
	}
	a, b := siblings[idx], siblings[next]
	// 交换前带上各自读到的 update_time（乐观锁）：排序也是这份数据的一次写入，
	// 谁在这期间改过哪一条就拒绝哪一条，而不是把对方的修改顺手抹平。
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{
		ID: a.ID, SortOrder: &b.SortOrder, ExpectedUpdatedAt: &a.UpdatedAt,
	}); err != nil {
		logger.Scene("page").With("id", a.ID).Error(err, "调整导航项排序失败")
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, id, navigationErrPageText(c, err), ""))
		return
	}
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{
		ID: b.ID, SortOrder: &a.SortOrder, ExpectedUpdatedAt: &b.UpdatedAt,
	}); err != nil {
		logger.Scene("page").With("id", b.ID).Error(err, "调整导航项排序失败")
		c.Redirect(http.StatusSeeOther, navListURLMenu(projectID, kind, id, navigationErrPageText(c, err), ""))
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
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

// SetupNavigationPages 注册导航菜单管理页与导航译文工作台（/admin 组，
// 中间件链由装配层统一挂好）。函数名沿用 SetupXxxPages 先例：本包已有 REST
// 路由的 SetupNavigationRoutes，不能同名。
// pageSvc 是页面契约：译文保存后按 i18n:content 依赖标记手工页面待重建
// （消费者侧断言 navTranslationPageMarker 端口，实现方不支持时降级为不标记）。
// adminPages 为 nil 时整体跳过。
func SetupNavigationPages(adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, pageSvc pagecontract.PageService,
	blocks blockcontract.BlockService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)
	// 面板块能力（超级菜单）：装配期注入；未注入时面板入口降级可见（PanelAvail=false）。
	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations", h.NavigationsPage)
	// GET 读编辑表单，但代理真正 POST /api/navigation/update 的权限动作。
	adminPages.GET("/navigations/edit", builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost), h.NavigationEditFragment)
	// 面板设置复用 navigation:update；新建面板块是**块的创建**，故挂 block:create。
	adminPages.POST("/navigations/panel", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.PanelSet)
	adminPages.POST("/navigations/panel/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.PanelCreate)
	adminPages.POST("/navigations/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationCreate)
	adminPages.POST("/navigations/add-source", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationAddSource)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)
	adminPages.POST("/navigations/delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), h.NavigationDelete)
	// 批量删除复用同一条删除路径与权限点（/api/navigation/delete）：批量只是单条的加速器，
	// 不是另一件事 —— 另立权限点会让「能删一个、不能删十个」这种状态出现。
	adminPages.POST("/navigations/bulk-delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), h.NavigationsBulkDelete)
	adminPages.POST("/navigations/move", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationMove)

	// 导航译文工作台（审计 I18N-007）：菜单标签不在页面文档里，页面翻译工作台看不到它，
	// 构建期靠 navigation.label 语境回填 —— 本页是那个语境的唯一维护入口。
	// 保存复用「修改导航」权限点：译文是导航项内容的一部分。
	translations := NewNavigationTranslationHandle(navigations, projects)
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}
	if pageSvc != nil {
		if marker, ok := pageSvc.(navTranslationPageMarker); ok {
			translations.SetPageMarker(marker)
		}
	}
	// 自动发布实例侧同样要标：菜单文字也烘在 presentation 实例的产物里，而那些实例
	// 只认 menu:{projectID}:{kind} 依赖（与导航项增删改走同一条派发链、同一份键）。
	// 实现方是本模块自己的 Service（持有 SetMenuStaleDispatcher 注入的端口）。
	if invalidator, ok := navigations.(navTranslationMenuInvalidator); ok {
		translations.SetMenuInvalidator(invalidator)
	}
	adminPages.GET("/navigations/translations", translations.NavigationTranslations)
	adminPages.POST("/navigations/translations/save", builtin.CasbinMiddlewareForPath("/api/navigation/update"), translations.SaveNavigationTranslations)
}
