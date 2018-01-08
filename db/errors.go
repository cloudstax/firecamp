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
	ErrDBInternal      = errors.New(StrErrDBInternal)
	ErrDBLimitExceeded = errors.New(StrErrDBLimitExceeded)
	ErrDBTableInUse    = errors.New(StrErrDBTableInUse)
	ErrDBTableNotFound = errors.New(StrErrDBTableNotFound)
	// ResourceNotFound is for dynamodb table.
	// Table which is being requested does not exist, or is too early in the CREATING state.
	// https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Programming.Errors.html
	ErrDBResourceNotFound = errors.New(StrErrDBResourceNotFound)
	// RecordNotFound is when the key does not exist in db.
	ErrDBRecordNotFound         = errors.New(StrErrDBRecordNotFound)
	ErrDBConditionalCheckFailed = errors.New(StrErrDBConditionalCheckFailed)
	ErrDBInvalidRequest         = errors.New(StrErrDBInvalidRequest)
)
