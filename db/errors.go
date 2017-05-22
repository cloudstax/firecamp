package db

import (
	"errors"
)

const (
	StrErrDBInternal               = "DB Internal Error"
	StrErrDBLimitExceeded          = "DB LimitExceeded"
	StrErrDBTableInUse             = "DB TableInUse"
	StrErrDBResourceNotFound       = "DB ResourceNotFound"
	StrErrDBTableNotFound          = "DB TableNotFound"
	StrErrDBRecordNotFound         = "DB RecordNotFound"
	StrErrDBConditionalCheckFailed = "DB ConditionalCheckFailed"
	StrErrDBInvalidRequest         = "DB invalid request"
)

// Define the possible errors returned by DB interfaces
var (
	ErrDBInternal               = errors.New(StrErrDBInternal)
	ErrDBLimitExceeded          = errors.New(StrErrDBLimitExceeded)
	ErrDBTableInUse             = errors.New(StrErrDBTableInUse)
	ErrDBTableNotFound          = errors.New(StrErrDBTableNotFound)
	ErrDBResourceNotFound       = errors.New(StrErrDBResourceNotFound)
	ErrDBRecordNotFound         = errors.New(StrErrDBRecordNotFound)
	ErrDBConditionalCheckFailed = errors.New(StrErrDBConditionalCheckFailed)
	ErrDBInvalidRequest         = errors.New(StrErrDBInvalidRequest)
)
