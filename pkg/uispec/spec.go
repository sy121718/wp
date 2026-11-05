package uispec

const (
	TypeStat      = "stat"
	TypeTable     = "table"
	TypeList      = "list"
	TypeAccordion = "accordion"
)

const (
	MaxBlocks     = 8
	MaxTitleRunes = 80
	MaxSourceLen  = 64
	MaxParams     = 8
	MaxParamKey   = 32
	MaxParamValue = 128
	MaxWordRunes  = 120
	MaxLimit      = 100
	MinLimit      = 1
)

type Block struct {
	Type string `json:"type"`

	Title string `json:"title,omitempty"`

	Source string `json:"source"`

	Params map[string]string `json:"params,omitempty"`

	Limit int `json:"limit,omitempty"`
}

type Spec struct {
	Text string `json:"text,omitempty"`

	Blocks []Block `json:"blocks,omitempty"`
}
