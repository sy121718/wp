package maildto

type SaveAccountReq struct {
	ID          uint64 `form:"id" json:"id"`
	Name        string `form:"name" json:"name"`
	Purpose     string `form:"purpose" json:"purpose"`
	FromName    string `form:"fromName" json:"fromName"`
	FromEmail   string `form:"fromEmail" json:"fromEmail"`
	ReplyTo     string `form:"replyTo" json:"replyTo"`
	Provider    string `form:"provider" json:"provider"`
	Host        string `form:"host" json:"host"`
	Port        int    `form:"port" json:"port"`
	Username    string `form:"username" json:"username"`
	Password    string `form:"password" json:"password"`
	Encryption  string `form:"encryption" json:"encryption"`
	RatePerHour int    `form:"ratePerHour" json:"ratePerHour"`
	OperatorID  uint64 `form:"-" json:"-"`
}

type TestSendReq struct {
	AccountID  uint64 `form:"accountId" json:"accountId"`
	ToEmail    string `form:"toEmail" json:"toEmail"`
	OperatorID uint64 `form:"-" json:"-"`
	Lang       string `form:"-" json:"-"`
}
