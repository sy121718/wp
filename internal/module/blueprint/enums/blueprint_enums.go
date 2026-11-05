package blueprintenums

import "go_wp/pkg/facing"

const (
	MsgCreateSuccess  = "MsgCreateSuccess"
	MsgUpdateSuccess  = "MsgUpdateSuccess"
	MsgPublishSuccess = "MsgPublishSuccess"
	MsgListSuccess    = "MsgListSuccess"
	MsgDetailSuccess  = "MsgDetailSuccess"
	MsgDeleteSuccess  = "MsgDeleteSuccess"

	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"
	ErrInvalidKind  = "ErrInvalidKind"
	ErrDataInvalid  = "ErrDataInvalid"
)

func Err(key string) error { return facing.New(key) }

