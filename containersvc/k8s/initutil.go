package k8ssvc

import (
	"io/ioutil"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// K8sServiceInitContainerImage is the common init container image for all services in k8s.
	K8sServiceInitContainerImage      = common.OrgName + common.SystemName + "-initcontainer"
	K8sServiceStopContainerImage      = common.OrgName + common.SystemName + "-stopcontainer"
	k8sServiceStopContainerPreStopCmd = "/firecamp-stopcontainer"

	EnvInitContainerTestMode    = "TESTMODE"
	EnvInitContainerCluster     = "CLUSTER"
	EnvInitContainerServiceName = "SERVICE_NAME"
	EnvInitContainerPodName     = "POD_NAME"

	staticipfilepath = common.DefaultConfigPath + "/firecamp-service-staticip"
)

// IsStaticIPFileExist returns whether the static ip file exists.
func IsStaticIPFileExist() (bool, error) {
	return utils.IsFileExist(staticipfilepath)
}

// ReadStaticIP reads the member's static ip from file.
func ReadStaticIP() (ip string, err error) {
	b, err := ioutil.ReadFile(staticipfilepath)
	if err != nil {
		glog.Errorln("read static ip file error", err, staticipfilepath)
		return "", err
	}
	return string(b), nil
}

// CreateStaticIPFile writes the static ip into file, so the container stop could delete the attached ip.
func CreateStaticIPFile(ip string) error {
	exist, err := utils.IsFileExist(staticipfilepath)
	if err != nil {
		glog.Errorln("check static ip file exist error", err, staticipfilepath)
		return err
	}

	if exist {
		// get the static ip and check if ip is changed.
		oldIP, err := ReadStaticIP()
		if err != nil {
			return err
		}

		// ip not change, return
		if oldIP == ip {
			return nil
		}

		glog.Infoln("service member static ip changed, old ip", oldIP, "newip", ip)
		exist = false
	}

	glog.Infoln("create/overwrite the staticip file", staticipfilepath, "ip", ip)

	err = utils.CreateOrOverwriteFile(staticipfilepath, []byte(ip), common.DefaultConfigFileMode)
	if err != nil {
		glog.Errorln("create/overwrite the staticip file error", err, "ip", ip, staticipfilepath)
		return err
	}
	return nil
}
