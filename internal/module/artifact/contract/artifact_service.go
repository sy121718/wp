package artifactcontract

import (
	"context"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
)

type (
	RecordReq     = artifactdto.RecordReq
	DetailReq     = artifactdto.DetailReq
	DetailByIDReq = artifactdto.DetailByIDReq
	ArtifactResp  = artifactdto.ArtifactResp

	GCCandidateResp = artifactdto.GCCandidateResp

	PageArtifactMissRow = artifactdto.PageArtifactMissRow

	ContentObjectGCReq  = artifactdto.ContentObjectGCReq
	ContentObjectGCResp = artifactdto.ContentObjectGCResp
)

const (
	PayloadStateAvailable = "available"
	PayloadStateGCPending = "gc_pending"
	PayloadStateDeleted   = "deleted"
)

type ArtifactService interface {
	Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)

	EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)

	Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error)

	DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error)

	ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error)

	ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []artifactdto.GCCandidateResp, err error)

	CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error)

	MarkPayloadState(ctx context.Context, ids []string, state string) (n int64, err error)

	GarbageCollectContentObjects(ctx context.Context, req *artifactdto.ContentObjectGCReq) (res *artifactdto.ContentObjectGCResp, err error)
}

type PageArtifactReader interface {
	ListPageArtifactHashes(ctx context.Context) (hashes []string, err error)

	PageArtifactHashByID(ctx context.Context, id string) (hash string, err error)

	PageArtifactPageID(ctx context.Context, id string) (pageID string, err error)

	TranslationMisses(ctx context.Context, pageIDs []string) (rows []artifactdto.PageArtifactMissRow, err error)
}
