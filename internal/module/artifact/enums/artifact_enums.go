package artifactenums

import "go_wp/pkg/facing"

const (
	ErrArtifactNotFound = "ErrArtifactNotFound"
	ErrArtifactMismatch = "ErrArtifactMismatch"
	ErrInvalidArtifact  = "ErrInvalidArtifact"

	ErrInvalidParam = "ErrInvalidParam"

	ErrContentRefQueryFailed = "ErrContentRefQueryFailed"

	ErrContentObjectDeleteNotApplied = "ErrContentObjectDeleteNotApplied"
)

const (
	MsgArtifactSaved = "MsgArtifactSaved"
	MsgArtifactFound = "MsgArtifactFound"
)

func Err(key string) error { return facing.New(key) }

