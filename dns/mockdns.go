package dns

import (
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/context"
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

func (m *MockDNS) GetOrCreateHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
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

func (m *MockDNS) UpdateServiceDNSRecord(ctx context.Context, dnsName string, hostname string, hostedZoneID string) error {
	return nil
}

func (m *MockDNS) WaitDNSRecordUpdated(ctx context.Context, dnsName string, hostname string, hostedZoneID string) (dnsHostname string, err error) {
	return hostname, nil
}

func (m *MockDNS) GetHostedZoneIDByName(ctx context.Context, domainName string, vpcID string, vpcRegion string, private bool) (hostedZoneID string, err error) {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	zone, ok := m.hostedZoneMap[domainName]
	if ok && zone.zoneVpc.vpcID == vpcID && zone.zoneVpc.vpcRegion == vpcRegion && zone.private == private {
		// hostedZone exists
		return zone.ID, nil
	}
	return "", ErrDomainNotFound
}

func (m *MockDNS) DeleteHostedZone(ctx context.Context, hostedZoneID string) error {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	key := ""
	for k, v := range m.hostedZoneMap {
		if v.ID == hostedZoneID {
			key = k
			break
		}
	}

	if key == "" {
		return ErrHostedZoneNotFound
	}

	delete(m.hostedZoneMap, key)
	return nil
}
