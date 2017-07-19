package main

import (
	"fmt"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/sdk"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/docker/log"
)

var logLevels = map[string]logrus.Level{
	"debug": logrus.DebugLevel,
	"info":  logrus.InfoLevel,
	"warn":  logrus.WarnLevel,
	"error": logrus.ErrorLevel,
}

func main() {
	levelVal := os.Getenv("LOG_LEVEL")
	if levelVal == "" {
		levelVal = "info"
	}
	if level, exists := logLevels[levelVal]; exists {
		logrus.SetLevel(level)
	} else {
		fmt.Fprintln(os.Stderr, "invalid log level: ", levelVal)
		os.Exit(1)
	}

	h := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)

	driver := openmanagedockerlogs.NewDriver()
	openmanagedockerlogs.NewHandler(&h, driver)

	logrus.Errorf("start listening on %s", common.LogDriverName)
	err := h.ServeUnix(common.LogDriverName, 0)
	if err != nil {
		panic(err)
	}
}
