package awsecs

import (
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/server/awsec2"
)

const ecsMetadataURLSuffix = ":51678/v1/metadata"

type EcsInfo struct {
	localContainerInsID string
	clusterID           string
}

func NewEcsInfo() (*EcsInfo, error) {
	md, err := getEcsMetadata()
	if err != nil {
		return nil, err
	}
	localContainerInsID, err := parseContainerInstanceID(md)
	if err != nil {
		return nil, err
	}
	clusterID, err := parseClusterID(md)
	if err != nil {
		return nil, err
	}

	s := &EcsInfo{
		localContainerInsID: localContainerInsID,
		clusterID:           clusterID,
	}
	return s, nil
}

func (s *EcsInfo) GetLocalContainerInstanceID() string {
	return s.localContainerInsID
}

func (s *EcsInfo) GetContainerClusterID() string {
	return s.clusterID
}

func getEcsMetadata() (string, error) {
	hostname, err := awsec2.GetLocalEc2Hostname()
	if err != nil {
		glog.Errorln("awsec2 GetLocalEc2Hostname error", err)
		return "", err
	}

	url := "http://" + hostname + ecsMetadataURLSuffix
	resp, err := http.Get(url)
	if err != nil {
		glog.Errorln("get url", url, "error", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorln("read body error", err, "url", url)
		return "", err
	}

	// example body: {"Cluster":"default","ContainerInstanceArn":"arn:aws:ecs:us-west-1:468784140593:container-instance/45c8b03a-8f4a-49cd-aeda-d8d870798721","Version":"Amazon ECS Agent - v1.13.0 (*aebcbca)"}
	return string(body), err
}

func parseClusterID(md string) (string, error) {
	start := strings.Index(md, "Cluster")
	if start == -1 {
		glog.Errorln("Bad ECS Metadata", md)
		return "", containersvc.ErrInvalidCluster
	}

	md1 := md[start:]
	start = strings.Index(md1, "\":\"")
	end := strings.Index(md1, "\",\"")
	if start == -1 || end == -1 {
		glog.Errorln("Bad ECS Metadata", md)
		return "", containersvc.ErrInvalidCluster
	}

	cluster := md1[start+3 : end]
	glog.Infoln("parseCluster", cluster, "md", md)
	return cluster, nil
}

func parseContainerInstanceID(md string) (string, error) {
	start := strings.Index(md, "ContainerInstanceArn")
	if start == -1 {
		glog.Errorln("Bad ECS Metadata", md)
		return "", containersvc.ErrInvalidContainerInstanceID
	}

	md1 := md[start:]
	start = strings.Index(md1, "\":\"")
	end := strings.Index(md1, "\",\"")
	if start == -1 || end == -1 {
		glog.Errorln("Bad ECS Metadata format", md)
		return "", containersvc.ErrInvalidContainerInstanceID
	}

	contInsID := md1[start+3 : end]
	glog.Infoln("parseContainerInstanceID", contInsID, "md", md)
	return contInsID, nil
}
