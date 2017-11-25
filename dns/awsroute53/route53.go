package awsroute53

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/utils"
)

// Route53 related const
const (
	ConflictingDomainExists = "ConflictingDomainExists"
	maxRetryCount           = 5
	retryInterval           = 3 * time.Second
)

// AWSRoute53 handles route53 related functions
type AWSRoute53 struct {
	sess *session.Session
}

// NewAWSRoute53 creates a AWSRoute53 instance
func NewAWSRoute53(sess *session.Session) *AWSRoute53 {
	r := new(AWSRoute53)
	r.sess = sess
	return r
}

// GetOrCreateHostedZoneIDByName gets the hostedZoneID. If hostedZone does not exist, will create it.
func (r *AWSRoute53) GetOrCreateHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	hostedZoneID, err = r.GetHostedZoneIDByName(ctx, domainName, vpcID, vpcRegion, private)
	if err != nil {
		if err != dns.ErrDomainNotFound {
			glog.Errorln("GetHostedZoneIDByName error", err, "domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
			return "", err
		}

		glog.Infoln("domain not exists, create it", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)

		hostedZoneID, err = r.createHostedZone(ctx, domainName, vpcID, vpcRegion, private)
		if err != nil {
			glog.Errorln("CreateHostedZone error", err, "domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
			return "", err
		}
	}

	glog.Infoln("get hostedZoneID", hostedZoneID, "for domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
	return hostedZoneID, nil
}

func (r *AWSRoute53) createHostedZone(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	callerRef := requuid
	if len(requuid) == 0 {
		callerRef = utils.GenCallerID()
	}

	svc := route53.New(r.sess)
	params := &route53.CreateHostedZoneInput{
		CallerReference: aws.String(callerRef),
		Name:            aws.String(domainName),
		HostedZoneConfig: &route53.HostedZoneConfig{
			Comment:     aws.String("hosted zone for firecamp services"),
			PrivateZone: aws.Bool(private),
		},
		VPC: &route53.VPC{
			VPCId:     aws.String(vpcID),
			VPCRegion: aws.String(vpcRegion),
		},
	}

	resp, err := svc.CreateHostedZone(params)
	if err != nil {
		glog.Errorln("CreateHostedZone error", err, "domainName", domainName,
			"vpc", vpcID, vpcRegion, "requuid", requuid, "resp", resp)

		if err.(awserr.Error).Code() == ConflictingDomainExists {
			// hosted zone exists
			return r.GetHostedZoneIDByName(ctx, domainName, vpcID, vpcRegion, private)
		}

		return "", err
	}

	glog.Infoln("created hosted zone for domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid, "resp", resp)

	hostedZoneID = *(resp.HostedZone.Id)
	return hostedZoneID, nil
}

// GetHostedZoneIDByName gets the hostedZoneID.
func (r *AWSRoute53) GetHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	svc := route53.New(r.sess)

	dnsName := domainName
	zoneID := ""
	for true {
		params := &route53.ListHostedZonesByNameInput{
			DNSName: aws.String(dnsName),
		}
		if len(zoneID) != 0 {
			params.HostedZoneId = aws.String(zoneID)
		}

		resp, err := svc.ListHostedZonesByName(params)
		if err != nil {
			glog.Errorln("ListHostedZonesByName error", err, "domain", domainName,
				"zoneID", zoneID, "vpc", vpcID, vpcRegion, "requuid", requuid)
			return "", err
		}

		glog.Infoln("list hosted zones by domain", domainName, "zoneID", zoneID, "requuid", requuid, "resp", resp)

		if len(resp.HostedZones) != 0 {
			// not sure why, but route53 automatically append char '.' to the domainName
			// note: route53 automatically converts the name to lower case
			internalDomainName := domainName + "."

			for _, zone := range resp.HostedZones {
				if *(zone.Name) != internalDomainName {
					// route53.ListHostedZonesByName returns hosted zones  ordered  by domain name.
					// So if domain name is not the same, return not found.
					glog.Infoln("zone is not for domain", domainName, "zone", zone, "requuid", requuid)
					return "", dns.ErrDomainNotFound
				}

				// check if the zone is target hosted zone
				find, err := r.isTargetHostedZone(ctx, svc, *(zone.Id), vpcID, vpcRegion, private)
				if err != nil {
					glog.Errorln("isTargetHostedZone error", err, "zone", zone, "requuid", requuid)
					return "", err
				}
				if find {
					glog.Infoln("find hosted zone", *(zone.Id), "for domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
					return *(zone.Id), nil
				}
				// not the target hosted zone, check the next one
			}
		}

		if *(resp.IsTruncated) {
			// has more hosted zones to list
			dnsName = *(resp.NextDNSName)
			zoneID = *(resp.NextHostedZoneId)
			continue
		}

		// no more hosted zones to list, break
		glog.Errorln("no more hosted zones to list for domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
		break
	}

	glog.Errorln("not find hosted zone for domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)
	return "", dns.ErrDomainNotFound
}

func (r *AWSRoute53) isTargetHostedZone(ctx context.Context, svc *route53.Route53, hostedZoneID string, targetVpcID string, targetVpcRegion string, private bool) (bool, error) {
	requuid := utils.GetReqIDFromContext(ctx)
	getParams := &route53.GetHostedZoneInput{
		Id: aws.String(hostedZoneID),
	}

	getResp, err := svc.GetHostedZone(getParams)
	if err != nil {
		glog.Errorln("GetHostedZone error", err, "zone", hostedZoneID, "requuid", requuid)
		return false, err
	}

	// for now, the zone should be private. Not expect the stateful app would be accessed publicly
	privateZone := *(getResp.HostedZone.Config.PrivateZone)
	if privateZone != private {
		glog.Infoln("expect", private, "zone, got a", privateZone, "zone, hostedZoneID",
			hostedZoneID, "requuid", requuid, "resp", getResp)
		return false, nil
	}

	// check the hosted zone has the target vpcID and vpcRegion
	for _, vpc := range getResp.VPCs {
		if *(vpc.VPCId) == targetVpcID && *(vpc.VPCRegion) == targetVpcRegion {
			glog.Infoln("find hosted zone", hostedZoneID, "requuid", requuid, "vpc", vpc)
			return true, nil
		}
	}

	glog.Infoln("zone's vpc doesn't match the target vpc", targetVpcID, targetVpcRegion,
		"zone", hostedZoneID, "requuid", requuid, "resp", getResp)
	return false, nil
}

// WaitDNSRecordUpdated waits till DNS lookup returns the expected hostIP.
func (r *AWSRoute53) WaitDNSRecordUpdated(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) (dnsIP string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	// wait till DNS lookup returns the updated value
	for i := 0; i < maxRetryCount; i++ {
		dnsIP, err = r.GetDNSRecord(ctx, dnsName, hostedZoneID)
		if err != nil {
			glog.Errorln("GetDNSRecord error", err, "hostedZoneID", hostedZoneID,
				"dnsName", dnsName, "hostIP", hostIP, "requuid", requuid)
			return dnsIP, err
		}
		if dnsIP == hostIP {
			glog.Infoln("dns record is updated to host ip", hostIP, "hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid)
			return dnsIP, nil
		}

		glog.Infoln("dns record is not updated to host ip", hostIP, "yet, current host ip", dnsIP,
			"hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid)
		time.Sleep(retryInterval)
	}

	glog.Errorln("dns record is not updated to host ip", hostIP, "hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid)
	return dnsIP, common.ErrTimeout
}

// UpdateDNSRecord updates the service's route53 record
func (r *AWSRoute53) UpdateDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error {
	return r.changeServiceDNSRecord(ctx, route53.ChangeActionUpsert, dnsName, hostIP, hostedZoneID)
}

func (r *AWSRoute53) changeServiceDNSRecord(ctx context.Context, action string, dnsName string, hostIP string, hostedZoneID string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	params := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZoneID),
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String(action),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(dnsName),
						Type: aws.String(route53.RRTypeA),
						ResourceRecords: []*route53.ResourceRecord{
							{
								Value: aws.String(hostIP),
							},
						},
						TTL: aws.Int64(common.ServiceMemberDomainNameTTLSeconds),
					},
				},
			},
		},
	}

	svc := route53.New(r.sess)
	resp, err := svc.ChangeResourceRecordSets(params)
	if err != nil {
		glog.Errorln("change service dns error", err, "requuid", requuid, "params", params)
		return r.convertError(err, action)
	}

	glog.Infoln("changed service dns", dnsName, "to", hostIP, "action", action, "requuid", requuid, "resp", resp)
	return nil
}

// GetDNSRecord returns the host ip for the dnsname.
func (r *AWSRoute53) GetDNSRecord(ctx context.Context, dnsName string, hostedZoneID string) (string, error) {
	requuid := utils.GetReqIDFromContext(ctx)
	svc := route53.New(r.sess)
	params := &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(hostedZoneID),
		MaxItems:        aws.String("1"),
		StartRecordName: aws.String(dnsName),
	}

	resp, err := svc.ListResourceRecordSets(params)
	if err != nil {
		glog.Errorln("getServiceDNSRecord error", err, "hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid)
		return "", r.convertError(err, "")
	}
	if len(resp.ResourceRecordSets) == 0 {
		glog.Errorln("no record set exists for hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid, "resp", resp)
		return "", dns.ErrDNSRecordNotFound
	}
	if len(resp.ResourceRecordSets) != 1 {
		glog.Errorln("more than 1 record sets for hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid, "resp", resp)
		return "", common.ErrInternal
	}

	recordSet := resp.ResourceRecordSets[0]
	if len(recordSet.ResourceRecords) != 1 {
		glog.Errorln("more than 1 records for hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid, "resp", resp)
		return "", common.ErrInternal
	}

	record := *(recordSet.ResourceRecords[0].Value)
	glog.Infoln("getServiceDNSRecord", record, "for hostedZoneID", hostedZoneID, "dnsName", dnsName, "requuid", requuid)
	return record, nil
}

// LookupLocalDNS looks up the given host using the local resolver.
func (r *AWSRoute53) LookupLocalDNS(ctx context.Context, dnsName string) (dnsIP string, err error) {
	return dns.LookupHost(dnsName)
}

// DeleteDNSRecord deletes one dns record.
func (r *AWSRoute53) DeleteDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error {
	return r.changeServiceDNSRecord(ctx, route53.ChangeActionDelete, dnsName, hostIP, hostedZoneID)
}

// DeleteHostedZone deletes the hostedZone. Please delete all dns records before DeleteHostedZone.
func (r *AWSRoute53) DeleteHostedZone(ctx context.Context, hostedZoneID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &route53.DeleteHostedZoneInput{
		Id: aws.String(hostedZoneID),
	}

	svc := route53.New(r.sess)
	_, err := svc.DeleteHostedZone(params)
	if err != nil {
		glog.Errorln("DeleteHostedZone error", err, "hostedZoneID", hostedZoneID, "requuid", requuid)
		return r.convertError(err, "")
	}

	glog.Infoln("deleted hostedZone", hostedZoneID, "requuid", requuid)
	return nil
}

func (r *AWSRoute53) convertError(err error, action string) error {
	switch err.(awserr.Error).Code() {
	case route53.ErrCodeNoSuchHostedZone:
		return dns.ErrHostedZoneNotFound
	case route53.ErrCodeInvalidChangeBatch:
		switch action {
		case route53.ChangeActionDelete:
			// The InvalidChangeBatch error on deletion means the record not found,
			// as we does send the correct request.
			return dns.ErrDNSRecordNotFound
		default:
			return err
		}
	default:
		return err
	}
}
