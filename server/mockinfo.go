package server

type MockServerInfo struct {
}

func NewMockServerInfo() *MockServerInfo {
	return &MockServerInfo{}
}

func (m *MockServerInfo) GetLocalHostname() string {
	return "localhost"
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
