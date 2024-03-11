package awsroute53

import (
	"flag"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
)

func TestRoute53(t *testing.T) {
	flag.Parse()

	region := "us-west-1"

	cfg := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession error %s", err)
	}

	e := awsec2.NewAWSEc2(sess)
	r := NewAWSRoute53(sess)

	ctx := context.Background()

	// 1. basic tests
	// create one vpc
	vpc1, err := e.CreateVpc(ctx)
	if err != nil {
		t.Fatalf("CreateVpc error %s", err)
	}
	defer e.DeleteVpc(ctx, vpc1)

	private := true

	// negative case: get hosted zone before creation
	domain := "route53test.com"
	id1, err := r.GetHostedZoneIDByName(ctx, domain, vpc1, region, private)
	if err == nil || err != dns.ErrDomainNotFound {
		t.Fatalf("get hosted zone before creation, expect ErrDomainNotFound, but got error %s, domain %s vpc %s %s",
			err, domain, vpc1, region)
	}

	// create hosted zone with vpc
	hostedZoneID1, err := r.GetOrCreateHostedZoneIDByName(ctx, domain, vpc1, region, private)
	if err != nil {
		t.Fatalf("GetOrCreateHostedZoneIDByName error %s for domain %s, vpc %s %s", err, domain, vpc1, region)
	}
	defer r.DeleteHostedZone(ctx, hostedZoneID1)

	// get hosted zone
	id1, err = r.GetHostedZoneIDByName(ctx, domain, vpc1, region, private)
	if err != nil || id1 != hostedZoneID1 {
		t.Fatalf("GetHostedZoneIDByName expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID1, id1, err, domain, vpc1, region)
	}

	// negative case: create the same hosted zone again
	id1, err = r.createHostedZone(ctx, domain, vpc1, region, private)
	if err != nil || id1 != hostedZoneID1 {
		t.Fatalf("createHostedZone again expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID1, id1, err, domain, vpc1, region)
	}

	// 2. multiple hosted zone tests
	// create one more vpc
	vpc2, err := e.CreateVpc(ctx)
	if err != nil {
		t.Fatalf("CreateVpc error %s", err)
	}
	defer e.DeleteVpc(ctx, vpc2)

	// negative case: get hosted zone before creation
	id1, err = r.GetHostedZoneIDByName(ctx, domain, vpc2, region, private)
	if err == nil || err != dns.ErrDomainNotFound {
		t.Fatalf("get hosted zone before creation, expect ErrDomainNotFound, but got error %s, domain %s vpc %s %s",
			err, domain, vpc2, region)
	}

	// create one more hosted zone
	hostedZoneID2, err := r.GetOrCreateHostedZoneIDByName(ctx, domain, vpc2, region, private)
	if err != nil {
		t.Fatalf("GetOrCreateHostedZoneIDByName error %s for domain %s, vpc %s %s", err, domain, vpc2, region)
	}
	defer r.DeleteHostedZone(ctx, hostedZoneID2)

	// get hosted zone
	id1, err = r.GetHostedZoneIDByName(ctx, domain, vpc2, region, private)
	if err != nil || id1 != hostedZoneID2 {
		t.Fatalf("GetHostedZoneIDByName expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID2, id1, err, domain, vpc2, region)
	}

	// negative case: create the same hosted zone again
	id1, err = r.createHostedZone(ctx, domain, vpc2, region, private)
	if err != nil || id1 != hostedZoneID2 {
		t.Fatalf("createHostedZone again expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID2, id1, err, domain, vpc2, region)
	}

	// get hosted zone
	id1, err = r.GetHostedZoneIDByName(ctx, domain, vpc1, region, private)
	if err != nil || id1 != hostedZoneID1 {
		t.Fatalf("GetHostedZoneIDByName expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID1, id1, err, domain, vpc1, region)
	}

	// get hosted zone
	id1, err = r.GetHostedZoneIDByName(ctx, domain, vpc2, region, private)
	if err != nil || id1 != hostedZoneID2 {
		t.Fatalf("GetHostedZoneIDByName expected id %s, got id %s error %s, for domain %s vpc %s %s",
			hostedZoneID2, id1, err, domain, vpc2, region)
	}

	// create the first dns record
	hostIP := "10.0.0.1"
	svcName := "svc1"
	dnsName := dns.GenDNSName(svcName, domain)
	err = r.UpdateDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)
	if err != nil {
		t.Fatalf("UpdateDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	hostIP1, err := r.WaitDNSRecordUpdated(ctx, dnsName, hostIP, hostedZoneID1)
	if err != nil {
		r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostIP1 != hostIP {
		r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)
		t.Fatalf("expect hostIP %s, get %s, dnsName %s hostedZone %s", hostIP, hostIP1, dnsName, hostedZoneID1)
	}

	// update dns again
	newhostIP := "10.0.0.2"
	err = r.UpdateDNSRecord(ctx, dnsName, newhostIP, hostedZoneID1)
	defer r.DeleteDNSRecord(ctx, dnsName, newhostIP, hostedZoneID1)
	if err != nil {
		r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)
		t.Fatalf("UpdateDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	hostIP1, err = r.WaitDNSRecordUpdated(ctx, dnsName, newhostIP, hostedZoneID1)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostIP1 != newhostIP {
		t.Fatalf("expect hostIP %s, get %s, dnsName %s hostedZone %s", newhostIP, hostIP1, dnsName, hostedZoneID1)
	}

	// create the second dns record
	svcName = "svc"
	dnsName = dns.GenDNSName(svcName, domain)
	err = r.UpdateDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)
	if err != nil {
		t.Fatalf("UpdateDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	defer r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID1)

	hostIP1, err = r.WaitDNSRecordUpdated(ctx, dnsName, hostIP, hostedZoneID1)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostIP1 != hostIP {
		t.Fatalf("expect hostIP %s, get %s, dnsName %s hostedZone %s", hostIP, hostIP1, dnsName, hostedZoneID1)
	}

	hostIP1, err = r.GetDNSRecord(ctx, dnsName, hostedZoneID1)
	if err != nil {
		t.Fatalf("GetDNSRecord error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostIP1 != hostIP {
		t.Fatalf("expect hostIP %s, get %s, dnsName %s hostedZone %s", hostIP, hostIP1, dnsName, hostedZoneID1)
	}

	// update dns record of hostedZoneID2
	err = r.UpdateDNSRecord(ctx, dnsName, hostIP, hostedZoneID2)
	if err != nil {
		t.Fatalf("UpdateDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID2)
	}
	defer r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID2)

	hostIP1, err = r.WaitDNSRecordUpdated(ctx, dnsName, hostIP, hostedZoneID2)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}
	if hostIP1 != hostIP {
		t.Fatalf("expect hostIP %s, get %s", hostIP, hostIP1)
	}

	hostIP1, err = r.GetDNSRecord(ctx, dnsName, hostedZoneID2)
	if err != nil {
		t.Fatalf("GetDNSRecord error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}
	if hostIP1 != hostIP {
		t.Fatalf("expect hostIP %s, get %s, dnsName %s hostedZone %s", hostIP, hostIP1, dnsName, hostedZoneID2)
	}

	// delete the dns record
	err = r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID2)
	if err != nil {
		t.Fatalf("DeleteDNSRecord error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}

	// negative case: delete the unexist dns record
	err = r.DeleteDNSRecord(ctx, dnsName, hostIP, hostedZoneID2)
	if err == nil {
		t.Fatalf("DeleteDNSRecord expect error but succeed, dnsName %s, hostedZone %s", dnsName, hostedZoneID2)
	}

	// negative case: get the unexist dns record
	hostIP1, err = r.GetDNSRecord(ctx, dnsName, hostedZoneID2)
	if err != dns.ErrDNSRecordNotFound {
		t.Fatalf("DeleteDNSRecord expect ErrDNSRecordNotFound but get error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}

	// delete the hostedZone
	err = r.DeleteHostedZone(ctx, hostedZoneID2)
	if err != nil {
		t.Fatalf("DeleteHostedZone error %s, hostedZone %s", err, hostedZoneID2)
	}

	// negative case: delete the unexist hostedZone
	err = r.DeleteHostedZone(ctx, hostedZoneID2)
	if err == nil {
		t.Fatalf("DeleteHostedZone expect error but succeed, hostedZone %s", hostedZoneID2)
	}

	// negative case: get dns record on unexist hostedZone
	hostIP1, err = r.GetDNSRecord(ctx, dnsName, hostedZoneID2)
	if err != dns.ErrHostedZoneNotFound {
		t.Fatalf("GetDNSRecord expect ErrHostedZoneNotFound but get error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}
}
