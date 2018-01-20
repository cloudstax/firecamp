package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/containersvc/k8s"
	"github.com/cloudstax/firecamp/docker/network"
	"github.com/cloudstax/firecamp/utils"
)

// For now, the deletestaticip container is only used on k8s for the service that requires static ip, such as Redis/Consul.
// The deletestaticip container simply deletes the ip attached to the local host.
// In the future, will integrate into k8s.

func main() {
	flag.Parse()

	// log to std
	utils.SetLogToStd()

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		fmt.Println("receive signal", sig)
		done <- true
	}()

	fmt.Println("awaiting signal")
	<-done
	fmt.Println("received signal")

	// wait for the main container to exit first
	time.Sleep(time.Second * 5)

	exist, err := k8ssvc.IsStaticIPFileExist()
	if err != nil {
		glog.Fatalln("check staticip file exist error", err)
	}

	if exist {
		// staticipfile exists. Must be the service that requires static ip. delete the ip from network.
		ip, err := k8ssvc.ReadStaticIP()
		if err != nil {
			glog.Fatalln("read static ip error", err)
		}

		err = dockernetwork.DeleteIP(ip, dockernetwork.DefaultNetworkInterface)
		if err != nil {
			glog.Fatalln("DeleteIP error", err, ip)
		}

		glog.Infoln("deleted ip", ip, "from network", dockernetwork.DefaultNetworkInterface)
	} else {
		glog.Errorln("not require to delete static ip")
	}

	glog.Flush()
}
