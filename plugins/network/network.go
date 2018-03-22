package dockernetwork

import (
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// DefaultNetworkInterface is the default static ip attach network.
	DefaultNetworkInterface = "eth0"
)

// ServiceNetwork handles the network for the service. It handles:
// 1. Update the dns record for the service member.
// 2. Assign/Unassign the static IP for the service member.
type ServiceNetwork struct {
	dbIns  db.DB
	dnsIns dns.DNS

	// server instance (ec2 on AWS) to assign/unassign network ip
	serverIns  server.Server
	serverInfo server.Info

	// the net interface to add/del ip
	ifname string
}

// NewServiceNetwork creates a ServiceNetwork instance.
func NewServiceNetwork(dbIns db.DB, dnsIns dns.DNS, serverIns server.Server, serverInfo server.Info) *ServiceNetwork {
	return &ServiceNetwork{
		dbIns:      dbIns,
		dnsIns:     dnsIns,
		serverIns:  serverIns,
		serverInfo: serverInfo,
		ifname:     DefaultNetworkInterface,
	}
}

// SetIfname sets the ifname. This is for the unit test only.
func (s *ServiceNetwork) SetIfname(ifname string) {
	s.ifname = ifname
}

// UpdateDNS updates the dns record of the service member to the private ip of the local server.
func (s *ServiceNetwork) UpdateDNS(ctx context.Context, domainName string, hostedZoneID string, member *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// update dns record
	dnsName := dns.GenDNSName(member.MemberName, domainName)
	privateIP := s.serverInfo.GetPrivateIP()

	err := s.dnsIns.UpdateDNSRecord(ctx, dnsName, privateIP, hostedZoneID)
	if err != nil {
		glog.Errorln("UpdateDNSRecord error", err, "requuid", requuid, "member", member)
		return err
	}

	// make sure DNS returns the updated record
	dnsIP, err := s.dnsIns.WaitDNSRecordUpdated(ctx, dnsName, privateIP, hostedZoneID)
	if err != nil {
		glog.Errorln("WaitDNSRecordUpdated error", err, "expect privateIP", privateIP, "got", dnsIP, "requuid", requuid, "member", member)
		return err
	}

	err = s.waitDNSLookup(ctx, dnsName, privateIP, requuid)
	if err != nil {
		glog.Errorln("waitDNSLookup error", err, "requuid", requuid, "member", member)
		return err
	}

	glog.Infoln("updated dns", dnsName, "to ip", privateIP, "requuid", requuid, "member", member)
	return nil
}

func (s *ServiceNetwork) waitDNSLookup(ctx context.Context, dnsName string, ip string, requuid string) error {
	// wait till the DNS record lookup on the node returns the updated ip.
	// This is to make sure DB doesn't get the invalid old host at the replication initialization.
	//
	// TODO After service is created, the first time DNS lookup from AWS Route53 takes around 60s.
	// Any way to enhance it? Also if container fails over to another node, the DNS lookup takes
	// around 16s to get the new ip, even if the default TTL is set to 3s.
	glog.Infoln("DNS record updated", dnsName, "wait till the local host refreshes it, requuid", requuid)

	maxWaitSeconds := time.Duration(90) * time.Second
	sleepSeconds := time.Duration(3) * time.Second
	for t := time.Duration(0); t < maxWaitSeconds; t += sleepSeconds {
		addr, err := s.dnsIns.LookupLocalDNS(ctx, dnsName)
		if err == nil && addr == ip {
			glog.Infoln("LookupLocalDNS", dnsName, "gets the expected ip", ip, "requuid", requuid)
			return nil
		}
		glog.Infoln("LookupLocalDNS", dnsName, "error", err, "get ip", addr, "requuid", requuid)
		time.Sleep(sleepSeconds)
	}

	glog.Errorln("local host waits the dns refreshes timed out, dnsname", dnsName, "expect ip", ip, "requuid", requuid)
	return common.ErrTimeout
}

// UpdateStaticIP unassigns the static ip from the old node and assigns to the local node.
func (s *ServiceNetwork) UpdateStaticIP(ctx context.Context, domainName string, member *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// get the static ip on the local server.
	netInterface, err := s.serverIns.GetInstanceNetworkInterface(ctx, s.serverInfo.GetLocalInstanceID())
	if err != nil {
		glog.Errorln("GetInstanceNetworkInterface error", err, "ServerInstanceID",
			s.serverInfo.GetLocalInstanceID(), "requuid", requuid, "member", member)
		return err
	}

	// whether the member's static ip is owned locally.
	localOwned := false
	var memberStaticIP *common.ServiceStaticIP
	for _, ip := range netInterface.PrivateIPs {
		serviceip, err := s.dbIns.GetServiceStaticIP(ctx, ip)
		if err != nil {
			if err != db.ErrDBRecordNotFound {
				glog.Errorln("GetServiceStaticIP error", err, "ip", ip, "requuid", requuid, "member", member)
				return err
			}

			// this is possible as ip is created at the network interface first, then put to db.
			glog.Infoln("ip", ip, "not found in db, network interface", netInterface.InterfaceID)
			continue
		}

		glog.Infoln("get service ip", serviceip, "requuid", requuid, "member", member)

		// if ip does not belong to the service, skip it
		if serviceip.ServiceUUID != member.ServiceUUID {
			continue
		}

		// ip belongs to the service, check if ip is for the current member.
		if ip == member.StaticIP {
			// ip is for the current member
			glog.Infoln("current node has the member's static ip, requuid", requuid, serviceip, member)
			localOwned = true
			memberStaticIP = serviceip
		} else {
			// ip belongs to the service but not for the current member.
			// unassign it to make sure other members do NOT take us as the previous member.

			// this should actually not happen, as the static ip will be unassigned when umount the member's volume.
			// here is just a protection to clean up the possible dangling ip. Q: how could the dangling ip happen?
			glog.Errorln("unassign dangling ip from local network", netInterface.InterfaceID,
				"server", netInterface.ServerInstanceID, serviceip, "requuid", requuid)

			err = s.serverIns.UnassignStaticIP(ctx, netInterface.InterfaceID, ip)
			if err != nil {
				glog.Errorln("UnassignStaticIP error", err, "ip", serviceip, "network interface",
					netInterface.InterfaceID, "requuid", requuid, "member", member)
				return err
			}
			// should not update db here, as another node may be in the process of taking over the static ip.

			// delete the possible ip from network
			err = s.DeleteIP(ip)
			if err != nil {
				glog.Errorln("delete ip error", err, "ip", ip, "requuid", requuid, "member", member)
				return err
			}
		}
	}

	if memberStaticIP == nil {
		// member's static ip is not owned by the local node, load from db.
		memberStaticIP, err = s.dbIns.GetServiceStaticIP(ctx, member.StaticIP)
		if err != nil {
			glog.Errorln("GetServiceStaticIP error", err, "requuid", requuid, "member", member)
			return err
		}
	}

	glog.Infoln("member's static ip", memberStaticIP, "requuid", requuid, "member", member)

	if localOwned {
		// the member's ip is owned by the local node, check whether need to update db.
		// The ServiceStaticIP in db may not be updated. For example, after ip is assigned to
		// the local node, node/plugin restarts before db is updated.
		if memberStaticIP.ServerInstanceID != s.serverInfo.GetLocalInstanceID() ||
			memberStaticIP.NetworkInterfaceID != netInterface.InterfaceID {
			newip := db.UpdateServiceStaticIP(memberStaticIP, s.serverInfo.GetLocalInstanceID(), netInterface.InterfaceID)
			err = s.dbIns.UpdateServiceStaticIP(ctx, memberStaticIP, newip)
			if err != nil {
				glog.Errorln("UpdateServiceStaticIP error", err, "ip", memberStaticIP, "requuid", requuid, "member", member)
				return err
			}

			glog.Infoln("UpdateServiceStaticIP to local node", newip, "requuid", requuid)
		}
	} else {
		// the member's ip is not owned by the local node, unassign it from the old owner,
		// assign to the local node and update db.
		err = s.serverIns.UnassignStaticIP(ctx, memberStaticIP.NetworkInterfaceID, memberStaticIP.StaticIP)
		if err != nil {
			glog.Errorln("UnassignStaticIP error", err, "ip", memberStaticIP, "requuid", requuid, member)
			return err
		}

		glog.Infoln("UnassignStaticIP from the old owner", memberStaticIP, "requuid", requuid, member)

		err = s.serverIns.AssignStaticIP(ctx, netInterface.InterfaceID, memberStaticIP.StaticIP)
		if err != nil {
			glog.Errorln("AssignStaticIP error", err, "ip", memberStaticIP, "local network interface",
				netInterface.InterfaceID, "requuid", requuid, member)
			return err
		}

		glog.Infoln("assigned static ip", memberStaticIP.StaticIP, "to local network",
			netInterface.InterfaceID, "member", member, "requuid", requuid)

		newip := db.UpdateServiceStaticIP(memberStaticIP, s.serverInfo.GetLocalInstanceID(), netInterface.InterfaceID)
		err = s.dbIns.UpdateServiceStaticIP(ctx, memberStaticIP, newip)
		if err != nil {
			glog.Errorln("UpdateServiceStaticIP error", err, "ip", memberStaticIP, "requuid", requuid, member)
			return err
		}

		glog.Infoln("updated static ip to local node", newip, "requuid", requuid, member)
	}

	// wait the DNS is updated to the static ip, this will only happen after service is created and DNS is not updated yet.
	dnsName := dns.GenDNSName(member.MemberName, domainName)
	err = s.waitDNSLookup(ctx, dnsName, memberStaticIP.StaticIP, requuid)
	if err != nil {
		glog.Errorln("waitDNSLookup error", err, "requuid", requuid, member)
		return err
	}

	return nil
}

// AddIP adds the ip to the net interface.
func (s *ServiceNetwork) AddIP(ip string) error {
	return AddIP(ip, s.ifname)
}

// DeleteIP deletes the ip from the net interface.
func (s *ServiceNetwork) DeleteIP(ip string) error {
	return DeleteIP(ip, s.ifname)
}
