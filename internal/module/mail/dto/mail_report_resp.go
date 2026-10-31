package maildto

type LinkStat struct {
	URL      string `json:"url"`
	Total    int64  `json:"total"`
	Contacts int64  `json:"contacts"`
}

type RecipientItem struct {
	ContactID uint64 `json:"contactId"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	SentAt    string `json:"sentAt"`
	ErrorKind string `json:"errorKind"`
	Opened    int64  `json:"opened"`
	Clicked   int64  `json:"clicked"`

	StatusTone string `json:"-"`
}

type CampaignReport struct {
	Campaign CampaignItem `json:"campaign"`

	Sent   int `json:"sent"`
	Failed int `json:"failed"`
	Target int `json:"target"`

	Opened       int64 `json:"opened"`
	Clicked      int64 `json:"clicked"`
	Unsubscribed int64 `json:"unsubscribed"`
	Complained   int64 `json:"complained"`
	Bounced      int64 `json:"bounced"`

	OpenEvents  int64 `json:"openEvents"`
	ClickEvents int64 `json:"clickEvents"`

	OpenRate  float64 `json:"openRate"`
	ClickRate float64 `json:"clickRate"`

	Links      []LinkStat      `json:"links"`
	Recipients []RecipientItem `json:"recipients"`
	Total      int64           `json:"total"`
}
