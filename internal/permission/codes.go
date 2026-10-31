package permission

const (
	AdminCreate Perm = "admin:create"

	AdminDelete Perm = "admin:delete"

	AdminDetail Perm = "admin:detail"

	AdminEdit Perm = "admin:edit"

	AdminList Perm = "admin:list"

	AdminMenuList Perm = "admin:menu_list"

	AdminMenuSave Perm = "admin:menu_save"

	AdminRoleList Perm = "admin:role_list"

	AdminRoleSave Perm = "admin:role_save"

	AnalyticsView Perm = "analytics:view"

	ArtifactDetail Perm = "artifact:detail"

	BlockBulkDelete Perm = "block:bulk_delete"

	BlockClone Perm = "block:clone"

	BlockCreate Perm = "block:create"

	BlockDelete Perm = "block:delete"

	BlockDetail Perm = "block:detail"

	BlockList Perm = "block:list"

	BlockUpdate Perm = "block:update"

	BlueprintCreate Perm = "blueprint:create"

	BlueprintDelete Perm = "blueprint:delete"

	BlueprintGet Perm = "blueprint:get"

	BlueprintInit Perm = "blueprint:init"

	BlueprintList Perm = "blueprint:list"

	BlueprintPublish Perm = "blueprint:publish"

	BlueprintUpdate Perm = "blueprint:update"

	BuildJobs Perm = "build:jobs"

	BuildQueue Perm = "build:queue"

	BuildRetry Perm = "build:retry"

	CommentList Perm = "comment:list"

	CommentReview Perm = "comment:review"

	ContentCollections Perm = "content:collections"

	ContentCreate Perm = "content:create"

	ContentDelete Perm = "content:delete"

	ContentGet Perm = "content:get"

	ContentList Perm = "content:list"

	ContentUpdate Perm = "content:update"

	ContenttemplateCreate Perm = "contenttemplate:create"

	ContenttemplateDelete Perm = "contenttemplate:delete"

	ContenttemplateGet Perm = "contenttemplate:get"

	ContenttemplateList Perm = "contenttemplate:list"

	ContenttemplateUpdate Perm = "contenttemplate:update"

	ContenttemplateActivate Perm = "contenttemplate:activate"

	DataruleAssignmentList Perm = "datarule:assignment_list"

	DataruleAssignmentSave Perm = "datarule:assignment_save"

	DataruleCreate Perm = "datarule:create"

	DataruleDelete Perm = "datarule:delete"

	DataruleDetail Perm = "datarule:detail"

	DataruleList Perm = "datarule:list"

	DataruleSchemaDetail Perm = "datarule:schema_detail"

	DataruleSchemaList Perm = "datarule:schema_list"

	DataruleUpdate Perm = "datarule:update"

	DeptCreate Perm = "dept:create"

	DeptDelete Perm = "dept:delete"

	DeptDetail Perm = "dept:detail"

	DeptList Perm = "dept:list"

	DeptUpdate Perm = "dept:update"

	DeptUserList Perm = "dept:user_list"

	DeptUserSave Perm = "dept:user_save"

	I18nView Perm = "i18n:view"

	I18nManage Perm = "i18n:manage"

	InventoryMovementList Perm = "inventory:movement_list"

	InventoryPurchaseCreate Perm = "inventory:purchase_create"

	InventoryPurchaseGet Perm = "inventory:purchase_get"

	InventoryPurchaseHistory Perm = "inventory:purchase_history"

	InventoryPurchaseList Perm = "inventory:purchase_list"

	InventoryPurchaseProduction Perm = "inventory:purchase_production"

	InventoryStockAlert Perm = "inventory:stock_alert"

	InventoryTransferList Perm = "inventory:transfer_list"
	InventoryTransferGet  Perm = "inventory:transfer_get"

	InventoryTransferOptions Perm = "inventory:transfer_options"
	InventoryTransferCreate  Perm = "inventory:transfer_create"
	InventoryTransferApprove Perm = "inventory:transfer_approve"
	InventoryTransferReject  Perm = "inventory:transfer_reject"
	InventoryTransferShip    Perm = "inventory:transfer_ship"
	InventoryTransferReceive Perm = "inventory:transfer_receive"
	InventoryTransferCancel  Perm = "inventory:transfer_cancel"

	InventoryPurchaseReceipt Perm = "inventory:purchase_receipt"

	InventoryPurchaseUpdate Perm = "inventory:purchase_update"

	InventoryReasonCreate Perm = "inventory:reason_create"

	InventoryReasonList Perm = "inventory:reason_list"

	InventoryReasonUpdate Perm = "inventory:reason_update"

	InventorySourceCreate Perm = "inventory:source_create"

	InventorySourceDelete Perm = "inventory:source_delete"

	InventorySourceGet Perm = "inventory:source_get"

	InventorySourceList Perm = "inventory:source_list"

	InventorySourceSummary Perm = "inventory:source_summary"

	InventorySourceUpdate Perm = "inventory:source_update"

	InventoryStockChange Perm = "inventory:stock_change"

	InventoryStockDeduct Perm = "inventory:stock_deduct"

	InventoryStockEnsure Perm = "inventory:stock_ensure"

	InventoryStockGet Perm = "inventory:stock_get"

	InventoryStockList Perm = "inventory:stock_list"

	InventoryStockSKU Perm = "inventory:stock_sku"

	InventoryWarehouseCreate Perm = "inventory:warehouse_create"

	InventoryWarehouseDelete Perm = "inventory:warehouse_delete"

	InventoryWarehouseGet Perm = "inventory:warehouse_get"

	InventoryWarehouseList Perm = "inventory:warehouse_list"

	InventoryWarehouseUpdate Perm = "inventory:warehouse_update"

	MailAccountDefault Perm = "mail:account_default"

	MailAccountDelete Perm = "mail:account_delete"

	MailAccountList Perm = "mail:account_list"

	MailAccountSave Perm = "mail:account_save"

	MailAccountTest Perm = "mail:account_test"

	MailAutomationDelete Perm = "mail:automation_delete"

	MailAutomationGet Perm = "mail:automation_get"

	MailAutomationLayout Perm = "mail:automation_layout"

	MailAutomationList Perm = "mail:automation_list"

	MailAutomationRunDetail Perm = "mail:automation_run_detail"

	MailAutomationRunList Perm = "mail:automation_run_list"

	MailAutomationSave Perm = "mail:automation_save"

	MailAutomationStart Perm = "mail:automation_start"

	MailAutomationStatus Perm = "mail:automation_status"

	MailAutomationTick Perm = "mail:automation_tick"

	MailCampaignDelete Perm = "mail:campaign_delete"

	MailCampaignGet Perm = "mail:campaign_get"

	MailCampaignList Perm = "mail:campaign_list"

	MailCampaignSave Perm = "mail:campaign_save"

	MailCampaignStart Perm = "mail:campaign_start"

	MailContactDelete Perm = "mail:contact_delete"

	MailContactImport Perm = "mail:contact_import"

	MailContactList Perm = "mail:contact_list"

	MailContactSave Perm = "mail:contact_save"

	MailContactStatus Perm = "mail:contact_status"

	MailContactTag Perm = "mail:contact_tag"

	MailTemplateDelete Perm = "mail:template_delete"

	MailTemplateList Perm = "mail:template_list"

	MailTemplateSave Perm = "mail:template_save"

	MasterdataChangeCount Perm = "masterdata:change_count"

	MasterdataChangeEntities Perm = "masterdata:change_entities"

	MasterdataChangeEntity Perm = "masterdata:change_entity"

	MasterdataChangeList Perm = "masterdata:change_list"

	MediaCategoryCreate Perm = "media:category_create"

	MediaCategoryDelete Perm = "media:category_delete"

	MediaCategoryTree Perm = "media:category_tree"

	MediaCategoryUpdate Perm = "media:category_update"

	MediaDelete Perm = "media:delete"

	MediaDetail Perm = "media:detail"

	MediaDownload Perm = "media:download"

	MediaDownloadBatch Perm = "media:download_batch"

	MediaList Perm = "media:list"

	MediaReferences Perm = "media:references"

	MediaReplace Perm = "media:replace"

	MediaUpdate Perm = "media:update"

	MediaUpload Perm = "media:upload"

	MediaVariantsGenerate Perm = "media:variants_generate"

	MembershipAssignList Perm = "membership:assign_list"

	MembershipAssignSet Perm = "membership:assign_set"

	MembershipAssignUnlock Perm = "membership:assign_unlock"

	MembershipEntitlementSave Perm = "membership:entitlement_save"

	MembershipRecalc Perm = "membership:recalc"

	MembershipResolve Perm = "membership:resolve"

	MembershipTierCreate Perm = "membership:tier_create"

	MembershipTierDelete Perm = "membership:tier_delete"

	MembershipTierGet Perm = "membership:tier_get"

	MembershipTierList Perm = "membership:tier_list"

	MembershipTierUpdate Perm = "membership:tier_update"

	MenuCreate Perm = "menu:create"

	MenuDelete Perm = "menu:delete"

	MenuDetail Perm = "menu:detail"

	MenuList Perm = "menu:list"

	MenuUpdate Perm = "menu:update"

	NavigationCreate Perm = "navigation:create"

	NavigationDelete Perm = "navigation:delete"

	NavigationGet Perm = "navigation:get"

	NavigationList Perm = "navigation:list"

	NavigationUpdate Perm = "navigation:update"

	OrderCancel Perm = "order:cancel"

	OrderCouponCountAudit Perm = "order:coupon_count_audit"

	OrderCouponCreate Perm = "order:coupon_create"

	OrderCouponDelete Perm = "order:coupon_delete"

	OrderCouponGet Perm = "order:coupon_get"

	OrderCouponList Perm = "order:coupon_list"

	OrderCouponRedemption Perm = "order:coupon_redemption"

	OrderCouponUpdate Perm = "order:coupon_update"

	OrderCouponValidate Perm = "order:coupon_validate"

	OrderCreate Perm = "order:create"

	OrderGet Perm = "order:get"

	OrderItemList Perm = "order:item_list"

	OrderList Perm = "order:list"

	OrderLogList Perm = "order:log_list"

	OrderNote Perm = "order:note"

	OrderOverview Perm = "order:overview"

	OrderCurrencyRateGet Perm = "order:currency_rate_get"
	OrderCurrencyRateSet Perm = "order:currency_rate_set"

	OrderRefund Perm = "order:refund"

	OrderReturnApprove Perm = "order:return_approve"

	OrderReturnGet Perm = "order:return_get"

	OrderReturnList Perm = "order:return_list"

	OrderReturnReceive Perm = "order:return_receive"

	OrderReturnReject Perm = "order:return_reject"

	OrderStatus Perm = "order:status"

	OrderShip Perm = "order:ship"

	OrderShipForm Perm = "order:ship_form"

	OrderShipmentList Perm = "order:shipment_list"

	OrderPendingShip Perm = "order:pending_ship"

	OrderCarrierCreate Perm = "order:carrier_create"

	OrderCarrierManage Perm = "order:carrier_manage"

	OrderCarrierList Perm = "order:carrier_list"

	PageArtifactGc Perm = "page:artifact_gc"

	PageArtifactRebuild Perm = "page:artifact_rebuild"

	PageBuild Perm = "page:build"

	PageCreate Perm = "page:create"

	PageDelete Perm = "page:delete"

	PageDetail Perm = "page:detail"

	PageDraftSave Perm = "page:draft_save"

	PageList Perm = "page:list"

	PagePublicationAudit Perm = "page:publication_audit"

	PagePublish Perm = "page:publish"

	PageRedirectCreate Perm = "page:redirect_create"

	PageRedirectDelete Perm = "page:redirect_delete"

	PageRedirectMerge Perm = "page:redirect_merge"

	PageRedirectView Perm = "page:redirect_view"

	PageRevisionList Perm = "page:revision_list"

	PageRollback Perm = "page:rollback"

	PageSEOPatrol Perm = "page:seo_patrol"

	PageURLUpdate Perm = "page:url_update"

	PermissionCreate Perm = "permission:create"

	PermissionDelete Perm = "permission:delete"

	PermissionDetail Perm = "permission:detail"

	PermissionList Perm = "permission:list"

	PermissionOptions Perm = "permission:options"

	PermissionUpdate Perm = "permission:update"

	PluginDetail Perm = "plugin:detail"

	PluginInstall Perm = "plugin:install"

	PluginList Perm = "plugin:list"

	PluginToggle Perm = "plugin:toggle"

	PluginUninstall Perm = "plugin:uninstall"

	PresentationCreate Perm = "presentation:create"

	PresentationDelete Perm = "presentation:delete"

	PresentationGet Perm = "presentation:get"

	PresentationGetByEntity Perm = "presentation:get_by_entity"

	PresentationList Perm = "presentation:list"

	PresentationPreview Perm = "presentation:preview"

	PresentationRebuild Perm = "presentation:rebuild"

	PresentationUpdateURL Perm = "presentation:update_url"

	ProductAttributeBulkDelete Perm = "product:attribute_bulk_delete"

	ProductAttributeCreate Perm = "product:attribute_create"

	ProductAttributeDelete Perm = "product:attribute_delete"

	ProductAttributeGet Perm = "product:attribute_get"

	ProductAttributeList Perm = "product:attribute_list"

	ProductAttributeSetValues Perm = "product:attribute_set_values"

	ProductAttributeUpdate Perm = "product:attribute_update"

	ProductBrandBulkDelete Perm = "product:brand_bulk_delete"

	ProductBrandCreate Perm = "product:brand_create"

	ProductBrandDelete Perm = "product:brand_delete"

	ProductBrandGet Perm = "product:brand_get"

	ProductBrandList Perm = "product:brand_list"

	ProductBrandUpdate Perm = "product:brand_update"

	ProductBulkDelete Perm = "product:bulk_delete"

	ProductBulkPricing Perm = "product:bulk_pricing"

	ProductCategoryBulkDelete Perm = "product:category_bulk_delete"

	ProductCategoryCreate Perm = "product:category_create"

	ProductCategoryDelete Perm = "product:category_delete"

	ProductCategoryGet Perm = "product:category_get"

	ProductCategoryList Perm = "product:category_list"

	ProductCategoryUpdate Perm = "product:category_update"

	ProductCreate Perm = "product:create"

	ProductDelete Perm = "product:delete"

	ProductTranslationsSave Perm = "product:translations_save"

	ProductDetailTemplateCreate Perm = "product:detail_template_create"

	ProductDetailTemplatePublish Perm = "product:detail_template_publish"

	ProductDetailTemplateApply Perm = "product:detail_template_apply"

	ProductDetailTemplateUpdateURL Perm = "product:detail_template_update_url"

	ProductDetailTemplateReapply Perm = "product:detail_template_reapply"

	ProductDetailTemplateRollback Perm = "product:detail_template_rollback"

	ProductPublishAttempts Perm = "product:publish_attempts"

	ProductGet Perm = "product:get"

	ProductList Perm = "product:list"

	ProductPricingAdjustment Perm = "product:pricing_adjustment"

	ProductPricingApply Perm = "product:pricing_apply"

	ProductPricingHistory Perm = "product:pricing_history"

	ProductPricingPreview Perm = "product:pricing_preview"

	ProductPricingRoundings Perm = "product:pricing_roundings"

	ProductPricingRules Perm = "product:pricing_rules"

	ProductRatingAdd Perm = "product:rating_add"

	ProductRatingDelete Perm = "product:rating_delete"

	ProductTagBulkDelete Perm = "product:tag_bulk_delete"

	ProductTagCreate Perm = "product:tag_create"

	ProductTagDelete Perm = "product:tag_delete"

	ProductTagGet Perm = "product:tag_get"

	ProductTagList Perm = "product:tag_list"

	ProductTagProducts Perm = "product:tag_products"

	ProductTagRecalc Perm = "product:tag_recalc"

	ProductTagRuleTypes Perm = "product:tag_rule_types"

	ProductTagUpdate Perm = "product:tag_update"

	ProductUpdate Perm = "product:update"

	ProductVariantCreate Perm = "product:variant_create"

	ProductVariantDelete Perm = "product:variant_delete"

	ProductVariantGenerate Perm = "product:variant_generate"

	ProductVariantSave Perm = "product:variant_save"

	ProductVariantUpdate Perm = "product:variant_update"

	ProductSEOScore         Perm = "product:seo_score"
	ProductCategorySEOScore Perm = "product:category_seo_score"
	ProductBrandSEOScore    Perm = "product:brand_seo_score"
	ProductCategoryParents  Perm = "product:category_parents"
	ProductVariantPreview   Perm = "product:variant_preview"

	ProjectCreate Perm = "project:create"

	ProjectDetail Perm = "project:detail"

	ProjectList Perm = "project:list"

	ProjectThemeActivate Perm = "project:theme_activate"

	ProjectThemeActive Perm = "project:theme_active"

	ProjectThemeCreate Perm = "project:theme_create"

	ProjectThemeDelete Perm = "project:theme_delete"

	ProjectThemeList Perm = "project:theme_list"

	ProjectThemeUpdate Perm = "project:theme_update"

	ProjectUpdate Perm = "project:update"

	SysConfigGet Perm = "sysconfig:get"

	SysConfigSave Perm = "sysconfig:save"

	SysConfigSystemSave Perm = "sysconfig:system_save"

	PublicationReceiptsPending Perm = "publication:receipts_pending"

	PublicationSEOAudit Perm = "publication:seo_audit"

	RoleCreate Perm = "role:create"

	RoleDelete Perm = "role:delete"

	RoleDetail Perm = "role:detail"

	RoleList Perm = "role:list"

	RoleMenuList Perm = "role:menu_list"

	RoleMenuSave Perm = "role:menu_save"

	RoleUpdate Perm = "role:update"

	RoleUserList Perm = "role:user_list"

	RoleUserSave Perm = "role:user_save"

	SEOAudit Perm = "seo:audit"

	UserCustomerDetail Perm = "user:customer_detail"

	UserCustomerList Perm = "user:customer_list"

	UserCustomerStatus Perm = "user:customer_status"

	UserCustomerUnlock     Perm = "user:customer_unlock"
	UserCustomerBulkStatus Perm = "user:customer_bulk_status"
	UserCustomerBulkUnlock Perm = "user:customer_bulk_unlock"

	WebhookDeliveryList Perm = "webhook:delivery_list"

	WebhookDeliveryRetry Perm = "webhook:delivery_retry"

	WebhookEndpointDelete Perm = "webhook:endpoint_delete"

	WebhookEndpointList Perm = "webhook:endpoint_list"

	WebhookEndpointSave Perm = "webhook:endpoint_save"

	WebhookEndpointStatus Perm = "webhook:endpoint_status"

	AIProviderList Perm = "ai:provider_list"

	AIProviderGet Perm = "ai:provider_get"

	AIProviderModelsList Perm = "ai:provider_models_list"

	AIProviderSave Perm = "ai:provider_save"

	AIProviderDelete Perm = "ai:provider_delete"

	AIProviderStatus Perm = "ai:provider_status"

	AIProviderModelsSave Perm = "ai:provider_models_save"

	AIProviderModelsRestore Perm = "ai:provider_models_restore"

	AIProviderModelsFetch Perm = "ai:provider_models_fetch"

	AISessionList Perm = "ai:session_list"

	AISessionGet Perm = "ai:session_get"

	AISessionEvents Perm = "ai:session_events"

	AISessionFoldPlan Perm = "ai:session_fold_plan"

	AISessionAppend Perm = "ai:session_append"

	AISessionRename Perm = "ai:session_rename"

	AISessionArchive Perm = "ai:session_archive"

	AISessionFold Perm = "ai:session_fold"

	AIChat Perm = "ai:chat"

	AITokenList Perm = "ai:token_list"

	AITokenCreate Perm = "ai:token_create"

	AITokenRevoke Perm = "ai:token_revoke"
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
	NavigationUpdate: {module: "navigation", name: "导航更新"},

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
	AISessionList:           {module: "ai", name: "会话列表"},
	AISessionGet:            {module: "ai", name: "会话详情"},
	AISessionEvents:         {module: "ai", name: "会话事件日志"},
	AISessionFoldPlan:       {module: "ai", name: "会话折叠建议"},
	AISessionAppend:         {module: "ai", name: "追加会话事件"},
	AISessionRename:         {module: "ai", name: "重命名会话"},
	AISessionArchive:        {module: "ai", name: "归档会话"},
	AISessionFold:           {module: "ai", name: "折叠会话"},
	AIChat:                  {module: "ai", name: "对话入口"},
	AITokenList:             {module: "ai", name: "外部访问令牌查看"},
	AITokenCreate:           {module: "ai", name: "外部访问令牌签发"},
	AITokenRevoke:           {module: "ai", name: "外部访问令牌撤销"},
}
