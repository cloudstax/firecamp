package dns

import (
	"strconv"
	"sync"
	"time"
)

type vpc struct {
	vpcID     string
	vpcRegion string
}

type hostedZone struct {
	ID      string
	zoneVpc vpc
	private bool
}

type MockDNS struct {
	hostedZoneMap map[string]hostedZone
	mlock         *sync.Mutex
}

func NewMockDNS() *MockDNS {
	return &MockDNS{
		hostedZoneMap: map[string]hostedZone{},
		mlock:         &sync.Mutex{},
	}
}

func (m *MockDNS) GetOrCreateHostedZoneIDByName(domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	zone, ok := m.hostedZoneMap[domainName]
	if ok && zone.zoneVpc.vpcID == vpcID && zone.zoneVpc.vpcRegion == vpcRegion {
		// hostedZone exists
		return zone.ID, nil
	}

	// hostedZone not exists, create it
	v := vpc{
		vpcID:     vpcID,
		vpcRegion: vpcRegion,
	}
	zone = hostedZone{
		ID:      strconv.FormatInt(time.Now().UnixNano(), 10),
		zoneVpc: v,
		private: private,
	}

	m.hostedZoneMap[domainName] = zone
	return zone.ID, nil
}

func (m *MockDNS) UpdateServiceDNSRecord(dnsName string, hostname string, hostedZoneID string) error {
	return nil
}

func (m *MockDNS) WaitDNSRecordUpdated(dnsName string, hostname string, hostedZoneID string) (dnsHostname string, err error) {
	return hostname, nil
}

func (m *MockDNS) GetHostedZoneIDByName(domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	zone, ok := m.hostedZoneMap[domainName]
	if ok && zone.zoneVpc.vpcID == vpcID && zone.zoneVpc.vpcRegion == vpcRegion && zone.private == private {
		// hostedZone exists
		return zone.ID, nil
	}
	return "", ErrDomainNotFound
}
