package executor

import "errors"

var (
	ErrUnknownExecutorType = errors.New("unknown executor type")
	ErrSessionNotFound     = errors.New("session not found")
	ErrExecutorClosed      = errors.New("executor closed")
)
