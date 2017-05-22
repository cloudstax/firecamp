package dns

import (
	"testing"
)

func TestDNS(t *testing.T) {
	domain := "example.com"
	vpcID := "vpc-1"
	vpcRegion := "us-west-1"
	private := true

	d := NewMockDNS()

	hostedZoneID, err := d.GetHostedZoneIDByName(domain, vpcID, vpcRegion, private)
	if err != ErrDomainNotFound {
		t.Fatalf("domain %s should not exist, error %s", domain, err)
	}

	hostedZoneID, err = d.GetOrCreateHostedZoneIDByName(domain, vpcID, vpcRegion, private)
	if err != nil {
		t.Fatalf("CreateHostedZone expected success, but got error %s, domain %s", err, domain)
	}

	id1, err := d.GetHostedZoneIDByName(domain, vpcID, vpcRegion, private)
	if err != nil || hostedZoneID != id1 {
		t.Fatalf("expect hostedZoneID %s for domain %s, but got id %s error %s", hostedZoneID, domain, id1, err)
	}

	id1, err = d.GetOrCreateHostedZoneIDByName(domain, vpcID, vpcRegion, private)
	if err != nil || hostedZoneID != id1 {
		t.Fatalf("CreateHostedZone expected hostedZoneID %s, but got id %s error %s, for domain %s",
			hostedZoneID, id1, err, domain)
	}
}
