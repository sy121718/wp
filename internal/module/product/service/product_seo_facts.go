package productservice

// SeoScoreFacts 汇总实体评分、搜索摘要与重复标题的业务事实。
type SeoScoreFacts struct {
	EntityType  string
	EntityID    string
	ProjectID   string
	Total       int
	ProfileType string
	Sections    []SeoScoreSection
	Title       string
	Description string
	URL         string
	Duplicates  []SeoTitleDuplicate
	Coverage    SeoTitleCoverage
}

// SeoScoreSection 保存单个维度的分值、权重与检查事实。
type SeoScoreSection struct {
	Key     string
	Score   int
	Max     int
	Weight  float64
	Percent int
	Checks  []SeoScoreCheck
}

// SeoScoreCheck 保存检查项的得分及是否跳过。
type SeoScoreCheck struct {
	Key     string
	Score   int
	Max     int
	Skipped bool
}

// SeoTitleDuplicate 标识同工程内标题重复的实体。
type SeoTitleDuplicate struct {
	ID    string
	Kind  string
	Title string
	Path  string
}

// SeoTitleCoverage 记录实际参与重复标题检查的来源及遗漏。
type SeoTitleCoverage struct {
	Included []string
	Omitted  []SeoTitleOmission
}

// SeoTitleOmission 记录未参与检查的来源与原因。
type SeoTitleOmission struct {
	Source string
	Reason string
}
