package maildto

import json "go_wp/pkg/jsonx"

type TemplateItem struct {
	ID          uint64 `json:"id"`
	TemplateKey string `json:"templateKey"`
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Subject     string `json:"subject"`
	BodyHTML    string `json:"bodyHtml"`
	BodyText    string `json:"bodyText"`

	BodyJSON json.RawMessage `json:"bodyJson,omitempty"`

	BodyCompiledHTML string   `json:"bodyCompiledHtml,omitempty"`
	BodyCompiledText string   `json:"bodyCompiledText,omitempty"`
	Variables        []string `json:"variables"`
	Status           int      `json:"status"`
	UpdateTime       string   `json:"updateTime"`
}

type SendResult struct {
	LogID      uint64 `json:"logId"`
	To         string `json:"to"`
	Queued     bool   `json:"queued"`
	Suppressed bool   `json:"suppressed"`
}
