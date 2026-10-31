package maildto

type SaveAccountReq struct {
	ID          uint64
	Name        string
	Purpose     string
	FromName    string
	FromEmail   string
	ReplyTo     string
	Provider    string
	Host        string
	Port        int
	Username    string
	Password    string
	Encryption  string
	RatePerHour int
	OperatorID  uint64
}

type TestSendReq struct {
	AccountID  uint64
	ToEmail    string
	OperatorID uint64
	Lang       string
}
