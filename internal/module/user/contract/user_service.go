package usercontract

import (
	"context"

	"gorm.io/gorm"

	mailcontract "go_wp/internal/module/mail/contract"
	userdto "go_wp/internal/module/user/dto"
)

const VisitorContextKey = "gowp_visitor_user_id"

const VisitorCSRFContextKey = "gowp_visitor_csrf_token"

type VisitorIdentityResolver interface {
	ResolveVisitorID(ctx context.Context, token string) (userID uint64, ok bool)
}

type UserService interface {
	VisitorIdentityResolver

	GuestAccountProvisioner

	Register(ctx context.Context, req *userdto.RegisterReq) (*userdto.RegisterResp, error)

	ActivateEmail(ctx context.Context, req *userdto.ActivateEmailReq) (*userdto.ActivateResp, error)

	ResendActivation(ctx context.Context, req *userdto.ResendActivationReq) error

	RequestPasswordReset(ctx context.Context, req *userdto.PasswordResetReqRequest) error

	ResetPassword(ctx context.Context, req *userdto.ResetPasswordReq) error
}

type GuestAccountProvisioner interface {
	EnsureGuestAccount(ctx context.Context, in *GuestAccountInput) (res *GuestAccountResult, err error)

	EnsureGuestAccountTx(ctx context.Context, tx *gorm.DB, in *GuestAccountInput) (res *GuestAccountResult, err error)

	SendGuestAccountMail(ctx context.Context, mail *GuestAccountMail) error

	LookupGuestAccount(ctx context.Context, email string) (userID uint64, found bool, err error)
}

type GuestAccountInput struct {
	Email string

	Name string

	Locale string

	RegisterIP string
}

type GuestAccountResult struct {
	UserID   uint64
	Username string

	Created bool

	PasswordMailed bool

	PendingMail *GuestAccountMail
}

type GuestAccountMail struct {
	Email    string
	Name     string
	Username string
	Password string
	Locale   string
}

type MailSender interface {
	SendTransactional(ctx context.Context, in *mailcontract.SendInput) (*mailcontract.SendOutcome, error)
}
