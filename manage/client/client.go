package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
)

// ManageClient is the client to talk with the management service.
type ManageClient struct {
	serverURL string
	cli       *http.Client
}

// NewManageClient creates a new ManageClient instance.
// Example serverURL: https://firecamp-manageserver.cluster-firecamp.com:27040/
func NewManageClient(serverURL string, tlsConf *tls.Config) *ManageClient {
	cli := &http.Client{}
	if tlsConf != nil {
		tr := &http.Transport{TLSClientConfig: tlsConf}
		cli = &http.Client{Transport: tr}
	}

	c := &ManageClient{
		serverURL: serverURL,
		cli:       cli,
	}
	return c
}

func (c *ManageClient) closeRespBody(resp *http.Response) {
	if resp.Body != nil {
		resp.Body.Close()
	}
}

// CreateService creates a new service
func (c *ManageClient) CreateService(ctx context.Context, r *manage.CreateServiceRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + r.Service.ServiceName
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// GetServiceAttr gets the service details information
func (c *ManageClient) GetServiceAttr(ctx context.Context, r *manage.ServiceCommonRequest) (*common.ServiceAttr, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + r.ServiceName
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetServiceAttributesResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.Service, err
}

// GetServiceStatus gets the service running status.
func (c *ManageClient) GetServiceStatus(ctx context.Context, r *manage.ServiceCommonRequest) (*common.ServiceStatus, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.GetServiceStatusOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &common.ServiceStatus{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res, err
}

// IsServiceInitialized checks whether the service is initialized
func (c *ManageClient) IsServiceInitialized(ctx context.Context, r *manage.ServiceCommonRequest) (bool, error) {
	attr, err := c.GetServiceAttr(ctx, r)
	if err != nil {
		return false, err
	}

	if attr.ServiceStatus == common.ServiceStatusActive {
		return true, nil
	}

	return false, nil
}

// SetServiceInitialized updates the service status to active
func (c *ManageClient) SetServiceInitialized(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.ServiceInitializedOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return manage.ConvertHTTPError(resp.StatusCode)
	}
	return nil
}

// ListServiceMember lists all serviceMembers of the service.
func (c *ManageClient) ListServiceMember(ctx context.Context, r *manage.ListServiceMemberRequest) ([]*common.ServiceMember, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.ListServiceMemberOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.ListServiceMemberResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.ServiceMembers, err
}

// ListService lists all services that match the required conditions
func (c *ManageClient) ListService(ctx context.Context, r *manage.ListServiceRequest) ([]*common.ServiceAttr, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.ListServiceOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.ListServiceResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.Services, err
}

// DeleteService deletes one service
func (c *ManageClient) DeleteService(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + r.ServiceName
	req, err := http.NewRequest(http.MethodDelete, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return manage.ConvertHTTPError(resp.StatusCode)
	}

	return nil
}

// GetConfigFile gets the config file.
func (c *ManageClient) GetConfigFile(ctx context.Context, r *manage.GetConfigFileRequest) (*common.ConfigFile, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.GetConfigFileOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetConfigFileResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.ConfigFile, err
}

// RunTask runs a task
func (c *ManageClient) RunTask(ctx context.Context, r *manage.RunTaskRequest) (taskID string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}

	urlStr := c.serverURL + manage.RunTaskOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.RunTaskResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.TaskID, err
}

// GetTaskStatus gets a task's status.
func (c *ManageClient) GetTaskStatus(ctx context.Context, r *manage.GetTaskStatusRequest) (*common.TaskStatus, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.GetTaskStatusOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetTaskStatusResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.Status, err
}

// DeleteTask deletes the service task.
func (c *ManageClient) DeleteTask(ctx context.Context, r *manage.DeleteTaskRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.DeleteTaskOp
	req, err := http.NewRequest(http.MethodDelete, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}

	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateMongoDBService creates a new catalog MongoDB ReplicaSet service.
func (c *ManageClient) CatalogCreateMongoDBService(ctx context.Context, r *manage.CatalogCreateMongoDBRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateMongoDBOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreatePostgreSQLService creates a new catalog PostgreSQL service.
func (c *ManageClient) CatalogCreatePostgreSQLService(ctx context.Context, r *manage.CatalogCreatePostgreSQLRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreatePostgreSQLOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateCassandraService creates a new catalog Cassandra service.
func (c *ManageClient) CatalogCreateCassandraService(ctx context.Context, r *manage.CatalogCreateCassandraRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateCassandraOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateZooKeeperService creates a new catalog ZooKeeper service.
func (c *ManageClient) CatalogCreateZooKeeperService(ctx context.Context, r *manage.CatalogCreateZooKeeperRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateZooKeeperOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateKafkaService creates a new catalog Kafka service.
func (c *ManageClient) CatalogCreateKafkaService(ctx context.Context, r *manage.CatalogCreateKafkaRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateKafkaOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateRedisService creates a new catalog Redis service.
func (c *ManageClient) CatalogCreateRedisService(ctx context.Context, r *manage.CatalogCreateRedisRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateRedisOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCreateCouchDBService creates a new catalog CouchDB service.
func (c *ManageClient) CatalogCreateCouchDBService(ctx context.Context, r *manage.CatalogCreateCouchDBRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + manage.CatalogCreateCouchDBOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return manage.ConvertHTTPError(resp.StatusCode)
}

// CatalogCheckServiceInit checks if a catalog service is initialized.
func (c *ManageClient) CatalogCheckServiceInit(ctx context.Context, r *manage.CatalogCheckServiceInitRequest) (initialized bool, statusMsg string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return false, "", err
	}

	urlStr := c.serverURL + manage.CatalogCheckServiceInitOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return false, "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return false, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.CatalogCheckServiceInitResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.Initialized, res.StatusMessage, err
}

// InternalGetServiceTask gets the service task ID.
func (c *ManageClient) InternalGetServiceTask(ctx context.Context, r *manage.InternalGetServiceTaskRequest) (taskID string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}

	urlStr := c.serverURL + manage.InternalGetServiceTaskOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.InternalGetServiceTaskResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.ServiceTaskID, err
}

// InternalListActiveServiceTasks lists the service active tasks.
func (c *ManageClient) InternalListActiveServiceTasks(ctx context.Context, r *manage.InternalListActiveServiceTasksRequest) (taskIDs map[string]bool, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + manage.InternalListActiveServiceTasksOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, manage.ConvertHTTPError(resp.StatusCode)
	}

	defer c.closeRespBody(resp)

	res := &manage.InternalListActiveServiceTasksResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.ServiceTaskIDs, err
}
