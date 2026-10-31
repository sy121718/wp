package mailservice

import (
	mailmodel "go_wp/internal/module/mail/model"
	"net/http"
)

type Service struct {
	m *mailmodel.MailModel

	cipherSecret string

	webhookClient *http.Client
}

func NewService(m *mailmodel.MailModel) *Service {
	return &Service{m: m}
}

func (s *Service) SetWebhookClient(c *http.Client) { s.webhookClient = c }
