package producthttp

import (
	"errors"
	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_translations.go — 商品译文工作台：候选收集、逐行校验、保存与「按依赖标记待重建」。
//
// 候选来自构建期同一份收集逻辑（productcontract.TranslationCandidate），译文写 sys_translation
// 后要把用到这些 key 的实例标记待重建 —— 少标一次的表现是「译好了但线上还是旧语言」。
//
// 这一屏的取数（工程 / 站点语言 / 商品域候选 / 现有译文 / 分组装配）与保存编排
//（逐行校验 / 变更判定 / 写入 / 标记重建）都在商品 service 的读模型里：
// 保存前必须先重建这一屏（逐行校验要候选原文、变更判定要现有译文、标记要实体身份），
// 两者是**同一次取数**，拆开就会各查一遍、且两条路径的视图慢慢长成两个形状。
//
// 控制器只剩三件事：读请求（查询串 / JSON 体）、调读模型、出口（整页渲染 / JSON 回执）。
// 译文读写端口与「标记待重建」端口也住在 service —— 装配层经构造函数转交，控制器不持有。
//
// 路由分两个组：整页 GET 在 /admin（页面组），保存 POST 在 /api/product（authorizedAPI 声明式组）。
// 注册入口因此由装配层调用（SetupProductTranslationRoutes 同时拿到两个组）。

// productTranslationPort 译文读写端口（= service 侧端口的别名；测试注入隔离 schema 的写入器）。
type productTranslationPort = productservice.ProductTranslationStorePort

// ProductTranslationInstancePort 译文变更后需要失效的自动发布实例端口（= service 侧端口别名）。
type ProductTranslationInstancePort = productservice.ProductTranslationInstancePort

// productTranslationsTemplate 工作台整页模板。
const productTranslationsTemplate = "admin/product/product_translations.html"

// ProductTranslationHandle 商品域翻译页处理器。
type ProductTranslationHandle struct {
	products productcontract.ProductService
}

// productTranslationWiring 把装配层给的端口转交给商品 service（端口只在 service 侧存在一份）。
type productTranslationWiring interface {
	SetTranslationInstances(port productservice.ProductTranslationInstancePort)
	SetI18nStaleMarker(m productservice.I18nStaleMarker)
}

// NewProductTranslationHandle 构造商品域翻译页处理器（instances 可为 nil）。
//
// projects 参数保留在签名里是为了**装配层的对称**（同一个 Setup 函数里几个依赖一起给），
// 取数已由 service 自己经 project 契约完成；pages 在这里转交给 service 做「保存后标记
// 手工页面待重建」（收窄成只要 MarkStaleForI18n 的端口）。
func NewProductTranslationHandle(products productcontract.ProductService, projects projectcontract.ProjectService,
	pages pagecontract.PageService, instances ProductTranslationInstancePort) *ProductTranslationHandle {
	_ = projects
	if w, ok := products.(productTranslationWiring); ok {
		w.SetTranslationInstances(instances)
		if pages != nil {
			w.SetI18nStaleMarker(pages)
		}
	}
	return &ProductTranslationHandle{products: products}
}

// SetContentTranslationStore 注入译文读写端口（测试用；生产走默认库）。
//
// 转发给商品 service：端口与保存编排住在那里。转交失败说明 products 不是本模块的 service
// 实现（装配缺陷），此时保存会退回默认库 —— 测试里表现为写库断言失败，不会静默通过。
func (h *ProductTranslationHandle) SetContentTranslationStore(port productTranslationPort) {
	if s, ok := h.products.(interface {
		SetTranslationStore(productservice.ProductTranslationStorePort)
	}); ok {
		s.SetTranslationStore(port)
	}
}

// translationsReader 取这一屏的读模型（保存编排与整屏取数在同一个接口里）。
func (h *ProductTranslationHandle) translationsReader() (productcontract.ProductTranslationsReader, error) {
	reader, ok := h.products.(productcontract.ProductTranslationsReader)
	if !ok {
		return nil, errors.New("商品译文工作台读模型未注入（装配缺陷）")
	}
	return reader, nil
}

// ProductTranslations GET /admin/products/translations：商品域翻译工作台。
func (h *ProductTranslationHandle) ProductTranslations(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	productID := strings.TrimSpace(c.Query("product"))

	reader, rerr := h.translationsReader()
	if rerr != nil {
		adminui.PageErrorInternal(c, "product", rerr)
		return
	}
	view, err := reader.ProductTranslationsPage(c.Request.Context(), &productdto.ProductTranslationsPageReq{
		ProjectID: projectID,
		ProductID: productID,
		Lang:      strings.TrimSpace(c.Query("lang")),
	})
	if err != nil {
		adminui.PageErrorInternal(c, "product", err)
		return
	}
	// 保存成功的回执不在这条 GET 上（?saved=1&n=N 那条读侧已整批删除）：结论由 JSON 回执
	// 存进 flash 行，刷新后的这一屏只负责渲染当前译文。
	c.HTML(http.StatusOK, productTranslationsTemplate, adminui.Prepare(c, adminui.ViewMap(view)))
}

// SaveProductTranslations POST /api/product/translations/save：保存译文（整表提交）。
//
// 出口是 JSON 回执（页面表单已改由 ui/json_write.js 提交）：
// 成功 → 基座存 flash 并刷新整页（译文状态 / 完成度 / 来源列都会变，只更新一行不够）；
// 失败 → 400 + 逐行结论拼成的一条 message，就地显示在这张表单的结论行里。
//
// 「逐行结论」为什么不进 data：基座只渲染 message。把事实在这里取词拼好，
// 与模板注入的 tf 是同一份实现（adminui.MsgText），两条出口不会给出不同的句子。
func (h *ProductTranslationHandle) SaveProductTranslations(c *gin.Context) {
	req, ok := binding.JSON[productdto.TranslationSaveReq](c)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	productID := strings.TrimSpace(req.ProductID)

	reader, rerr := h.translationsReader()
	if rerr != nil {
		logger.Scene("product").With("project", projectID).With("product", productID).
			Error(rerr, "商品翻译工作台保存前重建数据失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgText(c, adminui.InternalMsg()))
		return
	}
	// 三组并行数组原样带过去：长度对齐与逐行校验都在读模型侧做（那里才有候选原文）。
	view, err := reader.SaveProductTranslations(c.Request.Context(), &productdto.SaveProductTranslationsReq{
		ProjectID: projectID,
		ProductID: productID,
		Lang:      strings.TrimSpace(req.Lang),
		Contexts:  req.Contexts,
		Hashes:    req.Hashes,
		Targets:   req.Targets,
	})
	if err != nil {
		logger.Scene("product").With("project", projectID).With("product", productID).
			Error(err, "商品翻译工作台保存失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgText(c, adminui.InternalMsg()))
		return
	}
	if !view.Saved {
		// 校验没过 / 语言不在启用清单 / 写入失败：三种都由读模型折成逐行（或页面级）事实。
		status := http.StatusBadRequest
		text := productTranslationErrorsText(c, view.Errors)
		if text == "" {
			// 没有逐行事实却也没保存成功 = 读模型侧的意外状态：按内部错误归口，不给空 message。
			status = http.StatusInternalServerError
			text = adminui.MsgText(c, adminui.InternalMsg())
		}
		response.ErrorWithMessage(c, status, text)
		return
	}
	response.SuccessWithMessage(c, adminui.MsgText(c, productTranslationsSavedMsg(c, view.Written)))
}

// productTranslationErrorsText 把逐行错误事实拼成一条可读结论。
//
// 前缀复用页面错误槽的标题词条（同一句话在页面上与回执里长得一样），
// 每行是「字段/语境：原因」，行间用分号 —— message 只有一行，换行在结论行里会挤成空白。
func productTranslationErrorsText(c *gin.Context, errs []productdto.TranslationRowErrorResp) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		text := adminui.MsgText(c, e.Msg)
		if e.Label != "" {
			text = e.Label + "：" + text
		}
		parts = append(parts, text)
	}
	title := adminui.MsgText(c, adminui.Msg{
		Key: productenums.ProductTranslationsErrorsTitle, Fallback: "未保存：请先修正以下问题"})
	return title + "：" + strings.Join(parts, "；")
}

// productTranslationsSavedMsg 译文保存成功的回执（0 条 = 没有需要写入的变化）。
//
// 复用工作台原有的两条词条（ProductTranslationsSavedNote / SavedNone），措辞与改造前一致；
// 出口是 JSON 回执（页面基座把它显示在结论行里，再刷新整页）。
//
// **例外 3（具名占位符句式）**：ProductTranslationsSavedNote 用的是具名占位符 `{n}`
// （i18n.FillTranslate），不是 `%s`；而模板注入的 tf 只有 `%s` 位置参数，一段 key + Args
// 表达不了它。要收成一条 `%s` 词条属于独立的文案迁移（page 模块已按迁移 613 收掉了，
// product 这一条还没做），不夹在「控制器不取词」这一步里做。
func productTranslationsSavedMsg(c *gin.Context, written int) adminui.Msg {
	if written > 0 {
		return adminui.Msg{Fallback: i18n.FillTranslate(shell.TranslateFor(c), productenums.ProductTranslationsSavedNote,
			"已保存 {n} 条译文；译文变更已标记待重建（下次构建生效）。",
			map[string]string{"n": strconv.Itoa(written)})}
	}
	return adminui.Msg{Key: productenums.ProductTranslationsSavedNone, Fallback: "没有需要写入的变化。"}
}
