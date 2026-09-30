package blockhttp

// block_page_handle.go — 全局块管理页（后台「全局块」入口）：页眉/页脚/区块的列表、
// 新建与删除。自 dashboard 迁回本模块；块内容编辑复用工作台（/workbench?block=ID）。
// stale 传播 / 引用检查已由装配层接管（internal/routers/block_page_bridge.go），
// 本页不再持有任何跨模块失效编排。

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// blocksPageTitle 全局块管理页标题（i18n key，与原 dashboard 枚举同值）。
const blocksPageTitle = "MsgBlocksTitle"

// blockPageHandle 全局块管理页处理器。
type blockPageHandle struct {
	blocks   blockcontract.BlockService
	projects projectcontract.ProjectService
	// pages 页面契约（只读用：块变更的影响面 —— 引用该块的页面数、待重建页面清单）。
	//
	// 可空：未注入时页面明确写「页面能力未装配，无法统计影响面」，而不是显示一个
	// 「0 个页面引用」的假结论（见 block_page_impact.go）。注入点是装配层
	// mountAdminPages 里的 SetupBlockPages 调用（可选变参）。
	pages pagecontract.PageService
}

// NewBlockPageHandle 创建全局块管理页处理器。
//
// pages 为可选变参：装配层尚未传入时页面照常工作，只是不显示影响面。
func NewBlockPageHandle(blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) *blockPageHandle {
	h := &blockPageHandle{blocks: blocks, projects: projects}
	if len(pages) > 0 {
		h.pages = pages[0]
	}
	return h
}

// blockRow 全局块列表行投影。
type blockRow struct {
	ID             string
	Name           string
	Kind           string
	KindLabel      string
	ReuseMode      string
	ReuseModeLabel string
	UpdatedAt      string
	// RefCountText 影响面：该块当前被多少页面引用（只读统计）。
	// 复制模式（template）显示「—」——它插入时已复制 AST，改它不影响任何页面。
	RefCountText string
}

// blocksPageData 全局块管理页数据。
type blocksPageData struct {
	Title             string
	Menu              string
	Projects          []projectcontract.ProjectResp
	SelectedProjectID string
	Headers           []blockRow
	Footers           []blockRow
	Blocks            []blockRow
	// Err / Done 上一次写动作的回执（删除结果、部分失败计数）。本页此前没有任何回执通道，
	// 删除成不成功在页面上看不出来 —— 批量动作必须把「跳过几个」说清楚。
	Err  string
	Done string
	// StaleImpact 只读影响面：待重建页面数与清单（块 / 内容 / 主题 / 导航变更都会产生）。
	StaleImpact gin.H
}

// templateMap 转 Jet 模板键 map（layout 以小写 title/menu 取值）。
func (d *blocksPageData) templateMap() gin.H {
	return gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProjectID,
		"Headers":         d.Headers,
		"Footers":         d.Footers,
		"Blocks":          d.Blocks,
		"Err":             d.Err,
		"Done":            d.Done,
		"StaleImpact":     d.StaleImpact,
	}
}

// blockText 一条待取词文案：i18n key + 中文兜底（兜底同时是词条缺失时的显示值）。
type blockText struct{ Key, Fallback string }

// blockKindLabels 块类型标签（docs/02-D §4/§5 全量白名单）：kind → {key, 中文兜底}。
//
// **key 与模板 admin/block/blocks.html 的 kind 下拉是同一批**（`admin.blocks.kind.*`）：
// 下拉是模板里 16 行硬编码的 tr 调用、本表是列表列直出的 KindLabel —— 两处同义，
// 因此必须共用同一条词条，否则同一个 kind 在下拉里与列表里会显示成两个词。
var blockKindLabels = map[string]blockText{
	"header":       {"admin.blocks.kind.header", "页眉"},
	"footer":       {"admin.blocks.kind.footer", "页脚"},
	"block":        {"admin.blocks.kind.block", "区块"},
	"announcement": {"admin.blocks.kind.announcement", "公告栏"},
	"sidebar":      {"admin.blocks.kind.sidebar", "侧边栏"},
	"breadcrumb":   {"admin.blocks.kind.breadcrumb", "面包屑"},
	"drawer":       {"admin.blocks.kind.drawer", "抽屉导航"},
	"search":       {"admin.blocks.kind.search", "搜索框"},
	"cta":          {"admin.blocks.kind.cta", "CTA 段"},
	"trust":        {"admin.blocks.kind.trust", "信任徽章"},
	"brands":       {"admin.blocks.kind.brands", "品牌墙"},
	"contact":      {"admin.blocks.kind.contact", "联系方式"},
	"about":        {"admin.blocks.kind.about", "关于我们"},
	"banner":       {"admin.blocks.kind.banner", "横幅"},
	"grid":         {"admin.blocks.kind.grid", "多栏布局"},
	"snippet":      {"admin.blocks.kind.snippet", "片段模板"},
}

// blockReuseModeLabels 复用方式标签（docs/02-D §5）：mode → {key, 中文兜底}。
var blockReuseModeLabels = map[string]blockText{
	"template": {"admin.blocks.reuseMode.template", "复制"},
	"global":   {"admin.blocks.reuseMode.global", "引用"},
}

// kindLabel 块类型 → 当前语言标签；未登记的 kind 落「区块」（与模板下拉的默认项同义）。
func kindLabel(tr func(key, fallback string) string, kind string) string {
	item, ok := blockKindLabels[kind]
	if !ok {
		item = blockKindLabels["block"]
	}
	return tr(item.Key, item.Fallback)
}

// reuseModeLabel 复用方式 → 当前语言标签；未知取值按「引用」显示（与旧实现的默认分支一致）。
func reuseModeLabel(tr func(key, fallback string) string, mode string) string {
	item, ok := blockReuseModeLabels[mode]
	if !ok {
		item = blockReuseModeLabels["global"]
	}
	return tr(item.Key, item.Fallback)
}

// toBlockRows 按 kind 精确过滤（header/footer 页眉页脚组）。
func toBlockRows(tr func(key, fallback string) string, blocks []blockcontract.BlockResp, kind string) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != kind {
			continue
		}
		rows = append(rows, toBlockRow(tr, b))
	}
	return rows
}

// toOtherBlockRows 其余全部类型（新 kind + snippet 片段模板）归入「区块/复用资产」组。
func toOtherBlockRows(tr func(key, fallback string) string, blocks []blockcontract.BlockResp) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind == "header" || b.Kind == "footer" {
			continue
		}
		rows = append(rows, toBlockRow(tr, b))
	}
	return rows
}

func toBlockRow(tr func(key, fallback string) string, b blockcontract.BlockResp) blockRow {
	return blockRow{
		ID: b.ID, Name: b.Name, Kind: b.Kind, KindLabel: kindLabel(tr, b.Kind),
		ReuseMode: b.ReuseMode, ReuseModeLabel: reuseModeLabel(tr, b.ReuseMode),
		UpdatedAt: b.UpdatedAt.Time().Format("2006-01-02 15:04"),
	}
}

// BlocksList 全局块管理页（GET /admin/blocks?project=X）。
func (h *blockPageHandle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	data := &blocksPageData{
		Title:    blocksPageTitle,
		Menu:     "blocks",
		Projects: projects,
		// 回执文案经本模块白名单收口（读侧不允许原样回显查询参数，见 block_err.go）：
		// ?err= 未命中落归口文案、?done= 未命中落空串。
		Err:  blockPageErr(c),
		Done: blockPageDone(c),
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProjectID = sel
	} else if len(projects) > 0 {
		data.SelectedProjectID = projects[0].ID
	}
	if data.SelectedProjectID != "" {
		blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: data.SelectedProjectID})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
			return
		}
		tr := shell.TranslateFor(c)
		data.Headers = toBlockRows(tr, blocks, "header")
		data.Footers = toBlockRows(tr, blocks, "footer")
		data.Blocks = toOtherBlockRows(tr, blocks)
		// 只读影响面（见 block_page_impact.go）：逐块引用页面数 + 待重建页面清单。
		h.fillRefCounts(tr, c.Request.Context(), data.Headers)
		h.fillRefCounts(tr, c.Request.Context(), data.Footers)
		h.fillRefCounts(tr, c.Request.Context(), data.Blocks)
	}
	// 影响面在两种分支（有工程 / 无工程）下都要给：模板是同一份，
	// 缺这个键会让取值链中断（HTTP 仍 200、后半页整块消失）。
	data.StaleImpact = h.blockStaleImpact(shell.TranslateFor(c), c.Request.Context())
	c.HTML(http.StatusOK, "admin/block/blocks", shell.Prepare(c, data.templateMap()))
}

// CreateBlock 新建全局块（POST /admin/blocks/create），成功后进工作台编辑内容。
//
// 失败一律 PRG 回列表页的 ?err= 通道（列表页早有「上一次操作未完成」提示条，批量删除在用）。
// 此前这里走 c.String(400, blockBizError(err))，而业务错误的 Error() 是 enums 常量、也就是
// i18n key —— 浏览器上落成一张**只有 ErrBlockDuplicate 字样的空白页**：没有页面壳、不是中文、
// 也没有任何回列表的入口（用户报告的现象）。同名冲突这类高频分支恰恰最需要说清楚。
func (h *blockPageHandle) CreateBlock(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	kind := strings.TrimSpace(c.PostForm("kind"))
	reuseMode := strings.TrimSpace(c.PostForm("reuseMode"))
	if projectID == "" || name == "" {
		badReq := blockcontract.ErrParamRequired
		if name == "" {
			badReq = blockcontract.ErrNameRequired
		}
		c.Redirect(http.StatusSeeOther, blockListURL(projectID, blockErrText(c, badReq)))
		return
	}
	block, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: kind, ReuseMode: reuseMode,
	})
	if err != nil {
		logger.Scene("block").With("op", "CreateBlock").With("project_id", projectID).Error(err, "创建全局块失败")
		c.Redirect(http.StatusSeeOther, blockListURL(projectID, blockErrText(c, err)))
		return
	}
	c.Redirect(http.StatusSeeOther, "/workbench?block="+block.ID)
}

// DeleteBlock 删除全局块（POST /admin/blocks/delete）。
// 删除后由 block service 统一编排 stale（传播器在装配层注入）：绑定该块的主题下
// 全部页面标待重建（产物退化为无页眉/页脚）。
func (h *blockPageHandle) DeleteBlock(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if id == "" {
		c.Redirect(http.StatusSeeOther, blockListURL(projectID, blockErrText(c, blockcontract.ErrParamRequired)))
		return
	}
	force := strings.TrimSpace(c.PostForm("force")) == "1"
	if err := h.blocks.Delete(c.Request.Context(), &blockcontract.DeleteReq{ID: id, Force: force}); err != nil {
		logger.Scene("block").With("op", "DeleteBlock").With("block_id", id).Error(err, "删除全局块失败")
		// blockRefErrText：被引用拒绝时把「哪一类引用、哪些实体」一起带回列表页（ARCH-02）。
		c.Redirect(http.StatusSeeOther, blockListURL(projectID, blockRefErrText(c, err)))
		return
	}
	c.Redirect(http.StatusSeeOther, blockListURL(projectID, ""))
}

// BlocksBulkDelete 批量删除全局块（POST /admin/blocks/bulk-delete，权限点 block:delete）。
//
// 逐条走**同一条单条删除路径**（h.blocks.Delete）：被页面引用的全局块由服务端拒绝，
// 其余照常删除 —— 单条失败不中断整批（整批回滚会让用户以为「一个都没删」然后反复重试）。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *blockPageHandle) BlocksBulkDelete(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		q := url.Values{}
		if projectID != "" {
			q.Set("project", projectID)
		}
		// 超限是 shell 的受控错误（值域只有 Count/Max）：走它的受控文案出口，
		// 而不是把 err.Error() 拼进 URL（读侧白名单也只认这条文案的归一形态）。
		q.Set("err", shell.BulkIDsFacingText(c, berr))
		c.Redirect(http.StatusSeeOther, "/admin/blocks?"+q.Encode())
		return
	}
	// 明细要带块名：批量列表里用户按名字认块，一串 uuid 定位不了任何东西。
	// 取名字是一次只读列表，失败只影响文案（记日志），不阻断删除流程。
	nameByID := map[string]string{}
	if projectID != "" {
		if list, lerr := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: projectID}); lerr == nil {
			for i := range list {
				nameByID[list[i].ID] = list[i].Name
			}
		} else {
			logger.Scene("block").With("op", "BlocksBulkDelete").Error(lerr, "批量删除前取块名失败，提示退化为 id 前缀")
		}
	}
	deleted, skipped := 0, 0
	// 逐条记跳过原因，最多带 3 条回列表页（整串有 512 字节的形状上限，读侧超限判伪造）。
	details := make([]string, 0, 3)
	for _, id := range ids {
		if err := h.blocks.Delete(c.Request.Context(), &blockcontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("block").With("op", "BlocksBulkDelete").With("block_id", id).Error(err, "批量删除全局块失败")
			skipped++
			if len(details) < 3 {
				name := strings.TrimSpace(nameByID[id])
				if name == "" {
					name = shortBlockID(id)
				}
				details = append(details, blockRefSkipDetail(c, name, err))
			}
			continue
		}
		deleted++
	}
	q := url.Values{}
	if projectID != "" {
		q.Set("project", projectID)
	}
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	if msg := blocksBulkDeleteResult(c, deleted, skipped, details); msg != "" {
		if skipped > 0 {
			q.Set("err", msg)
		} else {
			q.Set("done", msg)
		}
	}
	target := "/admin/blocks"
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	c.Redirect(http.StatusSeeOther, target)
}

// blocksBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
// details 是逐条跳过原因（最多几条，由调用方截断）：全部跳过 / 部分跳过时附在结论之后，
// 形状是「受控模板句 + ：+ 定位」，读侧白名单按形态 3（前缀匹配）放行。
func blocksBulkDeleteResult(c *gin.Context, deleted, skipped int, details []string) string {
	// 模板取自 block_err.go 的 blockBulkResultTemplates —— 那里同时也是读侧的
	// 白名单来源：写侧改措辞时读侧跟着变，不会静默失配成「系统内部错误」。
	tail := ""
	if len(details) > 0 {
		tail = "：" + strings.Join(details, "；")
		if skipped > len(details) {
			tail += "…"
		}
	}
	switch {
	case deleted == 0 && skipped == 0:
		return blockBulkFilled(c, blockBulkResultTemplates[0], nil)
	case skipped == 0:
		return blockBulkFilled(c, blockBulkResultTemplates[1], map[string]string{"count": strconv.Itoa(deleted)})
	case deleted == 0:
		return truncateRunes(blockBulkFilled(c, blockBulkResultTemplates[2],
			map[string]string{"count": strconv.Itoa(skipped)})+tail, shell.NoticeMaxBytes-1)
	default:
		return truncateRunes(blockBulkFilled(c, blockBulkResultTemplates[3],
			map[string]string{"deleted": strconv.Itoa(deleted), "skipped": strconv.Itoa(skipped)})+tail, shell.NoticeMaxBytes-1)
	}
}

// shortBlockID 取块 id 的前 8 位做提示里的退化定位（取名字失败时用）。
func shortBlockID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}

// SaveBlockContent 工作台保存块内容（POST /admin/blocks/save-content，JSON）。
// 工作台块编辑的保存入口：保存后由 block service 统一编排 stale
// （传播器已在装配层注入，REST /api/block/update 与本保存路径同源传播）。
func (h *blockPageHandle) SaveBlockContent(c *gin.Context) {
	var req struct {
		ID       string          `json:"id" binding:"required"`
		Name     string          `json:"name"`
		Document json.RawMessage `json:"document"`
		// ReturnURL 保存成功后的回跳目标（站内相对路径）。
		//
		// 来源：菜单页「新建面板块并编辑」跳到 /workbench?block=ID&returnUrl=...，
		// 工作台把它随保存请求体带回来 —— 块存完能直接回到菜单编辑器并重新展开那一项。
		ReturnURL string `json:"returnUrl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// JSON 端点：出口必须是 JSON。调用方是工作台的 saveDraft，它对响应做 r.json()、
		// 再按 code >= 400 取 message 弹提示；原先的 c.String(400, "参数不合法") 是纯文本，
		// r.json() 直接 reject 落入 catch —— 用户只看到保存状态变红，一句话提示都没有。
		// 归口走本包既有的 paramBindFail（400 + 受控文案 + 绑定原文只进 warn 日志）。
		paramBindFail(c, err)
		return
	}
	// 名称取现值（工作台只改文档）。
	current, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: req.ID})
	if err != nil {
		// 状态码与文案都归口到 REST 出口用的同一份判定（block_http.go）：
		// 业务错误（块不存在 / 缺工程作用域）→ 各自的状态码 + 模块词条 key（由 response
		// 按请求语言翻译）；基础设施故障 → 500 + 归口文案，原文只进日志。
		//
		// 原先这里是 `if err != nil || current == nil` 一把吞成 404 纯文本：既把
		// 「读不到库」伪装成「块不存在」，又让整条 JSON 契约断在这里。
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	if current == nil {
		// 契约上「Detail 无错」即命中，这是防御分支：静默继续会把一次写操作建在空块上。
		response.ErrorWithMessage(c, http.StatusNotFound, blockenums.ErrBlockNotFound)
		return
	}
	name := current.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	if _, uerr := h.blocks.Update(c.Request.Context(), &blockcontract.UpdateReq{
		ID: req.ID, Name: name, Document: req.Document,
	}); uerr != nil {
		// 这条分支原先**只回 500 归口文案、不落任何日志**（F 线实测：合成 payload 触发 500，
		// 日志里空空如也，排障无从下手）。响应形状不变（仍是归口文案），
		// 错误原文只进结构化日志 —— 与同文件其它写路径（CreateBlock / DeleteBlock）的写法一致。
		logger.Scene("block").With("op", "SaveBlockContent").With("block_id", req.ID).
			With("path", c.Request.URL.Path).Error(uerr, "工作台保存块内容失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	// 回跳（303 PRG）：只有**站内相对路径**才接受，其它一律拒绝并落默认列表页。
	//
	// 开放重定向在后台同样是「看起来像本站自己发起的跳转」：//evil.example.com 是协议
	// 相对 URL，浏览器会当外站处理；绝对 URL 更直接。判据与语言切换回跳共用同一份
	//（shell.LocalReturnPath）。没传（普通块编辑）时不跳，保存后留在工作台。
	if raw := strings.TrimSpace(req.ReturnURL); raw != "" {
		target := shell.LocalReturnPath(raw)
		if target == "" {
			target = "/admin/blocks"
		}
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	// 成功回执走 response 出口（与上面错误路径同族）：message 传 key，由 response 层按键取词，
	// 不再把中文写在这里 —— 原先的 gin.H{"message": "已保存，关联页面将标记为待重建"}
	// 是英文界面上唯一说中文的那一句（同文件其余路径都走了 response + enums）。
	response.SuccessWithMessage(c, blockenums.ContentSavedRebuildQueued)
}

// blockListURL 全局块列表页回跳地址（PRG）：保留工程上下文，并带上操作结论文案。
//
// 结论文案统一走 ?err=（列表页已有渲染位与「上一次操作未完成」前缀），本页不再有
// 「直出裸文本、没有页面壳」的第二条反馈通道。
func blockListURL(projectID, errText string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if e := strings.TrimSpace(errText); e != "" {
		q.Set("err", e)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/blocks?" + enc
	}
	return "/admin/blocks"
}

// blockErrInternalFallback 非业务错误（基础设施故障）的兜底文案：中文原文，兼作取词兜底。
const blockErrInternalFallback = "系统内部错误，请稍后重试"

// blockErrText 业务错误 → 当前语言文案。
//
// 业务错误的 Error() 就是 enums 常量，而 enums 常量即 i18n key（真文案在 sys_i18n，
// 迁移 058 + 237 已 seed 全部 ErrBlock* 词条），所以这里按请求语言取词、以 key 作兜底：
// 直接把 err.Error() 铺到页面上正是「页面上出现 ErrBlockDuplicate 裸 key」的来源。
// 非业务错误原文只进日志（调用方已按操作记过一条），对外回通用提示。
func blockErrText(c *gin.Context, err error) string {
	tr := shell.TranslateFor(c)
	if key := blockErrKey(err); key != "" {
		return tr(key, key)
	}
	return tr(shell.MsgInternalError, blockErrInternalFallback)
}

// blockErrSentinels 全部 block 业务 sentinel（= 词条 key 的来源）。
// 新增业务错误时在此同步登记：漏登记的后果是它被当成内部故障（通用提示 + 日志），
// 方向是安全的（不泄漏内部细节），但用户拿到的是不可行动的提示。
var blockErrSentinels = []error{
	blockcontract.ErrParamRequired,
	blockcontract.ErrNotFound,
	blockcontract.ErrProjectNotFound,
	blockcontract.ErrProjectRequired,
	blockcontract.ErrNameRequired,
	blockcontract.ErrInvalidDoc,
	blockcontract.ErrInvalidKind,
	blockcontract.ErrInvalidCategory,
	blockcontract.ErrDuplicate,
	blockcontract.ErrInvalidReuseMode,
	blockcontract.ErrBlockInUse,
}

// blockErrKey 业务错误 → i18n 词条 key（非业务错误返回空串，按内部故障处理）。
//
// 一律取 **sentinel 自己的 Error()**（enums 常量、也即词条 key），不能取 err.Error()：
// 被 fmt.Errorf("...: %w", err) 包过的错误，Error() 是整句话，拿去查词条必然查不到
// —— 页面上就会出现那句内部包装文案。
func blockErrKey(err error) string {
	if err == nil {
		return ""
	}
	for _, sentinel := range blockErrSentinels {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}

// SetupBlockPages 注册全局块管理页（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupBlockRoutes，不能同名。
// adminPages 为 nil 时整体跳过。
//
// pages 为可选变参（装配层传入 page 契约后「影响面」才可用，见 block_page_impact.go）。
// 用变参而不是必填参数：漏传时页面降级为「影响面未装配」的明确提示，而不是启动失败 ——
// 这一批只做可见性，不该把块管理页的可用性与它绑死。
func SetupBlockPages(adminPages *gin.RouterGroup,
	blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) {
	if adminPages == nil {
		return
	}
	h := NewBlockPageHandle(blocks, projects, pages...)
	adminPages.GET("/blocks", h.BlocksList)
	// 待重建影响面清单（只读抽屉片段）：页头徽章是入口，清单在抽屉里。
	// 鉴权复用列表页读权限点（GET 与 /api/block/list 的策略动词一致）。
	adminPages.GET("/blocks/stale/drawer", builtin.CasbinMiddlewareForPath("/api/block/list"), h.BlocksStaleDrawer)
	adminPages.POST("/blocks/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.CreateBlock)
	adminPages.POST("/blocks/delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.DeleteBlock)
	// 批量删除复用单条删除的权限点（不新增权限点、不写迁移）：能删一个块的人就能删一批。
	adminPages.POST("/blocks/bulk-delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.BlocksBulkDelete)
	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), h.SaveBlockContent)
}
