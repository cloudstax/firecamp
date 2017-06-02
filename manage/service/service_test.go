package manageservice

import (
	"flag"
	"os"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/server/awsec2"
)

// TODO more test to cover every function in service.go
func TestMain(m *testing.M) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")
	m.Run()
}

func TestDeviceName(t *testing.T) {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Errorln("failed to create session, error", err)
		os.Exit(-1)
	}

	dbIns := db.NewMemDB()
	serverIns := awsec2.NewAWSEc2(sess)
	dnsIns := dns.NewMockDNS()
	s := NewManageService(dbIns, serverIns, dnsIns)

	ctx := context.Background()

	cluster := "cluster1"
	servicePrefix := "service-"

	// /dev/xvdg ~ xvdz
	devSeq := "g"
	for i := 0; i < 20; i++ {
		service := servicePrefix + strconv.Itoa(i)
		expectDev := "/dev/xvd" + devSeq
		dev, err := s.createDevice(ctx, cluster, service)
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// /dev/xvdba ~ z
	devSeq = "a"
	for i := 0; i < 26; i++ {
		service := servicePrefix + "b" + strconv.Itoa(i)
		expectDev := "/dev/xvdb" + devSeq
		dev, err := s.createDevice(ctx, cluster, service)
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// /dev/xvdca ~ z
	devSeq = "a"
	for i := 0; i < 26; i++ {
		service := servicePrefix + "c" + strconv.Itoa(i)
		expectDev := "/dev/xvdc" + devSeq
		dev, err := s.createDevice(ctx, cluster, service)
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// create device item for the existing service
	service := servicePrefix + "b0"
	dev, err := s.createDevice(ctx, cluster, service)
	if err != nil || dev != "/dev/xvdba" {
		t.Fatalf("create service %s again, expectDev /dev/xvdba, get %s, error %s", service, dev, err)
	}
}

func TestServiceCreation(t *testing.T) {
	glog.Infoln("===== TestServiceCreation start ====")

	dbIns := db.NewMemDB()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	s := NewManageService(dbIns, serverIns, dnsIns)
	TestUtil_ServiceCreateion(t, s, dbIns)
}

func TestServiceCreationRetry(t *testing.T) {
	// special tests to simulate the failure and retry
	glog.Infoln("===== TestCreationRetry start ====")

	dbIns := db.NewMemDB()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	s := NewManageService(dbIns, serverIns, dnsIns)
	TestUtil_ServiceCreationRetry(t, s, dbIns, dnsIns)
}
