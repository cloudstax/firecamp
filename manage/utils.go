package manage

import (
	"net/http"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/db"
)

func ConvertToHTTPError(err error) (errmsg string, errcode int) {
	switch err {
	case common.ErrServiceExist:
		return err.Error(), http.StatusConflict
	case common.ErrTimeout:
		return err.Error(), http.StatusRequestTimeout
	case db.ErrDBInvalidRequest:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	case db.ErrDBRecordNotFound:
		return http.StatusText(http.StatusNotFound), http.StatusNotFound
	case db.ErrDBConditionalCheckFailed:
		return err.Error(), http.StatusPreconditionFailed
	case db.ErrDBTableNotFound:
		return http.StatusText(http.StatusNotFound), http.StatusNotFound
	}
	return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
}

func ConvertHTTPError(httperrcode int) error {
	switch httperrcode {
	case http.StatusOK:
		return nil
	case http.StatusConflict:
		return common.ErrServiceExist
	case http.StatusRequestTimeout:
		return common.ErrTimeout
	case http.StatusBadRequest:
		return common.ErrInvalidArgs
	case http.StatusNotFound:
		return common.ErrNotFound
	case http.StatusPreconditionFailed:
		return common.ErrConditionalCheckFailed
	default:
		return common.ErrInternal
	}
}
