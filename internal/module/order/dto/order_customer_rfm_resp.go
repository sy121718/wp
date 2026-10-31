package orderdto

type CustomerRfmItem struct {
	UserID int64 `json:"userId"`

	LastOrderAt string `json:"lastOrderAt"`

	RecencyDays int `json:"recencyDays"`

	Frequency int64 `json:"frequency"`

	MonetaryCents int64 `json:"monetaryCents"`

	MonetaryLabel string `json:"monetaryLabel"`

	RScore     int `json:"rScore"`
	FScore     int `json:"fScore"`
	MScore     int `json:"mScore"`
	TotalScore int `json:"totalScore"`

	Segment string `json:"segment"`

	SegmentLabel string `json:"segmentLabel"`
}

type CustomerRfmResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`

	Customers int64 `json:"customers"`
	Vip       int64 `json:"vip"`
	Potential int64 `json:"potential"`
	LowValue  int64 `json:"lowValue"`

	Total int64             `json:"total"`
	Items []CustomerRfmItem `json:"items"`
}

type CustomerRfmSegmentIDsResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	Segment   string `json:"segment"`

	UserIDs []int64 `json:"userIds"`

	Total int64 `json:"total"`
}
