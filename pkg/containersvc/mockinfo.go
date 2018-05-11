package containersvc

type MockContainerSvcInfo struct {
}

func NewMockContainerSvcInfo() *MockContainerSvcInfo {
	return &MockContainerSvcInfo{}
}

func (m *MockContainerSvcInfo) GetLocalContainerInstanceID() string {
	return "local-containerInstanceID"
}

func (m *MockContainerSvcInfo) GetContainerClusterID() string {
	return "local-clusterID"
}
