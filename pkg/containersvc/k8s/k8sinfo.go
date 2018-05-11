package k8ssvc

type K8sInfo struct {
	localContainerInsID string
	clusterID           string
}

func NewK8sInfo(clustername string, fullhostname string) *K8sInfo {
	// k8s uses the node's full hostname as the node's name, such as ip-172-31-17-23.us-west-2.compute.internal
	// see the result of "kubectl get nodes".
	return &K8sInfo{
		localContainerInsID: fullhostname,
		clusterID:           clustername,
	}
}

func (s *K8sInfo) GetLocalContainerInstanceID() string {
	return s.localContainerInsID
}

func (s *K8sInfo) GetContainerClusterID() string {
	return s.clusterID
}
