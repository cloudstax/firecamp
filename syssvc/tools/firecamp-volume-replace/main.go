package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

var (
	cluster  = flag.String("cluster", "", "firecamp cluster name")
	service  = flag.String("service-name", "", "firecamp service name")
	badVolID = flag.String("bad-volumeid", "", "the volume to replace")
	newVolID = flag.String("new-volumeid", "", "the new volume id")
)

func main() {
	flag.Parse()

	// log to std
	utils.SetLogToStd()

	if *cluster == "" || *service == "" || *badVolID == "" || *newVolID == "" {
		fmt.Println("please specify cluster, service, bad-volumeid and new-volumeid")
		os.Exit(-1)
	}

	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		fmt.Println("awsec2 GetLocalEc2Region error", err)
		os.Exit(-1)
	}

	config := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		fmt.Println("failed to create session, error", err)
		os.Exit(-1)
	}

	ctx := context.Background()
	dbIns := awsdynamodb.NewDynamoDB(sess, *cluster)

	// get service uuid
	svc, err := dbIns.GetService(ctx, *cluster, *service)
	if err != nil {
		fmt.Println("GetService", *cluster, *service, "error", err)
		os.Exit(-1)
	}

	// list all service members
	members, err := dbIns.ListServiceMembers(ctx, svc.ServiceUUID)
	if err != nil {
		fmt.Println("ListServiceMembers error", err, svc)
		os.Exit(-1)
	}

	for _, m := range members {
		if m.Volumes.JournalVolumeID == *badVolID || m.Volumes.PrimaryVolumeID == *badVolID {
			// update the service member
			err := dbIns.UpdateServiceMemberVolume(ctx, m, *newVolID, *badVolID)
			if err != nil {
				fmt.Println("UpdateServiceMemberVolume error", err, m)
				os.Exit(-1)
			}
			fmt.Println("successfully replaced the service member volume")
			return
		}
	}

	fmt.Println("bad volume does not belong to service")
	os.Exit(-1)
}
