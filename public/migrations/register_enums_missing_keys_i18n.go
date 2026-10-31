package migrations

import "sync"

func registerEnumsMissingKeysI18n() {
	registerEnumsMissingKeysI18nOnce.Do(registerEnumsMissingKeysI18nSeed)
}

var registerEnumsMissingKeysI18nOnce sync.Once

func registerEnumsMissingKeysI18nSeed() {

	registerSeed(Seed{
		Version:   "430-i18n-enums-missing-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 195 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +

			"'ErrMenuDepthExceeded','ErrMenuParentMustBeDir','ErrMenuParentNotFound','ErrSuperAdminOnly'," +

			"'ErrInvalidParam','MsgCollectAccepted'," +

			"'ErrArtifactMismatch','ErrArtifactNotFound','ErrContentObjectDeleteNotApplied'," +
			"'ErrContentRefQueryFailed','ErrInvalidArtifact','MsgArtifactFound','MsgArtifactSaved'," +

			"'ErrBlockDuplicate','ErrBlockInUse','ErrBlockInvalidCategory','ErrBlockInvalidDoc'," +
			"'ErrBlockInvalidKind','ErrBlockInvalidReuseMode','ErrBlockNameRequired','ErrBlockParamRequired'," +
			"'ErrProjectNotFound','MsgBlockCloned','MsgBlockTitle'," +

			"'ErrDataInvalid','ErrInvalidKind','MsgCreateSuccess','MsgDeleteSuccess'," +
			"'MsgDetailSuccess','MsgListSuccess','MsgPublishSuccess','MsgUpdateSuccess'," +

			"'ErrExecutorMissing','ErrInvalidSource','ErrJobNotFound','MsgJobRetried','MsgQueueFound'," +

			"'ErrCollectionUnsupported','ErrInvalidField','ErrInvalidType','ErrSlugTaken'," +
			"'MsgCollectionsSuccess'," +

			"'ErrFieldBindingInvalid','ErrStructureTemplateInUse','ErrTemplateInUse'," +

			"'ErrAttachmentNotFound','ErrAttachmentNotImage','ErrAttachmentReferenced'," +
			"'ErrDownloadEmpty','ErrDownloadFailed','ErrDownloadStorageNotLocal'," +
			"'ErrReplaceExtMismatch','ErrReplaceFailed','ErrUploadEmpty','ErrUploadFailed'," +
			"'ErrVariantStorageNotLocal','MsgBadRequest','MsgSuccess'," +

			"'ErrInvalidTarget','ErrPathTaken'," +

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

			"'ErrInstallParse','ErrMigrationFailed','ErrPluginNotFound','ErrStorageFailure'," +
			"'ErrToggleFailed','ErrUninstallFailed','ErrUnsafePackage','ErrVersionRegress'," +
			"'MsgInstallFailed','MsgInstallSuccess','MsgToggleSuccess','MsgUninstallSuccess'," +

			"'ErrBuildFailed','ErrEntityMissing','ErrNoTemplate','ErrRegistryMissing'," +
			"'ErrSamePath','ErrTemplateTypeMismatch','MsgPreviewSuccess','MsgRebuildSuccess'," +
			"'MsgUpdateURLSuccess'," +

			"'ErrAttrInUse','ErrAttrKeyRequired','ErrAttrKeyTaken','ErrAttrNameRequired'," +
			"'ErrAttrNotFound','ErrAttrProjectMismatch','ErrAttrValueLabelMissing','ErrBrandInUse'," +
			"'ErrBrandNameRequired','ErrBrandNotFound','ErrBrandProjectMismatch','ErrBrandSlugTaken'," +
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
			"'MsgPricingApplySuccess'," +
			"'MsgPricingPreviewSuccess','MsgVariantGenerateSuccess','PricingSkipCostMissing'," +
			"'PricingSkipOutOfRange'," +

			"'ErrInvalidName','ErrInvalidSettings','ErrInvalidThemeSettings','ErrProjectInternal'," +
			"'ErrThemeDuplicateName','ErrThemeInternal','ErrThemeIsActive','ErrThemeNameRequired'," +
			"'ErrThemeNotFound','ErrThemeProjectIDEmpty','MsgProjectCreated','MsgProjectUpdated'," +

			"'ErrReceiptPending','ErrRouteActiveRename','ErrRouteNotFound','ErrRouteOccupied'," +
			"'MsgDeactivated','MsgRolledBack'," +

			"'MsgDashboardTitle')",
		SQL: mustSQL("430_i18n_enums_missing_keys.sql"),
	})
}

func init() { registerEnumsMissingKeysI18n() }
