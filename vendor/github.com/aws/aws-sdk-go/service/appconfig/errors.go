// Code generated by private/model/cli/gen-api/main.go. DO NOT EDIT.

package appconfig

import (
	"github.com/aws/aws-sdk-go/private/protocol"
)

const (

	// ErrCodeBadRequestException for service response error code
	// "BadRequestException".
	//
	// The input fails to satisfy the constraints specified by an Amazon Web Services
	// service.
	ErrCodeBadRequestException = "BadRequestException"

	// ErrCodeConflictException for service response error code
	// "ConflictException".
	//
	// The request could not be processed because of conflict in the current state
	// of the resource.
	ErrCodeConflictException = "ConflictException"

	// ErrCodeInternalServerException for service response error code
	// "InternalServerException".
	//
	// There was an internal failure in the AppConfig service.
	ErrCodeInternalServerException = "InternalServerException"

	// ErrCodePayloadTooLargeException for service response error code
	// "PayloadTooLargeException".
	//
	// The configuration size is too large.
	ErrCodePayloadTooLargeException = "PayloadTooLargeException"

	// ErrCodeResourceNotFoundException for service response error code
	// "ResourceNotFoundException".
	//
	// The requested resource could not be found.
	ErrCodeResourceNotFoundException = "ResourceNotFoundException"

	// ErrCodeServiceQuotaExceededException for service response error code
	// "ServiceQuotaExceededException".
	//
	// The number of one more AppConfig resources exceeds the maximum allowed. Verify
	// that your environment doesn't exceed the following service quotas:
	//
	// Applications: 100 max
	//
	// Deployment strategies: 20 max
	//
	// Configuration profiles: 100 max per application
	//
	// Environments: 20 max per application
	//
	// To resolve this issue, you can delete one or more resources and try again.
	// Or, you can request a quota increase. For more information about quotas and
	// to request an increase, see Service quotas for AppConfig (https://docs.aws.amazon.com/general/latest/gr/appconfig.html#limits_appconfig)
	// in the Amazon Web Services General Reference.
	ErrCodeServiceQuotaExceededException = "ServiceQuotaExceededException"
)

var exceptionFromCode = map[string]func(protocol.ResponseMetadata) error{
	"BadRequestException":           newErrorBadRequestException,
	"ConflictException":             newErrorConflictException,
	"InternalServerException":       newErrorInternalServerException,
	"PayloadTooLargeException":      newErrorPayloadTooLargeException,
	"ResourceNotFoundException":     newErrorResourceNotFoundException,
	"ServiceQuotaExceededException": newErrorServiceQuotaExceededException,
}