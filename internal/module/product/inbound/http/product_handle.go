package producthttp

// 与商品/变体接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发，key 派生、值的归一与排序一律在 service。

// 四个出口：读配置、存配置、整单校验、SKU 数据源。
// 整单校验在这里是**后端硬校验**：前端片段调的是同一个 service 方法，
// 因此「构造请求绕过前端」得到的结论与前台一致（验收 4）。

// Package producthttp product 模块 HTTP 入口（issue #5 / T3a）。

// 与标签 / 分类接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定、覆盖操作人与转发：规则校验、算价、落库与留痕一律在 service。

// 与分类 / 品牌接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：规则校验、重算时机、引用校验一律在 service。

// 与商品/属性接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：slug 派生、判环、引用校验一律在 service。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/pkg/response"
)

// CreateAttribute 新建属性组（可同时带初始属性值）。
func (h *Handle) CreateAttribute(c *gin.Context) {
	req := &productdto.CreateAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateAttribute 修改属性组本身（值走 SetAttributeValues）。
func (h *Handle) UpdateAttribute(c *gin.Context) {
	req := &productdto.UpdateAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// SetAttributeValues 整体保存属性值（全量替换：请求里没有的值被删除）。
func (h *Handle) SetAttributeValues(c *gin.Context) {
	req := &productdto.SetAttributeValuesReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SetAttributeValues(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetAttribute 属性组详情。
func (h *Handle) GetAttribute(c *gin.Context) {
	req := &productdto.GetAttributeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListAttributes 属性组列表。
func (h *Handle) ListAttributes(c *gin.Context) {
	req := &productdto.ListAttributeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListAttributes(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteAttribute 删除属性组（被商品引用时拒绝）。
func (h *Handle) DeleteAttribute(c *gin.Context) {
	req := &productdto.DeleteAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteAttribute(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// GetBundleConfig 读某商品的捆绑配置（含每项 SKU 快照与真源可用量）。
func (h *Handle) GetBundleConfig(c *gin.Context) {
	req := &productdto.GetBundleConfigReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// SetBundleConfig 保存捆绑配置（整体替换）。
func (h *Handle) SetBundleConfig(c *gin.Context) {
	req := &productdto.SetBundleConfigReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：配置变更要落主数据变更记录，操作人只能来自会话。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.SetBundleConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleSaveSuccess, res)
}

// ValidateBundleSelection 整单硬校验 + 算价（纯读，不写库）。
func (h *Handle) ValidateBundleSelection(c *gin.Context) {
	req := &productdto.ValidateBundleSelectionReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ValidateBundleSelection(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgBundleValidateSuccess, res)
}

// ListBundleSKUs 可挑选的 SKU 清单（跨商品，后台配置器的下拉数据源）。
func (h *Handle) ListBundleSKUs(c *gin.Context) {
	req := &productdto.ListBundleSKUReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListBundleSKUs(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Handle product 接口处理器。
type Handle struct {
	svc productcontract.ProductService
}

// NewHandle 构造。
func NewHandle(svc productcontract.ProductService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建商品（自动生成首个变体）。
func (h *Handle) Create(c *gin.Context) {
	req := &productdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// Update 修改商品。
func (h *Handle) Update(c *gin.Context) {
	req := &productdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// Get 商品详情（含变体与价格区间）。
func (h *Handle) Get(c *gin.Context) {
	req := &productdto.GetReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// List 商品列表。
func (h *Handle) List(c *gin.Context) {
	req := &productdto.ListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Delete 删除商品。
func (h *Handle) Delete(c *gin.Context) {
	req := &productdto.DeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// CreateVariant 新增变体（未填字段由商品级默认值补齐）。
func (h *Handle) CreateVariant(c *gin.Context) {
	req := &productdto.CreateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.CreateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateVariant 修改变体（编辑路径不做默认值填充）。
func (h *Handle) UpdateVariant(c *gin.Context) {
	req := &productdto.UpdateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.UpdateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// DeleteVariant 删除变体。
func (h *Handle) DeleteVariant(c *gin.Context) {
	req := &productdto.DeleteVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.DeleteVariant(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// GenerateVariants 按勾选的属性值生成全部变体组合（issue #8）。
//
// 未勾选任何属性值（或 selections 缺省）= 无表单路径：按商品全部参与变体的
// 属性组与其启用值生成；新变体逐字段继承商品级默认值。
func (h *Handle) GenerateVariants(c *gin.Context) {
	req := &productdto.GenerateVariantsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.GenerateVariants(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgVariantGenerateSuccess, res)
}

// 编辑路径的默认值语义由 service 保证：本层只做绑定与转发，不补字段。
var _ = http.StatusOK

// ListPricingRuleTypes 内置定价规则类型清单（后台下拉与参数说明的唯一来源）。
func (h *Handle) ListPricingRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRuleTypes(c.Request.Context()))
}

// ListPricingRoundingOptions 尾数处理清单。
func (h *Handle) ListPricingRoundingOptions(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRoundingOptions(c.Request.Context()))
}

// PreviewPricing 按规则试算（不落库、不留痕）。
func (h *Handle) PreviewPricing(c *gin.Context) {
	req := &productdto.PricingPreviewReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingPreviewSuccess, res)
}

// ApplyPricing 应用调价（算价 → 写回 product_variants.price → 留痕）。
func (h *Handle) ApplyPricing(c *gin.Context) {
	req := &productdto.PricingApplyReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	// 操作人取自会话，客户端传什么都不作数（留痕的操作人不可伪造）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.ApplyPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
}

// ListPriceAdjustments 调价留痕列表（按工程）。
func (h *Handle) ListPriceAdjustments(c *gin.Context) {
	req := &productdto.ListPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListPriceAdjustments(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// GetPriceAdjustment 单批次留痕详情（含逐变体「原价 → 新价」明细）。
func (h *Handle) GetPriceAdjustment(c *gin.Context) {
	req := &productdto.GetPriceAdjustmentReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetPriceAdjustment(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// operatorFromContext 从会话取操作人（issue #13 定价留痕 / issue #19 变更记录共用）。
//
// 优先取登录名：留痕与变更记录都要能直接读懂「谁改的」（与库存流水的 operator_id 同口径，
// 后台页面路径也一直是用登录名）。登录名缺失（脚本 / 测试路径）时退回数值 id ——
// 会话中间件写入的 user_id 是 int64，这里只做展示用的文本化，不参与任何鉴权判断。
// 两者都没有时返回空串：留痕字段允许为空。
//
// 不走 shell.CurrentUserIDText：要保留「先登录名、后 id 文本」的组合语义，且非 int64
// 原始值分支要照旧（脚本 / 测试路径可能写入别的形状），shell 入口只认 int64。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}

// CreateTag 新建标签（kind=manual 手工 / kind=rule 自动并带内置规则与参数）。
func (h *Handle) CreateTag(c *gin.Context) {
	req := &productdto.CreateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateTag 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *Handle) UpdateTag(c *gin.Context) {
	req := &productdto.UpdateTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetTag 标签详情（含命中商品列表）。
func (h *Handle) GetTag(c *gin.Context) {
	req := &productdto.GetTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetTag(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListTags 标签列表（工程内；每个标签带当前归属数量）。
func (h *Handle) ListTags(c *gin.Context) {
	req := &productdto.ListTagReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagProducts 某标签命中的商品（验收 4）。
func (h *Handle) ListTagProducts(c *gin.Context) {
	req := &productdto.ListTagProductsReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListTagProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// ListTagRuleTypes 内置规则类型清单（后台规则下拉的唯一来源）。
func (h *Handle) ListTagRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListTagRuleTypes(c.Request.Context()))
}

// DeleteTag 删除标签（连同它在商品上的引用一起解绑）。
func (h *Handle) DeleteTag(c *gin.Context) {
	req := &productdto.DeleteTagReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// RecalcTags 手动触发重算（重算时机之一：可按标签或按工程整体重算）。
func (h *Handle) RecalcTags(c *gin.Context) {
	req := &productdto.RecalcTagsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RecalcTags(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// CreateCategory 新建分类（parentId 为空即顶级）。
func (h *Handle) CreateCategory(c *gin.Context) {
	req := &productdto.CreateCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateCategory 修改分类（改名 / 换父级 / 排序 / SEO 字段）。
func (h *Handle) UpdateCategory(c *gin.Context) {
	req := &productdto.UpdateCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetCategory 分类详情。
func (h *Handle) GetCategory(c *gin.Context) {
	req := &productdto.GetCategoryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetCategory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListCategories 分类树。
func (h *Handle) ListCategories(c *gin.Context) {
	req := &productdto.ListCategoryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListCategories(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteCategory 删除分类（有子级或被商品引用时拒绝）。
func (h *Handle) DeleteCategory(c *gin.Context) {
	req := &productdto.DeleteCategoryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// CreateBrand 新建品牌。
func (h *Handle) CreateBrand(c *gin.Context) {
	req := &productdto.CreateBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateBrand 修改品牌。
func (h *Handle) UpdateBrand(c *gin.Context) {
	req := &productdto.UpdateBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetBrand 品牌详情。
func (h *Handle) GetBrand(c *gin.Context) {
	req := &productdto.GetBrandReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBrand(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListBrands 品牌列表。
func (h *Handle) ListBrands(c *gin.Context) {
	req := &productdto.ListBrandReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListBrands(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteBrand 删除品牌（被商品引用时拒绝）。
func (h *Handle) DeleteBrand(c *gin.Context) {
	req := &productdto.DeleteBrandReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteBrand(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}
