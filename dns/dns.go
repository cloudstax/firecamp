package dns

import (
	"errors"
)

var ErrDomainNotFound = errors.New("DomainNotFound")

type DNS interface {
	GetOrCreateHostedZoneIDByName(domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)
	GetHostedZoneIDByName(domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)

	// update the dnsName to point to hostname in the hosted zone.
	// dnsName would be like db-0.domainName, hostname is CNAME.
	UpdateServiceDNSRecord(dnsName string, hostname string, hostedZoneID string) error
	// Wait till dns record is updated to hostname.
	WaitDNSRecordUpdated(dnsName string, hostname string, hostedZoneID string) (dnsHostname string, err error)
}
