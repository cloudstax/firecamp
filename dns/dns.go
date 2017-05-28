package dns

import (
	"errors"

	"golang.org/x/net/context"
)

var (
	ErrDomainNotFound     = errors.New("DomainNotFound")
	ErrHostedZoneNotFound = errors.New("HostedZoneNotFound")
)

type DNS interface {
	GetOrCreateHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)
	GetHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)
	DeleteHostedZone(ctx context.Context, hostedZoneID string) error

	// update the dnsName to point to hostname in the hosted zone.
	// dnsName would be like db-0.domainName, hostname is CNAME.
	UpdateServiceDNSRecord(ctx context.Context, dnsName string, hostname string, hostedZoneID string) error
	// Wait till dns record is updated to hostname.
	WaitDNSRecordUpdated(ctx context.Context, dnsName string, hostname string, hostedZoneID string) (dnsHostname string, err error)
}
