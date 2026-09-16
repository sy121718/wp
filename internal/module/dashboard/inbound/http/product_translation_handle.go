package dashboardhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// productTranslationPort 商品翻译工作台使用的译文读写端口。
//
// 生产实现 = pkg/i18n.ContentWriter（sys_translation 表 + 默认数据库）。
//
// 工程作用域（审计 I18N-009）：商品译文与页面译文同一张表的同一套隔离规则，
// 工作台按本工程读写 —— 工作台本来就是按工程选商品的（project 查询参数）。
type productTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// ProductTranslationInstancePort 商品译文变更后需要失效的自动发布实例端口。
//
// 消费者侧最窄接口：编排层把 presentation 契约实现传进来即可（跨模块只依赖契约）。
type ProductTranslationInstancePort interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}

// productTranslationHandle 商品域翻译页处理器。
type productTranslationHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	// instances 自动发布实例失效端口（可空：为空时只做 page 侧标记）。
	instances ProductTranslationInstancePort
	// writer 译文读写端口（为 nil 时按默认库惰性构造；测试注入隔离 schema 的写入器）。
	writer productTranslationPort
}

// NewProductTranslationHandle 构造商品域翻译页处理器（instances 可为 nil）。
func NewProductTranslationHandle(products productcontract.ProductService, projects projectcontract.ProjectService,
	pages pagecontract.PageService, instances ProductTranslationInstancePort) *productTranslationHandle {
	return &productTranslationHandle{products: products, projects: projects, pages: pages, instances: instances}
}

// SetContentTranslationStore 注入译文读写端口（测试用；生产走默认库）。
func (h *productTranslationHandle) SetContentTranslationStore(port productTranslationPort) {
	h.writer = port
}

// port 返回译文读写端口（未注入时用默认库）。
func (h *productTranslationHandle) port() (productTranslationPort, error) {
	if h.writer != nil {
		return h.writer, nil
	}
	return i18n.NewContentWriterDefault()
}

// SetupProductTranslationRoutes 注册商品域翻译页路由（挂 /admin 页面组）。
//
// saveGuard 由调用方传入（页面组层需要挂 Casbin 权限点中间件），与页面翻译工作台
// 同一口径：保存改的是商品的展示文本，复用商品更新权限点；GET 属安全方法，
// 页面组已有 Session + CSRF。
func SetupProductTranslationRoutes(adminPages *gin.RouterGroup, saveGuard gin.HandlerFunc,
	products productcontract.ProductService, projects projectcontract.ProjectService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *productTranslationHandle {
	handle := NewProductTranslationHandle(products, projects, pages, instances)
	adminPages.GET("/products/translations", handle.ProductTranslations)
	if saveGuard != nil {
		adminPages.POST("/products/translations/save", saveGuard, handle.SaveProductTranslations)
		return handle
	}
	adminPages.POST("/products/translations/save", handle.SaveProductTranslations)
	return handle
}

// ProductTranslations GET /admin/products/translations：商品域翻译工作台。
func (h *productTranslationHandle) ProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.Query("project"))
	productID := strings.TrimSpace(c.Query("product"))
	lang := strings.TrimSpace(c.Query("lang"))

	data, err := h.build(ctx, projectID, productID, lang)
	if err != nil {
		logger.Scene("product").With("project", projectID).With("product", productID).
			Error(err, "打开商品翻译工作台失败")
		c.Redirect(http.StatusSeeOther, "/admin/products")
		return
	}
	if saved := strings.TrimSpace(c.Query("saved")); saved == "1" {
		data.Saved = true
		n, _ := strconv.Atoi(strings.TrimSpace(c.Query("n")))
		if n > 0 {
			data.SavedNote = "已保存 " + strconv.Itoa(n) + " 条译文；译文变更已标记待重建（下次构建生效）。"
		} else {
			data.SavedNote = "没有需要写入的变化。"
		}
	}
	c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
}

// SaveProductTranslations POST /admin/products/translations/save：保存译文（整表提交）。
func (h *productTranslationHandle) SaveProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("project"))
	productID := strings.TrimSpace(c.PostForm("product"))
	lang := strings.TrimSpace(c.PostForm("lang"))

	data, err := h.build(ctx, projectID, productID, lang)
	if err != nil {
		logger.Scene("product").With("project", projectID).Error(err, "商品翻译工作台保存前重建数据失败")
		c.Redirect(http.StatusSeeOther, "/admin/products")
		return
	}
	if !h.langAllowed(ctx, data.ProjectID, lang) {
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationLangInvalid})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationInvalid})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, contextName+"："+translationMsg(c, dashboardenums.MsgTranslationStale))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue // 空输入 = 本行不写入（不删除库中已有译文）
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateProductTarget(contextName, source, target); verr != "" {
			rowErrors = append(rowErrors, contextName+"："+verr)
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			// 工程作用域（审计 I18N-009）：写入本工程自己的译文行。
			ProjectID:  projectID,
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.SourceText, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		writeKeys = append(writeKeys, key)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		c.Redirect(http.StatusSeeOther, translationLocation(projectID, productID, lang, 0))
		return
	}

	port, perr := h.port()
	if perr != nil {
		logger.Scene("product").With("project", projectID).Error(perr, "内容译文存储不可用")
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationSaveFailed})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	// 第二步：变更判定。只有译文文本确实变化才写库并触发重建（幂等，重复保存零写入）。
	// 变更判定按**本工程**读现有译文（工程行优先、回落全局行）。
	before, berr := port.LoadDetailsForProject(ctx, projectID, lang, productWriteHashes(items))
	if berr != nil {
		logger.Scene("product").With("project", projectID).Error(berr, "读取现有译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changedKeys := make([]string, 0, len(items))
	targetChanged := false
	for i, item := range items {
		prev := before[writeKeys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		if prev.TargetText != item.TargetText {
			targetChanged = true
		}
		pending = append(pending, item)
		changedKeys = append(changedKeys, writeKeys[i])
	}

	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = port.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("product").With("project", projectID).With("lang", lang).Error(uerr, "写入商品译文失败")
			data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationSaveFailed})
			c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
			return
		}
	}

	// 第三步：译文变化 → 标记待重建。
	//   1) 手工页面：i18n:content 依赖条目配套的全站标记（与页面翻译工作台同一链路）；
	//   2) 自动发布实例：按 direct_content 键精确标记受影响实体（商品页面属这类）。
	if targetChanged {
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("product").With("project", projectID).Error(merr, "商品译文保存后标记页面待重建失败")
			}
		}
		h.markInstancesStale(ctx, data, changedKeys)
	}
	c.Redirect(http.StatusSeeOther, translationLocation(projectID, productID, lang, written))
}

// markInstancesStale 按 direct_content 键标记受影响实体的自动发布实例待重建。
//
// 译文按 (原文 hash, 语境) 寻址，实体身份只在候选里；同一段文本可能来自多个实体，
// 因此按「语境 + 指纹」回查本次渲染出的候选行，命中即视为该实体受影响
// （保守超集：宁可多标记，也不漏标记 —— 与页面侧「引用块即登记依赖」同一口径）。
//
// 未注入实例端口 / 无变化 / 标记失败：只记日志，不影响保存结果（译文已落库）。
func (h *productTranslationHandle) markInstancesStale(ctx context.Context, data *productTranslationsData, changedKeys []string) {
	if h.instances == nil || data == nil || len(changedKeys) == 0 {
		return
	}
	for _, ref := range data.translationAffectedEntities(changedKeys) {
		dep := pipeline.DirectContentKey(ref.EntityType, ref.EntityID)
		if _, err := h.instances.MarkStaleByDependency(ctx, dep.Kind, dep.Key); err != nil {
			logger.Scene("product").With("entity_type", ref.EntityType).With("entity_id", ref.EntityID).
				Error(err, "商品译文保存后标记自动发布实例待重建失败")
		}
	}
}

// translationEntityRef 受影响的商品域实体（自动发布实例标记用）。
type translationEntityRef struct {
	EntityType string
	EntityID   string
}
