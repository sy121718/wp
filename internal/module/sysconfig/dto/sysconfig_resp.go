package sysconfigdto

import "go_wp/pkg/utils"

type DictOption struct {
	Code string `json:"code"`

	Label string `json:"label"`

	Symbol string `json:"symbol,omitempty"`

	UIAvailable bool `json:"uiAvailable"`
}

type CountryOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type Group struct {
	Key string `json:"key"`

	Name string `json:"name"`

	Data map[string]any `json:"data"`

	Remark string `json:"remark"`

	Status int `json:"status"`

	Version int64 `json:"version"`

	UpdateTime utils.JSONTime `json:"updateTime"`
}
