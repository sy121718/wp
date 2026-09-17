// codes.go — 权限点代码常量（sys_permission.permission_code 的代码侧单一来源）。
//
// 每条常量都带中文名、所属模块与它绑定的路由（方法 + 绝对路径），供注册期登记、启动期
// upsert 使用；生成规则：`模块:动作` → `模块动作`（下划线分段转驼峰，URL / ID / SKU / SEO 保持惯用写法）。
//
// **不要手工改常量字符串**：它必须与库中既有 permission_code 逐字一致，改名等于新建一个
// 权限点（旧 code 上的角色 / 用户授权会失效）。新增权限点走「加一条常量 + 在路由注册处声明」，
// 启动期自动 upsert 入库，不再需要写 seed 迁移。
package permission

const (
	// —— admin（9）——
	// 新建管理员（POST /api/admin/create）
	AdminCreate Perm = "admin:create"
	// 删除管理员（POST /api/admin/delete）
	AdminDelete Perm = "admin:delete"
	// 管理员详情（GET /api/admin/detail）
	AdminDetail Perm = "admin:detail"
	// 编辑管理员（POST /api/admin/edit）
	AdminEdit Perm = "admin:edit"
	// 管理员列表（GET /api/admin/list）
	AdminList Perm = "admin:list"
	// 管理员菜单查看（GET /api/admin/menu/list）
	AdminMenuList Perm = "admin:menu_list"
	// 管理员菜单分配（POST /api/admin/menu/save）
	AdminMenuSave Perm = "admin:menu_save"
	// 管理员角色查看（GET /api/admin/role/list）
	AdminRoleList Perm = "admin:role_list"
	// 管理员角色分配（POST /api/admin/role/save）
	AdminRoleSave Perm = "admin:role_save"

	// —— analytics（1）——
	// 访问统计查看（GET /api/analytics/summary）
	AnalyticsView Perm = "analytics:view"

	// —— artifact（1）——
	// 构建产物详情（GET /api/artifact/detail）
	ArtifactDetail Perm = "artifact:detail"

	// —— block（6）——
	// 克隆区块（POST /api/block/clone）
	BlockClone Perm = "block:clone"
	// 新建区块（POST /api/block/create）
	BlockCreate Perm = "block:create"
	// 删除区块（POST /api/block/delete）
	BlockDelete Perm = "block:delete"
	// 区块详情（GET /api/block/detail）
	BlockDetail Perm = "block:detail"
	// 区块列表（GET /api/block/list）
	BlockList Perm = "block:list"
	// 更新区块（POST /api/block/update）
	BlockUpdate Perm = "block:update"

	// —— blueprint（7）——
	// 模板创建（POST /api/blueprint/create）
	BlueprintCreate Perm = "blueprint:create"
	// 模板删除（POST /api/blueprint/delete）
	BlueprintDelete Perm = "blueprint:delete"
	// 模板详情（GET /api/blueprint/get）
	BlueprintGet Perm = "blueprint:get"
	// 初始化页面（GET /api/blueprint/init）
	BlueprintInit Perm = "blueprint:init"
	// 模板列表（GET /api/blueprint/list）
	BlueprintList Perm = "blueprint:list"
	// 模板发布（POST /api/blueprint/publish）
	BlueprintPublish Perm = "blueprint:publish"
	// 模板更新（POST /api/blueprint/update）
	BlueprintUpdate Perm = "blueprint:update"

	// —— build（3）——
	// 构建任务列表（GET /api/build/jobs）
	BuildJobs Perm = "build:jobs"
	// 构建队列状态（GET /api/build/queue）
	BuildQueue Perm = "build:queue"
	// 重试构建（POST /api/build/retry）
	BuildRetry Perm = "build:retry"

	// —— content（6）——
	// 查看内容集合元数据（GET /api/content/collections）
	ContentCollections Perm = "content:collections"
	// 内容创建（POST /api/content/create）
	ContentCreate Perm = "content:create"
	// 内容删除（POST /api/content/delete）
	ContentDelete Perm = "content:delete"
	// 内容详情（GET /api/content/get）
	ContentGet Perm = "content:get"
	// 内容列表（GET /api/content/list）
	ContentList Perm = "content:list"
	// 内容更新（POST /api/content/update）
	ContentUpdate Perm = "content:update"

	// —— contenttemplate（4）——
	// 模板创建（POST /api/contenttemplate/create）
	ContenttemplateCreate Perm = "contenttemplate:create"
	// 模板详情（GET /api/contenttemplate/get）
	ContenttemplateGet Perm = "contenttemplate:get"
	// 模板列表（GET /api/contenttemplate/list）
	ContenttemplateList Perm = "contenttemplate:list"
	// 模板更新（POST /api/contenttemplate/update）
	ContenttemplateUpdate Perm = "contenttemplate:update"

	// —— datarule（9）——
	// 规则分配查看（GET /api/datarule/assignment/list）
	DataruleAssignmentList Perm = "datarule:assignment_list"
	// 规则分配保存（POST /api/datarule/assignment/save）
	DataruleAssignmentSave Perm = "datarule:assignment_save"
	// 新建数据规则（POST /api/datarule/create）
	DataruleCreate Perm = "datarule:create"
	// 删除数据规则（POST /api/datarule/delete）
	DataruleDelete Perm = "datarule:delete"
	// 数据规则详情（GET /api/datarule/detail）
	DataruleDetail Perm = "datarule:detail"
	// 数据规则列表（GET /api/datarule/list）
	DataruleList Perm = "datarule:list"
	// 数据域详情（GET /api/datarule/schema/detail）
	DataruleSchemaDetail Perm = "datarule:schema_detail"
	// 数据域清单（GET /api/datarule/schema/list）
	DataruleSchemaList Perm = "datarule:schema_list"
	// 更新数据规则（POST /api/datarule/update）
	DataruleUpdate Perm = "datarule:update"

	// —— dept（7）——
	// 新建部门（POST /api/dept/create）
	DeptCreate Perm = "dept:create"
	// 删除部门（POST /api/dept/delete）
	DeptDelete Perm = "dept:delete"
	// 部门详情（GET /api/dept/detail）
	DeptDetail Perm = "dept:detail"
	// 部门树（GET /api/dept/tree）
	DeptList Perm = "dept:list"
	// 更新部门（POST /api/dept/update）
	DeptUpdate Perm = "dept:update"
	// 部门用户查看（GET /api/dept/user/list）
	DeptUserList Perm = "dept:user_list"
	// 部门用户分配（POST /api/dept/user/save）
	DeptUserSave Perm = "dept:user_save"

	// —— inventory（30）——
	// 查看物料清单（GET /api/inventory/bom/get）
	InventoryBomGet Perm = "inventory:bom_get"
	// 维护物料清单（POST /api/inventory/bom/set）
	InventoryBomSet Perm = "inventory:bom_set"
	// 库存流水列表（GET /api/inventory/movement/list）
	InventoryMovementList Perm = "inventory:movement_list"
	// 新建采购单（POST /api/inventory/purchase/create）
	InventoryPurchaseCreate Perm = "inventory:purchase_create"
	// 采购单详情（GET /api/inventory/purchase/get）
	InventoryPurchaseGet Perm = "inventory:purchase_get"
	// 进货历史（GET /api/inventory/purchase/history）
	InventoryPurchaseHistory Perm = "inventory:purchase_history"
	// 采购单列表（GET /api/inventory/purchase/list）
	InventoryPurchaseList Perm = "inventory:purchase_list"
	// 生产入库（POST /api/inventory/purchase/production）
	InventoryPurchaseProduction Perm = "inventory:purchase_production"
	// 采购收货入库（POST /api/inventory/purchase/receipt）
	InventoryPurchaseReceipt Perm = "inventory:purchase_receipt"
	// 修改采购单（POST /api/inventory/purchase/update）
	InventoryPurchaseUpdate Perm = "inventory:purchase_update"
	// 新建变动原因（POST /api/inventory/reason/create）
	InventoryReasonCreate Perm = "inventory:reason_create"
	// 变动原因列表（GET /api/inventory/reason/list）
	InventoryReasonList Perm = "inventory:reason_list"
	// 修改变动原因（POST /api/inventory/reason/update）
	InventoryReasonUpdate Perm = "inventory:reason_update"
	// 新建货源（POST /api/inventory/source/create）
	InventorySourceCreate Perm = "inventory:source_create"
	// 删除货源（POST /api/inventory/source/delete）
	InventorySourceDelete Perm = "inventory:source_delete"
	// 货源详情（GET /api/inventory/source/get）
	InventorySourceGet Perm = "inventory:source_get"
	// 货源列表（GET /api/inventory/source/list）
	InventorySourceList Perm = "inventory:source_list"
	// 货源关联方统计（GET /api/inventory/source/summary）
	InventorySourceSummary Perm = "inventory:source_summary"
	// 修改货源（POST /api/inventory/source/update）
	InventorySourceUpdate Perm = "inventory:source_update"
	// 按 SKU 增减库存（POST /api/inventory/stock/change）
	InventoryStockChange Perm = "inventory:stock_change"
	// 按 SKU 扣减库存（POST /api/inventory/stock/deduct）
	InventoryStockDeduct Perm = "inventory:stock_deduct"
	// 库存记录生成（POST /api/inventory/stock/ensure）
	InventoryStockEnsure Perm = "inventory:stock_ensure"
	// 库存记录详情（GET /api/inventory/stock/get）
	InventoryStockGet Perm = "inventory:stock_get"
	// 库存记录列表（GET /api/inventory/stock/list）
	InventoryStockList Perm = "inventory:stock_list"
	// 某 SKU 各仓库存（GET /api/inventory/stock/sku）
	InventoryStockSKU Perm = "inventory:stock_sku"
	// 新建仓库（POST /api/inventory/warehouse/create）
	InventoryWarehouseCreate Perm = "inventory:warehouse_create"
	// 删除仓库（POST /api/inventory/warehouse/delete）
	InventoryWarehouseDelete Perm = "inventory:warehouse_delete"
	// 仓库详情（GET /api/inventory/warehouse/get）
	InventoryWarehouseGet Perm = "inventory:warehouse_get"
	// 仓库列表（GET /api/inventory/warehouse/list）
	InventoryWarehouseList Perm = "inventory:warehouse_list"
	// 修改仓库（POST /api/inventory/warehouse/update）
	InventoryWarehouseUpdate Perm = "inventory:warehouse_update"

	// —— mail（26）——
	// 设默认发信账号（POST /api/mail/account/default）
	MailAccountDefault Perm = "mail:account_default"
	// 删除发信账号（POST /api/mail/account/delete）
	MailAccountDelete Perm = "mail:account_delete"
	// 发信账号列表（GET /api/mail/account/list）
	MailAccountList Perm = "mail:account_list"
	// 保存发信账号（POST /api/mail/account/save）
	MailAccountSave Perm = "mail:account_save"
	// 测试发送邮件（POST /api/mail/account/test）
	MailAccountTest Perm = "mail:account_test"
	// 删除自动化流程（POST /api/mail/automation/delete）
	MailAutomationDelete Perm = "mail:automation_delete"
	// 自动化流程详情（GET /api/mail/automation/get）
	MailAutomationGet Perm = "mail:automation_get"
	// 保存画布位置（POST /api/mail/automation/layout）
	MailAutomationLayout Perm = "mail:automation_layout"
	// 自动化流程列表（GET /api/mail/automation/list）
	MailAutomationList Perm = "mail:automation_list"
	// 自动化实例排障详情（GET /api/mail/automation/run/detail）
	MailAutomationRunDetail Perm = "mail:automation_run_detail"
	// 自动化实例列表（GET /api/mail/automation/run/list）
	MailAutomationRunList Perm = "mail:automation_run_list"
	// 保存自动化流程（POST /api/mail/automation/save）
	MailAutomationSave Perm = "mail:automation_save"
	// 把联系人加入流程（POST /api/mail/automation/start）
	MailAutomationStart Perm = "mail:automation_start"
	// 启停自动化流程（POST /api/mail/automation/status）
	MailAutomationStatus Perm = "mail:automation_status"
	// 补投延时实例（POST /api/mail/automation/tick）
	MailAutomationTick Perm = "mail:automation_tick"
	// 删除群发活动（POST /api/mail/campaign/delete）
	MailCampaignDelete Perm = "mail:campaign_delete"
	// 群发活动详情（GET /api/mail/campaign/get）
	MailCampaignGet Perm = "mail:campaign_get"
	// 群发活动列表（GET /api/mail/campaign/list）
	MailCampaignList Perm = "mail:campaign_list"
	// 保存群发活动（POST /api/mail/campaign/save）
	MailCampaignSave Perm = "mail:campaign_save"
	// 启动群发活动（POST /api/mail/campaign/start）
	MailCampaignStart Perm = "mail:campaign_start"
	// 导入联系人（POST /api/mail/contact/import）
	MailContactImport Perm = "mail:contact_import"
	// 联系人列表（GET /api/mail/contact/list）
	MailContactList Perm = "mail:contact_list"
	// 修改联系人状态（POST /api/mail/contact/status）
	MailContactStatus Perm = "mail:contact_status"
	// 删除邮件模板（POST /api/mail/template/delete）
	MailTemplateDelete Perm = "mail:template_delete"
	// 邮件模板列表（GET /api/mail/template/list）
	MailTemplateList Perm = "mail:template_list"
	// 保存邮件模板（POST /api/mail/template/save）
	MailTemplateSave Perm = "mail:template_save"

	// —— masterdata（4）——
	// 变更记录计数（GET /api/masterdata/change/count）
	MasterdataChangeCount Perm = "masterdata:change_count"
	// 实体变更历史清单（GET /api/masterdata/change/entities）
	MasterdataChangeEntities Perm = "masterdata:change_entities"
	// 单实体变更历史（GET /api/masterdata/change/entity）
	MasterdataChangeEntity Perm = "masterdata:change_entity"
	// 变更记录列表（GET /api/masterdata/change/list）
	MasterdataChangeList Perm = "masterdata:change_list"

	// —— media（14）——
	// 新建媒体分类（POST /api/media/category/create）
	MediaCategoryCreate Perm = "media:category_create"
	// 删除媒体分类（POST /api/media/category/delete）
	MediaCategoryDelete Perm = "media:category_delete"
	// 媒体分类树（GET /api/media/category/tree）
	MediaCategoryTree Perm = "media:category_tree"
	// 更新媒体分类（POST /api/media/category/update）
	MediaCategoryUpdate Perm = "media:category_update"
	// 删除媒体（POST /api/media/delete）
	MediaDelete Perm = "media:delete"
	// 媒体详情（GET /api/media/detail）
	MediaDetail Perm = "media:detail"
	// 下载媒体资源包（GET /api/media/download）
	MediaDownload Perm = "media:download"
	// 批量下载媒体（GET /api/media/download/batch）
	MediaDownloadBatch Perm = "media:download_batch"
	// 媒体列表（GET /api/media/list）
	MediaList Perm = "media:list"
	// 媒体引用来源（GET /api/media/references）
	MediaReferences Perm = "media:references"
	// 媒体换图（POST /api/media/replace）
	MediaReplace Perm = "media:replace"
	// 更新媒体（POST /api/media/update）
	MediaUpdate Perm = "media:update"
	// 上传媒体（POST /api/media/upload）
	MediaUpload Perm = "media:upload"
	// 重新生成媒体变体（POST /api/media/variants/generate）
	MediaVariantsGenerate Perm = "media:variants_generate"

	// —— menu（5）——
	// 新建菜单（POST /api/menu/create）
	MenuCreate Perm = "menu:create"
	// 删除菜单（POST /api/menu/delete）
	MenuDelete Perm = "menu:delete"
	// 菜单详情（GET /api/menu/detail）
	MenuDetail Perm = "menu:detail"
	// 菜单树（GET /api/menu/tree）
	MenuList Perm = "menu:list"
	// 更新菜单（POST /api/menu/update）
	MenuUpdate Perm = "menu:update"

	// —— navigation（5）——
	// 导航创建（POST /api/navigation/create）
	NavigationCreate Perm = "navigation:create"
	// 导航删除（POST /api/navigation/delete）
	NavigationDelete Perm = "navigation:delete"
	// 导航详情（GET /api/navigation/get）
	NavigationGet Perm = "navigation:get"
	// 导航列表（GET /api/navigation/list）
	NavigationList Perm = "navigation:list"
	// 导航更新（POST /api/navigation/update）
	NavigationUpdate Perm = "navigation:update"

	// —— order（22）——
	// 取消订单（POST /api/order/cancel）
	OrderCancel Perm = "order:cancel"
	// 券计数对账（GET /api/order/coupon/count-audit）
	OrderCouponCountAudit Perm = "order:coupon_count_audit"
	// 新建优惠码（POST /api/order/coupon/create）
	OrderCouponCreate Perm = "order:coupon_create"
	// 删除优惠码（POST /api/order/coupon/delete）
	OrderCouponDelete Perm = "order:coupon_delete"
	// 优惠码详情（GET /api/order/coupon/get）
	OrderCouponGet Perm = "order:coupon_get"
	// 优惠码列表（GET /api/order/coupon/list）
	OrderCouponList Perm = "order:coupon_list"
	// 优惠码核销记录（GET /api/order/coupon/redemption/list）
	OrderCouponRedemption Perm = "order:coupon_redemption"
	// 修改优惠码（POST /api/order/coupon/update）
	OrderCouponUpdate Perm = "order:coupon_update"
	// 优惠码试算（GET /api/order/coupon/validate）
	OrderCouponValidate Perm = "order:coupon_validate"
	// 新建订单（POST /api/order/create）
	OrderCreate Perm = "order:create"
	// 订单详情（GET /api/order/get）
	OrderGet Perm = "order:get"
	// 订单项列表（GET /api/order/item/list）
	OrderItemList Perm = "order:item_list"
	// 订单列表（GET /api/order/list）
	OrderList Perm = "order:list"
	// 状态流转流水（GET /api/order/log/list）
	OrderLogList Perm = "order:log_list"
	// 订单备注（POST /api/order/note）
	OrderNote Perm = "order:note"
	// 订单退款（POST /api/order/refund）
	OrderRefund Perm = "order:refund"
	// 同意退货（POST /api/order/return/approve）
	OrderReturnApprove Perm = "order:return_approve"
	// 退货申请详情（GET /api/order/return/get）
	OrderReturnGet Perm = "order:return_get"
	// 退货申请列表（GET /api/order/return/list）
	OrderReturnList Perm = "order:return_list"
	// 退货入库（POST /api/order/return/receive）
	OrderReturnReceive Perm = "order:return_receive"
	// 拒绝退货（POST /api/order/return/reject）
	OrderReturnReject Perm = "order:return_reject"
	// 订单状态流转（POST /api/order/status）
	OrderStatus Perm = "order:status"

	// —— page（20）——
	// 回收产物文件（POST /api/page/artifact/gc）
	PageArtifactGc Perm = "page:artifact_gc"
	// 重建产物文件（POST /api/page/artifact/rebuild）
	PageArtifactRebuild Perm = "page:artifact_rebuild"
	// 构建页面（POST /api/page/build）
	PageBuild Perm = "page:build"
	// 新建页面（POST /api/page/create）
	PageCreate Perm = "page:create"
	// 删除页面（POST /api/page/delete）
	PageDelete Perm = "page:delete"
	// 页面详情（GET /api/page/detail）
	PageDetail Perm = "page:detail"
	// 保存草稿（POST /api/page/draft/save）
	PageDraftSave Perm = "page:draft_save"
	// 页面列表（GET /api/page/list）
	PageList Perm = "page:list"
	// 发布面巡检（GET /api/page/publication/audit）
	PagePublicationAudit Perm = "page:publication_audit"
	// 发布页面（POST /api/page/publish）
	PagePublish Perm = "page:publish"
	// 新增重定向（POST /api/page/redirect/create）
	PageRedirectCreate Perm = "page:redirect_create"
	// 删除重定向（POST /api/page/redirect/delete）
	PageRedirectDelete Perm = "page:redirect_delete"
	// 合并重定向链（POST /api/page/redirect/merge）
	PageRedirectMerge Perm = "page:redirect_merge"
	// 重定向列表（GET /api/page/redirect）
	PageRedirectView Perm = "page:redirect_view"
	// 修订记录（GET /api/page/revision/list）
	PageRevisionList Perm = "page:revision_list"
	// 回滚页面（POST /api/page/rollback）
	PageRollback Perm = "page:rollback"
	// 绑定系统页面（POST /api/page/site-slot/bind）
	PageSiteSlotBind Perm = "page:site_slot_bind"
	// 系统页面槽位列表（GET /api/page/site-slot/list）
	PageSiteSlotList Perm = "page:site_slot_list"
	// 解绑系统页面（POST /api/page/site-slot/unbind）
	PageSiteSlotUnbind Perm = "page:site_slot_unbind"
	// 更新页面URL（POST /api/page/url/update）
	PageURLUpdate Perm = "page:url_update"

	// —— permission（6）——
	// 新建权限点（POST /api/permission/create）
	PermissionCreate Perm = "permission:create"
	// 删除权限点（POST /api/permission/delete）
	PermissionDelete Perm = "permission:delete"
	// 权限点详情（GET /api/permission/detail）
	PermissionDetail Perm = "permission:detail"
	// 权限点列表（GET /api/permission/list）
	PermissionList Perm = "permission:list"
	// 权限点选项（GET /api/permission/options）
	PermissionOptions Perm = "permission:options"
	// 更新权限点（POST /api/permission/update）
	PermissionUpdate Perm = "permission:update"

	// —— plugin（5）——
	// 插件详情（GET /api/plugin/detail）
	PluginDetail Perm = "plugin:detail"
	// 插件安装（POST /api/plugin/install）
	PluginInstall Perm = "plugin:install"
	// 插件列表（GET /api/plugin/list）
	PluginList Perm = "plugin:list"
	// 插件启停（POST /api/plugin/toggle）
	PluginToggle Perm = "plugin:toggle"
	// 插件卸载（POST /api/plugin/uninstall）
	PluginUninstall Perm = "plugin:uninstall"

	// —— presentation（8）——
	// 实例创建（POST /api/presentation/create）
	PresentationCreate Perm = "presentation:create"
	// 实例删除（POST /api/presentation/delete）
	PresentationDelete Perm = "presentation:delete"
	// 实例详情（GET /api/presentation/get）
	PresentationGet Perm = "presentation:get"
	// 实例按实体查询（GET /api/presentation/get-by-entity）
	PresentationGetByEntity Perm = "presentation:get_by_entity"
	// 实例列表（GET /api/presentation/list）
	PresentationList Perm = "presentation:list"
	// 实例预览（POST /api/presentation/preview）
	PresentationPreview Perm = "presentation:preview"
	// 实例重建（POST /api/presentation/rebuild）
	PresentationRebuild Perm = "presentation:rebuild"
	// 详情页改 URL（POST /api/presentation/update-url）
	PresentationUpdateURL Perm = "presentation:update_url"

	// —— product（43）——
	// 属性组创建（POST /api/product/attribute/create）
	ProductAttributeCreate Perm = "product:attribute_create"
	// 属性组删除（POST /api/product/attribute/delete）
	ProductAttributeDelete Perm = "product:attribute_delete"
	// 属性组详情（GET /api/product/attribute/get）
	ProductAttributeGet Perm = "product:attribute_get"
	// 属性组列表（GET /api/product/attribute/list）
	ProductAttributeList Perm = "product:attribute_list"
	// 属性值保存（POST /api/product/attribute/set-values）
	ProductAttributeSetValues Perm = "product:attribute_set_values"
	// 属性组更新（POST /api/product/attribute/update）
	ProductAttributeUpdate Perm = "product:attribute_update"
	// 商品品牌创建（POST /api/product/brand/create）
	ProductBrandCreate Perm = "product:brand_create"
	// 商品品牌删除（POST /api/product/brand/delete）
	ProductBrandDelete Perm = "product:brand_delete"
	// 商品品牌详情（GET /api/product/brand/get）
	ProductBrandGet Perm = "product:brand_get"
	// 商品品牌列表（GET /api/product/brand/list）
	ProductBrandList Perm = "product:brand_list"
	// 商品品牌更新（POST /api/product/brand/update）
	ProductBrandUpdate Perm = "product:brand_update"
	// 捆绑配置读取（GET /api/product/bundle/get）
	ProductBundleGet Perm = "product:bundle_get"
	// 捆绑配置保存（POST /api/product/bundle/set）
	ProductBundleSet Perm = "product:bundle_set"
	// 捆绑可选 SKU（GET /api/product/bundle/skus）
	ProductBundleSkus Perm = "product:bundle_skus"
	// 捆绑整单校验（POST /api/product/bundle/validate）
	ProductBundleValidate Perm = "product:bundle_validate"
	// 商品分类创建（POST /api/product/category/create）
	ProductCategoryCreate Perm = "product:category_create"
	// 商品分类删除（POST /api/product/category/delete）
	ProductCategoryDelete Perm = "product:category_delete"
	// 商品分类详情（GET /api/product/category/get）
	ProductCategoryGet Perm = "product:category_get"
	// 商品分类列表（GET /api/product/category/list）
	ProductCategoryList Perm = "product:category_list"
	// 商品分类更新（POST /api/product/category/update）
	ProductCategoryUpdate Perm = "product:category_update"
	// 商品创建（POST /api/product/create）
	ProductCreate Perm = "product:create"
	// 商品删除（POST /api/product/delete）
	ProductDelete Perm = "product:delete"
	// 商品详情（GET /api/product/get）
	ProductGet Perm = "product:get"
	// 商品列表（GET /api/product/list）
	ProductList Perm = "product:list"
	// 定价留痕详情（GET /api/product/pricing/adjustment）
	ProductPricingAdjustment Perm = "product:pricing_adjustment"
	// 定价应用落库（POST /api/product/pricing/apply）
	ProductPricingApply Perm = "product:pricing_apply"
	// 定价留痕列表（GET /api/product/pricing/history）
	ProductPricingHistory Perm = "product:pricing_history"
	// 定价试算预览（POST /api/product/pricing/preview）
	ProductPricingPreview Perm = "product:pricing_preview"
	// 定价尾数处理（GET /api/product/pricing/roundings）
	ProductPricingRoundings Perm = "product:pricing_roundings"
	// 定价规则类型（GET /api/product/pricing/rules）
	ProductPricingRules Perm = "product:pricing_rules"
	// 商品标签创建（POST /api/product/tag/create）
	ProductTagCreate Perm = "product:tag_create"
	// 商品标签删除（POST /api/product/tag/delete）
	ProductTagDelete Perm = "product:tag_delete"
	// 商品标签详情（GET /api/product/tag/get）
	ProductTagGet Perm = "product:tag_get"
	// 商品标签列表（GET /api/product/tag/list）
	ProductTagList Perm = "product:tag_list"
	// 商品标签命中商品（GET /api/product/tag/products）
	ProductTagProducts Perm = "product:tag_products"
	// 商品标签重算（POST /api/product/tag/recalc）
	ProductTagRecalc Perm = "product:tag_recalc"
	// 商品标签规则类型（GET /api/product/tag/rule-types）
	ProductTagRuleTypes Perm = "product:tag_rule_types"
	// 商品标签更新（POST /api/product/tag/update）
	ProductTagUpdate Perm = "product:tag_update"
	// 商品更新（POST /api/product/update）
	ProductUpdate Perm = "product:update"
	// 变体创建（POST /api/product/variant/create）
	ProductVariantCreate Perm = "product:variant_create"
	// 变体删除（POST /api/product/variant/delete）
	ProductVariantDelete Perm = "product:variant_delete"
	// 变体组合生成（POST /api/product/variant/generate）
	ProductVariantGenerate Perm = "product:variant_generate"
	// 变体更新（POST /api/product/variant/update）
	ProductVariantUpdate Perm = "product:variant_update"

	// —— project（12）——
	// 新建项目（POST /api/project/create）
	ProjectCreate Perm = "project:create"
	// 项目详情（GET /api/project/detail）
	ProjectDetail Perm = "project:detail"
	// 项目列表（GET /api/project/list）
	ProjectList Perm = "project:list"
	// 激活主题（POST /api/theme/activate）
	ProjectThemeActivate Perm = "project:theme_activate"
	// 当前主题（GET /api/theme/active）
	ProjectThemeActive Perm = "project:theme_active"
	// 新建主题（POST /api/theme/create）
	ProjectThemeCreate Perm = "project:theme_create"
	// 删除主题（POST /api/theme/delete）
	ProjectThemeDelete Perm = "project:theme_delete"
	// 导出主题包（GET /api/theme/export）
	// 导入主题包（POST /api/theme/import）
	// 主题列表（GET /api/theme/list）
	ProjectThemeList Perm = "project:theme_list"
	// 更新主题（POST /api/theme/update）
	ProjectThemeUpdate Perm = "project:theme_update"
	// 更新项目（POST /api/project/update）
	ProjectUpdate Perm = "project:update"

	// —— publication（2）——
	// 待处理发布回执（GET /api/publication/receipts/pending）
	PublicationReceiptsPending Perm = "publication:receipts_pending"
	// SEO 审计（POST /api/publication/seo-audit）
	PublicationSEOAudit Perm = "publication:seo_audit"

	// —— role（9）——
	// 新建角色（POST /api/role/create）
	RoleCreate Perm = "role:create"
	// 删除角色（POST /api/role/delete）
	RoleDelete Perm = "role:delete"
	// 角色详情（GET /api/role/detail）
	RoleDetail Perm = "role:detail"
	// 角色列表（GET /api/role/list）
	RoleList Perm = "role:list"
	// 角色菜单查看（GET /api/role/menu/list）
	RoleMenuList Perm = "role:menu_list"
	// 角色菜单分配（POST /api/role/menu/save）
	RoleMenuSave Perm = "role:menu_save"
	// 更新角色（POST /api/role/update）
	RoleUpdate Perm = "role:update"
	// 角色用户查看（GET /api/role/user/list）
	RoleUserList Perm = "role:user_list"
	// 角色用户分配（POST /api/role/user/save）
	RoleUserSave Perm = "role:user_save"

	// —— user（4）——
	// 查看客户详情（GET /api/customer/get）
	UserCustomerDetail Perm = "user:customer_detail"
	// 查看客户列表（GET /api/customer/list）
	UserCustomerList Perm = "user:customer_list"
	// 停用/启用客户账号（POST /api/customer/status）
	UserCustomerStatus Perm = "user:customer_status"
	// 解除客户账号锁定（POST /api/customer/unlock）
	UserCustomerUnlock Perm = "user:customer_unlock"

	// —— webhook（6）——
	// 投递日志（GET /api/webhook/delivery/list）
	WebhookDeliveryList Perm = "webhook:delivery_list"
	// 重投投递（POST /api/webhook/delivery/retry）
	WebhookDeliveryRetry Perm = "webhook:delivery_retry"
	// 删除集成端点（POST /api/webhook/endpoint/delete）
	WebhookEndpointDelete Perm = "webhook:endpoint_delete"
	// 集成端点列表（GET /api/webhook/endpoint/list）
	WebhookEndpointList Perm = "webhook:endpoint_list"
	// 保存集成端点（POST /api/webhook/endpoint/save）
	WebhookEndpointSave Perm = "webhook:endpoint_save"
	// 启停集成端点（POST /api/webhook/endpoint/status）
	WebhookEndpointStatus Perm = "webhook:endpoint_status"
)

// specs 权限点元数据：中文名与所属模块。启动期 upsert 时写进 sys_permission 的
// permission_name / module（仅新建行；已存在的行不覆盖人工改过的名字）。
var specs = map[Perm]spec{
	// —— admin ——
	AdminCreate:   {module: "admin", name: "新建管理员"},
	AdminDelete:   {module: "admin", name: "删除管理员"},
	AdminDetail:   {module: "admin", name: "管理员详情"},
	AdminEdit:     {module: "admin", name: "编辑管理员"},
	AdminList:     {module: "admin", name: "管理员列表"},
	AdminMenuList: {module: "admin", name: "管理员菜单查看"},
	AdminMenuSave: {module: "admin", name: "管理员菜单分配"},
	AdminRoleList: {module: "admin", name: "管理员角色查看"},
	AdminRoleSave: {module: "admin", name: "管理员角色分配"},

	// —— analytics ——
	AnalyticsView: {module: "analytics", name: "访问统计查看"},

	// —— artifact ——
	ArtifactDetail: {module: "artifact", name: "构建产物详情"},

	// —— block ——
	BlockClone:  {module: "block", name: "克隆区块"},
	BlockCreate: {module: "block", name: "新建区块"},
	BlockDelete: {module: "block", name: "删除区块"},
	BlockDetail: {module: "block", name: "区块详情"},
	BlockList:   {module: "block", name: "区块列表"},
	BlockUpdate: {module: "block", name: "更新区块"},

	// —— blueprint ——
	BlueprintCreate:  {module: "blueprint", name: "模板创建"},
	BlueprintDelete:  {module: "blueprint", name: "模板删除"},
	BlueprintGet:     {module: "blueprint", name: "模板详情"},
	BlueprintInit:    {module: "blueprint", name: "初始化页面"},
	BlueprintList:    {module: "blueprint", name: "模板列表"},
	BlueprintPublish: {module: "blueprint", name: "模板发布"},
	BlueprintUpdate:  {module: "blueprint", name: "模板更新"},

	// —— build ——
	BuildJobs:  {module: "build", name: "构建任务列表"},
	BuildQueue: {module: "build", name: "构建队列状态"},
	BuildRetry: {module: "build", name: "重试构建"},

	// —— content ——
	ContentCollections: {module: "content", name: "查看内容集合元数据"},
	ContentCreate:      {module: "content", name: "内容创建"},
	ContentDelete:      {module: "content", name: "内容删除"},
	ContentGet:         {module: "content", name: "内容详情"},
	ContentList:        {module: "content", name: "内容列表"},
	ContentUpdate:      {module: "content", name: "内容更新"},

	// —— contenttemplate ——
	ContenttemplateCreate: {module: "contenttemplate", name: "模板创建"},
	ContenttemplateGet:    {module: "contenttemplate", name: "模板详情"},
	ContenttemplateList:   {module: "contenttemplate", name: "模板列表"},
	ContenttemplateUpdate: {module: "contenttemplate", name: "模板更新"},

	// —— datarule ——
	DataruleAssignmentList: {module: "datarule", name: "规则分配查看"},
	DataruleAssignmentSave: {module: "datarule", name: "规则分配保存"},
	DataruleCreate:         {module: "datarule", name: "新建数据规则"},
	DataruleDelete:         {module: "datarule", name: "删除数据规则"},
	DataruleDetail:         {module: "datarule", name: "数据规则详情"},
	DataruleList:           {module: "datarule", name: "数据规则列表"},
	DataruleSchemaDetail:   {module: "datarule", name: "数据域详情"},
	DataruleSchemaList:     {module: "datarule", name: "数据域清单"},
	DataruleUpdate:         {module: "datarule", name: "更新数据规则"},

	// —— dept ——
	DeptCreate:   {module: "dept", name: "新建部门"},
	DeptDelete:   {module: "dept", name: "删除部门"},
	DeptDetail:   {module: "dept", name: "部门详情"},
	DeptList:     {module: "dept", name: "部门树"},
	DeptUpdate:   {module: "dept", name: "更新部门"},
	DeptUserList: {module: "dept", name: "部门用户查看"},
	DeptUserSave: {module: "dept", name: "部门用户分配"},

	// —— inventory ——
	InventoryBomGet:             {module: "inventory", name: "查看物料清单"},
	InventoryBomSet:             {module: "inventory", name: "维护物料清单"},
	InventoryMovementList:       {module: "inventory", name: "库存流水列表"},
	InventoryPurchaseCreate:     {module: "inventory", name: "新建采购单"},
	InventoryPurchaseGet:        {module: "inventory", name: "采购单详情"},
	InventoryPurchaseHistory:    {module: "inventory", name: "进货历史"},
	InventoryPurchaseList:       {module: "inventory", name: "采购单列表"},
	InventoryPurchaseProduction: {module: "inventory", name: "生产入库"},
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

	// —— mail ——
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
	MailContactImport:       {module: "mail", name: "导入联系人"},
	MailContactList:         {module: "mail", name: "联系人列表"},
	MailContactStatus:       {module: "mail", name: "修改联系人状态"},
	MailTemplateDelete:      {module: "mail", name: "删除邮件模板"},
	MailTemplateList:        {module: "mail", name: "邮件模板列表"},
	MailTemplateSave:        {module: "mail", name: "保存邮件模板"},

	// —— masterdata ——
	MasterdataChangeCount:    {module: "masterdata", name: "变更记录计数"},
	MasterdataChangeEntities: {module: "masterdata", name: "实体变更历史清单"},
	MasterdataChangeEntity:   {module: "masterdata", name: "单实体变更历史"},
	MasterdataChangeList:     {module: "masterdata", name: "变更记录列表"},

	// —— media ——
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

	// —— menu ——
	MenuCreate: {module: "menu", name: "新建菜单"},
	MenuDelete: {module: "menu", name: "删除菜单"},
	MenuDetail: {module: "menu", name: "菜单详情"},
	MenuList:   {module: "menu", name: "菜单树"},
	MenuUpdate: {module: "menu", name: "更新菜单"},

	// —— navigation ——
	NavigationCreate: {module: "navigation", name: "导航创建"},
	NavigationDelete: {module: "navigation", name: "导航删除"},
	NavigationGet:    {module: "navigation", name: "导航详情"},
	NavigationList:   {module: "navigation", name: "导航列表"},
	NavigationUpdate: {module: "navigation", name: "导航更新"},

	// —— order ——
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
	OrderRefund:           {module: "order", name: "订单退款"},
	OrderReturnApprove:    {module: "order", name: "同意退货"},
	OrderReturnGet:        {module: "order", name: "退货申请详情"},
	OrderReturnList:       {module: "order", name: "退货申请列表"},
	OrderReturnReceive:    {module: "order", name: "退货入库"},
	OrderReturnReject:     {module: "order", name: "拒绝退货"},
	OrderStatus:           {module: "order", name: "订单状态流转"},

	// —— page ——
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
	PageSiteSlotBind:     {module: "page", name: "绑定系统页面"},
	PageSiteSlotList:     {module: "page", name: "系统页面槽位列表"},
	PageSiteSlotUnbind:   {module: "page", name: "解绑系统页面"},
	PageURLUpdate:        {module: "page", name: "更新页面URL"},

	// —— permission ——
	PermissionCreate:  {module: "permission", name: "新建权限点"},
	PermissionDelete:  {module: "permission", name: "删除权限点"},
	PermissionDetail:  {module: "permission", name: "权限点详情"},
	PermissionList:    {module: "permission", name: "权限点列表"},
	PermissionOptions: {module: "permission", name: "权限点选项"},
	PermissionUpdate:  {module: "permission", name: "更新权限点"},

	// —— plugin ——
	PluginDetail:    {module: "plugin", name: "插件详情"},
	PluginInstall:   {module: "plugin", name: "插件安装"},
	PluginList:      {module: "plugin", name: "插件列表"},
	PluginToggle:    {module: "plugin", name: "插件启停"},
	PluginUninstall: {module: "plugin", name: "插件卸载"},

	// —— presentation ——
	PresentationCreate:      {module: "presentation", name: "实例创建"},
	PresentationDelete:      {module: "presentation", name: "实例删除"},
	PresentationGet:         {module: "presentation", name: "实例详情"},
	PresentationGetByEntity: {module: "presentation", name: "实例按实体查询"},
	PresentationList:        {module: "presentation", name: "实例列表"},
	PresentationPreview:     {module: "presentation", name: "实例预览"},
	PresentationRebuild:     {module: "presentation", name: "实例重建"},
	PresentationUpdateURL:   {module: "presentation", name: "详情页改 URL"},

	// —— product ——
	ProductAttributeCreate:    {module: "product", name: "属性组创建"},
	ProductAttributeDelete:    {module: "product", name: "属性组删除"},
	ProductAttributeGet:       {module: "product", name: "属性组详情"},
	ProductAttributeList:      {module: "product", name: "属性组列表"},
	ProductAttributeSetValues: {module: "product", name: "属性值保存"},
	ProductAttributeUpdate:    {module: "product", name: "属性组更新"},
	ProductBrandCreate:        {module: "product", name: "商品品牌创建"},
	ProductBrandDelete:        {module: "product", name: "商品品牌删除"},
	ProductBrandGet:           {module: "product", name: "商品品牌详情"},
	ProductBrandList:          {module: "product", name: "商品品牌列表"},
	ProductBrandUpdate:        {module: "product", name: "商品品牌更新"},
	ProductBundleGet:          {module: "product", name: "捆绑配置读取"},
	ProductBundleSet:          {module: "product", name: "捆绑配置保存"},
	ProductBundleSkus:         {module: "product", name: "捆绑可选 SKU"},
	ProductBundleValidate:     {module: "product", name: "捆绑整单校验"},
	ProductCategoryCreate:     {module: "product", name: "商品分类创建"},
	ProductCategoryDelete:     {module: "product", name: "商品分类删除"},
	ProductCategoryGet:        {module: "product", name: "商品分类详情"},
	ProductCategoryList:       {module: "product", name: "商品分类列表"},
	ProductCategoryUpdate:     {module: "product", name: "商品分类更新"},
	ProductCreate:             {module: "product", name: "商品创建"},
	ProductDelete:             {module: "product", name: "商品删除"},
	ProductGet:                {module: "product", name: "商品详情"},
	ProductList:               {module: "product", name: "商品列表"},
	ProductPricingAdjustment:  {module: "product", name: "定价留痕详情"},
	ProductPricingApply:       {module: "product", name: "定价应用落库"},
	ProductPricingHistory:     {module: "product", name: "定价留痕列表"},
	ProductPricingPreview:     {module: "product", name: "定价试算预览"},
	ProductPricingRoundings:   {module: "product", name: "定价尾数处理"},
	ProductPricingRules:       {module: "product", name: "定价规则类型"},
	ProductTagCreate:          {module: "product", name: "商品标签创建"},
	ProductTagDelete:          {module: "product", name: "商品标签删除"},
	ProductTagGet:             {module: "product", name: "商品标签详情"},
	ProductTagList:            {module: "product", name: "商品标签列表"},
	ProductTagProducts:        {module: "product", name: "商品标签命中商品"},
	ProductTagRecalc:          {module: "product", name: "商品标签重算"},
	ProductTagRuleTypes:       {module: "product", name: "商品标签规则类型"},
	ProductTagUpdate:          {module: "product", name: "商品标签更新"},
	ProductUpdate:             {module: "product", name: "商品更新"},
	ProductVariantCreate:      {module: "product", name: "变体创建"},
	ProductVariantDelete:      {module: "product", name: "变体删除"},
	ProductVariantGenerate:    {module: "product", name: "变体组合生成"},
	ProductVariantUpdate:      {module: "product", name: "变体更新"},

	// —— project ——
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

	// —— publication ——
	PublicationReceiptsPending: {module: "publication", name: "待处理发布回执"},
	PublicationSEOAudit:        {module: "publication", name: "SEO 审计"},

	// —— role ——
	RoleCreate:   {module: "role", name: "新建角色"},
	RoleDelete:   {module: "role", name: "删除角色"},
	RoleDetail:   {module: "role", name: "角色详情"},
	RoleList:     {module: "role", name: "角色列表"},
	RoleMenuList: {module: "role", name: "角色菜单查看"},
	RoleMenuSave: {module: "role", name: "角色菜单分配"},
	RoleUpdate:   {module: "role", name: "更新角色"},
	RoleUserList: {module: "role", name: "角色用户查看"},
	RoleUserSave: {module: "role", name: "角色用户分配"},

	// —— user ——
	UserCustomerDetail: {module: "user", name: "查看客户详情"},
	UserCustomerList:   {module: "user", name: "查看客户列表"},
	UserCustomerStatus: {module: "user", name: "停用/启用客户账号"},
	UserCustomerUnlock: {module: "user", name: "解除客户账号锁定"},

	// —— webhook ——
	WebhookDeliveryList:   {module: "webhook", name: "投递日志"},
	WebhookDeliveryRetry:  {module: "webhook", name: "重投投递"},
	WebhookEndpointDelete: {module: "webhook", name: "删除集成端点"},
	WebhookEndpointList:   {module: "webhook", name: "集成端点列表"},
	WebhookEndpointSave:   {module: "webhook", name: "保存集成端点"},
	WebhookEndpointStatus: {module: "webhook", name: "启停集成端点"},
}
