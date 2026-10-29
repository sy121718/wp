package productdto

// SeoScoreResp 共享评分算法产生的数值事实；文案、等级、颜色与交互位置由前端决定。
type SeoScoreResp struct {
	EntityType  string                  `json:"entityType"`
	EntityID    string                  `json:"entityId"`
	ProjectID   string                  `json:"projectId"`
	Total       int                     `json:"total"`
	ProfileType string                  `json:"profileType"`
	Sections    []SeoScoreSectionResp   `json:"sections"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	URL         string                  `json:"url"`
	Duplicates  []SeoTitleDuplicateResp `json:"duplicates"`
	Coverage    SeoTitleCoverageResp    `json:"coverage"`
}

type SeoScoreSectionResp struct {
	Key     string              `json:"key"`
	Score   int                 `json:"score"`
	Max     int                 `json:"max"`
	Weight  float64             `json:"weight"`
	Percent int                 `json:"percent"`
	Checks  []SeoScoreCheckResp `json:"checks"`
}

type SeoScoreCheckResp struct {
	Key     string `json:"key"`
	Score   int    `json:"score"`
	Max     int    `json:"max"`
	Skipped bool   `json:"skipped"`
}

type SeoTitleDuplicateResp struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// SeoTitleCoverageResp 明示检查覆盖的实体域，未覆盖不能表述为“全站无重复”。
type SeoTitleCoverageResp struct {
	Included []string               `json:"included"`
	Omitted  []SeoTitleOmissionResp `json:"omitted"`
}

type SeoTitleOmissionResp struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}
