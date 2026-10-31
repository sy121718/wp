package pagedto

type RedirectProjectOptionResp struct {
	ID   string
	Name string
}

type RedirectItemResp struct {
	SourcePath string
	TargetPath string
	StatusCode int
	Effective  bool

	MultiHop bool

	Loop bool

	FinalPath string

	OwnerKind string
	OwnerID   string

	OwnerTitle string
	UpdatedAt  string
}

type RedirectListResp struct {
	ProjectID      string
	Projects       []RedirectProjectOptionResp
	Items          []RedirectItemResp
	Total          int
	EffectiveCount int
	MultiHopCount  int
	LoopCount      int
}
