package k8ssvc

import (
	"fmt"
)

// GetStatefulSetPodNames returns the pod names of the StatefulSet service.
func GetStatefulSetPodNames(service string, replicas int64, backward bool) []string {
	podnames := make([]string, replicas)
	for i := int64(0); i < replicas; i++ {
		idx := i
		if backward {
			idx = replicas - i - 1
		}
		podnames[i] = fmt.Sprintf("%s-%d", service, idx)
	}
	return podnames
}
