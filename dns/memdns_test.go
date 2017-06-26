package dns

import (
	"testing"

	"golang.org/x/net/context"
)

func TestDNS(t *testing.T) {
	domain := "example.com"
	dnsname := "m1.example.com"
	hostIP := "127.0.0.1"
	vpcID := "vpc-1"
	vpcRegion := "us-west-1"
	private := true

	ctx := context.Background()

	d := NewMockDNS()

	// negative case: get unexist HostedZone
	hostedZoneID, err := d.GetHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != ErrDomainNotFound {
		t.Fatalf("domain %s should not exist, error %s", domain, err)
	}

	// negative case: get dns record on unexist HostedZone
	_, err = d.GetDNSRecord(ctx, dnsname, "zoneid")
	if err != ErrHostedZoneNotFound {
		t.Fatalf("hostedzone should not exist")
	}

	hostedZoneID, err = d.GetOrCreateHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != nil {
		t.Fatalf("CreateHostedZone expected success, but got error %s, domain %s", err, domain)
	}

	id1, err := d.GetHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != nil || hostedZoneID != id1 {
		t.Fatalf("expect hostedZoneID %s for domain %s, but got id %s error %s", hostedZoneID, domain, id1, err)
	}

	id1, err = d.GetOrCreateHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != nil || hostedZoneID != id1 {
		t.Fatalf("CreateHostedZone expected hostedZoneID %s, but got id %s error %s, for domain %s",
			hostedZoneID, id1, err, domain)
	}

	// negative case: get unexist dns record
	_, err = d.GetDNSRecord(ctx, dnsname, hostedZoneID)
	if err != ErrDNSRecordNotFound {
		t.Fatalf("dnsname %s should not have host yet", dnsname)
	}

	err = d.UpdateDNSRecord(ctx, dnsname, hostIP, hostedZoneID)
	if err != nil {
		t.Fatalf("UpdateDNSRecord error %s", err)
	}

	hostIP1, err := d.GetDNSRecord(ctx, dnsname, hostedZoneID)
	if err != nil {
		t.Fatalf("GetDNSRecord error %s", err)
	}
	if hostIP1 != hostIP {
		t.Fatalf("expect hostIP %s, get %s", hostIP, hostIP1)
	}

	err = d.DeleteDNSRecord(ctx, dnsname, hostIP, hostedZoneID)
	if err != nil {
		t.Fatalf("DeleteDNSRecord error %s", err)
	}

	// negative case: get deleted dns record
	_, err = d.GetDNSRecord(ctx, dnsname, hostedZoneID)
	if err != ErrDNSRecordNotFound {
		t.Fatalf("dnsname %s should not have host yet", dnsname)
	}
	// negative case: delete the deleted dns record
	err = d.DeleteDNSRecord(ctx, dnsname, hostIP, hostedZoneID)
	if err != ErrDNSRecordNotFound {
		t.Fatalf("delete unexist dns record, expect success, get error %s", err)
	}

	err = d.DeleteHostedZone(ctx, hostedZoneID)
	if err != nil {
		t.Fatalf("DeleteHostedZone error %s", err)
	}

	err = d.DeleteHostedZone(ctx, hostedZoneID)
	if err != ErrHostedZoneNotFound {
		t.Fatalf("Delete unexist HostedZone, expect ErrHostedZoneNotFound, got error %s", err)
	}
}
