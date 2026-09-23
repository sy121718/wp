package migrations

import "sync"

// register_enums_missing_keys_i18n.go — enums 常量里「值即 key、库里却没有词条」的那一批（430）。
//
// 见 430_i18n_enums_missing_keys.sql 的头部。病根与 429 同源：模块 enums 是**两处独立登记**
// （Go 常量 + sys_i18n 词条），漏了后一处不会有任何报错或门禁变红 —— 取词函数按 fallback
// 回落，而 enums 这条链的 fallback 恰好就是 key 本身，于是把内部常量名摆到了运营面前
// （`ErrAttachmentReferenced`、`MsgTranslationDocInvalid` 这种英文裸串）。
//
// 判据是**读值不读名**（同一包里 key 形态与中文常量形态并存）：全部 728 个常量按值筛出
// 语言 key 后，剔掉 webhook 的 4 个事件类型标识（order.paid / order.refunded /
// product.updated / content.published 是 EventType 常量、点分二段、不进 err|msg 命名空间、
// 不进取词链），再与库里对账，以**库里的实际状态**为准：
//
//	zh-CN 与 en-US 都有 361 · 只有 zh-CN 99 · 完全没有 119
//
// 本批补后两类共 218 个 key / 337 行：
//
//	「完全没有」119 个 × 2 语言 = 238 行（category 取模块名、http_code 取实际语义）
//	「只有 zh-CN」 99 个 × 1 语言 =  99 行（en-US 沿用该 key 既有 zh-CN 行的 category / http_code，
//	                                        同一 key 两行必须一致 —— loader 装载时 http_code 取首个非零值）
//
// 注册方式：本文件自带 init()（与 register_facing_missing_keys_i18n.go 同形），
// 不改 register.go —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerEnumsMissingKeysI18n() {
	registerEnumsMissingKeysI18nOnce.Do(registerEnumsMissingKeysI18nSeed)
}

// registerEnumsMissingKeysI18nOnce 让重复调用成为空操作。
var registerEnumsMissingKeysI18nOnce sync.Once

// registerEnumsMissingKeysI18nSeed 注册 430（真正干活的那一半）。
func registerEnumsMissingKeysI18nSeed() {
	// 门槛 = 本批 218 个 key 的 **en-US** 行都在（= 218 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// key 列表**全量枚举**而不是挑几个代表：本批的 119 个「完全没有」的 key 里，任何一个
	// 被别的批次补上都会让计数偏小 —— 只有当 218 个全在时计数才达标，而那只可能是本批跑过。
	// 挑子集则相反：子集被别处补上就会误判「本批已完成」。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行（? 被换成表名），让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "430-i18n-enums-missing-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 218 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			// 按模块分组，便于核对「本批覆盖了哪些域」。
			// admin（4 个）
			"'ErrMenuDepthExceeded','ErrMenuParentMustBeDir','ErrMenuParentNotFound','ErrSuperAdminOnly'," +
			// analytics（2 个）
			"'ErrInvalidParam','MsgCollectAccepted'," +
			// artifact（7 个）
			"'ErrArtifactMismatch','ErrArtifactNotFound','ErrContentObjectDeleteNotApplied'," +
			"'ErrContentRefQueryFailed','ErrInvalidArtifact','MsgArtifactFound','MsgArtifactSaved'," +
			// block（11 个）
			"'ErrBlockDuplicate','ErrBlockInUse','ErrBlockInvalidCategory','ErrBlockInvalidDoc'," +
			"'ErrBlockInvalidKind','ErrBlockInvalidReuseMode','ErrBlockNameRequired','ErrBlockParamRequired'," +
			"'ErrProjectNotFound','MsgBlockCloned','MsgBlockTitle'," +
			// blueprint（8 个）
			"'ErrDataInvalid','ErrInvalidKind','MsgCreateSuccess','MsgDeleteSuccess'," +
			"'MsgDetailSuccess','MsgListSuccess','MsgPublishSuccess','MsgUpdateSuccess'," +
			// build（5 个）
			"'ErrExecutorMissing','ErrInvalidSource','ErrJobNotFound','MsgJobRetried','MsgQueueFound'," +
			// content（5 个）
			"'ErrCollectionUnsupported','ErrInvalidField','ErrInvalidType','ErrSlugTaken'," +
			"'MsgCollectionsSuccess'," +
			// contenttemplate（3 个）
			"'ErrFieldBindingInvalid','ErrStructureTemplateInUse','ErrTemplateInUse'," +
			// media（13 个）
			"'ErrAttachmentNotFound','ErrAttachmentNotImage','ErrAttachmentReferenced'," +
			"'ErrDownloadEmpty','ErrDownloadFailed','ErrDownloadStorageNotLocal'," +
			"'ErrReplaceExtMismatch','ErrReplaceFailed','ErrUploadEmpty','ErrUploadFailed'," +
			"'ErrVariantStorageNotLocal','MsgBadRequest','MsgSuccess'," +
			// navigation（2 个）
			"'ErrInvalidTarget','ErrPathTaken'," +
			// page（39 个）
			"'ErrBlueprintInvalid','ErrBlueprintUnavailable','ErrDraftVersionConflict'," +
			"'ErrInvalidDocument','ErrInvalidPath','ErrNoStagedArtifact','ErrPageNotFound'," +
			"'ErrPathOccupied','ErrRebuildRequired','ErrRedirectLoop','ErrRedirectNotFound'," +
			"'ErrRedirectOccupied','ErrRedirectTargetMiss','ErrRedirectUnavailable'," +
			"'ErrRollbackTargetMiss','MsgBuildReady','MsgDraftSaved','MsgFieldRequired'," +
			"'MsgPageCreated','MsgPageDeleted','MsgPageDetail','MsgPageTranslationsTitle'," +
			"'MsgPagesTitle','MsgPublished','MsgRedirectCreated','MsgRedirectDeleted'," +
			"'MsgRedirectMerged','MsgRevisionsListed','MsgRollbackDone','MsgSiteSlotBound'," +
			"'MsgSiteSlotUnbound','MsgTranslationDocInvalid','MsgTranslationInvalid'," +
			"'MsgTranslationLangInvalid','MsgTranslationSaveFailed','MsgTranslationSiteScanSkipped'," +
			"'MsgTranslationSiteScanTooMany','MsgTranslationStale','MsgURLUpdated'," +
			// plugin（12 个）
			"'ErrInstallParse','ErrMigrationFailed','ErrPluginNotFound','ErrStorageFailure'," +
			"'ErrToggleFailed','ErrUninstallFailed','ErrUnsafePackage','ErrVersionRegress'," +
			"'MsgInstallFailed','MsgInstallSuccess','MsgToggleSuccess','MsgUninstallSuccess'," +
			// presentation（9 个）
			"'ErrBuildFailed','ErrEntityMissing','ErrNoTemplate','ErrRegistryMissing'," +
			"'ErrSamePath','ErrTemplateTypeMismatch','MsgPreviewSuccess','MsgRebuildSuccess'," +
			"'MsgUpdateURLSuccess'," +
			// product（79 个）
			"'ErrAttrInUse','ErrAttrKeyRequired','ErrAttrKeyTaken','ErrAttrNameRequired'," +
			"'ErrAttrNotFound','ErrAttrProjectMismatch','ErrAttrValueLabelMissing','ErrBrandInUse'," +
			"'ErrBrandNameRequired','ErrBrandNotFound','ErrBrandProjectMismatch','ErrBrandSlugTaken'," +
			"'ErrBundleMaxOptionsInvalid','ErrBundleNotConfigured','ErrBundleOptionRequired'," +
			"'ErrBundleOptionsExceeded','ErrBundleQtyAboveMax','ErrBundleQtyAboveStock'," +
			"'ErrBundleQtyBelowMin','ErrBundleQtyInvalid','ErrBundleQtyRangeInvalid'," +
			"'ErrBundleSelfReference','ErrBundleShapeInvalid','ErrBundleStockUnavailable'," +
			"'ErrBundleTotalAboveMax','ErrBundleTotalBelowMin','ErrBundleTotalRangeInvalid'," +
			"'ErrBundleTotalUnreachable','ErrBundleVariantDuplicated','ErrBundleVariantNotFound'," +
			"'ErrBundleVariantNotInConfig','ErrBundleVariantProjectMismatch','ErrBundleVariantRequired'," +
			"'ErrCategoryCycle','ErrCategoryHasChildren','ErrCategoryInUse','ErrCategoryNameRequired'," +
			"'ErrCategoryNotFound','ErrCategoryParentMismatch','ErrCategoryProjectMismatch'," +
			"'ErrCategorySlugTaken','ErrCollectionFilterInvalid','ErrCollectionSourceInvalid'," +
			"'ErrMissingProjectContext','ErrNameRequired','ErrPricingAdjustmentNotFound'," +
			"'ErrPricingFilterEmpty','ErrPricingNothingChanged','ErrPricingRoundingInvalid'," +
			"'ErrPricingRuleParamsInvalid','ErrPricingRuleTypeInvalid','ErrPricingScopeInvalid'," +
			"'ErrPricingTargetNotFound','ErrPricingTargetRequired','ErrPricingTargetTooMany'," +
			"'ErrSkuTaken','ErrTagKindInvalid','ErrTagNameRequired','ErrTagNotFound'," +
			"'ErrTagNotManual','ErrTagProjectMismatch','ErrTagRuleNotAllowed','ErrTagRuleParamsInvalid'," +
			"'ErrTagRuleTypeInvalid','ErrTagSlugTaken','ErrVariantHasStock'," +
			"'ErrVariationAttributeInvalid','ErrVariationCountLimit','ErrVariationDimensionLimit'," +
			"'ErrVariationNoDimension','ErrVariationSelectionEmpty','ErrVariationValueInvalid'," +
			"'MsgBundleSaveSuccess','MsgBundleValidateSuccess','MsgPricingApplySuccess'," +
			"'MsgPricingPreviewSuccess','MsgVariantGenerateSuccess','PricingSkipCostMissing'," +
			"'PricingSkipOutOfRange'," +
			// project（12 个）
			"'ErrInvalidName','ErrInvalidSettings','ErrInvalidThemeSettings','ErrProjectInternal'," +
			"'ErrThemeDuplicateName','ErrThemeInternal','ErrThemeIsActive','ErrThemeNameRequired'," +
			"'ErrThemeNotFound','ErrThemeProjectIDEmpty','MsgProjectCreated','MsgProjectUpdated'," +
			// publication（6 个）
			"'ErrReceiptPending','ErrRouteActiveRename','ErrRouteNotFound','ErrRouteOccupied'," +
			"'MsgDeactivated','MsgRolledBack'," +
			// workbench（1 个）
			"'MsgDashboardTitle')",
		SQL: mustSQL("430_i18n_enums_missing_keys.sql"),
	})
}

func init() { registerEnumsMissingKeysI18n() }
