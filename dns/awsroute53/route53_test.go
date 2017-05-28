package awsroute53

import (
	"flag"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/openconnectio/openmanage/dns"
	"github.com/openconnectio/openmanage/server/awsec2"
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

	// create the same hosted zone again
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

	// create the same hosted zone again
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
	hostname := "myhost"
	svcName := "svc1"
	dnsName := dns.GenDNSName(svcName, domain)
	err = r.UpdateServiceDNSRecord(ctx, dnsName, hostname, hostedZoneID1)
	if err != nil {
		t.Fatalf("UpdateServiceDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	hostname1, err := r.WaitDNSRecordUpdated(ctx, dnsName, hostname, hostedZoneID1)
	if err != nil {
		r.deleteDNSRecord(ctx, dnsName, hostname, hostedZoneID1)
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostname1 != hostname {
		r.deleteDNSRecord(ctx, dnsName, hostname, hostedZoneID1)
		t.Fatalf("expect hostname %s, get %s, dnsName %s hostedZone %s", hostname, hostname1, dnsName, hostedZoneID1)
	}

	// update dns again
	newhostname := "myhost2"
	err = r.UpdateServiceDNSRecord(ctx, dnsName, newhostname, hostedZoneID1)
	defer r.deleteDNSRecord(ctx, dnsName, newhostname, hostedZoneID1)
	if err != nil {
		r.deleteDNSRecord(ctx, dnsName, hostname, hostedZoneID1)
		t.Fatalf("UpdateServiceDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	hostname1, err = r.WaitDNSRecordUpdated(ctx, dnsName, newhostname, hostedZoneID1)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostname1 != newhostname {
		t.Fatalf("expect hostname %s, get %s, dnsName %s hostedZone %s", newhostname, hostname1, dnsName, hostedZoneID1)
	}

	// create the second dns record
	svcName = "svc"
	dnsName = dns.GenDNSName(svcName, domain)
	err = r.UpdateServiceDNSRecord(ctx, dnsName, hostname, hostedZoneID1)
	if err != nil {
		t.Fatalf("UpdateServiceDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID1)
	}
	defer r.deleteDNSRecord(ctx, dnsName, hostname, hostedZoneID1)

	hostname1, err = r.WaitDNSRecordUpdated(ctx, dnsName, hostname, hostedZoneID1)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID1)
	}
	if hostname1 != hostname {
		t.Fatalf("expect hostname %s, get %s, dnsName %s hostedZone %s", hostname, hostname1, dnsName, hostedZoneID1)
	}

	// update dns record of hostedZoneID2
	err = r.UpdateServiceDNSRecord(ctx, dnsName, hostname, hostedZoneID2)
	if err != nil {
		t.Fatalf("UpdateServiceDNSRecord error %s, svc %s domain %s hostedZone %s", err, svcName, domain, hostedZoneID2)
	}
	defer r.deleteDNSRecord(ctx, dnsName, hostname, hostedZoneID2)

	hostname1, err = r.WaitDNSRecordUpdated(ctx, dnsName, hostname, hostedZoneID2)
	if err != nil {
		t.Fatalf("WaitDNSRecordUpdated error %s, dnsName %s, hostedZone %s", err, dnsName, hostedZoneID2)
	}
	if hostname1 != hostname {
		t.Fatalf("expect hostname %s, get %s", hostname, hostname1)
	}
}
