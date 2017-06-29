package manage

import (
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

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

func CreateConfigFile(ctx context.Context, dbIns db.DB, cfgfile *common.ConfigFile, requuid string) (*common.ConfigFile, error) {
	err := dbIns.CreateConfigFile(ctx, cfgfile)
	if err == nil {
		glog.Infoln("created config file", db.PrintConfigFile(cfgfile), "requuid", requuid)
		return cfgfile, nil
	}

	if err != db.ErrDBConditionalCheckFailed {
		glog.Errorln("CreateConfigFile error", err, db.PrintConfigFile(cfgfile), "requuid", requuid)
		return nil, err
	}

	// config file exists, check whether it is a retry request.
	currcfg, err := dbIns.GetConfigFile(ctx, cfgfile.ServiceUUID, cfgfile.FileID)
	if err != nil {
		glog.Errorln("get existing config file error", err, db.PrintConfigFile(cfgfile), "requuid", requuid)
		return nil, err
	}

	skipMtime := true
	skipContent := true
	if !db.EqualConfigFile(currcfg, cfgfile, skipMtime, skipContent) {
		glog.Errorln("config file not match, current", db.PrintConfigFile(currcfg),
			"new", db.PrintConfigFile(cfgfile), "requuid", requuid)
		return nil, common.ErrConfigMismatch
	}

	glog.Infoln("config file exists", db.PrintConfigFile(currcfg), "requuid", requuid)
	return currcfg, nil
}
