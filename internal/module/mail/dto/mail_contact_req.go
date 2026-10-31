package maildto

type ImportContactsReq struct {
	Content []byte

	DefaultTags []string

	ConsentDeclared bool
	ConsentSource   string

	UpdateExisting bool
	OperatorID     uint64
}

type ContactFilterReq struct {
	Keyword  string
	Status   string
	Tags     []string
	Page     int
	PageSize int
}

type UpdateContactStatusReq struct {
	ID         uint64
	Status     string
	Note       string
	OperatorID uint64
}

type SaveContactReq struct {
	ID uint64

	Email string
	Name  string

	Tags []string

	Source string

	ConsentSource string

	Status     string
	OperatorID uint64
}

type DeleteContactsReq struct {
	IDs        []uint64
	OperatorID uint64
}

type TagContactsReq struct {
	IDs        []uint64
	Add        []string
	Remove     []string
	OperatorID uint64
}

type PullSystemUsersReq struct {
	OnlyEmailNotify bool
	DefaultTags     []string
	OperatorID      uint64
}
