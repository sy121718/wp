package migrations

import (
	"embed"
	"fmt"
)

//go:embed *.sql
var migrationSQLFS embed.FS

var registeredSQLFiles = map[string]struct{}{}

func mustSQL(name string) string {
	data, err := migrationSQLFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("迁移 SQL %s 不存在（//go:embed *.sql 未包含该文件）: %v", name, err))
	}
	registeredSQLFiles[name] = struct{}{}
	return string(data)
}

func init() {
	registerCoreSchemaAndAccess()
	registerMediaAndUniqueConstraints()
	registerCatalogAndInventory()
	registerCurrency()
	registerI18nDataLayer()
	registerIdentityAndMail()
	registerOrderAndSiteSlots()
	registerAnalyticsSeoAndPermissions()
	registerAdminI18nSeedsAndLatest()

	registerOrderFulfillment()

	registerOrderI18nB67()
	registerPluginPatrolI18n()
	registerBulkNoticeI18n()
	registerAdminRemainingTemplatesI18n()

	registerNavigationMenuI18n()

	registerContentTemplateStructureI18n()

	registerWorkbenchFacingI18n()

	registerFragmentLoginPanelI18n()

	registerI18nViewPermission()

	registerBuildJobLease()

	registerBlockUsageI18n()

	registerProductTagHitsI18n()

	registerProductRelatedIDsI18n()

	registerManifestDedup()

	registerBuildJobIntent()

	registerPagePublicationPlans()

	registerProductOutbox()

	registerProductTagCrossProjectI18n()

	registerProductEditI18n()

	registerProductReadonlyI18n()

	registerProductPreviewOverrideI18n()

	registerInventoryMovesEmptyI18n()

	registerClientFilterEmptyI18n()

	registerSysReference()

	registerPageExcludedLangs()
	registerLocaleAdmissionI18n()

	registerSettingsLocaleModeHintI18n()

	registerPageLangsRepublishI18n()

	registerPageTranslationMissesEntryI18n()

	registerSystemSettingsPage()

	registerSystemSettingsMenu()

	registerSystemSettingsHintCurrency()

	registerSystemSettingsHintTradeScope()

	registerSystemSettingsHintCurrencyNoConversion()

	registerAdminI18nOptionUIAvailable()

	registerPartitionRLSReconcile()

	registerAIProvider()

	registerAISession()

	registerAII18n()

	registerAIMenu()

	registerAISessionI18n()

	registerAIPresetI18n()

	registerAIStalePermissionCleanup()

	registerAISessionChatI18n()

	registerAISessionUsageI18n()

	registerAISessionUsageNotesI18n()

	registerAIEventProviderModel()

	registerAISessionUsageHoverI18n()

	registerAISessionTrendOtherI18n()

	registerAIMenuMerge()

	registerAILeadI18n()

	registerAIMenuMergeFollowup()

	registerAIEventUser()

	registerAIUserRequiredI18n()

	registerAICallLog()

	registerAISessionCallsI18n()

	registerAISessionToolsI18n()

	registerDashboardOverviewI18n()

	registerAIToolCallLog()

	registerAIAccessToken()

	registerAITokenI18n()

	registerAIMcpMenu()

	registerAIMcpI18n()

	registerAIMcpI18nAuth()

	registerAISessionDetailI18n()

	registerAIConfigGroup()

	registerAIMcpErrorTextI18n()

	registerAIMcpSwitchI18n()

	registerDashboardRangeI18n()

	registerDashboardKpiI18n()

	registerDashboardChartTabsI18n()

	registerDashboardRankTabsI18n()

	registerDashboardPageKindNotFoundI18n()

	registerCustomerCatalog()

	registerCustomerOverviewI18n()

	registerDashboardNewCustomersKPI18n()

	registerCustomerSegmentFilterI18n()

	registerCustomerRfmMenu()

	registerCustomerRfmI18n()

	registerCustomerCohortMenu()

	registerCustomerCohortI18n()

	registerCustomerMinOrdersI18n()

	registerCustomerRfmFilterI18n()

	registerCustomerTierFilterI18n()

	registerAICallLogCachedTokens()

	registerAIUsageMetricsI18n()

	registerAIFabI18n()

	registerAIToolIdempotency()

	registerAIFabThinkingI18n()

	registerDashboardAIBoxI18n()
	registerAIFabImageI18n()
	registerAIFabDismissI18n()
	registerDashboardAIChipsI18n()
	registerDashboardAIResumeI18n()
	registerDashboardKPIWordingI18n()
	registerDashboardTrendGranularityI18n()

	registerOrderSalesOverviewI18n()

	registerOrderOverviewMenu()

	registerOrderSalesCustomerMixI18n()

	registerOrderSalesGroupI18n()

	registerOrderSalesTrendLineI18n()
	registerRemoveOrphanFilterI18n()

	registerDateFilterI18n()

	registerMenusTreePageI18n()

	registerMenuComponentRetire()

	registerPageTitleI18n()

	registerMenuDeadPath()

	registerMenuButtonCode()

	registerNavigationJumpI18n()

	registerAdminUINewKeysI18n()

	registerAdminBlocksWriteUII18n()

	registerBlockBulkDeletePermission()

	registerAdminI18nWriteUII18n()

	registerBlockBulkDeleteGrantCopy()

	registerMailContactWriteUII18n()

	registerNavigationWriteUII18n()

	registerNavigationAddSourceUII18n()

	registerNavigationSortTiedI18n()

	registerAdminCouponsWriteUII18n()

	registerAdminOrdersWriteUII18n()

	registerAdminReturnsWriteUII18n()

	registerAdminCommentWriteUII18n()

	registerAdminPluginWriteUII18n()

	registerAdminContentTemplateWriteUII18n()

	registerAdminCustomerWriteUII18n()

	registerAdminSystemWriteUII18n()

	registerAdminCustomerFilterI18n()

	registerPageTranslationsSavedI18n()

	registerMailPageNoticeI18n()

	registerTranslationStaleMarkI18n()

	registerMailAutomationDefinitionV2I18n()

	registerMailAutomationCanvasSaveHintI18n()

	registerMailAutomationCanvasFieldI18n()

	registerMailAutomationCanvasV2I18n()

	registerMailAutomationNodesI18n()

	registerMailTemplateBodyJSON()

	registerMailTemplateEditorI18n()

	registerMailAutomationFrequency()

	registerMailAutomationFrequencyI18n()

	registerMailAutomationFrequencyFieldI18n()

	registerMenuDedupMembership()

	registerDateRangePickerI18n()

	registerAdminSideEffectTasks()

	registerAdminBulkReworkI18n()

	registerAdminDataruleEditorI18n()

	registerAdminI18nBulkDeleteI18n()

	registerAIUIBlocksEmptyI18n()

	registerAIProviderModelsWriteGate()

	registerAIPickerNoPermissionI18n()

	registerAICreateRefreshI18n()

	registerThemeSettingsCustomizeI18n()

	registerMediaErrorCodesI18n()

	registerFileCategoryI18n()

	registerPageMissingErrorI18n()
}
