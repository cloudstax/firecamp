package firecampdockervolume

import (
	"net"
	"os/exec"
	"strings"

	"github.com/golang/glog"
)

func addIP(ip string, ifname string) error {
	var args []string
	args = append(args, "ip", "addr", "add", ip, "dev", ifname)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("add ip failed, arguments", args, "output", string(output[:]), "error", err)
		return err
	}

	glog.Infoln("added ip", ip, "to eth0")
	return nil
}

func delIP(ip string, ifname string) error {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		glog.Errorln("get net interface addrs error", err)
		return err
	}

	for _, addr := range addrs {
		addrstr := addr.String()
		if strings.HasPrefix(addrstr, ip) {
			err = delOneIP(addrstr, ifname)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func delOneIP(ip string, ifname string) error {
	var args []string
	args = append(args, "ip", "addr", "del", ip, "dev", ifname)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("del ip failed, arguments", args, "output", string(output[:]), "error", err)
		return err
	}

	glog.Infoln("deleted ip", ip, "from", ifname)
	return nil
}
