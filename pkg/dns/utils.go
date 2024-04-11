package dns

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/server"
)

// AWS DNS (Route53) supports 10,000 resource record sets per hosted zone.
// http://docs.aws.amazon.com/Route53/latest/DeveloperGuide/DNSLimitations.html.
//
// Would be enough to host all services' members of one cluster in one hosted zone.
// Every cluster should have its own hosted zone and dns namespace. The customer
// could create the same services in two clusters, without impacting each other.
// For one cluster, the default domain name would be cluster-DomainNameSuffix.com,
// which will be used to create the hosted zone.
// For one service in the cluster, the dns name of one service member would be
// serviceMember.cluster-DomainNameSuffix.com. For example, db-0.cluster-firecamp.com
//
// AWS VPC belongs to one region. The EC2 instances in different AZs could use the same VPC.
//
// test:
// 1. create 1 hosted zone and 1 record set.
// from the ec2 instance in a different vpc, could NOT nslookup the record.
// from the ec2 instance in the same vpc, could nslookup the record.
// 2. create 2 hosted zones in the same vpc and 2 record sets.
// from the ec2 instance in the same vpc, could nslookup both records.
// 3. create 2 hosted zones on the different vpcs and 2 record sets.
// from one ec2 instance, could NOT nslookup the record in the hosted zone2 of the different vpc2.
// after add vpc1 to the hosted zone2, could nslookup.
const dnsNameSeparator = "."

// GenDNSName generates the dns name for the service member
func GenDNSName(svcMemberName string, domainName string) string {
	return svcMemberName + dnsNameSeparator + domainName
}

// GenDefaultDomainName generates the default domain name for the cluster
// example: cluster-firecamp.com
func GenDefaultDomainName(clusterName string) string {
	return clusterName + common.NameSeparator + common.DomainNameSuffix + common.DomainSeparator + common.DomainCom
}

// RegisterDNSName registers the dns name
func RegisterDNSName(ctx context.Context, domainName string, dnsName string, serverInfo server.Info, dnsIns DNS) error {
	if !strings.HasSuffix(dnsName, domainName) {
		return ErrDomainNotFound
	}

	private := true
	vpcID := serverInfo.GetLocalVpcID()
	vpcRegion := serverInfo.GetLocalRegion()
	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(ctx, domainName, vpcID, vpcRegion, private)
	if err != nil {
		return err
	}

	privateIP := serverInfo.GetPrivateIP()
	return dnsIns.UpdateDNSRecord(ctx, dnsName, privateIP, hostedZoneID)
}

// GetDomainNameFromDNSName extracts the domain name from the dns name.
// example: aa1.test.com, return test.com
func GetDomainNameFromDNSName(dnsname string) (string, error) {
	names := strings.Split(dnsname, dnsNameSeparator)
	if len(names) < 3 {
		return "", ErrDomainNotFound
	}
	l := len(names)
	domain := names[l-2] + dnsNameSeparator + names[l-1]
	return domain, nil
}

// GetDefaultManageServiceDNSName returns the default management service dnsname.
// example: firecamp-manageserver.cluster-firecamp.com
func GetDefaultManageServiceDNSName(cluster string) string {
	domain := GenDefaultDomainName(cluster)
	return GenDNSName(common.ManageServiceName, domain)
}

// GetManageServiceURL returns the manage server url.
func GetManageServiceURL(dnsname string, tlsEnabled bool) string {
	if tlsEnabled {
		return "https://" + dnsname + ":" + strconv.Itoa(common.ManageHTTPServerPort) + "/"
	}
	return "http://" + dnsname + ":" + strconv.Itoa(common.ManageHTTPServerPort) + "/"
}

// GetDefaultManageServiceURL returns the default management service address.
// example: https://firecamp-manageserver.cluster-firecamp.com:27040/
func GetDefaultManageServiceURL(cluster string, tlsEnabled bool) string {
	domain := GenDefaultDomainName(cluster)
	dnsname := GenDNSName(common.ManageServiceName, domain)
	return GetManageServiceURL(dnsname, tlsEnabled)
}

// FormatManageServiceURL formats the url to like https://firecamp-manageserver.cluster-firecamp.com:27040/
func FormatManageServiceURL(surl string, tlsEnabled bool) string {
	if !strings.HasPrefix(surl, "http") {
		// add http prefix
		if tlsEnabled {
			return "https://" + surl + "/"
		}
		return "http://" + surl + "/"
	}

	// has http:// prefix, check if ends with "/"
	if !strings.HasSuffix(surl, "/") {
		return surl + "/"
	}
	return surl
}

// GetDefaultCatalogServiceDNSName returns the default catalog service dnsname.
// example: firecamp-catalogserver.cluster-firecamp.com
func GetDefaultCatalogServiceDNSName(cluster string) string {
	domain := GenDefaultDomainName(cluster)
	return GenDNSName(common.CatalogServiceName, domain)
}

// GetCatalogServiceURL returns the default catalog service address.
// example: https://firecamp-catalogserver.cluster-firecamp.com:27041/
func GetCatalogServiceURL(dnsname string, tlsEnabled bool) string {
	if tlsEnabled {
		return "https://" + dnsname + ":" + strconv.Itoa(common.CatalogServerHTTPPort) + "/"
	}
	return "http://" + dnsname + ":" + strconv.Itoa(common.CatalogServerHTTPPort) + "/"
}

// GetDefaultCatalogServiceURL returns the default catalog service address.
// example: https://firecamp-catalogserver.cluster-firecamp.com:27041/
func GetDefaultCatalogServiceURL(cluster string, tlsEnabled bool) string {
	domain := GenDefaultDomainName(cluster)
	dnsname := GenDNSName(common.CatalogServiceName, domain)
	return GetCatalogServiceURL(dnsname, tlsEnabled)
}

// LookupHost looks up the given host using the local resolver.
func LookupHost(host string) (addr string, err error) {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return "", err
	}
	if len(addrs) != 1 {
		errmsg := fmt.Sprintf("get more than 1 address for %s, %s", host, addrs)
		return "", errors.New(errmsg)
	}
	return addrs[0], nil
}
