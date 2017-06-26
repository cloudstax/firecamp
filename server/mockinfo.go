package server

type MockServerInfo struct {
}

func NewMockServerInfo() *MockServerInfo {
	return &MockServerInfo{}
}

func (m *MockServerInfo) GetPrivateIP() string {
	return "127.0.0.1"
}

func (m *MockServerInfo) GetLocalAvailabilityZone() string {
	return "local-az"
}

func (m *MockServerInfo) GetLocalRegion() string {
	return "local-region"
}

func (m *MockServerInfo) GetLocalInstanceID() string {
	return "local-instance"
}

func (m *MockServerInfo) GetLocalVpcID() string {
	return "local-vpc"
}

func (m *MockServerInfo) GetLocalRegionAZs() []string {
	return []string{"local-az1", "local-az2", "local-az3"}
}
