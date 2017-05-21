package api

import "errors"

var (
	ErrInternal               = errors.New("InternalError")
	ErrTimeout                = errors.New("Timeout")
	ErrSystemCreating         = errors.New("System tables are at the creating status, please retry later")
	ErrServiceExist           = errors.New("Service exists")
	ErrServiceDeleting        = errors.New("Service deleting")
	ErrServiceDeleted         = errors.New("Service deleted")
	ErrConfigMismatch         = errors.New("Config mismatch")
	ErrInvalidArgs            = errors.New("InvalidArgs")
	ErrUnsupportedPlatform    = errors.New("Not supported container platform")
	ErrNotFound               = errors.New("NotFound")
	ErrConditionalCheckFailed = errors.New("ConditionalCheckFailed")
)
