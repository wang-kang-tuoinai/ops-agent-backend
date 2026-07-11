package utils

import "errors"

var (
	ErrLockConflict = errors.New("resource is locked")
)
