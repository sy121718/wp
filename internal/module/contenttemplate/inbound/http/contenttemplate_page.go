package contenttemplatehttp

// 模板 Document 的可视化编辑走 /workbench?template=…（复用页面工作台）；
// 本文件提供后台列表页与带样例实体参数的编辑跳转。

// 模板 Document 的可视化编辑走 /workbench?template=…（复用页面工作台）；
// 这里只注册后台列表页与带样例实体参数的编辑跳转。
//
// 权限：注册落点在 contenttemplate_page_router.go；页面 GET 的 Casbin 待补
//（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3），写动作已挂 /api/contenttemplate/* 权限点。

// 分工：数据的取回与聚合在 contenttemplate service（Impact），这里只把它翻译成
// 模板能直接渲染的形状，并把「查不出来 / 可能不完整」这两种状态说清楚 ——
// 把「没有引用」和「查不出引用」显示成同一个空白列表，是这一页最危险的误导。

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/dto"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/contenttemplate/dto"
	"go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/module/contenttemplate/model"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

const contentTemplatesListPath = "/admin/content-templates"

// contentTemplatesNotReadyText 能力未装配时的回执（本页在 h.templates 为空时整体降级）。
const contentTemplatesNotReadyText = "内容模板能力未装配，无法删除。"

// contentTemplatePageHandle 内容模板后台页。
type contentTemplatePageHandle struct {
	templates contenttemplatecontract.ContentTemplateService
	projects  projectcontract.ProjectService
	products  productcontract.ProductService
	contents  contentcontract.ContentService
}

func newContentTemplatePageHandle(templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) *contentTemplatePageHandle {
	return &contentTemplatePageHandle{
		templates: templates, projects: projects, products: products, contents: contents,
	}
}

// ContentTemplatePageHandle 导出类型别名：外部测试包（public/test/contenttemplate/feature）
// 需要命名构造器返回的句柄类型才能驱动页面处理器。
// 别名指向未导出类型是合法 Go，读起来也明确指向后者（对齐 page 模块的 PagesAdminHandle）。
type ContentTemplatePageHandle = contentTemplatePageHandle

// NewContentTemplatePageHandle 构造内容模板后台页处理器（装配与测试共用同一入口）。
func NewContentTemplatePageHandle(templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) *ContentTemplatePageHandle {
	return newContentTemplatePageHandle(templates, projects, products, contents)
}

// ContentTemplatesPage GET /admin/content-templates：按实体类型列出模板，跳转可视化编辑。
func (h *contentTemplatePageHandle) ContentTemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		// 工程列表装载失败：**降级渲染**，不拿走整个页面（与主题管理页、admin 六页同一判据）。
		//
		// 原先这里是 `c.String(500, shell.MsgInternalError)`：浏览器里没有页面，只有一块纯文本，
		// 而且那块文本是**未翻译的裸 key**（页面上直接显示 `MsgInternalError` 这串英文）——
		// 侧栏、页头、筛选栏、列表全部消失，用户既不能改筛选也不能去别的菜单。
		//
		// 三个键的取值都是**刻意的**：
		//   · Ready=true —— 它是「本页能力可用吗」，而工程列表读不到并不代表内容模板能力没装配
		//     （那是 h.templates == nil 的情形，另有一条提示）。取 false 会让模板只渲染
		//     「能力未装配，本页暂不可用」，把那句话挂在一次取数失败上就是误报；
		//   · ImpactAvailable=false + ImpactNote —— 引用面同样没读到，「无引用」会让人
		//     以为可以放心删，必须与「查不出来」长得不一样；
		//   · LoadFailed=true —— 列表空是因为**没读出来**，不是「还没有模板」，
		//     由模板据此换掉空态标题（空态误导比什么都不显示更糟）。
		// 装载失败是这次请求真实发生的事（列表页自己的提示条）；原文只进日志（contentTemplateInternalText）。
		data := gin.H{
			"title": "MsgContentTemplatesTitle", "menu": "content-templates",
			"Projects": []projectcontract.ProjectResp{}, "SelectedProject": "",
			"EntityType": strings.TrimSpace(c.Query("entityType")),
			"Err":        contentTemplateInternalText(c, err),
			"Ready":      true,
			"LoadFailed": true,
			"Templates":  []gin.H{}, "TemplateCount": 0,
			"ImpactAvailable": false,
			"ImpactNote":      contentTemplateImpactLoadFailedText(c),
		}
		c.HTML(http.StatusOK, "admin/contenttemplate/content_templates.html", shell.Prepare(c, data))
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	data := gin.H{
		"title": "MsgContentTemplatesTitle", "menu": "content-templates",
		"Projects": projects, "SelectedProject": selected,
		"EntityType": entityType,
		// 写动作的结论不再回带（走 shell.RenderJump 提示页，见 content_template_err.go）；
		// 这一页的提示条只由「列表取数失败」填充（见上面的降级分支）。
		"Err": "",
	}
	if h.templates == nil {
		data["Ready"] = false
		c.HTML(http.StatusOK, "admin/contenttemplate/content_templates.html", shell.Prepare(c, data))
		return
	}
	// 引用反查先于列表：影响面（哪些模板被页面 / 实例引用）是这一页的删前决策依据。
	// 取不到时降级成 Available=false（「查不出来」），绝不显示成「没有引用」——
	// 后者会让人以为可以放心删。
	impact, impactErr := h.templates.Impact(ctx, &contenttemplatedto.ImpactReq{ProjectID: selected})
	if impactErr != nil || impact == nil {
		impact = &contenttemplatedto.ImpactResp{Available: false}
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: entityType, ProjectID: selected})
	if err != nil {
		shell.PageError(c, "content_template", err)
		return
	}
	// 展示标签（实体类型 / 角色 / 槽位）在 handler 取词：service 拿不到请求语言，
	// 模板直接渲染的文本也不经过 pkg/response 的 translate（见 enums 的 contenttemplate_labels.go）。
	tr := shell.TranslateFor(c)
	refsByTemplate := contentTemplateRefsByTemplate(tr, impact)
	rows := make([]gin.H, 0, len(list))
	for _, t := range list {
		// 结构模板（页眉 / 页脚）不是内容实体：工作台以**无样例实体**模式打开它 ——
		// 不去挑样例实体（挑也挑不到：header / footer 没有实体来源，sampleEntityID
		// 会返回「不支持该类型」），也不该拿一个别的类型的实体去顶替。
		isStructure := contenttemplatemodel.IsStructureTemplateType(t.EntityType)
		editURL, sampleNote := "", ""
		if isStructure {
			editURL = workbenchTemplateURL(t.ID, t.EntityType, "", selected)
		} else {
			sampleID, sampleHint, sampleErr := h.sampleEntityID(ctx, selected, t.EntityType)
			if sampleErr == nil && strings.TrimSpace(sampleHint) == "" && sampleID != "" {
				editURL = workbenchTemplateURL(t.ID, t.EntityType, sampleID, selected)
			}
			sampleNote = sampleErrText(c, sampleHint, sampleErr)
		}
		refs := refsByTemplate[t.ID]
		if refs == nil {
			// 没有引用时也要给**非 nil 的空切片**：Jet 的 len() 对 nil 会报错，
			// 而那一行的报错表现是「HTTP 200 + 之后整块 HTML 消失」。
			refs = &contentTemplateImpactRow{Pages: []contentTemplatePageRef{}, Instances: []contentTemplateInstanceRef{}}
		}
		rows = append(rows, gin.H{
			"ID": t.ID, "Name": t.Name, "EntityType": t.EntityType,
			"DraftVersion": t.DraftVersion, "UpdatedAt": t.UpdatedAt,
			// 结构模板（页眉 / 页脚）与内容实体模板在这张表里是两类东西：前者没有实体来源、
			// 也不接受字段绑定，两者的可编辑性与删除后果都不同，页面上必须一眼分得开。
			"IsStructure": isStructure,
			"TypeLabel":   contentTemplateTypeLabel(tr, t.EntityType),
			"RoleLabel":   contentTemplateRoleLabel(tr, t.TemplateRole),
			"IsDefault":   t.IsDefault,
			// SampleErr 是**模板数据**（形态③）：依赖错误只能出归口文案，原文进日志。
			"EditURL": editURL, "SampleErr": sampleNote,
			// 引用明细（页面 / 实例）：删除与切换生效前必须看得见的东西，所以给
			// 「是哪几张页面、哪个实例」而不是一个计数（计数只够做提示，不够做决策）。
			"RefPages": refs.Pages, "RefInstances": refs.Instances, "RefCount": refs.Count,
			"RefPageCount": len(refs.Pages), "RefInstanceCount": len(refs.Instances),
		})
	}
	data["Templates"] = rows
	data["TemplateCount"] = len(rows)
	data["Ready"] = true
	// 影响面能力的三种状态各说各话：可用 / 未装配 / 有文档解析不了（影响面可能不完整）。
	// 未装配与「没有引用」必须长得不一样，否则运营会拿一个空白引用列表当证据去删模板。
	data["ImpactAvailable"] = impact.Available
	data["ImpactNote"] = contentTemplateImpactNote(c, impact, impactErr)
	c.HTML(http.StatusOK, "admin/contenttemplate/content_templates.html", shell.Prepare(c, data))
}

// ContentTemplatesActivate 切换生效模板（POST /admin/content-templates/activate）。
//
// 与 JSON 接口 POST /api/contenttemplate/activate 是同一个动作、同一条权限点
// （contenttemplate:activate）：同一（工程, 类型）下**多套存着、单套生效**，
// 切换后旧的那套不再生效。页面上的「设为生效」按钮走这里，不重复实现业务规则 ——
// service 内部负责事务（旧的置 false、目标置 true）与依赖扇出。
//
// 回执：成功 / 失败都由 contentTemplateJump 渲染整页提示（文案走响应体）；
// 回跳地址由 shell.BackPath 从表单 action 的 query 读回，保留工程与实体类型筛选
// （否则用户切完一次就丢了当前视图）。
func (h *contentTemplatePageHandle) ContentTemplatesActivate(c *gin.Context) {
	if h.templates == nil {
		contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplatesNotReadyText))
		return
	}
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplateMissingIDText))
		return
	}
	if _, err := h.templates.Activate(c.Request.Context(), &contenttemplatedto.ActivateReq{ID: id}); err != nil {
		// 错误原文只进日志：文案经本页白名单归口（content_template_err.go）。
		contentTemplateJump(c, false, contentTemplateErrText(c, err))
		return
	}
	contentTemplateJump(c, true, contentTemplateJumpText(c, contentTemplateActivateDoneText))
}

// ContentTemplatesBulkDelete 批量删除内容模板（POST /admin/content-templates/bulk-delete）。
//
// 逐条走**同一条单条删除路径**（h.templates.Delete）：被自动发布实例引用的模板由数据库
// 外键拒绝，其余照常删除 —— 单条失败不中断整批（整批回滚会让用户以为「一个都没删」然后反复重试）。
// 结果按「已删除 N 个 / 跳过 M 个」渲染成提示页，回跳地址保留工程与实体类型筛选。
func (h *contentTemplatePageHandle) ContentTemplatesBulkDelete(c *gin.Context) {
	if h.templates == nil {
		contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplatesNotReadyText))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）由 shell 的受控出口生成，保持可见。
		contentTemplateJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.templates.Delete(c.Request.Context(), &contenttemplatedto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	// 有跳过就是失败提示（更显眼，用户下次会去看剩下那些）；全成功才是成功提示。
	contentTemplateJump(c, skipped == 0, contentTemplatesBulkDeleteResult(deleted, skipped))
}

// contentTemplatesBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」等于把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func contentTemplatesBulkDeleteResult(deleted, skipped int) string {
	// 模板取自 content_template_err.go 的 contentTemplatesBulkResultTemplates ——
	// 那里同时是读侧白名单的来源：写侧改措辞时读侧跟着变，不会静默失配成归口文案。
	switch {
	case deleted == 0 && skipped == 0:
		return contentTemplatesBulkResultTemplates[0]
	case skipped == 0:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[1], deleted)
	case deleted == 0:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[2], skipped)
	default:
		return fmt.Sprintf(contentTemplatesBulkResultTemplates[3], deleted, skipped)
	}
}

// ContentTemplateEditPage GET /admin/content-templates/edit：302 到工作台（保留 query）。
//
// 结构的判定取**模板行自己的 entity_type**：结构模板（页眉 / 页脚）不需要样例实体，
// 直接进工作台的无实体模式；内容实体模板缺样例实体时仍按老路自动挑一条（挑不到就
// 渲染提示页说明原因），不静默放行 —— 没有样例实体的内容模板画布只能看到空白组件。
//
// 失败不再 302 + `?err=` 回列表页，改由 contentTemplateJump 渲染整页提示（文案走响应体）。
func (h *contentTemplatePageHandle) ContentTemplateEditPage(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("id"))
	if templateID == "" {
		contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplateMissingIDText))
		return
	}
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	if projectID == "" {
		projectID = strings.TrimSpace(c.Query("project"))
	}
	if h.templates != nil {
		tpl, err := h.templates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
		if err != nil {
			contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplateNotFoundText))
			return
		}
		if contenttemplatemodel.IsStructureTemplateType(tpl.EntityType) {
			c.Redirect(http.StatusFound, workbenchTemplateURL(templateID, tpl.EntityType, "", projectID))
			return
		}
		if entityType == "" {
			entityType = tpl.EntityType
		}
		if entityID == "" {
			sampleID, hint, serr := h.sampleEntityID(c.Request.Context(), projectID, entityType)
			if serr != nil || strings.TrimSpace(hint) != "" {
				contentTemplateJump(c, false, contentTemplateJumpText(c, sampleErrText(c, hint, serr)))
				return
			}
			entityID = sampleID
		}
	}
	if entityID == "" || entityType == "" {
		contentTemplateJump(c, false, contentTemplateJumpText(c, contentTemplateSampleMissingText))
		return
	}
	c.Redirect(http.StatusFound, workbenchTemplateURL(templateID, entityType, entityID, projectID))
}

// workbenchTemplateURL 工作台模板编辑入口。
//
// entityId 为空 = 无实体模式（结构模板：页眉 / 页脚）；该参数缺省而不是留空串，
// 免得「带了空 entityId」与「没带」在日志与排查里长得一样。
func workbenchTemplateURL(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	if entityType != "" {
		q.Set("entityType", entityType)
	}
	if entityID != "" {
		q.Set("entityId", entityID)
	}
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return "/workbench?" + q.Encode()
}

// sampleEntityID 为某实体类型选一条预览样例。
//
// 两个返回值分开是**故意的**：hint 是可直接展示的可行动提示（依赖没装配 / 工程内还没有
// 可预览的实体 / 该实体类型不支持），err 是依赖错误（products.List / contents.List 上抛，
// 可能是 PG 原文，带表名与 SQLSTATE）。合成一个 error 就再也分不清「该给运营看」还是
// 「只该进日志」—— 这一页的 SampleErr 与编辑入口提示页都会被原样渲染。
func (h *contentTemplatePageHandle) sampleEntityID(ctx context.Context, projectID, entityType string) (id, hint string, err error) {
	switch entityType {
	case "product":
		if h.products == nil {
			return "", contentTemplateHintProductNoMod, nil
		}
		list, lerr := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Page: 1, Size: 1})
		if lerr != nil {
			return "", "", lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoProduct, nil
		}
		return list[0].ID, "", nil
	case "article":
		if h.contents == nil {
			return "", contentTemplateHintContentNoMod, nil
		}
		list, lerr := h.contents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: 1})
		if lerr != nil {
			return "", "", lerr
		}
		if len(list) == 0 {
			return "", contentTemplateHintNoArticle, nil
		}
		return list[0].ID, "", nil
	default:
		// 实体类型来自查询参数：只回一句**固定**文案，不回显入参 ——
		// 回显等于把请求方的输入原样反射进提示页 / SampleErr 再渲染一次。
		return "", contentTemplateHintEntityTypeMiss, nil
	}
}

// contentTemplateTranslate 展示层取词函数（与 shell.TranslateFor(c) 同形）。
//
// 展示标签一律经它取词，**中文原文留在 enums 里当兜底**：模板直接渲染的文本
// 不经过 pkg/response 的 translate，在 Go 侧写死中文等于英文界面恒中文。
type contentTemplateTranslate = func(key, fallback string) string

// contentTemplateLabel 取一条标签的当前语言文本。
//
// key 为空（认不出的取值 / 纯定位说明）直接给兜底：`tr("", …)` 查不到行，
// 白白多一次查表，语义也不清。
func contentTemplateLabel(tr contentTemplateTranslate, pair contenttemplateenums.LabelPair) string {
	if tr == nil || strings.TrimSpace(pair.Key) == "" {
		return pair.Fallback
	}
	return tr(pair.Key, pair.Fallback)
}

// contentTemplatePageRef 页面引用的一行。
//
// 用具体结构体而不是 gin.H：模板按字段取值时类型是确定的 —— map[string]interface{}
// 的取值在 Jet 里要经过一层接口解包，任何一处拿不准都会表现为「那一行之后的 HTML
// 整块消失（HTTP 仍 200）」这种极难定位的现象。
type contentTemplatePageRef struct {
	Label string
	Path  string
	Slots string
	URL   string
}

// contentTemplateInstanceRef 实例引用的一行。
type contentTemplateInstanceRef struct {
	Label  string
	Entity string
	Path   string
	Slots  string
}

// contentTemplateImpactRow 一套模板的引用投影（模板渲染直接消费）。
//
// 计数单独给（RefPageCount / RefInstanceCount / RefCount）：模板里不再对切片调 len()，
// 少一层「模板运行时才知道类型对不对」的风险。
type contentTemplateImpactRow struct {
	Pages     []contentTemplatePageRef
	Instances []contentTemplateInstanceRef
	Count     int
}

// contentTemplateRefsByTemplate 把一次反查的结果按模板归集。
//
// 一次扫描服务整页：列表页每套模板都要显示「引用 N 处」，逐个模板各扫一遍页面与实例
// 文档就是 N 次全表扫描（页面一多这一页就点不动）。
func contentTemplateRefsByTemplate(tr contentTemplateTranslate, impact *contenttemplatedto.ImpactResp) map[string]*contentTemplateImpactRow {
	out := map[string]*contentTemplateImpactRow{}
	if impact == nil {
		return out
	}
	for _, ref := range impact.References {
		row := out[ref.TemplateID]
		if row == nil {
			row = &contentTemplateImpactRow{Pages: []contentTemplatePageRef{}, Instances: []contentTemplateInstanceRef{}}
			out[ref.TemplateID] = row
		}
		slots := contentTemplateSlotsLabel(tr, ref.Slots)
		switch ref.Kind {
		case contenttemplatedto.ReferenceKindPage:
			label := strings.TrimSpace(ref.PageTitle)
			if label == "" {
				label = strings.TrimSpace(ref.PagePath)
			}
			if label == "" {
				label = ref.PageID
			}
			row.Pages = append(row.Pages, contentTemplatePageRef{
				Label: label, Path: strings.TrimSpace(ref.PagePath), Slots: slots,
				// 页面给可视化编辑入口：引用明细要能「顺着点过去改」，否则它只是好看。
				URL: "/workbench?id=" + url.QueryEscape(ref.PageID),
			})
		case contenttemplatedto.ReferenceKindInstance:
			label := strings.TrimSpace(ref.URLPath)
			if label == "" {
				label = strings.TrimSpace(ref.EntityType + " " + ref.EntityID)
			}
			row.Instances = append(row.Instances, contentTemplateInstanceRef{
				Label: label, Entity: strings.TrimSpace(ref.EntityType + " " + ref.EntityID),
				Path: strings.TrimSpace(ref.URLPath), Slots: slots,
			})
		}
	}
	for _, row := range out {
		row.Count = len(row.Pages) + len(row.Instances)
	}
	return out
}

// contentTemplateSlotsLabel 槽位名 → 面向运营的说法（排序后拼接；空列表返回空串）。
//
// 判据复用 builder 的槽位常量而不是就地写 "header"/"footer"：构建期就是按这两个
// 常量合并绑定的，写死字符串的话槽位改名只会在这一页静默失效（引用列少一半）。
func contentTemplateSlotsLabel(tr contentTemplateTranslate, slots []string) string {
	if len(slots) == 0 {
		return ""
	}
	labels := make([]string, 0, len(slots))
	for _, slot := range slots {
		switch slot {
		case builder.SlotHeader:
			labels = append(labels, contentTemplateLabel(tr, contenttemplateenums.LabelSlotHeader))
		case builder.SlotFooter:
			labels = append(labels, contentTemplateLabel(tr, contenttemplateenums.LabelSlotFooter))
		default:
			// 认不出的槽位原样显示（不是空白）：兜底行为与改前一致，
			// 让手改数据带来的怪值照样说得清。
			labels = append(labels, slot)
		}
	}
	sort.Strings(labels)
	return strings.Join(labels, " / ")
}

// contentTemplateTypeLabel 实体类型 → 当前语言的展示名（结构模板与内容实体模板要一眼分得开）。
//
// 取值映射的真源在 contenttemplateenums.EntityTypeLabel，本函数只负责取词。
func contentTemplateTypeLabel(tr contentTemplateTranslate, entityType string) string {
	return contentTemplateLabel(tr, contenttemplateenums.EntityTypeLabel(entityType))
}

// contentTemplateRoleLabel 模板角色 → 当前语言的展示名（真源同上）。
func contentTemplateRoleLabel(tr contentTemplateTranslate, role string) string {
	return contentTemplateLabel(tr, contenttemplateenums.TemplateRoleLabel(role))
}

// contentTemplateImpactNote 影响面状态提示（空串 = 无需提示）。
//
// 三种状态各说各话，不能合并：
//
//	· 读取失败 —— 归口文案（错误原文只进日志，见 content_template_err.go）；
//	· 端口未装配 —— 「查不出来」，不是「没有引用」；
//	· 有文档解析不了 —— 影响面可能不完整（>0 的计数必须显示出来）。
func contentTemplateImpactNote(c *gin.Context, impact *contenttemplatedto.ImpactResp, err error) string {
	if err != nil {
		return contentTemplateInternalText(c, err)
	}
	if impact == nil || !impact.Available {
		return contentTemplateImpactUnavailableText
	}
	if impact.Unparsable > 0 {
		return fmt.Sprintf(contentTemplateImpactUnparsableTemplate, impact.Unparsable)
	}
	return ""
}
