package maildto

type ImportRowError struct {
	Line   int    `json:"line"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type ImportContactsResp struct {
	Total      int              `json:"total"`
	Imported   int              `json:"imported"`
	Updated    int              `json:"updated"`
	Skipped    int              `json:"skipped"`
	Suppressed int              `json:"suppressed"`
	Errors     []ImportRowError `json:"errors"`
}

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

type ContactListResp struct {
	Items []ContactItem `json:"items"`
	Total int64         `json:"total"`
}
