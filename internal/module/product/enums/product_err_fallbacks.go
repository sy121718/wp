package productenums

// 错误 key、中文兜底和状态码均在 enums 声明。响应出口负责取词与 JSON 协议，
// HTTP 层不再维护另一份错误名单或状态映射；内部诊断不进入对外文案。

import (
	"fmt"
	"net/http"
	"strconv"

	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

// errorDefinition 将每项错误的兜底与可选状态码放在同一声明，避免两张表漂移。
type errorDefinition struct {
	fallback string
	status   int
}

var errFallbacks = map[string]errorDefinition{
	ErrDetailTemplateNameRequired: {fallback: "模板名不能为空"},
	ErrDetailTemplatePathRequired: {fallback: "请填写新的访问路径。"},
	ErrDetailTemplateUnavailable:  {fallback: "详情页模板能力尚未就绪，请稍后重试。", status: http.StatusInternalServerError},
	ErrDetailTemplatePreviewEmpty: {fallback: "预览未生成有效内容，请检查模板后重试。", status: http.StatusInternalServerError},
	ErrInvalidParam:               {fallback: "请求参数无效"},
	ErrBulkRejected:               {fallback: "批量操作未执行：其中有条目不能处理（明细见 data.items）。", status: http.StatusConflict},
	ErrNotFound:                   {fallback: "商品或变体不存在", status: http.StatusNotFound},
	ErrSlugTaken:                  {fallback: "该 URL 段在本工程已被占用", status: http.StatusConflict},
	ErrSkuTaken:                   {fallback: "该 SKU 编码在本商品下已被占用", status: http.StatusConflict},
	ErrNameRequired:               {fallback: "商品名称必填"},
	ErrAttrNotFound:               {fallback: "属性组不存在", status: http.StatusNotFound},
	ErrAttrProjectMismatch:        {fallback: "属性组不属于该商品所在工程"},
	ErrSkuContainerMissing:        {fallback: "SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码"},
	ErrContainerSkuInvalid:        {fallback: "主体 SKU 不合法：必须是非空字符串"},
	ErrContainerSKUTaken:          {fallback: "该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）", status: http.StatusConflict},
	ErrVariantSKUEmpty:            {fallback: "变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）"},
	ErrVariantListDuplicated:      {fallback: "变体清单包含重复规格，请移除重复行后重新保存", status: http.StatusConflict},
	ErrVariantOptionsInvalid:      {fallback: "变体的规格组合不合法：属性组或属性值不属于该商品"},
	ErrVariantStockDuplicated:     {fallback: "同一个仓库里同一个 SKU 只能出现一次：请把它合并成一行再保存"},
	ErrVariantHasStock:            {fallback: "变体仍有库存，不能删除（请改为停用）", status: http.StatusConflict},
	ErrPricingRuleTypeInvalid:     {fallback: "定价规则类型不是内置类型"},
	ErrPricingRuleParamsInvalid:   {fallback: "定价规则参数不合法（参数键 / 类型 / 取值范围）"},
	ErrPricingRoundingInvalid:     {fallback: "尾数处理不是内置选项"},
	ErrPricingScopeInvalid:        {fallback: "定价作用范围不合法"},
	ErrPricingTargetRequired:      {fallback: "该作用范围必须指定目标（变体 id 或商品 id）"},
	ErrPricingTargetNotFound:      {fallback: "商品或变体不存在、没有可改价的变体（捆绑容器只有容器价，不能按规则改价）"},
	ErrPricingFilterEmpty:         {fallback: "筛选集至少要给一个筛选条件"},
	ErrPricingCostMissing:         {fallback: "未填成本价，按成本类规则无法计算", status: http.StatusConflict},
	ErrPricingTargetChanged:       {fallback: "定价范围已发生变化，请重新预览后再应用", status: http.StatusConflict},
	ErrPricingOutOfRange:          {fallback: "计算结果为负或超过 9999999999.99", status: http.StatusConflict},
	ErrPricingNothingChanged:      {fallback: "按该规则算下来没有任何价格变化"},
	ErrPricingTargetTooMany:       {fallback: "命中的商品数超过单批上限，请收紧筛选条件"},
	ErrPricingAdjustmentNotFound:  {fallback: "调价批次不存在", status: http.StatusNotFound},
	ErrAttrInUse:                  {fallback: "属性组已被商品引用，不能删除", status: http.StatusConflict},
	ErrAttrKeyRequired:            {fallback: "属性组标识不能改为空"},
	ErrAttrKeyTaken:               {fallback: "该属性组标识在本工程已被占用", status: http.StatusConflict},
	ErrAttrNameRequired:           {fallback: "属性组名称必填"},
	ErrBrandInUse:                 {fallback: "品牌已被商品引用，不能删除", status: http.StatusConflict},
	ErrBrandNameRequired:          {fallback: "品牌名称必填"},
	ErrBrandNotFound:              {fallback: "品牌不存在", status: http.StatusNotFound},
	ErrBrandProjectMismatch:       {fallback: "品牌不属于该商品所在工程"},
	ErrBrandSlugTaken:             {fallback: "该品牌 URL 段在本工程已被占用", status: http.StatusConflict},
	ErrCategoryCycle:              {fallback: "不能把分类挂到自身或自己的后代下"},
	ErrCategoryHasChildren:        {fallback: "分类仍有子级，不能删除", status: http.StatusConflict},
	ErrCategoryInUse:              {fallback: "分类已被商品引用，不能删除", status: http.StatusConflict},
	ErrCategoryNameRequired:       {fallback: "分类名称必填"},
	ErrCategoryNotFound:           {fallback: "分类不存在", status: http.StatusNotFound},
	ErrCategoryParentMismatch:     {fallback: "父分类不属于该分类所在工程"},
	ErrCategoryProjectMismatch:    {fallback: "分类不属于该商品所在工程"},
	ErrCategorySlugTaken:          {fallback: "该分类 URL 段在本工程已被占用", status: http.StatusConflict},
	ErrCollectionFilterInvalid:    {fallback: "过滤维度不在集合源白名单内"},
	ErrCollectionSourceInvalid:    {fallback: "集合源标识不合法（不是本模块实现的源）"},
	ErrInvalidField:               {fallback: "提交了不支持的字段（不在商品字段白名单内）"},
	ErrInvalidType:                {fallback: "实体类型不合法"},
	ErrRelatedInvalid:             {fallback: "相关商品引用不合法：引用的商品必须存在、属于本工程，且不能指向自己"},
	ErrTagCrossProject:            {fallback: "标签仍被其它工程的商品引用，不能删除", status: http.StatusConflict},
	ErrTagKindInvalid:             {fallback: "标签类型不是 manual / rule"},
	ErrTagNameRequired:            {fallback: "标签名称必填"},
	ErrTagNotFound:                {fallback: "标签不存在", status: http.StatusNotFound},
	ErrTagNotManual:               {fallback: "自动标签的归属由重算维护，不能手工挂载", status: http.StatusConflict},
	ErrTagProjectMismatch:         {fallback: "标签不属于该商品所在工程"},
	ErrTagRuleNotAllowed:          {fallback: "手工标签不能带自动规则"},
	ErrTagRuleParamsInvalid:       {fallback: "规则参数不合法（键 / 类型 / 取值范围）"},
	ErrTagRuleTypeInvalid:         {fallback: "规则类型不是内置类型（不接受自由表达式）"},
	ErrTagSlugTaken:               {fallback: "该标签 URL 段在本工程已被占用", status: http.StatusConflict},
	ErrVariationAttributeInvalid:  {fallback: "勾选的属性组未参与该商品的变体"},
	ErrVariationCountLimit:        {fallback: "变体组合数超过上限"},
	ErrVariationDimensionLimit:    {fallback: "参与变体的属性维度超过上限"},
	ErrVariationNoDimension:       {fallback: "没有可参与变体的属性值"},
	ErrVariationValueInvalid:      {fallback: "勾选的属性值不属于该属性组或已停用"},
	ErrVariationSelectionEmpty:    {fallback: "未勾选任何属性值：请至少勾选一个属性值，或改用「生成全部组合」"},

	ErrAttrValueLabelMissing:   {fallback: "属性值名称必填"},                            // 迁移 430_i18n_enums_missing_keys.sql
	ErrMissingProjectContext:   {fallback: "构建期拿不到工程上下文，无法读取实体字段（属装配缺陷，请联系管理员）"}, // 迁移 430
	ErrCommentPurchaseRequired: {fallback: "购买过该商品才能发表评论"},                       // 迁移 468_product_comment_policy_i18n.sql
}

// FallbackFor 返回声明处的中文兜底；未知 key 返回空串。
func FallbackFor(key string) string {
	return errFallbacks[key].fallback
}

// Err 构造包含 key、兜底与可选状态码的业务错误。
func Err(key string) error {
	definition := errFallbacks[key]
	return facing.WithStatus(facing.New(key, definition.fallback), definition.status)
}

// ErrIDsTooMany 批量 id 超过上限（service 归一时产生）。
//
// 为什么不用 Err(key)：这条提示带两个数字（上限与本次条数），而这两个只有 *bulkids.IDsError
// 知道（Count 是**去重后**的条数，拿上限重算会算错）。key 走参数化协议
// `shell.err.bulkIdsTooMany|上限|条数`，出口按请求语言把 %s 填进去。
//
// 兜底是**填好数字的中文成品句**，不是模板：出口的 businessText 先取译文（词条见迁移 277，
// 中英各一行），只有译文拿不到时才用兜底 —— 于是「i18n 未初始化 / 缺词条」的环境里显示的
// 仍是可行动的一句话，而不是 key 原文。
func ErrIDsTooMany(err *bulkids.IDsError) error {
	if err == nil {
		return Err(ErrInvalidParam)
	}
	max, count := strconv.Itoa(err.Max), strconv.Itoa(err.Count)
	return facing.WithStatus(facing.New(
		fmt.Sprintf("%s|%s|%s", bulkids.MsgTooMany, max, count),
		fmt.Sprintf(bulkids.TooManyTemplate, max, count)),
		http.StatusBadRequest)
}

// 业务错误 key；取词、兜底与状态码均由本文件声明。
const (
	ErrInvalidParam              = "ErrInvalidParam"
	ErrBulkRejected              = "ErrBulkRejected"
	ErrInvalidType               = "ErrInvalidType"
	ErrInvalidField              = "ErrInvalidField"
	ErrNotFound                  = "ErrNotFound"
	ErrSlugTaken                 = "ErrSlugTaken"
	ErrSkuTaken                  = "ErrSkuTaken"
	ErrVariantHasStock           = "ErrVariantHasStock"
	ErrNameRequired              = "ErrNameRequired"
	ErrCommentPurchaseRequired   = "product.err.commentPurchaseRequired"
	ErrMissingProjectContext     = "ErrMissingProjectContext"
	ErrRelatedInvalid            = "ErrRelatedInvalid"
	ErrAttrNameRequired          = "ErrAttrNameRequired"
	ErrAttrKeyRequired           = "ErrAttrKeyRequired"
	ErrAttrKeyTaken              = "ErrAttrKeyTaken"
	ErrAttrNotFound              = "ErrAttrNotFound"
	ErrAttrProjectMismatch       = "ErrAttrProjectMismatch"
	ErrAttrInUse                 = "ErrAttrInUse"
	ErrAttrValueLabelMissing     = "ErrAttrValueLabelMissing"
	ErrCategoryNameRequired      = "ErrCategoryNameRequired"
	ErrCategoryNotFound          = "ErrCategoryNotFound"
	ErrCategorySlugTaken         = "ErrCategorySlugTaken"
	ErrCategoryParentMismatch    = "ErrCategoryParentMismatch"
	ErrCategoryCycle             = "ErrCategoryCycle"
	ErrCategoryHasChildren       = "ErrCategoryHasChildren"
	ErrCategoryInUse             = "ErrCategoryInUse"
	ErrCategoryProjectMismatch   = "ErrCategoryProjectMismatch"
	ErrBrandNameRequired         = "ErrBrandNameRequired"
	ErrBrandNotFound             = "ErrBrandNotFound"
	ErrBrandSlugTaken            = "ErrBrandSlugTaken"
	ErrBrandInUse                = "ErrBrandInUse"
	ErrBrandProjectMismatch      = "ErrBrandProjectMismatch"
	ErrTagNameRequired           = "ErrTagNameRequired"
	ErrTagNotFound               = "ErrTagNotFound"
	ErrTagSlugTaken              = "ErrTagSlugTaken"
	ErrTagProjectMismatch        = "ErrTagProjectMismatch"
	ErrTagCrossProject           = "ErrTagCrossProject"
	ErrTagKindInvalid            = "ErrTagKindInvalid"
	ErrTagRuleNotAllowed         = "ErrTagRuleNotAllowed"
	ErrTagRuleTypeInvalid        = "ErrTagRuleTypeInvalid"
	ErrTagRuleParamsInvalid      = "ErrTagRuleParamsInvalid"
	ErrTagNotManual              = "ErrTagNotManual"
	ErrCollectionSourceInvalid   = "ErrCollectionSourceInvalid"
	ErrCollectionFilterInvalid   = "ErrCollectionFilterInvalid"
	ErrVariationNoDimension      = "ErrVariationNoDimension"
	ErrVariationDimensionLimit   = "ErrVariationDimensionLimit"
	ErrVariationCountLimit       = "ErrVariationCountLimit"
	ErrVariationAttributeInvalid = "ErrVariationAttributeInvalid"
	ErrVariationValueInvalid     = "ErrVariationValueInvalid"
	ErrVariationSelectionEmpty   = "ErrVariationSelectionEmpty"
	ErrVariantSKUEmpty           = "ErrVariantSKUEmpty"
	ErrVariantListDuplicated     = "ErrVariantListDuplicated"
	ErrVariantOptionsInvalid     = "ErrVariantOptionsInvalid"
	ErrVariantStockDuplicated    = "ErrVariantStockDuplicated"
	ErrSkuContainerMissing       = "ErrSkuContainerMissing"
	ErrContainerSkuInvalid       = "ErrContainerSkuInvalid"
	ErrContainerSKUTaken         = "ErrContainerSKUTaken"
	ErrPricingRuleTypeInvalid    = "ErrPricingRuleTypeInvalid"
	ErrPricingRuleParamsInvalid  = "ErrPricingRuleParamsInvalid"
	ErrPricingRoundingInvalid    = "ErrPricingRoundingInvalid"
	ErrPricingScopeInvalid       = "ErrPricingScopeInvalid"
	ErrPricingTargetRequired     = "ErrPricingTargetRequired"
	ErrPricingTargetNotFound     = "ErrPricingTargetNotFound"
	ErrPricingFilterEmpty        = "ErrPricingFilterEmpty"
	ErrPricingNothingChanged     = "ErrPricingNothingChanged"
	ErrPricingCostMissing        = "ErrPricingCostMissing"
	ErrPricingOutOfRange         = "ErrPricingOutOfRange"
	ErrPricingTargetChanged      = "ErrPricingTargetChanged"
	ErrPricingAdjustmentNotFound = "ErrPricingAdjustmentNotFound"
	ErrPricingTargetTooMany      = "ErrPricingTargetTooMany"
)
