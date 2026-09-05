package apperr

import "errors"

var (
	ErrMySQL   = errors.New("mysql")
	ErrCache   = errors.New("cache")
	ErrHandler = errors.New("handler")
	ErrMQ      = errors.New("mq")
)
