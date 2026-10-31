package maildto

type AccountItem struct {
	ID             uint64 `json:"id"`
	Name           string `json:"name"`
	Purpose        string `json:"purpose"`
	IsDefault      bool   `json:"isDefault"`
	FromName       string `json:"fromName"`
	FromEmail      string `json:"fromEmail"`
	ReplyTo        string `json:"replyTo"`
	Provider       string `json:"provider"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Encryption     string `json:"encryption"`
	RatePerHour    int    `json:"ratePerHour"`
	Status         int    `json:"status"`
	HasPassword    bool   `json:"hasPassword"`
	Incomplete     bool   `json:"incomplete"`
	LastCheckAt    string `json:"lastCheckAt"`
	LastCheckError string `json:"lastCheckError"`
}

type TestSendResp struct {
	OK        bool   `json:"ok"`
	To        string `json:"to"`
	ErrorKind string `json:"errorKind"`
	Error     string `json:"error"`
}
