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

// product_translations_handle.go — 商品译文工作台：候选收集、逐行校验、保存与「按依赖标记待重建」。
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
	pageview.Render(c, view)
}

// SaveProductTranslations POST /api/product/translations/save：保存译文（整表提交）。
//
// 出口是 JSON 回执（页面表单已改由 ui/json_write.js 提交）：成功 → 基座存 flash 并刷新整页；
// 失败 → 逐行结论的 400（响应出口取词一次）。逐行校验、变更判定、写入与待重建标记都在读模型。
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
	response.SuccessMsg(c, view.SaveMessage)
}

// replySaveRejected 保存失败的两种形状：读模型报错走统一失败出口，逐行拒绝给 400 + 逐行事实。
func replySaveRejected(c *gin.Context, view *productdto.ProductTranslationsPageResp, err error) {
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	response.ErrorRows(c, http.StatusBadRequest, productservice.TranslationsErrorsTitle(), view.SaveRows)
}

// page 取这一屏的读模型并调它（装配缺陷在这里变成错误，不是请求期的类型断言）。
func (h *ProductTranslationHandle) page(c *gin.Context, req *productdto.ProductTranslationsPageReq) (*productdto.ProductTranslationsPageResp, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.ProductTranslationsPage(c.Request.Context(), req)
}

// save 同 page，作用于保存编排。
func (h *ProductTranslationHandle) save(c *gin.Context, req *productdto.SaveProductTranslationsReq) (*productdto.ProductTranslationsPageResp, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.SaveProductTranslations(c.Request.Context(), req)
}
