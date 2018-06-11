package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/manage/error"
)

// ManageClient is the client to talk with the management service.
// ManageClient API calls return the custom error with http status code and error message.
// This is for the catalog service to pass back to the cli.
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
	urlStr := c.serverURL + manage.CreateServiceOp
	return c.createService(ctx, r, urlStr)
}

// CreateManageService creates the service at the control plane.
func (c *ManageClient) CreateManageService(ctx context.Context, r *manage.CreateServiceRequest) error {
	urlStr := c.serverURL + manage.CreateManageServiceOp
	return c.createService(ctx, r, urlStr)
}

// CreateContainerService creates the service at the corresponding container platform.
func (c *ManageClient) CreateContainerService(ctx context.Context, r *manage.CreateServiceRequest) error {
	urlStr := c.serverURL + manage.CreateContainerServiceOp
	return c.createService(ctx, r, urlStr)
}

func (c *ManageClient) createService(ctx context.Context, r *manage.CreateServiceRequest, url string) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}

	return c.convertHTTPError(resp)
}

// ScaleService scales the service.
// TODO only support scale out now.
func (c *ManageClient) ScaleService(ctx context.Context, r *manage.ScaleServiceRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.ScaleServiceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// UpdateServiceConfig updates the service config
func (c *ManageClient) UpdateServiceConfig(ctx context.Context, r *manage.UpdateServiceConfigRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.UpdateServiceConfigOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// UpdateServiceResource updates the service resource
func (c *ManageClient) UpdateServiceResource(ctx context.Context, r *manage.UpdateServiceResourceRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.UpdateServiceResourceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// UpdateMemberConfig updates the service member config
func (c *ManageClient) UpdateMemberConfig(ctx context.Context, r *manage.UpdateMemberConfigRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.UpdateMemberConfigOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// StopService stops the service containers
func (c *ManageClient) StopService(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.StopServiceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// StartService starts the service containers
func (c *ManageClient) StartService(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.StartServiceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// UpgradeService upgrades the service to the current release
func (c *ManageClient) UpgradeService(ctx context.Context, r *manage.UpgradeServiceRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.UpgradeServiceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// GetServiceAttr gets the service details information
func (c *ManageClient) GetServiceAttr(ctx context.Context, r *manage.ServiceCommonRequest) (*common.ServiceAttr, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + r.ServiceName
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetServiceAttributesResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.Service, nil
}

// GetServiceStatus gets the service running status.
func (c *ManageClient) GetServiceStatus(ctx context.Context, r *manage.ServiceCommonRequest) (*common.ServiceStatus, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.GetServiceStatusOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &common.ServiceStatus{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res, nil
}

// IsServiceInitialized checks whether the service is initialized
func (c *ManageClient) IsServiceInitialized(ctx context.Context, r *manage.ServiceCommonRequest) (bool, error) {
	attr, err := c.GetServiceAttr(ctx, r)
	if err != nil {
		return false, err
	}

	if attr.Meta.ServiceStatus == common.ServiceStatusActive {
		return true, nil
	}

	return false, nil
}

// SetServiceInitialized updates the service status to active
func (c *ManageClient) SetServiceInitialized(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.ServiceInitializedOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return c.convertHTTPError(resp)
	}
	return nil
}

// ListServiceMember lists all serviceMembers of the service.
func (c *ManageClient) ListServiceMember(ctx context.Context, r *manage.ListServiceMemberRequest) ([]*common.ServiceMember, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.ListServiceMemberOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.ListServiceMemberResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.ServiceMembers, nil
}

// ListService lists all services that match the required conditions
func (c *ManageClient) ListService(ctx context.Context, r *manage.ListServiceRequest) ([]*common.ServiceAttr, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.ListServiceOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.ListServiceResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.Services, nil
}

// DeleteService deletes one service and returns the service's volume IDs
func (c *ManageClient) DeleteService(ctx context.Context, r *manage.DeleteServiceRequest) (volIDs []string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return volIDs, err
	}

	urlStr := c.serverURL + manage.DeleteServiceOp
	req, err := http.NewRequest(http.MethodDelete, urlStr, bytes.NewReader(b))
	if err != nil {
		return volIDs, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return volIDs, err
	}
	if resp.StatusCode != http.StatusOK {
		return volIDs, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.DeleteServiceResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return volIDs, c.convertError(err)
	}
	return res.VolumeIDs, nil
}

// GetServiceConfigFile gets the service config file.
func (c *ManageClient) GetServiceConfigFile(ctx context.Context, r *manage.GetServiceConfigFileRequest) (*common.ConfigFile, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.GetServiceConfigFileOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetConfigFileResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.ConfigFile, nil
}

// GetMemberConfigFile gets the config file of one service member.
func (c *ManageClient) GetMemberConfigFile(ctx context.Context, r *manage.GetMemberConfigFileRequest) (*common.ConfigFile, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.GetMemberConfigFileOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetConfigFileResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.ConfigFile, nil
}

// RunTask runs a task
func (c *ManageClient) RunTask(ctx context.Context, r *manage.RunTaskRequest) (taskID string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", c.convertError(err)
	}

	urlStr := c.serverURL + manage.RunTaskOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.RunTaskResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return "", c.convertError(err)
	}
	return res.TaskID, nil
}

// GetTaskStatus gets a task's status.
func (c *ManageClient) GetTaskStatus(ctx context.Context, r *manage.GetTaskStatusRequest) (*common.TaskStatus, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.GetTaskStatusOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetTaskStatusResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.Status, nil
}

// DeleteTask deletes the service task.
func (c *ManageClient) DeleteTask(ctx context.Context, r *manage.DeleteTaskRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.DeleteTaskOp
	req, err := http.NewRequest(http.MethodDelete, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}

	return c.convertHTTPError(resp)
}

// Service Management Requests

// RollingRestartService rolling restarts the service containers
func (c *ManageClient) RollingRestartService(ctx context.Context, r *manage.ServiceCommonRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return c.convertError(err)
	}

	urlStr := c.serverURL + manage.RollingRestartServiceOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return c.convertError(err)
	}
	return c.convertHTTPError(resp)
}

// GetServiceTaskStatus gets the service management task status
func (c *ManageClient) GetServiceTaskStatus(ctx context.Context, r *manage.ServiceCommonRequest) (complete bool, statusMsg string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return false, "", c.convertError(err)
	}

	urlStr := c.serverURL + manage.GetServiceTaskStatusOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return false, "", c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return false, "", c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.GetServiceTaskStatusResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return false, "", c.convertError(err)
	}
	return res.Complete, res.StatusMessage, nil
}

// InternalGetServiceTask gets the service task ID.
func (c *ManageClient) InternalGetServiceTask(ctx context.Context, r *manage.InternalGetServiceTaskRequest) (taskID string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", c.convertError(err)
	}

	urlStr := c.serverURL + manage.InternalGetServiceTaskOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.InternalGetServiceTaskResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return "", c.convertError(err)
	}
	return res.ServiceTaskID, nil
}

// InternalListActiveServiceTasks lists the service active tasks.
func (c *ManageClient) InternalListActiveServiceTasks(ctx context.Context, r *manage.InternalListActiveServiceTasksRequest) (taskIDs map[string]bool, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, c.convertError(err)
	}

	urlStr := c.serverURL + manage.InternalListActiveServiceTasksOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, c.convertError(err)
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, c.convertError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &manage.InternalListActiveServiceTasksResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return nil, c.convertError(err)
	}
	return res.ServiceTaskIDs, nil
}

func (c *ManageClient) convertError(err error) error {
	return clienterr.New(http.StatusInternalServerError, err.Error())
}

func (c *ManageClient) convertHTTPError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return c.convertError(err)
	}
	return clienterr.New(resp.StatusCode, string(body))
}
