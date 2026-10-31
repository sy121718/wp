package productdto

import (
	"time"
)

type PublishRecordRowResp struct {
	ProductID   string
	ProductName string
	ProductSlug string

	HasInstance bool

	Online bool

	Stale bool

	PublishedAt time.Time

	ArtifactVersion int64
	ArtifactHash    string

	Langs []PublishRecordLangResp
}

type PublishRecordLangResp struct {
	Lang string

	OnlinePath string

	Online bool

	Stale bool

	PublishedAt time.Time

	ArtifactVersion int64
	ArtifactHash    string
}
