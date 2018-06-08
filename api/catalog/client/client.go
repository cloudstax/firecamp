package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
)

// CatalogServiceClient is the client to talk with the catalog management service.
type CatalogServiceClient struct {
	serverURL string
	cli       *http.Client
}

// NewCatalogServiceClient creates a new CatalogManageClient instance.
// could be https://firecamp-catalogserver.cluster-firecamp.com:27041/ to directly talk with the catalog service.
// or https://firecamp-manageserver.cluster-firecamp.com:27040/ to talk with FireCamp management service, which
// will forward the catalog request to the catalog service.
func NewCatalogServiceClient(serverURL string, tlsConf *tls.Config) *CatalogServiceClient {
	cli := &http.Client{}
	if tlsConf != nil {
		tr := &http.Transport{TLSClientConfig: tlsConf}
		cli = &http.Client{Transport: tr}
	}

	c := &CatalogServiceClient{
		serverURL: serverURL,
		cli:       cli,
	}
	return c
}

func (c *CatalogServiceClient) closeRespBody(resp *http.Response) {
	if resp.Body != nil {
		resp.Body.Close()
	}
}

// Catalog Service Requests

// CatalogCreateMongoDBService creates a new catalog MongoDB ReplicaSet service.
func (c *CatalogServiceClient) CatalogCreateMongoDBService(ctx context.Context, r *catalog.CatalogCreateMongoDBRequest) (keyfileContent string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}

	urlStr := c.serverURL + catalog.CatalogCreateMongoDBOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCreateMongoDBResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.KeyFileContent, err
}

// CatalogCreatePostgreSQLService creates a new catalog PostgreSQL service.
func (c *CatalogServiceClient) CatalogCreatePostgreSQLService(ctx context.Context, r *catalog.CatalogCreatePostgreSQLRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreatePostgreSQLOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}

	return c.convertHTTPError(resp)
}

// CatalogCreateCassandraService creates a new catalog Cassandra service.
func (c *CatalogServiceClient) CatalogCreateCassandraService(ctx context.Context, r *catalog.CatalogCreateCassandraRequest) (jmxUser string, jmxPasswd string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", "", err
	}

	urlStr := c.serverURL + catalog.CatalogCreateCassandraOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCreateCassandraResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.JmxRemoteUser, res.JmxRemotePasswd, err
}

// CatalogScaleCassandraService scales the Cassandra service.
func (c *CatalogServiceClient) CatalogScaleCassandraService(ctx context.Context, r *catalog.CatalogScaleCassandraRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogScaleCassandraOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateZooKeeperService creates a new catalog ZooKeeper service.
func (c *CatalogServiceClient) CatalogCreateZooKeeperService(ctx context.Context, r *catalog.CatalogCreateZooKeeperRequest) (jmxUser string, jmxPasswd string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", "", err
	}

	urlStr := c.serverURL + catalog.CatalogCreateZooKeeperOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCreateZooKeeperResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.JmxRemoteUser, res.JmxRemotePasswd, err
}

// CatalogCreateKafkaService creates a new catalog Kafka service.
func (c *CatalogServiceClient) CatalogCreateKafkaService(ctx context.Context, r *catalog.CatalogCreateKafkaRequest) (jmxUser string, jmxPasswd string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", "", err
	}

	urlStr := c.serverURL + catalog.CatalogCreateKafkaOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCreateKafkaResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.JmxRemoteUser, res.JmxRemotePasswd, err
}

// CatalogCreateKafkaSinkESService creates a new catalog Kafka sink elasticsearch service.
func (c *CatalogServiceClient) CatalogCreateKafkaSinkESService(ctx context.Context, r *catalog.CatalogCreateKafkaSinkESRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateKafkaSinkESOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateKafkaManagerService creates a new catalog Kafka Manager service.
func (c *CatalogServiceClient) CatalogCreateKafkaManagerService(ctx context.Context, r *catalog.CatalogCreateKafkaManagerRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateKafkaManagerOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateRedisService creates a new catalog Redis service.
func (c *CatalogServiceClient) CatalogCreateRedisService(ctx context.Context, r *catalog.CatalogCreateRedisRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateRedisOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateCouchDBService creates a new catalog CouchDB service.
func (c *CatalogServiceClient) CatalogCreateCouchDBService(ctx context.Context, r *catalog.CatalogCreateCouchDBRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateCouchDBOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateConsulService creates a new catalog Consul service.
// return the consul server ips.
func (c *CatalogServiceClient) CatalogCreateConsulService(ctx context.Context, r *catalog.CatalogCreateConsulRequest) (serverIPs []string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	urlStr := c.serverURL + catalog.CatalogCreateConsulOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCreateConsulResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.ConsulServerIPs, err
}

// CatalogCreateElasticSearchService creates a new catalog ElasticSearch service.
func (c *CatalogServiceClient) CatalogCreateElasticSearchService(ctx context.Context, r *catalog.CatalogCreateElasticSearchRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateElasticSearchOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateKibanaService creates a new catalog Kibana service.
func (c *CatalogServiceClient) CatalogCreateKibanaService(ctx context.Context, r *catalog.CatalogCreateKibanaRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateKibanaOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateLogstashService creates a new catalog Logstash service.
func (c *CatalogServiceClient) CatalogCreateLogstashService(ctx context.Context, r *catalog.CatalogCreateLogstashRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateLogstashOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCreateTelegrafService creates a new catalog Telegraf service.
func (c *CatalogServiceClient) CatalogCreateTelegrafService(ctx context.Context, r *catalog.CatalogCreateTelegrafRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	urlStr := c.serverURL + catalog.CatalogCreateTelegrafOp
	req, err := http.NewRequest(http.MethodPut, urlStr, bytes.NewReader(b))
	if err != nil {
		return err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return err
	}
	return c.convertHTTPError(resp)
}

// CatalogCheckServiceInit checks if a catalog service is initialized.
func (c *CatalogServiceClient) CatalogCheckServiceInit(ctx context.Context, r *catalog.CatalogCheckServiceInitRequest) (initialized bool, statusMsg string, err error) {
	b, err := json.Marshal(r)
	if err != nil {
		return false, "", err
	}

	urlStr := c.serverURL + catalog.CatalogCheckServiceInitOp
	req, err := http.NewRequest(http.MethodGet, urlStr, bytes.NewReader(b))
	if err != nil {
		return false, "", err
	}

	resp, err := c.cli.Do(req)
	if err != nil {
		return false, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", c.convertHTTPError(resp)
	}

	defer c.closeRespBody(resp)

	res := &catalog.CatalogCheckServiceInitResponse{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	return res.Initialized, res.StatusMessage, err
}

func (c *CatalogServiceClient) convertHTTPError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return errors.New(string(body))
}
