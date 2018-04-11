package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/docker/go-plugins-helpers/sdk"
	"github.com/sirupsen/logrus"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/plugins/log"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

const socketAddress = "/run/docker/plugins/" + common.SystemName + "log.sock"

var logLevels = map[string]logrus.Level{
	"debug": logrus.DebugLevel,
	"info":  logrus.InfoLevel,
	"warn":  logrus.WarnLevel,
	"error": logrus.ErrorLevel,
}

func main() {
	flag.Parse()
	// set the glog to std
	utils.SetLogToStd()

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

	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		fmt.Fprintln(os.Stderr, "awsec2 GetLocalEc2Region error", err)
		os.Exit(2)
	}

	cluster := os.Getenv("CLUSTER")
	if len(cluster) == 0 {
		fmt.Fprintln(os.Stderr, "CLUSTER name not set")
		os.Exit(2)
	}

	h := sdk.NewHandler(`{"Implements": ["LoggingDriver"]}`)

	firecampdockerlog.NewHandler(&h, region, cluster)

	logrus.Errorf("start listening on %s\n", socketAddress)
	err = h.ServeUnix(socketAddress, 0)
	if err != nil {
		panic(err)
	}
}
