package dns

import (
	"testing"

	"golang.org/x/net/context"
)

func TestDNS(t *testing.T) {
	domain := "example.com"
	vpcID := "vpc-1"
	vpcRegion := "us-west-1"
	private := true

	ctx := context.Background()

	d := NewMockDNS()

	hostedZoneID, err := d.GetHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != ErrDomainNotFound {
		t.Fatalf("domain %s should not exist, error %s", domain, err)
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

	err = d.DeleteHostedZone(ctx, hostedZoneID)
	if err != nil {
		t.Fatalf("DeleteHostedZone error %s", err)
	}

	err = d.DeleteHostedZone(ctx, hostedZoneID)
	if err != ErrHostedZoneNotFound {
		t.Fatalf("Delete unexist HostedZone, expect ErrHostedZoneNotFound, got error %s", err)
	}
}
