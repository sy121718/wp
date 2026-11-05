package maildto

// 联系人的批量删除 / 改状态 / 打标签共用页面上的一张表单与同一批勾选框，
// 因此三者都会收到整张表单的字段；各 handler 只读取自己关心的字段。

type MailIDForm struct {
	ID string `form:"id"`
}

type MailIDsForm struct {
	IDs []string `form:"ids"`
}

type MailAccountSaveForm struct {
	ID          string `form:"id"`
	Name        string `form:"name"`
	Purpose     string `form:"purpose"`
	FromName    string `form:"from_name"`
	FromEmail   string `form:"from_email"`
	ReplyTo     string `form:"reply_to"`
	Provider    string `form:"provider"`
	Host        string `form:"host"`
	Port        string `form:"port"`
	Username    string `form:"username"`
	Password    string `form:"password"`
	Encryption  string `form:"encryption"`
	RatePerHour string `form:"rate_per_hour"`
}

type MailAccountTestForm struct {
	ID      string `form:"id"`
	ToEmail string `form:"to_email"`
}

type MailTemplateSaveForm struct {
	TemplateKey string `form:"template_key"`
	Locale      string `form:"locale"`
	Name        string `form:"name"`
	Subject     string `form:"subject"`
	Variables   string `form:"variables"`
	BodyHTML    string `form:"body_html"`
	BodyText    string `form:"body_text"`
}

type MailTemplateDeleteForm struct {
	TemplateKey string `form:"template_key"`
	Locale      string `form:"locale"`
}

type MailContactSaveForm struct {
	ID            string `form:"id"`
	Email         string `form:"email"`
	Name          string `form:"name"`
	Tags          string `form:"tags"`
	Source        string `form:"source"`
	ConsentSource string `form:"consent_source"`
	Status        string `form:"status"`
}

type MailContactBulkForm struct {
	IDs    []string `form:"ids"`
	Status string   `form:"status"`
	Add    string   `form:"add"`
	Remove string   `form:"remove"`
}

type MailContactDeleteForm struct {
	ID string `form:"id"`
	MailContactBulkForm
}

type MailContactTagForm struct {
	MailContactBulkForm
}

type MailContactStatusForm struct {
	ID     string `form:"id"`
	Status string `form:"status"`
	Note   string `form:"note"`
}

type MailContactBulkStatusForm struct {
	MailContactBulkForm
}

type MailContactImportForm struct {
	Content         string `form:"content"`
	ConsentDeclared string `form:"consent_declared"`
	ConsentSource   string `form:"consent_source"`
	UpdateExisting  string `form:"update_existing"`
	Tags            string `form:"tags"`
}

type MailCampaignSaveForm struct {
	ID         string `form:"id"`
	Name       string `form:"name"`
	AccountID  string `form:"account_id"`
	TemplateID string `form:"template_id"`
	TargetTags string `form:"target_tags"`
	Subject    string `form:"subject"`
}

type MailCampaignStartForm struct {
	ID string `form:"id"`
}

type MailAutomationStatusForm struct {
	ID     string `form:"id"`
	Status string `form:"status"`
}

type MailAutomationTickForm struct {
	Limit string `form:"limit"`
}
