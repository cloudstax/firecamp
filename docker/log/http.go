package firecampdockerlogs

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/docker/docker/daemon/logger"
	"github.com/docker/go-plugins-helpers/sdk"
)

type StartLoggingRequest struct {
	File string
	Info logger.Info
}

type StopLoggingRequest struct {
	File string
}

type CapabilitiesResponse struct {
	Err string
	Cap logger.Capability
}

type ReadLogsRequest struct {
	Info   logger.Info
	Config logger.ReadConfig
}

const START_LOGGING_REQUEST string = "/LogDriver.StartLogging"
const STOP_LOGGING_REQUEST string = "/LogDriver.StopLogging"
const LOG_CAPABILITIES_REQUEST string = "/LogDriver.Capabilities"

func NewHandler(h *sdk.Handler, d LogDriver) {
	h.HandleFunc(START_LOGGING_REQUEST, startLoggingHandleFunc(d))
	h.HandleFunc(STOP_LOGGING_REQUEST, stopLoggingHandleFunc(d))
	h.HandleFunc(LOG_CAPABILITIES_REQUEST, capabilityHandleFunc(d))
}

type response struct {
	Err string
}

func respond(err error, w http.ResponseWriter) {
	var res response
	if err != nil {
		res.Err = err.Error()
	}
	json.NewEncoder(w).Encode(&res)
}

func startLoggingHandleFunc(d LogDriver) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req StartLoggingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Info.ContainerID == "" {
			respond(errors.New("must provide container id in log context"), w)
			return
		}

		err := d.StartLogging(req.File, req.Info)
		respond(err, w)
	}
}

func stopLoggingHandleFunc(d LogDriver) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req StopLoggingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := d.StopLogging(req.File)
		respond(err, w)
	}
}

func capabilityHandleFunc(d LogDriver) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(
			&CapabilitiesResponse{
				Cap: d.GetCapability(),
			},
		)
	}
}
