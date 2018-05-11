package main

import (
	"flag"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/pkg/db/awsdynamodb"
	"github.com/cloudstax/firecamp/pkg/server/awsec2"
	"github.com/cloudstax/firecamp/pkg/utils"
)

// This tool is for the stateless service to load the service configs from DB.

var (
	cluster    = flag.String("cluster", "", "The firecamp cluster")
	service    = flag.String("service-name", "", "The firecamp service name")
	outputfile = flag.String("outputfile", "", "The output file to store the service configs")
)

func main() {
	flag.Parse()

	utils.SetLogDir()

	// log error to std
	flag.Set("stderrthreshold", "INFO")
	defer glog.Flush()

	if len(*cluster) == 0 || len(*service) == 0 || len(*outputfile) == 0 {
		glog.Fatalln("please specify cluster", *cluster, "service", *service, "and outputfile", *outputfile)
	}

	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		glog.Fatalln("awsec2 GetLocalEc2Region error", err)
	}

	cfg := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Fatalln("failed to create session, error", err)
	}

	dbIns := awsdynamodb.NewDynamoDB(sess, *cluster)

	ctx := context.Background()

	svc, err := dbIns.GetService(ctx, *cluster, *service)
	if err != nil {
		glog.Fatalln("GetService error", err, "cluster", *cluster, "service", *service)
	}

	attr, err := dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Fatalln("GetServiceAttr error", err, svc)
	}

	for _, cfg := range attr.Spec.ServiceConfigs {
		if cfg.FileName == catalog.SERVICE_FILE_NAME {
			cfgfile, err := dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Fatalln("GetConfigFile error", err, "service", *service, cfg)
			}

			err = utils.CreateOrOverwriteFile(*outputfile, []byte(cfgfile.Spec.Content), utils.DefaultFileMode)
			if err != nil {
				glog.Fatalln("create outputfile", *outputfile, "error", err, "service", *service)
			}

			glog.Infoln("successfully created the service config file", *outputfile, "service", *service)
			return
		}
	}

	glog.Fatalln("service config file not found", *service)
}
