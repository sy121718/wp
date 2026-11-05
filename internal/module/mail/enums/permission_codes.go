package mailenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	MailAccountDefault = "mail:account_default"
	MailAccountDelete = "mail:account_delete"
	MailAccountList = "mail:account_list"
	MailAccountSave = "mail:account_save"
	MailAccountTest = "mail:account_test"
	MailAutomationDelete = "mail:automation_delete"
	MailAutomationGet = "mail:automation_get"
	MailAutomationLayout = "mail:automation_layout"
	MailAutomationList = "mail:automation_list"
	MailAutomationRunDetail = "mail:automation_run_detail"
	MailAutomationRunList = "mail:automation_run_list"
	MailAutomationSave = "mail:automation_save"
	MailAutomationStart = "mail:automation_start"
	MailAutomationStatus = "mail:automation_status"
	MailAutomationTick = "mail:automation_tick"
	MailCampaignDelete = "mail:campaign_delete"
	MailCampaignGet = "mail:campaign_get"
	MailCampaignList = "mail:campaign_list"
	MailCampaignSave = "mail:campaign_save"
	MailCampaignStart = "mail:campaign_start"
	MailContactDelete = "mail:contact_delete"
	MailContactImport = "mail:contact_import"
	MailContactList = "mail:contact_list"
	MailContactSave = "mail:contact_save"
	MailContactStatus = "mail:contact_status"
	MailContactTag = "mail:contact_tag"
	MailTemplateDelete = "mail:template_delete"
	MailTemplateList = "mail:template_list"
	MailTemplateSave = "mail:template_save"
)
