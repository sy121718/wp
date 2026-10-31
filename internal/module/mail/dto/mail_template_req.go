package maildto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

type SaveTemplateReq struct {
	TemplateKey string
	Locale      string
	Name        string
	Subject     string
	BodyHTML    string
	BodyText    string

	BodyJSON json.RawMessage

	Variables  []string
	OperatorID uint64
}

func (r *SaveTemplateReq) BindForm(v url.Values) error {
	raw := strings.TrimSpace(v.Get("body_json"))
	if raw == "" {
		return nil
	}
	r.BodyJSON = json.RawMessage(raw)
	return nil
}

type SendTemplateReq struct {
	TemplateKey string
	Locale      string
	To          string
	Vars        map[string]any

	AccountID uint64

	CampaignID uint64
	ContactID  uint64
	OperatorID uint64
}
