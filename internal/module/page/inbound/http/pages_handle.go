package pagehttp

// 页面管理列表页（后台「页面」入口）：列出/新建站点工程与页面，
// 行内直达可视化工作台。交互遵循后台 HTMX 规范：HTMX 请求返回
// Jet 片段，否则完整页面/重定向。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// pagesAdminHandle 页面列表与翻译工作台的页面处理器（从 dashboard 回迁）。
// 只持有本页面需要的契约；构造见 NewPagesAdminHandle。
type pagesAdminHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
	// blocks 全局块契约（只读）：翻译工作台收集页眉/页脚绑定与 globalref 内的候选。
	blocks blockcontract.BlockService
	// blueprints 蓝图候选（新建页面时的空白草稿模板）。
	// 可空：端口未注入时表单不显示蓝图选项，建页照常（blueprintOptions 返回空切片）。
	blueprints blueprintcontract.BlueprintService
	// siteIndex 全站可翻译内容索引缓存（跨页面复用提示 + 全站完成度，见
	// page_translations_index.go）。
	siteIndex siteContentIndexCache
	// contentStore 内容译文读写端口（翻译工作台）。
	// 为 nil 时按默认实现（pkg/i18n.ContentWriter + 默认数据库）惰性构造；
	// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
	contentStore contentTranslationPort
}

// NewPagesAdminHandle 创建页面列表 / 翻译工作台处理器；各契约为对应模块 contract。
func NewPagesAdminHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, blueprints blueprintcontract.BlueprintService) *pagesAdminHandle {
	return &pagesAdminHandle{pages: pages, projects: projects, blocks: blocks, blueprints: blueprints}
}

// PagesAdminHandle 导出类型别名：外部测试包（public/test/page/feature）需要命名
// 构造器返回的句柄类型；别名指向未导出类型是合法 Go，读起来也明确指向后者。
type PagesAdminHandle = pagesAdminHandle

// SetBlueprints 注入蓝图契约（装配期调用；可空）。
func (h *pagesAdminHandle) SetBlueprints(b blueprintcontract.BlueprintService) { h.blueprints = b }

// blueprintOptions 拉取蓝图候选；蓝图端口未注入或查询失败时返回空列表
// （表单不显示蓝图选项，建页照常走空白草稿）。
func (h *pagesAdminHandle) blueprintOptions(ctx context.Context) []blueprintOption {
	if h.blueprints == nil {
		return nil
	}
	list, err := h.blueprints.List(ctx, &blueprintdto.ListReq{})
	if err != nil {
		logger.Scene("page").With("err", err).Warn("蓝图列表读取失败，新建页面表单不显示蓝图选项")
		return nil
	}
	out := make([]blueprintOption, 0, len(list))
	for _, b := range list {
		out = append(out, blueprintOption{ID: b.ID, Name: b.Name})
	}
	return out
}

// pagesPageData 页面列表页数据。
// 模板键统一小写（admin/layout.html 以 {{.title}}/{{.menu}} 取值，
// Jet 对 map 键不做大小写兜底）。
type pagesPageData struct {
	Title    string
	Menu     string
	Projects []projectcontract.ProjectResp
	Pages    []pageRow
	// Blueprints 蓝图候选（审计 VIS-010）：「从蓝图开始」是新建页面流程里的一个选项，
	// 不是另一个需要先去的页面。
	Blueprints []blueprintOption
	// Err / Done 是列表页回带的操作结论（?err= / ?done=）：单条删除与批量删除共用这一对键。
	// 批量结果按「已删除 N 个页面 / 跳过 M 个」写进 Done（有跳过时写 Err，警告条更显眼）。
	Err  string
	Done string
}

// blueprintOption 新建页面表单里的蓝图选项。
type blueprintOption struct {
	ID   string
	Name string
}

// templateMap 转为模板所需的小写键 map。
func (d *pagesPageData) templateMap() gin.H {
	return gin.H{
		"title":      d.Title,
		"menu":       d.Menu,
		"Projects":   d.Projects,
		"Pages":      d.Pages,
		"Blueprints": d.Blueprints,
		"Err":        d.Err,
		"Done":       d.Done,
	}
}

// pageRow 列表行投影（含状态文案）。
type pageRow struct {
	ID        string
	ProjectID string
	Kind      string
	DraftPath string
	Active    bool
	Staged    bool
	Stale     bool
	Version   int64
	UpdatedAt string
}

// PagesList 页面列表页。
func (h *pagesAdminHandle) PagesList(c *gin.Context) {
	data, err := h.buildPagesData(c)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, pageenums.MsgInternalError)
		return
	}
	c.HTML(http.StatusOK, "admin/pages", shell.Prepare(c, data.templateMap()))
}

// buildPagesData 组装列表页数据。
//
// 页面按主题浏览（020_themes.sql：主题下面才是页面）：取第一个工程的
// 激活主题过滤页面；无工程或无主题时 themeID 为空列全部页面。
func (h *pagesAdminHandle) buildPagesData(c *gin.Context) (*pagesPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	// 第一步：取当前聚焦工程的激活主题。
	themeID := ""
	if len(projects) > 0 {
		if theme, err := h.projects.GetActiveTheme(ctx, projects[0].ID); err == nil && theme != nil {
			themeID = theme.ID
		}
	}
	// 第二步：按当前工程与激活主题过滤页面。
	projectID := ""
	if len(projects) > 0 {
		projectID = projects[0].ID
	}
	pages, err := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: projectID, ThemeID: themeID})
	if err != nil {
		return nil, err
	}
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		rows = append(rows, pageRow{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt.Time().Format("2006-01-02 15:04"),
		})
	}
	return &pagesPageData{
		Title: pageenums.MsgPagesTitle, Menu: "pages",
		Projects: projects, Pages: rows,
		// 蓝图候选（审计 VIS-010）：把「从蓝图开始」放进新建页面流程，
		// 而不是要求编辑者先去另一个页面建好蓝图再回来。
		Blueprints: h.blueprintOptions(ctx),
		// 操作结论走 query 回带（PRG）：单条删除与批量删除共用这一对键，
		// 页面本身不做筛选，故回跳不带其它参数。
		Err:  strings.TrimSpace(c.Query("err")),
		Done: strings.TrimSpace(c.Query("done")),
	}, nil
}

// CreateProject 新建站点工程（HTMX 表单提交，成功后整页刷新列表）。
func (h *pagesAdminHandle) CreateProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "项目名称不能为空")
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectcontract.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		logger.Scene("page").With("name", name).Error(err, "创建站点工程失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, pageenums.MsgInternalError)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}

// CreatePage 新建页面（默认空白草稿，创建后可进工作台编辑）。
func (h *pagesAdminHandle) CreatePage(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	path := strings.TrimSpace(c.PostForm("draftPath"))
	if projectID == "" || path == "" {
		c.String(http.StatusBadRequest, "项目与页面路径不能为空")
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 默认空白草稿：layout.mode 为编译端必填校验项（full/boxed）。
	// 选了蓝图则以蓝图为准（审计 VIS-010）：page.Create 会用 InitPageDocument 复制
	// 蓝图 AST 并重生成节点 ID，这里传的空白文档只是「没选蓝图」时的兜底。
	if _, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         path,
		DraftDocument:     json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		BlueprintID:       strings.TrimSpace(c.PostForm("blueprintId")),
	}); err != nil {
		logger.Scene("page").With("projectId", projectID).With("path", path).Error(err, "创建页面失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, pageenums.MsgInternalError)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}

// DeletePage 单条删除页面（POST /admin/pages/delete）。
//
// 语义**逐字复用** API 的 /api/page/delete（同一个 svc.Delete）：页面有已激活产物时
// 不拒绝，而是先按 active 路径把访问面下线、清掉路由占用与媒体引用，再软删页面
// （见 service/page_delete.go）。批量删除必须走这同一条路径 —— 单条拒绝 / 批量跳过的
// 规则都由 service 决定，handler 不另立一套。
func (h *pagesAdminHandle) DeletePage(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.Redirect(http.StatusSeeOther, pagesBackURL("缺少页面 id，未执行删除。", ""))
		return
	}
	if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("pageId", id).Error(err, "删除页面失败")
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageErrorMessage(err), ""))
		return
	}
	c.Redirect(http.StatusSeeOther, pagesBackURL("", "已删除 1 个页面。"))
}

// PagesBulkDelete 批量删除页面（POST /admin/pages/bulk-delete）。
//
// 逐条走同一条单条删除路径：某一条失败（已不存在、路径清理失败等）只计入跳过数，
// 整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，不静默部分成功。
func (h *pagesAdminHandle) PagesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusSeeOther, pagesBackURL(berr.Error(), ""))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("page").With("pageId", id).Error(err, "批量删除页面失败")
			skipped++
			continue
		}
		deleted++
	}
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	msg := pagesBulkDeleteResult(deleted, skipped)
	if skipped > 0 {
		c.Redirect(http.StatusSeeOther, pagesBackURL(msg, ""))
		return
	}
	c.Redirect(http.StatusSeeOther, pagesBackURL("", msg))
}

// pagesBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func pagesBulkDeleteResult(deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return "没有勾选任何页面，列表未改动。"
	case skipped == 0:
		return fmt.Sprintf("已删除 %d 个页面。", deleted)
	case deleted == 0:
		return fmt.Sprintf("%d 个页面都未能删除，列表未改动。", skipped)
	default:
		return fmt.Sprintf("已删除 %d 个，%d 个未能删除（可能已被删除或路径清理失败）。", deleted, skipped)
	}
}

// pagesBackURL 列表页回跳地址（PRG）。两条文案都由服务端拼装（受控文本 + 计数），
// 经 QueryEscape 回带；模板侧 Jet 默认 HTML 转义，不构成注入面。
func pagesBackURL(errText, doneText string) string {
	q := url.Values{}
	if errText != "" {
		q.Set("err", errText)
	}
	if doneText != "" {
		q.Set("done", doneText)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/pages?" + enc
	}
	return "/admin/pages"
}
