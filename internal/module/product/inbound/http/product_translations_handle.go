package producthttp

import (
	"errors"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 商品译文的页面读取与 JSON 保存入口。
// 页面分别传递工程范围与译文业务对象；保存只消费独立命令结果。

// productTranslationPort 译文读写端口（= service 侧端口的别名；测试注入隔离 schema 的写入器）。
type productTranslationPort = productservice.ProductTranslationStorePort

// ProductTranslationInstancePort 译文变更后需要失效的自动发布实例端口（= service 侧端口别名）。
type ProductTranslationInstancePort = productservice.ProductTranslationInstancePort

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

// translationsReader 取得译文查询与保存契约。
func (h *ProductTranslationHandle) translationsReader() (productcontract.ProductTranslationsReader, error) {
	reader, ok := h.products.(productcontract.ProductTranslationsReader)
	if !ok {
		return nil, errors.New("商品译文工作台读模型未注入（装配缺陷）")
	}
	return reader, nil
}

// ProductTranslations GET /admin/products/translations：商品域翻译工作台。
func (h *ProductTranslationHandle) ProductTranslations(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductTranslationsPageReq](c)
	if !ok {
		return
	}
	h.renderPage(c, req)
}

// renderPage 取这一屏的读模型并渲染（失败走 JSON 出口）。
func (h *ProductTranslationHandle) renderPage(c *gin.Context, req *productdto.ProductTranslationsPageReq) {
	view, err := h.page(c, req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Translations", view)))
}

// SaveProductTranslations POST /api/product/translations/save：保存译文（整表提交）。
//
// 出口是 JSON 回执（页面表单已改由 ui/write-base.js 提交）：成功 → 基座存 flash 并刷新整页；
// 失败 → 逐行结论的 400（响应出口取词一次）。逐行校验、变更判定、写入与待重建标记都由 service 执行。
func (h *ProductTranslationHandle) SaveProductTranslations(c *gin.Context) {
	req, ok := binding.JSON[productdto.SaveProductTranslationsReq](c)
	if !ok {
		return
	}
	h.saveAndReply(c, req)
}

// saveAndReply 保存并回执：成功一句事实文案；失败按「读模型报错 / 逐行拒绝」两档回（取词在出口发生一次）。
func (h *ProductTranslationHandle) saveAndReply(c *gin.Context, req *productdto.SaveProductTranslationsReq) {
	view, err := h.save(c, req)
	if err != nil || !view.Saved {
		replySaveRejected(c, view, err)
		return
	}
	response.SuccessMsg(c, productservice.TranslationsSavedMsg(view.Written, view.StaleMarkFailed))
}

// replySaveRejected 保存失败的两种形状：读模型报错走统一失败出口，逐行拒绝给 400 + 逐行事实。
func replySaveRejected(c *gin.Context, view *productcontract.TranslationSaveResult, err error) {
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	response.ErrorRows(c, http.StatusBadRequest, productservice.TranslationsErrorsTitle(), view.Errors)
}

// page 调用译文业务查询；装配缺失返回错误。
func (h *ProductTranslationHandle) page(c *gin.Context, req *productdto.ProductTranslationsPageReq) (*productcontract.ProductTranslations, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.QueryProductTranslations(c.Request.Context(), req)
}

// save 调用独立的译文保存命令。
func (h *ProductTranslationHandle) save(c *gin.Context, req *productdto.SaveProductTranslationsReq) (*productcontract.TranslationSaveResult, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.SaveProductTranslations(c.Request.Context(), req)
}
