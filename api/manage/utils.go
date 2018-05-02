package manage

import (
	"errors"
	"io/ioutil"
	"net/http"
)

func ConvertHTTPError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusConflict:
		return errors.New("ServiceExist: " + string(body))
	case http.StatusRequestTimeout:
		return errors.New("Timeout: " + string(body))
	case http.StatusBadRequest:
		return errors.New("InvalidArgs: " + string(body))
	case http.StatusNotFound:
		return errors.New("NotFound: " + string(body))
	case http.StatusPreconditionFailed:
		return errors.New("ConditionalCheckFailed: " + string(body))
	default:
		return errors.New("InternalError: " + string(body))
	}
}
