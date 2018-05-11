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
	// key: dnsname, value: hostIP
	records map[string]string
}

type MockDNS struct {
	// key: domainName
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
		records: map[string]string{},
	}

	m.hostedZoneMap[domainName] = zone
	return zone.ID, nil
}

func (m *MockDNS) UpdateDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	for _, zone := range m.hostedZoneMap {
		if zone.ID == hostedZoneID {
			zone.records[dnsName] = hostIP
			return nil
		}
	}

	return ErrHostedZoneNotFound
}

func (m *MockDNS) WaitDNSRecordUpdated(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) (dnsIP string, err error) {
	return hostIP, nil
}

func (m *MockDNS) DeleteDNSRecord(ctx context.Context, dnsName string, hostIP string, hostedZoneID string) error {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	for _, zone := range m.hostedZoneMap {
		if zone.ID == hostedZoneID {
			host, ok := zone.records[dnsName]
			if !ok {
				return ErrDNSRecordNotFound
			}
			if host != hostIP {
				return ErrInvalidRequest
			}
			delete(zone.records, dnsName)
			return nil
		}
	}

	return ErrHostedZoneNotFound
}

func (m *MockDNS) GetDNSRecord(ctx context.Context, dnsName string, hostedZoneID string) (hostIP string, err error) {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	for _, zone := range m.hostedZoneMap {
		if zone.ID == hostedZoneID {
			host, ok := zone.records[dnsName]
			if !ok {
				return "", ErrDNSRecordNotFound
			}
			return host, nil
		}
	}

	return "", ErrHostedZoneNotFound
}

func (m *MockDNS) LookupLocalDNS(ctx context.Context, dnsName string) (dnsIP string, err error) {
	for _, zone := range m.hostedZoneMap {
		host, ok := zone.records[dnsName]
		if ok {
			return host, nil
		}
	}

	return "", ErrDNSRecordNotFound
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
