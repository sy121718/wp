package pagehttp

import (
	pagedto "go_wp/internal/module/page/dto"
)

type RedirectPageResp struct {
	ErrKey string

	CreateSource string
	CreateTarget string
	CreateFailed bool

	Projects        []pagedto.RedirectProjectOptionResp
	SelectedProject string
	Items           []pagedto.RedirectItemResp
	Total           int
	EffectiveCount  int

	MultiHopCount int
	LoopCount     int
}
