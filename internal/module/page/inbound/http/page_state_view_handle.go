package pagehttp

import "go_wp/pkg/utils"

type MsgPair struct {
	Key      string
	Fallback string

	Args []string
}

func (m MsgPair) Empty() bool { return m.Key == "" && m.Fallback == "" }

type StaleItemResp struct {
	ID          string
	Path        string
	ProjectName string

	Published bool

	FailedStageKey      string
	FailedStageFallback string

	FailedAt *utils.JSONTime
}

type StaleOverviewResp struct {
	Available bool
	Total     int
	Pages     []StaleItemResp
	Limit     int
	Truncated bool
}
