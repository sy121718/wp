package maildto

// mail_contact_resp.go — 联系人相关响应。

// ImportRowError 一行导入失败的原因（逐行报告，不让整批失败）。
type ImportRowError struct {
	Line   int    `json:"line"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// ImportContactsResp 导入结果。
type ImportContactsResp struct {
	Total      int              `json:"total"`
	Imported   int              `json:"imported"`
	Updated    int              `json:"updated"`
	Skipped    int              `json:"skipped"`
	Suppressed int              `json:"suppressed"`
	Errors     []ImportRowError `json:"errors"`
}

// ContactItem 联系人条目。
type ContactItem struct {
	ID            uint64   `json:"id"`
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	UserID        uint64   `json:"userId"`
	Source        string   `json:"source"`
	Status        string   `json:"status"`
	Tags          []string `json:"tags"`
	SubscribedAt  string   `json:"subscribedAt"`
	ConsentSource string   `json:"consentSource"`
	CreateTime    string   `json:"createTime"`
}

// ContactListResp 联系人列表。
type ContactListResp struct {
	Items []ContactItem `json:"items"`
	Total int64         `json:"total"`
}
