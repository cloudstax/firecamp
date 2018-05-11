package clienterr

// An Error wraps lower level errors with http status code
type Error interface {
	error

	Code() int
}

// New returns an Error object described by the code, message,
func New(errcode int, errmsg string) Error {
	return newRequestError(errcode, errmsg)
}

type requestError struct {
	code int
	msg  string
}

func newRequestError(code int, msg string) requestError {
	return requestError{
		code: code,
		msg:  msg,
	}
}

func (e requestError) Error() string {
	return e.msg
}

func (e requestError) Code() int {
	return e.code
}
