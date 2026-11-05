package permission

// 权限标识的过渡别名：实体已按模块属主落在各 module/enums。
// 消费点迁移完成后本文件整体删除，不保留第二份定义。
import (
	adminenums "go_wp/internal/module/admin/enums"
	aienums "go_wp/internal/module/ai/enums"
	analyticsenums "go_wp/internal/module/analytics/enums"
	artifactenums "go_wp/internal/module/artifact/enums"
	blockenums "go_wp/internal/module/block/enums"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	buildenums "go_wp/internal/module/build/enums"
	commentenums "go_wp/internal/module/comment/enums"
	contentenums "go_wp/internal/module/content/enums"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	inventoryenums "go_wp/internal/module/inventory/enums"
	mailenums "go_wp/internal/module/mail/enums"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	mediaenums "go_wp/internal/module/media/enums"
	membershipenums "go_wp/internal/module/membership/enums"
	navigationenums "go_wp/internal/module/navigation/enums"
	orderenums "go_wp/internal/module/order/enums"
	pageenums "go_wp/internal/module/page/enums"
	pluginenums "go_wp/internal/module/plugin/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	productenums "go_wp/internal/module/product/enums"
	projectenums "go_wp/internal/module/project/enums"
	publicationenums "go_wp/internal/module/publication/enums"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	userenums "go_wp/internal/module/user/enums"
	webhookenums "go_wp/internal/module/webhook/enums"
)

const (
	AdminCreate Perm = adminenums.AdminCreate
	AdminDelete Perm = adminenums.AdminDelete
	AdminDetail Perm = adminenums.AdminDetail
	AdminEdit Perm = adminenums.AdminEdit
	AdminList Perm = adminenums.AdminList
	AdminMenuList Perm = adminenums.AdminMenuList
	AdminMenuSave Perm = adminenums.AdminMenuSave
	AdminRoleList Perm = adminenums.AdminRoleList
	AdminRoleSave Perm = adminenums.AdminRoleSave
	DataruleAssignmentList Perm = adminenums.DataruleAssignmentList
	DataruleAssignmentSave Perm = adminenums.DataruleAssignmentSave
	DataruleCreate Perm = adminenums.DataruleCreate
	DataruleDelete Perm = adminenums.DataruleDelete
	DataruleDetail Perm = adminenums.DataruleDetail
	DataruleList Perm = adminenums.DataruleList
	DataruleSchemaDetail Perm = adminenums.DataruleSchemaDetail
	DataruleSchemaList Perm = adminenums.DataruleSchemaList
	DataruleUpdate Perm = adminenums.DataruleUpdate
	DeptCreate Perm = adminenums.DeptCreate
	DeptDelete Perm = adminenums.DeptDelete
	DeptDetail Perm = adminenums.DeptDetail
	DeptList Perm = adminenums.DeptList
	DeptUpdate Perm = adminenums.DeptUpdate
	DeptUserList Perm = adminenums.DeptUserList
	DeptUserSave Perm = adminenums.DeptUserSave
	I18nManage Perm = adminenums.I18nManage
	I18nView Perm = adminenums.I18nView
	MenuCreate Perm = adminenums.MenuCreate
	MenuDelete Perm = adminenums.MenuDelete
	MenuDetail Perm = adminenums.MenuDetail
	MenuList Perm = adminenums.MenuList
	MenuUpdate Perm = adminenums.MenuUpdate
	PermissionCreate Perm = adminenums.PermissionCreate
	PermissionDelete Perm = adminenums.PermissionDelete
	PermissionDetail Perm = adminenums.PermissionDetail
	PermissionList Perm = adminenums.PermissionList
	PermissionOptions Perm = adminenums.PermissionOptions
	PermissionUpdate Perm = adminenums.PermissionUpdate
	RoleCreate Perm = adminenums.RoleCreate
	RoleDelete Perm = adminenums.RoleDelete
	RoleDetail Perm = adminenums.RoleDetail
	RoleList Perm = adminenums.RoleList
	RoleMenuList Perm = adminenums.RoleMenuList
	RoleMenuSave Perm = adminenums.RoleMenuSave
	RoleUpdate Perm = adminenums.RoleUpdate
	RoleUserList Perm = adminenums.RoleUserList
	RoleUserSave Perm = adminenums.RoleUserSave
	SEOAudit Perm = adminenums.SEOAudit

	AIChat Perm = aienums.AIChat
	AIProviderDelete Perm = aienums.AIProviderDelete
	AIProviderGet Perm = aienums.AIProviderGet
	AIProviderList Perm = aienums.AIProviderList
	AIProviderModelsAppend Perm = aienums.AIProviderModelsAppend
	AIProviderModelsCandidates Perm = aienums.AIProviderModelsCandidates
	AIProviderModelsFetch Perm = aienums.AIProviderModelsFetch
	AIProviderModelsList Perm = aienums.AIProviderModelsList
	AIProviderModelsRestore Perm = aienums.AIProviderModelsRestore
	AIProviderModelsSave Perm = aienums.AIProviderModelsSave
	AIProviderSave Perm = aienums.AIProviderSave
	AIProviderStatus Perm = aienums.AIProviderStatus
	AISessionAppend Perm = aienums.AISessionAppend
	AISessionArchive Perm = aienums.AISessionArchive
	AISessionEvents Perm = aienums.AISessionEvents
	AISessionFold Perm = aienums.AISessionFold
	AISessionFoldPlan Perm = aienums.AISessionFoldPlan
	AISessionGet Perm = aienums.AISessionGet
	AISessionList Perm = aienums.AISessionList
	AISessionRename Perm = aienums.AISessionRename
	AISessionSend Perm = aienums.AISessionSend
	AITokenCreate Perm = aienums.AITokenCreate
	AITokenList Perm = aienums.AITokenList
	AITokenRevoke Perm = aienums.AITokenRevoke
	AITokenToggle Perm = aienums.AITokenToggle

	AnalyticsView Perm = analyticsenums.AnalyticsView

	ArtifactDetail Perm = artifactenums.ArtifactDetail

	BlockBulkDelete Perm = blockenums.BlockBulkDelete
	BlockClone Perm = blockenums.BlockClone
	BlockCreate Perm = blockenums.BlockCreate
	BlockDelete Perm = blockenums.BlockDelete
	BlockDetail Perm = blockenums.BlockDetail
	BlockList Perm = blockenums.BlockList
	BlockUpdate Perm = blockenums.BlockUpdate

	BlueprintCreate Perm = blueprintenums.BlueprintCreate
	BlueprintDelete Perm = blueprintenums.BlueprintDelete
	BlueprintGet Perm = blueprintenums.BlueprintGet
	BlueprintInit Perm = blueprintenums.BlueprintInit
	BlueprintList Perm = blueprintenums.BlueprintList
	BlueprintPublish Perm = blueprintenums.BlueprintPublish
	BlueprintUpdate Perm = blueprintenums.BlueprintUpdate

	BuildJobs Perm = buildenums.BuildJobs
	BuildQueue Perm = buildenums.BuildQueue
	BuildRetry Perm = buildenums.BuildRetry

	CommentList Perm = commentenums.CommentList
	CommentReview Perm = commentenums.CommentReview

	ContentCollections Perm = contentenums.ContentCollections
	ContentCreate Perm = contentenums.ContentCreate
	ContentDelete Perm = contentenums.ContentDelete
	ContentGet Perm = contentenums.ContentGet
	ContentList Perm = contentenums.ContentList
	ContentUpdate Perm = contentenums.ContentUpdate

	ContenttemplateActivate Perm = contenttemplateenums.ContenttemplateActivate
	ContenttemplateCreate Perm = contenttemplateenums.ContenttemplateCreate
	ContenttemplateDelete Perm = contenttemplateenums.ContenttemplateDelete
	ContenttemplateGet Perm = contenttemplateenums.ContenttemplateGet
	ContenttemplateList Perm = contenttemplateenums.ContenttemplateList
	ContenttemplateUpdate Perm = contenttemplateenums.ContenttemplateUpdate

	InventoryMovementList Perm = inventoryenums.InventoryMovementList
	InventoryPurchaseCreate Perm = inventoryenums.InventoryPurchaseCreate
	InventoryPurchaseGet Perm = inventoryenums.InventoryPurchaseGet
	InventoryPurchaseHistory Perm = inventoryenums.InventoryPurchaseHistory
	InventoryPurchaseList Perm = inventoryenums.InventoryPurchaseList
	InventoryPurchaseProduction Perm = inventoryenums.InventoryPurchaseProduction
	InventoryPurchaseReceipt Perm = inventoryenums.InventoryPurchaseReceipt
	InventoryPurchaseUpdate Perm = inventoryenums.InventoryPurchaseUpdate
	InventoryReasonCreate Perm = inventoryenums.InventoryReasonCreate
	InventoryReasonList Perm = inventoryenums.InventoryReasonList
	InventoryReasonUpdate Perm = inventoryenums.InventoryReasonUpdate
	InventorySourceCreate Perm = inventoryenums.InventorySourceCreate
	InventorySourceDelete Perm = inventoryenums.InventorySourceDelete
	InventorySourceGet Perm = inventoryenums.InventorySourceGet
	InventorySourceList Perm = inventoryenums.InventorySourceList
	InventorySourceSummary Perm = inventoryenums.InventorySourceSummary
	InventorySourceUpdate Perm = inventoryenums.InventorySourceUpdate
	InventoryStockAlert Perm = inventoryenums.InventoryStockAlert
	InventoryStockChange Perm = inventoryenums.InventoryStockChange
	InventoryStockDeduct Perm = inventoryenums.InventoryStockDeduct
	InventoryStockEnsure Perm = inventoryenums.InventoryStockEnsure
	InventoryStockGet Perm = inventoryenums.InventoryStockGet
	InventoryStockList Perm = inventoryenums.InventoryStockList
	InventoryStockSKU Perm = inventoryenums.InventoryStockSKU
	InventoryTransferApprove Perm = inventoryenums.InventoryTransferApprove
	InventoryTransferCancel Perm = inventoryenums.InventoryTransferCancel
	InventoryTransferCreate Perm = inventoryenums.InventoryTransferCreate
	InventoryTransferGet Perm = inventoryenums.InventoryTransferGet
	InventoryTransferList Perm = inventoryenums.InventoryTransferList
	InventoryTransferOptions Perm = inventoryenums.InventoryTransferOptions
	InventoryTransferReceive Perm = inventoryenums.InventoryTransferReceive
	InventoryTransferReject Perm = inventoryenums.InventoryTransferReject
	InventoryTransferShip Perm = inventoryenums.InventoryTransferShip
	InventoryWarehouseCreate Perm = inventoryenums.InventoryWarehouseCreate
	InventoryWarehouseDelete Perm = inventoryenums.InventoryWarehouseDelete
	InventoryWarehouseGet Perm = inventoryenums.InventoryWarehouseGet
	InventoryWarehouseList Perm = inventoryenums.InventoryWarehouseList
	InventoryWarehouseUpdate Perm = inventoryenums.InventoryWarehouseUpdate

	MailAccountDefault Perm = mailenums.MailAccountDefault
	MailAccountDelete Perm = mailenums.MailAccountDelete
	MailAccountList Perm = mailenums.MailAccountList
	MailAccountSave Perm = mailenums.MailAccountSave
	MailAccountTest Perm = mailenums.MailAccountTest
	MailAutomationDelete Perm = mailenums.MailAutomationDelete
	MailAutomationGet Perm = mailenums.MailAutomationGet
	MailAutomationLayout Perm = mailenums.MailAutomationLayout
	MailAutomationList Perm = mailenums.MailAutomationList
	MailAutomationRunDetail Perm = mailenums.MailAutomationRunDetail
	MailAutomationRunList Perm = mailenums.MailAutomationRunList
	MailAutomationSave Perm = mailenums.MailAutomationSave
	MailAutomationStart Perm = mailenums.MailAutomationStart
	MailAutomationStatus Perm = mailenums.MailAutomationStatus
	MailAutomationTick Perm = mailenums.MailAutomationTick
	MailCampaignDelete Perm = mailenums.MailCampaignDelete
	MailCampaignGet Perm = mailenums.MailCampaignGet
	MailCampaignList Perm = mailenums.MailCampaignList
	MailCampaignSave Perm = mailenums.MailCampaignSave
	MailCampaignStart Perm = mailenums.MailCampaignStart
	MailContactDelete Perm = mailenums.MailContactDelete
	MailContactImport Perm = mailenums.MailContactImport
	MailContactList Perm = mailenums.MailContactList
	MailContactSave Perm = mailenums.MailContactSave
	MailContactStatus Perm = mailenums.MailContactStatus
	MailContactTag Perm = mailenums.MailContactTag
	MailTemplateDelete Perm = mailenums.MailTemplateDelete
	MailTemplateList Perm = mailenums.MailTemplateList
	MailTemplateSave Perm = mailenums.MailTemplateSave

	MasterdataChangeCount Perm = masterdataenums.MasterdataChangeCount
	MasterdataChangeEntities Perm = masterdataenums.MasterdataChangeEntities
	MasterdataChangeEntity Perm = masterdataenums.MasterdataChangeEntity
	MasterdataChangeList Perm = masterdataenums.MasterdataChangeList

	MediaCategoryCreate Perm = mediaenums.MediaCategoryCreate
	MediaCategoryDelete Perm = mediaenums.MediaCategoryDelete
	MediaCategoryTree Perm = mediaenums.MediaCategoryTree
	MediaCategoryUpdate Perm = mediaenums.MediaCategoryUpdate
	MediaDelete Perm = mediaenums.MediaDelete
	MediaDetail Perm = mediaenums.MediaDetail
	MediaDownload Perm = mediaenums.MediaDownload
	MediaDownloadBatch Perm = mediaenums.MediaDownloadBatch
	MediaList Perm = mediaenums.MediaList
	MediaReferences Perm = mediaenums.MediaReferences
	MediaReplace Perm = mediaenums.MediaReplace
	MediaUpdate Perm = mediaenums.MediaUpdate
	MediaUpload Perm = mediaenums.MediaUpload
	MediaVariantsGenerate Perm = mediaenums.MediaVariantsGenerate

	MembershipAssignList Perm = membershipenums.MembershipAssignList
	MembershipAssignSet Perm = membershipenums.MembershipAssignSet
	MembershipAssignUnlock Perm = membershipenums.MembershipAssignUnlock
	MembershipEntitlementSave Perm = membershipenums.MembershipEntitlementSave
	MembershipRecalc Perm = membershipenums.MembershipRecalc
	MembershipResolve Perm = membershipenums.MembershipResolve
	MembershipTierCreate Perm = membershipenums.MembershipTierCreate
	MembershipTierDelete Perm = membershipenums.MembershipTierDelete
	MembershipTierGet Perm = membershipenums.MembershipTierGet
	MembershipTierList Perm = membershipenums.MembershipTierList
	MembershipTierUpdate Perm = membershipenums.MembershipTierUpdate

	NavigationCreate Perm = navigationenums.NavigationCreate
	NavigationDelete Perm = navigationenums.NavigationDelete
	NavigationDeleteBulk Perm = navigationenums.NavigationDeleteBulk
	NavigationGet Perm = navigationenums.NavigationGet
	NavigationList Perm = navigationenums.NavigationList
	NavigationMove Perm = navigationenums.NavigationMove
	NavigationSourceAdd Perm = navigationenums.NavigationSourceAdd
	NavigationTranslate Perm = navigationenums.NavigationTranslate
	NavigationUpdate Perm = navigationenums.NavigationUpdate

	OrderCancel Perm = orderenums.OrderCancel
	OrderCarrierCreate Perm = orderenums.OrderCarrierCreate
	OrderCarrierList Perm = orderenums.OrderCarrierList
	OrderCarrierManage Perm = orderenums.OrderCarrierManage
	OrderCouponCountAudit Perm = orderenums.OrderCouponCountAudit
	OrderCouponCreate Perm = orderenums.OrderCouponCreate
	OrderCouponDelete Perm = orderenums.OrderCouponDelete
	OrderCouponGet Perm = orderenums.OrderCouponGet
	OrderCouponList Perm = orderenums.OrderCouponList
	OrderCouponRedemption Perm = orderenums.OrderCouponRedemption
	OrderCouponUpdate Perm = orderenums.OrderCouponUpdate
	OrderCouponValidate Perm = orderenums.OrderCouponValidate
	OrderCreate Perm = orderenums.OrderCreate
	OrderCurrencyRateGet Perm = orderenums.OrderCurrencyRateGet
	OrderCurrencyRateSet Perm = orderenums.OrderCurrencyRateSet
	OrderGet Perm = orderenums.OrderGet
	OrderItemList Perm = orderenums.OrderItemList
	OrderList Perm = orderenums.OrderList
	OrderLogList Perm = orderenums.OrderLogList
	OrderNote Perm = orderenums.OrderNote
	OrderOverview Perm = orderenums.OrderOverview
	OrderPendingShip Perm = orderenums.OrderPendingShip
	OrderRefund Perm = orderenums.OrderRefund
	OrderReturnApprove Perm = orderenums.OrderReturnApprove
	OrderReturnGet Perm = orderenums.OrderReturnGet
	OrderReturnList Perm = orderenums.OrderReturnList
	OrderReturnReceive Perm = orderenums.OrderReturnReceive
	OrderReturnReject Perm = orderenums.OrderReturnReject
	OrderShip Perm = orderenums.OrderShip
	OrderShipForm Perm = orderenums.OrderShipForm
	OrderShipmentList Perm = orderenums.OrderShipmentList
	OrderStatus Perm = orderenums.OrderStatus

	PageArtifactGc Perm = pageenums.PageArtifactGc
	PageArtifactRebuild Perm = pageenums.PageArtifactRebuild
	PageBuild Perm = pageenums.PageBuild
	PageCreate Perm = pageenums.PageCreate
	PageDelete Perm = pageenums.PageDelete
	PageDetail Perm = pageenums.PageDetail
	PageDraftSave Perm = pageenums.PageDraftSave
	PageList Perm = pageenums.PageList
	PagePublicationAudit Perm = pageenums.PagePublicationAudit
	PagePublish Perm = pageenums.PagePublish
	PageRedirectCreate Perm = pageenums.PageRedirectCreate
	PageRedirectDelete Perm = pageenums.PageRedirectDelete
	PageRedirectMerge Perm = pageenums.PageRedirectMerge
	PageRedirectView Perm = pageenums.PageRedirectView
	PageRevisionList Perm = pageenums.PageRevisionList
	PageRollback Perm = pageenums.PageRollback
	PageSEOPatrol Perm = pageenums.PageSEOPatrol
	PageTranslationsSave Perm = pageenums.PageTranslationsSave
	PageURLUpdate Perm = pageenums.PageURLUpdate

	PluginDetail Perm = pluginenums.PluginDetail
	PluginInstall Perm = pluginenums.PluginInstall
	PluginList Perm = pluginenums.PluginList
	PluginToggle Perm = pluginenums.PluginToggle
	PluginUninstall Perm = pluginenums.PluginUninstall

	PresentationCreate Perm = presentationenums.PresentationCreate
	PresentationDelete Perm = presentationenums.PresentationDelete
	PresentationGet Perm = presentationenums.PresentationGet
	PresentationGetByEntity Perm = presentationenums.PresentationGetByEntity
	PresentationList Perm = presentationenums.PresentationList
	PresentationPreview Perm = presentationenums.PresentationPreview
	PresentationRebuild Perm = presentationenums.PresentationRebuild
	PresentationUpdateURL Perm = presentationenums.PresentationUpdateURL

	ProductAttributeBulkDelete Perm = productenums.ProductAttributeBulkDelete
	ProductAttributeCreate Perm = productenums.ProductAttributeCreate
	ProductAttributeDelete Perm = productenums.ProductAttributeDelete
	ProductAttributeGet Perm = productenums.ProductAttributeGet
	ProductAttributeList Perm = productenums.ProductAttributeList
	ProductAttributeSetValues Perm = productenums.ProductAttributeSetValues
	ProductAttributeUpdate Perm = productenums.ProductAttributeUpdate
	ProductBrandBulkDelete Perm = productenums.ProductBrandBulkDelete
	ProductBrandCreate Perm = productenums.ProductBrandCreate
	ProductBrandDelete Perm = productenums.ProductBrandDelete
	ProductBrandGet Perm = productenums.ProductBrandGet
	ProductBrandList Perm = productenums.ProductBrandList
	ProductBrandSEOScore Perm = productenums.ProductBrandSEOScore
	ProductBrandUpdate Perm = productenums.ProductBrandUpdate
	ProductBulkDelete Perm = productenums.ProductBulkDelete
	ProductBulkPricing Perm = productenums.ProductBulkPricing
	ProductCategoryBulkDelete Perm = productenums.ProductCategoryBulkDelete
	ProductCategoryCreate Perm = productenums.ProductCategoryCreate
	ProductCategoryDelete Perm = productenums.ProductCategoryDelete
	ProductCategoryGet Perm = productenums.ProductCategoryGet
	ProductCategoryList Perm = productenums.ProductCategoryList
	ProductCategoryParents Perm = productenums.ProductCategoryParents
	ProductCategorySEOScore Perm = productenums.ProductCategorySEOScore
	ProductCategoryUpdate Perm = productenums.ProductCategoryUpdate
	ProductCreate Perm = productenums.ProductCreate
	ProductDelete Perm = productenums.ProductDelete
	ProductDetailTemplateApply Perm = productenums.ProductDetailTemplateApply
	ProductDetailTemplateCreate Perm = productenums.ProductDetailTemplateCreate
	ProductDetailTemplatePublish Perm = productenums.ProductDetailTemplatePublish
	ProductDetailTemplateReapply Perm = productenums.ProductDetailTemplateReapply
	ProductDetailTemplateRollback Perm = productenums.ProductDetailTemplateRollback
	ProductDetailTemplateUpdateURL Perm = productenums.ProductDetailTemplateUpdateURL
	ProductGet Perm = productenums.ProductGet
	ProductList Perm = productenums.ProductList
	ProductPricingAdjustment Perm = productenums.ProductPricingAdjustment
	ProductPricingApply Perm = productenums.ProductPricingApply
	ProductPricingHistory Perm = productenums.ProductPricingHistory
	ProductPricingPreview Perm = productenums.ProductPricingPreview
	ProductPricingRoundings Perm = productenums.ProductPricingRoundings
	ProductPricingRules Perm = productenums.ProductPricingRules
	ProductPublishAttempts Perm = productenums.ProductPublishAttempts
	ProductRatingAdd Perm = productenums.ProductRatingAdd
	ProductRatingDelete Perm = productenums.ProductRatingDelete
	ProductSEOScore Perm = productenums.ProductSEOScore
	ProductTagBulkDelete Perm = productenums.ProductTagBulkDelete
	ProductTagCreate Perm = productenums.ProductTagCreate
	ProductTagDelete Perm = productenums.ProductTagDelete
	ProductTagGet Perm = productenums.ProductTagGet
	ProductTagList Perm = productenums.ProductTagList
	ProductTagProducts Perm = productenums.ProductTagProducts
	ProductTagRecalc Perm = productenums.ProductTagRecalc
	ProductTagRuleTypes Perm = productenums.ProductTagRuleTypes
	ProductTagUpdate Perm = productenums.ProductTagUpdate
	ProductTranslationsSave Perm = productenums.ProductTranslationsSave
	ProductUpdate Perm = productenums.ProductUpdate
	ProductVariantCreate Perm = productenums.ProductVariantCreate
	ProductVariantDelete Perm = productenums.ProductVariantDelete
	ProductVariantGenerate Perm = productenums.ProductVariantGenerate
	ProductVariantPreview Perm = productenums.ProductVariantPreview
	ProductVariantSave Perm = productenums.ProductVariantSave
	ProductVariantUpdate Perm = productenums.ProductVariantUpdate

	ProjectCreate Perm = projectenums.ProjectCreate
	ProjectDetail Perm = projectenums.ProjectDetail
	ProjectList Perm = projectenums.ProjectList
	ProjectThemeActivate Perm = projectenums.ProjectThemeActivate
	ProjectThemeActive Perm = projectenums.ProjectThemeActive
	ProjectThemeCreate Perm = projectenums.ProjectThemeCreate
	ProjectThemeDelete Perm = projectenums.ProjectThemeDelete
	ProjectThemeList Perm = projectenums.ProjectThemeList
	ProjectThemeUpdate Perm = projectenums.ProjectThemeUpdate
	ProjectUpdate Perm = projectenums.ProjectUpdate

	PublicationReceiptsPending Perm = publicationenums.PublicationReceiptsPending
	PublicationSEOAudit Perm = publicationenums.PublicationSEOAudit

	SysConfigGet Perm = sysconfigenums.SysConfigGet
	SysConfigSave Perm = sysconfigenums.SysConfigSave
	SysConfigSystemSave Perm = sysconfigenums.SysConfigSystemSave

	UserCustomerBulkStatus Perm = userenums.UserCustomerBulkStatus
	UserCustomerBulkUnlock Perm = userenums.UserCustomerBulkUnlock
	UserCustomerDetail Perm = userenums.UserCustomerDetail
	UserCustomerList Perm = userenums.UserCustomerList
	UserCustomerStatus Perm = userenums.UserCustomerStatus
	UserCustomerUnlock Perm = userenums.UserCustomerUnlock

	WebhookDeliveryList Perm = webhookenums.WebhookDeliveryList
	WebhookDeliveryRetry Perm = webhookenums.WebhookDeliveryRetry
	WebhookEndpointDelete Perm = webhookenums.WebhookEndpointDelete
	WebhookEndpointList Perm = webhookenums.WebhookEndpointList
	WebhookEndpointSave Perm = webhookenums.WebhookEndpointSave
	WebhookEndpointStatus Perm = webhookenums.WebhookEndpointStatus

)

var specs = map[Perm]spec{

	AdminCreate:   {module: "admin", name: "新建管理员"},
	AdminDelete:   {module: "admin", name: "删除管理员"},
	AdminDetail:   {module: "admin", name: "管理员详情"},
	AdminEdit:     {module: "admin", name: "编辑管理员"},
	AdminList:     {module: "admin", name: "管理员列表"},
	AdminMenuList: {module: "admin", name: "管理员菜单查看"},
	AdminMenuSave: {module: "admin", name: "管理员菜单分配"},
	AdminRoleList: {module: "admin", name: "管理员角色查看"},
	AdminRoleSave: {module: "admin", name: "管理员角色分配"},

	AnalyticsView: {module: "analytics", name: "访问统计查看"},

	ArtifactDetail: {module: "artifact", name: "构建产物详情"},

	BlockBulkDelete: {module: "block", name: "批量删除区块"},
	BlockClone:      {module: "block", name: "克隆区块"},
	BlockCreate:     {module: "block", name: "新建区块"},
	BlockDelete:     {module: "block", name: "删除区块"},
	BlockDetail:     {module: "block", name: "区块详情"},
	BlockList:       {module: "block", name: "区块列表"},
	BlockUpdate:     {module: "block", name: "更新区块"},

	BlueprintCreate:  {module: "blueprint", name: "模板创建"},
	BlueprintDelete:  {module: "blueprint", name: "模板删除"},
	BlueprintGet:     {module: "blueprint", name: "模板详情"},
	BlueprintInit:    {module: "blueprint", name: "初始化页面"},
	BlueprintList:    {module: "blueprint", name: "模板列表"},
	BlueprintPublish: {module: "blueprint", name: "模板发布"},
	BlueprintUpdate:  {module: "blueprint", name: "模板更新"},

	BuildJobs:  {module: "build", name: "构建任务列表"},
	BuildQueue: {module: "build", name: "构建队列状态"},
	BuildRetry: {module: "build", name: "重试构建"},

	CommentList:   {module: "comment", name: "评论审核列表"},
	CommentReview: {module: "comment", name: "评论批量审核"},

	ContentCollections: {module: "content", name: "查看内容集合元数据"},
	ContentCreate:      {module: "content", name: "内容创建"},
	ContentDelete:      {module: "content", name: "内容删除"},
	ContentGet:         {module: "content", name: "内容详情"},
	ContentList:        {module: "content", name: "内容列表"},
	ContentUpdate:      {module: "content", name: "内容更新"},

	ContenttemplateCreate:   {module: "contenttemplate", name: "模板创建"},
	ContenttemplateDelete:   {module: "contenttemplate", name: "模板删除"},
	ContenttemplateGet:      {module: "contenttemplate", name: "模板详情"},
	ContenttemplateList:     {module: "contenttemplate", name: "模板列表"},
	ContenttemplateUpdate:   {module: "contenttemplate", name: "模板更新"},
	ContenttemplateActivate: {module: "contenttemplate", name: "模板切换生效"},

	DataruleAssignmentList: {module: "datarule", name: "规则分配查看"},
	DataruleAssignmentSave: {module: "datarule", name: "规则分配保存"},
	DataruleCreate:         {module: "datarule", name: "新建数据规则"},
	DataruleDelete:         {module: "datarule", name: "删除数据规则"},
	DataruleDetail:         {module: "datarule", name: "数据规则详情"},
	DataruleList:           {module: "datarule", name: "数据规则列表"},
	DataruleSchemaDetail:   {module: "datarule", name: "数据域详情"},
	DataruleSchemaList:     {module: "datarule", name: "数据域清单"},
	DataruleUpdate:         {module: "datarule", name: "更新数据规则"},

	DeptCreate:   {module: "dept", name: "新建部门"},
	DeptDelete:   {module: "dept", name: "删除部门"},
	DeptDetail:   {module: "dept", name: "部门详情"},
	DeptList:     {module: "dept", name: "部门树"},
	DeptUpdate:   {module: "dept", name: "更新部门"},
	DeptUserList: {module: "dept", name: "部门用户查看"},
	DeptUserSave: {module: "dept", name: "部门用户分配"},

	I18nView:   {module: "i18n", name: "文案词条查看"},
	I18nManage: {module: "i18n", name: "文案词条管理"},

	InventoryMovementList:       {module: "inventory", name: "库存流水列表"},
	InventoryPurchaseCreate:     {module: "inventory", name: "新建采购单"},
	InventoryPurchaseGet:        {module: "inventory", name: "采购单详情"},
	InventoryPurchaseHistory:    {module: "inventory", name: "进货历史"},
	InventoryPurchaseList:       {module: "inventory", name: "采购单列表"},
	InventoryPurchaseProduction: {module: "inventory", name: "生产入库"},
	InventoryStockAlert:         {module: "inventory", name: "库存预警"},
	InventoryTransferList:       {module: "inventory", name: "调拨单列表"},
	InventoryTransferGet:        {module: "inventory", name: "调拨单详情"},
	InventoryTransferOptions:    {module: "inventory", name: "调拨候选货品"},
	InventoryTransferCreate:     {module: "inventory", name: "建调拨单"},
	InventoryTransferApprove:    {module: "inventory", name: "同意调拨"},
	InventoryTransferReject:     {module: "inventory", name: "驳回调拨"},
	InventoryTransferShip:       {module: "inventory", name: "调拨出货"},
	InventoryTransferReceive:    {module: "inventory", name: "调拨签收"},
	InventoryTransferCancel:     {module: "inventory", name: "取消调拨"},
	InventoryPurchaseReceipt:    {module: "inventory", name: "采购收货入库"},
	InventoryPurchaseUpdate:     {module: "inventory", name: "修改采购单"},
	InventoryReasonCreate:       {module: "inventory", name: "新建变动原因"},
	InventoryReasonList:         {module: "inventory", name: "变动原因列表"},
	InventoryReasonUpdate:       {module: "inventory", name: "修改变动原因"},
	InventorySourceCreate:       {module: "inventory", name: "新建货源"},
	InventorySourceDelete:       {module: "inventory", name: "删除货源"},
	InventorySourceGet:          {module: "inventory", name: "货源详情"},
	InventorySourceList:         {module: "inventory", name: "货源列表"},
	InventorySourceSummary:      {module: "inventory", name: "货源关联方统计"},
	InventorySourceUpdate:       {module: "inventory", name: "修改货源"},
	InventoryStockChange:        {module: "inventory", name: "按 SKU 增减库存"},
	InventoryStockDeduct:        {module: "inventory", name: "按 SKU 扣减库存"},
	InventoryStockEnsure:        {module: "inventory", name: "库存记录生成"},
	InventoryStockGet:           {module: "inventory", name: "库存记录详情"},
	InventoryStockList:          {module: "inventory", name: "库存记录列表"},
	InventoryStockSKU:           {module: "inventory", name: "某 SKU 各仓库存"},
	InventoryWarehouseCreate:    {module: "inventory", name: "新建仓库"},
	InventoryWarehouseDelete:    {module: "inventory", name: "删除仓库"},
	InventoryWarehouseGet:       {module: "inventory", name: "仓库详情"},
	InventoryWarehouseList:      {module: "inventory", name: "仓库列表"},
	InventoryWarehouseUpdate:    {module: "inventory", name: "修改仓库"},

	MailAccountDefault:      {module: "mail", name: "设默认发信账号"},
	MailAccountDelete:       {module: "mail", name: "删除发信账号"},
	MailAccountList:         {module: "mail", name: "发信账号列表"},
	MailAccountSave:         {module: "mail", name: "保存发信账号"},
	MailAccountTest:         {module: "mail", name: "测试发送邮件"},
	MailAutomationDelete:    {module: "mail", name: "删除自动化流程"},
	MailAutomationGet:       {module: "mail", name: "自动化流程详情"},
	MailAutomationLayout:    {module: "mail", name: "保存画布位置"},
	MailAutomationList:      {module: "mail", name: "自动化流程列表"},
	MailAutomationRunDetail: {module: "mail", name: "自动化实例排障详情"},
	MailAutomationRunList:   {module: "mail", name: "自动化实例列表"},
	MailAutomationSave:      {module: "mail", name: "保存自动化流程"},
	MailAutomationStart:     {module: "mail", name: "把联系人加入流程"},
	MailAutomationStatus:    {module: "mail", name: "启停自动化流程"},
	MailAutomationTick:      {module: "mail", name: "补投延时实例"},
	MailCampaignDelete:      {module: "mail", name: "删除群发活动"},
	MailCampaignGet:         {module: "mail", name: "群发活动详情"},
	MailCampaignList:        {module: "mail", name: "群发活动列表"},
	MailCampaignSave:        {module: "mail", name: "保存群发活动"},
	MailCampaignStart:       {module: "mail", name: "启动群发活动"},
	MailContactDelete:       {module: "mail", name: "删除联系人"},
	MailContactImport:       {module: "mail", name: "导入联系人"},
	MailContactList:         {module: "mail", name: "联系人列表"},
	MailContactSave:         {module: "mail", name: "保存联系人"},
	MailContactStatus:       {module: "mail", name: "修改联系人状态"},
	MailContactTag:          {module: "mail", name: "批量打标签"},
	MailTemplateDelete:      {module: "mail", name: "删除邮件模板"},
	MailTemplateList:        {module: "mail", name: "邮件模板列表"},
	MailTemplateSave:        {module: "mail", name: "保存邮件模板"},

	MasterdataChangeCount:    {module: "masterdata", name: "变更记录计数"},
	MasterdataChangeEntities: {module: "masterdata", name: "实体变更历史清单"},
	MasterdataChangeEntity:   {module: "masterdata", name: "单实体变更历史"},
	MasterdataChangeList:     {module: "masterdata", name: "变更记录列表"},

	MediaCategoryCreate:   {module: "media", name: "新建媒体分类"},
	MediaCategoryDelete:   {module: "media", name: "删除媒体分类"},
	MediaCategoryTree:     {module: "media", name: "媒体分类树"},
	MediaCategoryUpdate:   {module: "media", name: "更新媒体分类"},
	MediaDelete:           {module: "media", name: "删除媒体"},
	MediaDetail:           {module: "media", name: "媒体详情"},
	MediaDownload:         {module: "media", name: "下载媒体资源包"},
	MediaDownloadBatch:    {module: "media", name: "批量下载媒体"},
	MediaList:             {module: "media", name: "媒体列表"},
	MediaReferences:       {module: "media", name: "媒体引用来源"},
	MediaReplace:          {module: "media", name: "媒体换图"},
	MediaUpdate:           {module: "media", name: "更新媒体"},
	MediaUpload:           {module: "media", name: "上传媒体"},
	MediaVariantsGenerate: {module: "media", name: "重新生成媒体变体"},

	MembershipAssignList:      {module: "membership", name: "会员归属列表"},
	MembershipAssignSet:       {module: "membership", name: "手工指定会员等级"},
	MembershipAssignUnlock:    {module: "membership", name: "取消手工锁定"},
	MembershipEntitlementSave: {module: "membership", name: "保存等级权益"},
	MembershipRecalc:          {module: "membership", name: "重算会员归属"},
	MembershipResolve:         {module: "membership", name: "解析会员身份"},
	MembershipTierCreate:      {module: "membership", name: "新建等级"},
	MembershipTierDelete:      {module: "membership", name: "删除等级"},
	MembershipTierGet:         {module: "membership", name: "等级详情"},
	MembershipTierList:        {module: "membership", name: "等级列表"},
	MembershipTierUpdate:      {module: "membership", name: "更新等级"},

	MenuCreate: {module: "menu", name: "新建菜单"},
	MenuDelete: {module: "menu", name: "删除菜单"},
	MenuDetail: {module: "menu", name: "菜单详情"},
	MenuList:   {module: "menu", name: "菜单树"},
	MenuUpdate: {module: "menu", name: "更新菜单"},

	NavigationCreate: {module: "navigation", name: "导航创建"},
	NavigationDelete: {module: "navigation", name: "导航删除"},
	NavigationGet:    {module: "navigation", name: "导航详情"},
	NavigationList:   {module: "navigation", name: "导航列表"},
	NavigationUpdate:    {module: "navigation", name: "导航更新"},
	NavigationMove:      {module: "navigation", name: "导航排序"},
	NavigationTranslate: {module: "navigation", name: "导航译文写入"},
	NavigationSourceAdd: {module: "navigation", name: "来源加入导航"},
	NavigationDeleteBulk: {module: "navigation", name: "导航批量删除"},

	OrderCancel:           {module: "order", name: "取消订单"},
	OrderCouponCountAudit: {module: "order", name: "券计数对账"},
	OrderCouponCreate:     {module: "order", name: "新建优惠码"},
	OrderCouponDelete:     {module: "order", name: "删除优惠码"},
	OrderCouponGet:        {module: "order", name: "优惠码详情"},
	OrderCouponList:       {module: "order", name: "优惠码列表"},
	OrderCouponRedemption: {module: "order", name: "优惠码核销记录"},
	OrderCouponUpdate:     {module: "order", name: "修改优惠码"},
	OrderCouponValidate:   {module: "order", name: "优惠码试算"},
	OrderCreate:           {module: "order", name: "新建订单"},
	OrderGet:              {module: "order", name: "订单详情"},
	OrderItemList:         {module: "order", name: "订单项列表"},
	OrderList:             {module: "order", name: "订单列表"},
	OrderLogList:          {module: "order", name: "状态流转流水"},
	OrderNote:             {module: "order", name: "订单备注"},
	OrderOverview:         {module: "order", name: "销售概览"},
	OrderCurrencyRateGet:  {module: "order", name: "读汇率"},
	OrderCurrencyRateSet:  {module: "order", name: "写汇率"},
	OrderRefund:           {module: "order", name: "订单退款"},
	OrderReturnApprove:    {module: "order", name: "同意退货"},
	OrderReturnGet:        {module: "order", name: "退货申请详情"},
	OrderReturnList:       {module: "order", name: "退货申请列表"},
	OrderReturnReceive:    {module: "order", name: "退货入库"},
	OrderReturnReject:     {module: "order", name: "拒绝退货"},
	OrderStatus:           {module: "order", name: "订单状态流转"},
	OrderShip:             {module: "order", name: "订单发货"},
	OrderShipForm:         {module: "order", name: "发货表单"},
	OrderShipmentList:     {module: "order", name: "发货单列表"},
	OrderPendingShip:      {module: "order", name: "待发货"},
	OrderCarrierCreate:    {module: "order", name: "新建承运商"},
	OrderCarrierManage:    {module: "order", name: "承运商配置"},
	OrderCarrierList:      {module: "order", name: "承运商列表"},

	PageArtifactGc:       {module: "page", name: "回收产物文件"},
	PageArtifactRebuild:  {module: "page", name: "重建产物文件"},
	PageBuild:            {module: "page", name: "构建页面"},
	PageCreate:           {module: "page", name: "新建页面"},
	PageDelete:           {module: "page", name: "删除页面"},
	PageDetail:           {module: "page", name: "页面详情"},
	PageDraftSave:        {module: "page", name: "保存草稿"},
	PageList:             {module: "page", name: "页面列表"},
	PagePublicationAudit: {module: "page", name: "发布面巡检"},
	PagePublish:          {module: "page", name: "发布页面"},
	PageRedirectCreate:   {module: "page", name: "新增重定向"},
	PageRedirectDelete:   {module: "page", name: "删除重定向"},
	PageRedirectMerge:    {module: "page", name: "合并重定向链"},
	PageRedirectView:     {module: "page", name: "重定向列表"},
	PageRevisionList:     {module: "page", name: "修订记录"},
	PageRollback:         {module: "page", name: "回滚页面"},
	PageSEOPatrol:        {module: "page", name: "SEO 合规巡检"},
	PageURLUpdate:        {module: "page", name: "更新页面URL"},
	PageTranslationsSave: {module: "page", name: "保存页面译文"},

	PermissionCreate:  {module: "permission", name: "新建权限点"},
	PermissionDelete:  {module: "permission", name: "删除权限点"},
	PermissionDetail:  {module: "permission", name: "权限点详情"},
	PermissionList:    {module: "permission", name: "权限点列表"},
	PermissionOptions: {module: "permission", name: "权限点选项"},
	PermissionUpdate:  {module: "permission", name: "更新权限点"},

	PluginDetail:    {module: "plugin", name: "插件详情"},
	PluginInstall:   {module: "plugin", name: "插件安装"},
	PluginList:      {module: "plugin", name: "插件列表"},
	PluginToggle:    {module: "plugin", name: "插件启停"},
	PluginUninstall: {module: "plugin", name: "插件卸载"},

	PresentationCreate:      {module: "presentation", name: "实例创建"},
	PresentationDelete:      {module: "presentation", name: "实例删除"},
	PresentationGet:         {module: "presentation", name: "实例详情"},
	PresentationGetByEntity: {module: "presentation", name: "实例按实体查询"},
	PresentationList:        {module: "presentation", name: "实例列表"},
	PresentationPreview:     {module: "presentation", name: "实例预览"},
	PresentationRebuild:     {module: "presentation", name: "实例重建"},
	PresentationUpdateURL:   {module: "presentation", name: "详情页改 URL"},

	ProductAttributeBulkDelete:     {module: "product", name: "属性组批量删除"},
	ProductAttributeCreate:         {module: "product", name: "属性组创建"},
	ProductAttributeDelete:         {module: "product", name: "属性组删除"},
	ProductAttributeGet:            {module: "product", name: "属性组详情"},
	ProductAttributeList:           {module: "product", name: "属性组列表"},
	ProductAttributeSetValues:      {module: "product", name: "属性值保存"},
	ProductAttributeUpdate:         {module: "product", name: "属性组更新"},
	ProductBrandBulkDelete:         {module: "product", name: "商品品牌批量删除"},
	ProductBrandCreate:             {module: "product", name: "商品品牌创建"},
	ProductBrandDelete:             {module: "product", name: "商品品牌删除"},
	ProductBrandGet:                {module: "product", name: "商品品牌详情"},
	ProductBrandList:               {module: "product", name: "商品品牌列表"},
	ProductBrandUpdate:             {module: "product", name: "商品品牌更新"},
	ProductBulkDelete:              {module: "product", name: "商品批量删除"},
	ProductBulkPricing:             {module: "product", name: "批量改价"},
	ProductSEOScore:                {module: "product", name: "商品 SEO 评分"},
	ProductCategorySEOScore:        {module: "product", name: "商品分类 SEO 评分"},
	ProductBrandSEOScore:           {module: "product", name: "商品品牌 SEO 评分"},
	ProductCategoryParents:         {module: "product", name: "商品父分类候选"},
	ProductVariantPreview:          {module: "product", name: "商品变体组合预览"},
	ProductCategoryBulkDelete:      {module: "product", name: "商品分类批量删除"},
	ProductCategoryCreate:          {module: "product", name: "商品分类创建"},
	ProductCategoryDelete:          {module: "product", name: "商品分类删除"},
	ProductCategoryGet:             {module: "product", name: "商品分类详情"},
	ProductCategoryList:            {module: "product", name: "商品分类列表"},
	ProductCategoryUpdate:          {module: "product", name: "商品分类更新"},
	ProductCreate:                  {module: "product", name: "商品创建"},
	ProductDelete:                  {module: "product", name: "商品删除"},
	ProductTranslationsSave:        {module: "product", name: "商品译文保存"},
	ProductDetailTemplateCreate:    {module: "product", name: "商品详情模板新建"},
	ProductDetailTemplatePublish:   {module: "product", name: "商品详情模板发布"},
	ProductDetailTemplateApply:     {module: "product", name: "商品详情模板切换"},
	ProductDetailTemplateUpdateURL: {module: "product", name: "商品详情模板改 URL"},
	ProductDetailTemplateReapply:   {module: "product", name: "详情页模板套用预设"},
	ProductDetailTemplateRollback:  {module: "product", name: "详情页模板回滚快照"},
	ProductGet:                     {module: "product", name: "商品详情"},
	ProductPublishAttempts:         {module: "product", name: "商品发布历史"},
	ProductList:                    {module: "product", name: "商品列表"},
	ProductPricingAdjustment:       {module: "product", name: "定价留痕详情"},
	ProductPricingApply:            {module: "product", name: "定价应用落库"},
	ProductPricingHistory:          {module: "product", name: "定价留痕列表"},
	ProductPricingPreview:          {module: "product", name: "定价试算预览"},
	ProductPricingRoundings:        {module: "product", name: "定价尾数处理"},
	ProductPricingRules:            {module: "product", name: "定价规则类型"},
	ProductRatingAdd:               {module: "product", name: "商品评分新增"},
	ProductRatingDelete:            {module: "product", name: "商品评分删除"},
	ProductTagBulkDelete:           {module: "product", name: "商品标签批量删除"},
	ProductTagCreate:               {module: "product", name: "商品标签创建"},
	ProductTagDelete:               {module: "product", name: "商品标签删除"},
	ProductTagGet:                  {module: "product", name: "商品标签详情"},
	ProductTagList:                 {module: "product", name: "商品标签列表"},
	ProductTagProducts:             {module: "product", name: "商品标签命中商品"},
	ProductTagRecalc:               {module: "product", name: "商品标签重算"},
	ProductTagRuleTypes:            {module: "product", name: "商品标签规则类型"},
	ProductTagUpdate:               {module: "product", name: "商品标签更新"},
	ProductUpdate:                  {module: "product", name: "商品更新"},
	ProductVariantCreate:           {module: "product", name: "变体创建"},
	ProductVariantDelete:           {module: "product", name: "变体删除"},
	ProductVariantGenerate:         {module: "product", name: "变体组合生成"},
	ProductVariantSave:             {module: "product", name: "变体清单保存"},
	ProductVariantUpdate:           {module: "product", name: "变体更新"},

	ProjectCreate:        {module: "project", name: "新建项目"},
	ProjectDetail:        {module: "project", name: "项目详情"},
	ProjectList:          {module: "project", name: "项目列表"},
	ProjectThemeActivate: {module: "project", name: "激活主题"},
	ProjectThemeActive:   {module: "project", name: "当前主题"},
	ProjectThemeCreate:   {module: "project", name: "新建主题"},
	ProjectThemeDelete:   {module: "project", name: "删除主题"},
	ProjectThemeList:     {module: "project", name: "主题列表"},
	ProjectThemeUpdate:   {module: "project", name: "更新主题"},
	ProjectUpdate:        {module: "project", name: "更新项目"},

	SysConfigGet:        {module: "sysconfig", name: "读取系统设置"},
	SysConfigSave:       {module: "sysconfig", name: "保存系统设置"},
	SysConfigSystemSave: {module: "sysconfig", name: "原子保存系统默认设置"},

	PublicationReceiptsPending: {module: "publication", name: "待处理发布回执"},
	PublicationSEOAudit:        {module: "publication", name: "SEO 审计"},

	RoleCreate:   {module: "role", name: "新建角色"},
	RoleDelete:   {module: "role", name: "删除角色"},
	RoleDetail:   {module: "role", name: "角色详情"},
	RoleList:     {module: "role", name: "角色列表"},
	RoleMenuList: {module: "role", name: "角色菜单查看"},
	RoleMenuSave: {module: "role", name: "角色菜单分配"},
	RoleUpdate:   {module: "role", name: "更新角色"},
	RoleUserList: {module: "role", name: "角色用户查看"},
	RoleUserSave: {module: "role", name: "角色用户分配"},

	SEOAudit: {module: "seo", name: "产物 SEO 体检"},

	UserCustomerDetail:     {module: "user", name: "查看客户详情"},
	UserCustomerList:       {module: "user", name: "查看客户列表"},
	UserCustomerStatus:     {module: "user", name: "停用/启用客户账号"},
	UserCustomerUnlock:     {module: "user", name: "解除客户账号锁定"},
	UserCustomerBulkStatus: {module: "user", name: "批量停用/启用客户账号"},
	UserCustomerBulkUnlock: {module: "user", name: "批量解除客户账号锁定"},

	WebhookDeliveryList:   {module: "webhook", name: "投递日志"},
	WebhookDeliveryRetry:  {module: "webhook", name: "重投投递"},
	WebhookEndpointDelete: {module: "webhook", name: "删除集成端点"},
	WebhookEndpointList:   {module: "webhook", name: "集成端点列表"},
	WebhookEndpointSave:   {module: "webhook", name: "保存集成端点"},
	WebhookEndpointStatus: {module: "webhook", name: "启停集成端点"},

	AIProviderList:          {module: "ai", name: "供应商列表"},
	AIProviderGet:           {module: "ai", name: "供应商详情"},
	AIProviderModelsList:    {module: "ai", name: "供应商可用模型列表"},
	AIProviderSave:          {module: "ai", name: "保存供应商"},
	AIProviderDelete:        {module: "ai", name: "删除供应商"},
	AIProviderStatus:        {module: "ai", name: "启停供应商"},
	AIProviderModelsSave:    {module: "ai", name: "保存模型目录"},
	AIProviderModelsRestore: {module: "ai", name: "恢复默认模型"},
	AIProviderModelsFetch:   {module: "ai", name: "拉取可用模型"},

	AIProviderModelsCandidates: {module: "ai", name: "模型候选清单"},
	AIProviderModelsAppend:     {module: "ai", name: "追加模型到目录"},
	AISessionList:              {module: "ai", name: "会话列表"},
	AISessionGet:               {module: "ai", name: "会话详情"},
	AISessionEvents:            {module: "ai", name: "会话事件日志"},
	AISessionFoldPlan:          {module: "ai", name: "会话折叠建议"},
	AISessionAppend:            {module: "ai", name: "追加会话事件"},
	AISessionRename:            {module: "ai", name: "重命名会话"},
	AISessionArchive:           {module: "ai", name: "归档会话"},
	AISessionFold:              {module: "ai", name: "折叠会话"},
	AISessionSend:              {module: "ai", name: "向指定会话发消息"},
	AIChat:                     {module: "ai", name: "对话入口"},
	AITokenList:                {module: "ai", name: "外部访问令牌查看"},
	AITokenCreate:              {module: "ai", name: "外部访问令牌签发"},
	AITokenRevoke:              {module: "ai", name: "外部访问令牌撤销"},
	AITokenToggle:              {module: "ai", name: "对外接入点开关"},
}
