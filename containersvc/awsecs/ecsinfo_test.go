package awsecs

import (
	"testing"
)

func TestParseContainerInstanceID(t *testing.T) {
	// good case
	bodys := "{\"Cluster\":\"default\",\"ContainerInstanceArn\":\"arn:aws:ecs:us-west-1:468784140593:container-instance/45c8b03a-8f4a-49cd-aeda-d8d870798721\",\"Version\":\"Amazon ECS Agent - v1.13.0 (*aebcbca)\"}"
	expectContInsID := "arn:aws:ecs:us-west-1:468784140593:container-instance/45c8b03a-8f4a-49cd-aeda-d8d870798721"
	expectClusterID := "default"

	contInsID, err := parseContainerInstanceID(bodys)
	if err != nil || contInsID != expectContInsID {
		t.Fatalf("expectContInsID %s got %s", expectContInsID, contInsID)
	}

	clusterID, err := parseClusterID(bodys)
	if err != nil || clusterID != expectClusterID {
		t.Fatalf("expectClusterID %s got %s", expectClusterID, clusterID)
	}

	// negative cases
	bodys = "{\"Cluster\":\"default\",\"ContainerInstanceA\":\"arn:aws:ecs:us-west-1:468784140593:container-instance/45c8b03a-8f4a-49cd-aeda-d8d870798721\",\"Version\":\"Amazon ECS Agent - v1.13.0 (*aebcbca)\"}"
	contInsID, err = parseContainerInstanceID(bodys)
	if err == nil {
		t.Fatalf("expect error but success, contInsID %s body %s", contInsID, bodys)
	}
	bodys = "{\"Clusr\":\"default\",\"ContainerInstanceA\":\"arn:aws:ecs:us-west-1:468784140593:container-instance/45c8b03a-8f4a-49cd-aeda-d8d870798721\",\"Version\":\"Amazon ECS Agent - v1.13.0 (*aebcbca)\"}"
	clusterID, err = parseClusterID(bodys)
	if err == nil {
		t.Fatalf("expect error but success, clusterID %s body %s", clusterID, bodys)
	}
}
