package productenums

import "net/http"

// StatusFor 是错误声明的一部分：只写状态码，文案由词库在 JSON 出口统一取词。
func StatusFor(key string) int {
	switch key {
	case ErrNotFound, ErrAttrNotFound, ErrBrandNotFound, ErrCategoryNotFound, ErrTagNotFound,
		ErrPricingAdjustmentNotFound:
		return http.StatusNotFound

	case ErrDetailTemplateUnavailable, ErrDetailTemplatePreviewEmpty:
		return http.StatusInternalServerError

	case ErrBulkRejected, ErrSlugTaken, ErrSkuTaken, ErrContainerSKUTaken, ErrVariantListDuplicated,
		ErrVariantHasStock, ErrPricingCostMissing, ErrPricingTargetChanged, ErrPricingOutOfRange,
		ErrAttrInUse, ErrAttrKeyTaken, ErrBrandInUse, ErrBrandSlugTaken, ErrCategoryHasChildren,
		ErrCategoryInUse, ErrCategorySlugTaken, ErrTagCrossProject, ErrTagNotManual, ErrTagSlugTaken:
		return http.StatusConflict
	}
	return 0
}
