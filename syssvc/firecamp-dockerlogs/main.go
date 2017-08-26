package main

import (
	"fmt"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/sdk"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/docker/log"
)

const socketAddress = "/run/docker/plugins/" + common.SystemName + "log.sock"

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

	driver := firecampdockerlogs.NewDriver()
	firecampdockerlogs.NewHandler(&h, driver)

	logrus.Errorf("start listening on %s\n", socketAddress)
	err := h.ServeUnix(socketAddress, 0)
	if err != nil {
		panic(err)
	}
}
