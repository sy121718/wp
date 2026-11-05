package productenums

import (
	"fmt"
	"net/http"
	"strconv"

	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

// Err 只声明 key 与状态码；文案由词库（sys_i18n）在 JSON 出口统一取词。
func Err(key string) error {
	return facing.WithStatus(facing.New(key), StatusFor(key))
}

func ErrIDsTooMany(err *bulkids.IDsError) error {
	if err == nil {
		return Err(ErrInvalidParam)
	}
	max, count := strconv.Itoa(err.Max), strconv.Itoa(err.Count)
	return facing.WithStatus(facing.New(
		fmt.Sprintf("%s|%s|%s", bulkids.MsgTooMany, max, count)),
		http.StatusBadRequest)
}

const (
	ErrInvalidParam              = "pBadParam"
	ErrBulkRejected              = "pBulkReject"
	ErrInvalidType               = "pBadType"
	ErrInvalidField              = "pBadField"
	ErrNotFound                  = "pNotFound"
	ErrSlugTaken                 = "pSlugTaken"
	ErrSkuTaken                  = "pSkuTaken"
	ErrVariantHasStock           = "pVarHasStock"
	ErrNameRequired              = "pNameRequired"
	ErrCommentPurchaseRequired   = "pReviewBuyReq"
	ErrMissingProjectContext     = "pProjectMissing"
	ErrRelatedInvalid            = "pRelatedInvalid"
	ErrAttrNameRequired          = "pAttrNameReq"
	ErrAttrKeyRequired           = "pAttrKeyReq"
	ErrAttrKeyTaken              = "pAttrKeyTaken"
	ErrAttrNotFound              = "pAttrNotFound"
	ErrAttrProjectMismatch       = "pAttrScope"
	ErrAttrInUse                 = "pAttrInUse"
	ErrAttrValueLabelMissing     = "pAttrLabelReq"
	ErrCategoryNameRequired      = "pCatNameReq"
	ErrCategoryNotFound          = "pCatNotFound"
	ErrCategorySlugTaken         = "pCatSlugTaken"
	ErrCategoryParentMismatch    = "pCatParentScope"
	ErrCategoryCycle             = "pCatCycle"
	ErrCategoryHasChildren       = "pCatHasChildren"
	ErrCategoryInUse             = "pCatInUse"
	ErrCategoryProjectMismatch   = "pCatScope"
	ErrBrandNameRequired         = "pBrandNameReq"
	ErrBrandNotFound             = "pBrandNotFound"
	ErrBrandSlugTaken            = "pBrandSlugTaken"
	ErrBrandInUse                = "pBrandInUse"
	ErrBrandProjectMismatch      = "pBrandScope"
	ErrTagNameRequired           = "pTagNameReq"
	ErrTagNotFound               = "pTagNotFound"
	ErrTagSlugTaken              = "pTagSlugTaken"
	ErrTagProjectMismatch        = "pTagScope"
	ErrTagCrossProject           = "pTagCrossScope"
	ErrTagKindInvalid            = "pTagKindInvalid"
	ErrTagRuleNotAllowed         = "pTagRuleDenied"
	ErrTagRuleTypeInvalid        = "pTagRuleTypeBad"
	ErrTagRuleParamsInvalid      = "pTagRuleArgsBad"
	ErrTagNotManual              = "pTagNotManual"
	ErrCollectionSourceInvalid   = "pCollSourceBad"
	ErrCollectionFilterInvalid   = "pCollFilterBad"
	ErrVariationNoDimension      = "pVarDimMissing"
	ErrVariationDimensionLimit   = "pVarDimLimit"
	ErrVariationCountLimit       = "pVarCountLimit"
	ErrVariationAttributeInvalid = "pVarAttrBad"
	ErrVariationValueInvalid     = "pVarValueBad"
	ErrVariationSelectionEmpty   = "pVarSelectEmpty"
	ErrVariantSKUEmpty           = "pVarSkuEmpty"
	ErrVariantListDuplicated     = "pVarListDup"
	ErrVariantOptionsInvalid     = "pVarOptionsBad"
	ErrVariantStockDuplicated    = "pVarStockDup"
	ErrSkuContainerMissing       = "pSkuBoxMissing"
	ErrContainerSkuInvalid       = "pBoxSkuInvalid"
	ErrContainerSKUTaken         = "pBoxSkuTaken"
	ErrPricingRuleTypeInvalid    = "pPriceRuleBad"
	ErrPricingRuleParamsInvalid  = "pPriceArgsBad"
	ErrPricingRoundingInvalid    = "pPriceRoundBad"
	ErrPricingScopeInvalid       = "pPriceScopeBad"
	ErrPricingTargetRequired     = "pPriceTargetReq"
	ErrPricingTargetNotFound     = "pPriceNotFound"
	ErrPricingFilterEmpty        = "pPriceFilterNil"
	ErrPricingNothingChanged     = "pPriceUnchanged"
	ErrPricingCostMissing        = "pPriceCostMiss"
	ErrPricingOutOfRange         = "pPriceRangeBad"
	ErrPricingTargetChanged      = "pPriceChanged"
	ErrPricingAdjustmentNotFound = "pPriceAdjMiss"
	ErrPricingTargetTooMany      = "pPriceTooMany"
)
