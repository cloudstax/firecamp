package dns

import (
	"errors"

	"golang.org/x/net/context"
)

var (
	ErrDomainNotFound     = errors.New("DomainNotFound")
	ErrHostedZoneNotFound = errors.New("HostedZoneNotFound")
	ErrDNSRecordNotFound  = errors.New("DNSRecordNotFound")
	ErrInvalidRequest     = errors.New("InvalidRequest")
)

type DNS interface {
	GetOrCreateHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)
	GetHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error)
	DeleteHostedZone(ctx context.Context, hostedZoneID string) error

	// update the dnsName to point to hostIP in the hosted zone.
	// dnsName would be like db-0.domainName, hostIP is the IPv4 address.
	UpdateDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error
	// Wait till dns record is updated to hostIP.
	WaitDNSRecordUpdated(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) (dnsIP string, err error)
	GetDNSRecord(ctx context.Context, dnsName string, hostedZoneID string) (hostIP string, err error)
	DeleteDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error
}
