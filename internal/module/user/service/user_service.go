package userservice

import (
	usercontract "go_wp/internal/module/user/contract"
	usermodel "go_wp/internal/module/user/model"
)

type Service struct {
	m *usermodel.UserModel

	pm    *usermodel.UserProfileModel
	prefm *usermodel.UserPreferenceModel
	mail  usercontract.MailSender

	siteName string
}

func NewService(
	m *usermodel.UserModel,
	pm *usermodel.UserProfileModel,
	prefm *usermodel.UserPreferenceModel,
	mail usercontract.MailSender,
	siteName string,
) *Service {
	if siteName == "" {
		siteName = "本站"
	}
	return &Service{m: m, pm: pm, prefm: prefm, mail: mail, siteName: siteName}
}

var _ usercontract.UserService = (*Service)(nil)

var _ usercontract.CustomerAdminPort = (*Service)(nil)
