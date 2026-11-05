package buildenums

import "go_wp/pkg/facing"

const (
	ErrInvalidParam    = "ErrInvalidParam"
	ErrJobNotFound     = "ErrJobNotFound"
	ErrExecutorMissing = "ErrExecutorMissing"
	ErrInvalidSource   = "ErrInvalidSource"
	ErrInvalidMenuKind = "ErrInvalidMenuKind"
)

const (
	MsgQueueFound = "MsgQueueFound"
	MsgJobRetried = "MsgJobRetried"
)

func Err(key string) error { return facing.New(key) }
