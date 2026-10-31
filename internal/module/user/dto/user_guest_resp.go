package userdto

type GuestAccountResp struct {
	UserID   uint64
	Username string

	Created bool

	PasswordMailed bool
}
