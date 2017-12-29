package couchdbcatalog

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/pbkdf2"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "2.1"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "couchdb:" + defaultVersion
	// InitContainerImage initializes the couchdb cluster.
	InitContainerImage = common.ContainerNamePrefix + "couchdb-init:" + defaultVersion

	// the application access port
	httpPort = 5984
	sslPort  = 6984

	// cluster internal ports
	localPort                       = 5986
	erlangClusterPort               = 4369
	erlangClusterListenPortMin      = 9100
	erlangClusterListenPortMax      = 9200
	erlangClusterListenPortMaxOnECS = 9190

	defaultShards      = 8
	defaultReadCopies  = 2
	defaultWriteCopies = 2

	passwdSha1OutputLen = 20
	passwdIter          = 10

	vmArgsConfFileName     = "vm.args"
	localIniConfFileName   = "local.ini"
	privateKeyConfFileName = "privkey.pem"
	certConfFileName       = "couchdb.pem"
	caCertConfFileName     = "ca-certificates.crt"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateCouchDBRequest) error {
	if r.Options.Replicas == 2 {
		return errors.New("invalid replicas, please create 1, 3 or more replicas")
	}
	if r.Options.EnableCors {
		if r.Options.Credentials && r.Options.Origins == "*" {
			return errors.New("can't set origins: * and credentials = true at the same time")
		}
		if len(r.Options.Origins) == 0 || len(r.Options.Headers) == 0 || len(r.Options.Methods) == 0 {
			return errors.New("cors is enabled, please set the origins, headers and methods")
		}
	}
	if r.Options.EnableSSL {
		if len(r.Options.CertFileContent) == 0 || len(r.Options.KeyFileContent) == 0 {
			return errors.New("ssl is enabled, please set the valid cert and key files")
		}
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogCouchDBOptions, existingEncryptPasswd string) (*manage.CreateServiceRequest, error) {
	// generate service ReplicaConfigs
	encryptedPasswd := existingEncryptPasswd
	if len(existingEncryptPasswd) == 0 {
		encryptedPasswd = encryptPasswd(opts.AdminPasswd)
	}
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, opts, encryptedPasswd)

	portMappings := []common.PortMapping{
		{ContainerPort: httpPort, HostPort: httpPort},
		{ContainerPort: sslPort, HostPort: sslPort},
	}
	if opts.Replicas != int64(1) {
		m := common.PortMapping{ContainerPort: localPort, HostPort: localPort}
		portMappings = append(portMappings, m)
		m = common.PortMapping{ContainerPort: erlangClusterPort, HostPort: erlangClusterPort}
		portMappings = append(portMappings, m)

		max := erlangClusterListenPortMax
		if platform == common.ContainerPlatformECS {
			max = erlangClusterListenPortMaxOnECS
		}
		for i := int64(erlangClusterListenPortMin); i <= int64(max); i++ {
			m = common.PortMapping{ContainerPort: i, HostPort: i}
			portMappings = append(portMappings, m)
		}
	}

	userAttr := &common.CouchDBUserAttr{
		Admin:           opts.Admin,
		EncryptedPasswd: encryptedPasswd,
		EnableCors:      opts.EnableCors,
		Credentials:     opts.Credentials,
		Origins:         opts.Origins,
		Headers:         opts.Headers,
		Methods:         opts.Methods,
		EnableSSL:       opts.EnableSSL,
	}
	b, err := json.Marshal(userAttr)
	if err != nil {
		glog.Errorln("Marshal UserAttr error", err, opts)
		return nil, err
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		Volume:         opts.Volume,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,

		UserAttr: &common.ServiceUserAttr{
			ServiceType: common.CatalogService_CouchDB,
			AttrBytes:   b,
		},
	}
	return req, nil
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogCouchDBOptions, encryptedPasswd string) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	uuid := utils.GenUUID()

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		// create the sys.conf file
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create vm.args file
		vmArgsContent := ""
		// AWS ECS allows up to 100 ports at a time for one container. If you map more than 100 ports,
		// ECS will not be able to place the task.
		// http://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definition_parameters.html
		if platform == common.ContainerPlatformECS {
			vmArgsContent = fmt.Sprintf(vmArgsConfigs, memberHost, erlangClusterListenPortMaxOnECS)
		} else {
			vmArgsContent = fmt.Sprintf(vmArgsConfigs, memberHost, erlangClusterListenPortMax)
		}
		vmArgsCfg := &manage.ReplicaConfigFile{
			FileName: vmArgsConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  vmArgsContent,
		}

		// The CouchDB init task follows the same way to assign the az to each node.
		// If the az selection algo is changed here, please also update the init task.
		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		// create the local.ini file
		enableCors := "false"
		if opts.EnableCors {
			enableCors = "true"
		}
		couchContent := fmt.Sprintf(couchConfigs, uuid+common.NameSeparator+member, enableCors, opts.Admin, encryptedPasswd)

		// set cluster configs
		if opts.Replicas >= int64(3) {
			placement := ""
			if len(azs) == 1 {
				placement = fmt.Sprintf(zonePlacementFmt, azs[0], 3)
			} else if len(azs) == 2 {
				placement = fmt.Sprintf(zonePlacementFmt, azs[0], 2) + zonePlaceSep + fmt.Sprintf(zonePlacementFmt, azs[1], 1)
			} else {
				// len(azs) >= 3
				placement = fmt.Sprintf(zonePlacementFmt, azs[0], 1) + zonePlaceSep + fmt.Sprintf(zonePlacementFmt, azs[1], 1) +
					zonePlaceSep + fmt.Sprintf(zonePlacementFmt, azs[2], 1)
			}

			clusterContent := fmt.Sprintf(clusterConfigs, opts.Replicas, defaultReadCopies, defaultWriteCopies, placement)
			couchContent += clusterContent
		}

		// set cors configs
		if opts.EnableCors {
			cred := "false"
			if opts.Credentials {
				cred = "true"
			}
			corsContent := fmt.Sprintf(corsConfigs, cred, opts.Origins, opts.Headers, opts.Methods)
			couchContent += corsContent
		}

		// set ssl configs
		if opts.EnableSSL {
			cafileContent := ""
			if len(opts.CACertFileContent) != 0 {
				cafileContent = cacertConfig
			}
			sslContent := fmt.Sprintf(sslConfigs, cafileContent)
			couchContent += sslContent
		}

		couchCfg := &manage.ReplicaConfigFile{
			FileName: localIniConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  couchContent,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, vmArgsCfg, couchCfg}

		// add ssl config files
		if opts.EnableSSL {
			certCfg := &manage.ReplicaConfigFile{
				FileName: certConfFileName,
				FileMode: common.DefaultConfigFileMode,
				Content:  opts.CertFileContent,
			}
			keyCfg := &manage.ReplicaConfigFile{
				FileName: privateKeyConfFileName,
				FileMode: common.DefaultConfigFileMode,
				Content:  opts.KeyFileContent,
			}
			configs = append(configs, certCfg, keyCfg)

			if len(opts.CACertFileContent) != 0 {
				caCfg := &manage.ReplicaConfigFile{
					FileName: caCertConfFileName,
					FileMode: common.DefaultConfigFileMode,
					Content:  opts.CACertFileContent,
				}
				configs = append(configs, caCfg)
			}
		}

		replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}

	return replicaCfgs
}

// couchdb encrypts password using pbkdf2, and store in local.ini with format -pbkdf2-EncryptedPasswd(160bits),entryptionSalt(128bits),Iteration(10)
// example: -pbkdf2-bd261f6d2d30500145a19c502220b02e250a77db,c995aadb9f35d8fc1e0fe9571a07e36b,10
// http://docs.couchdb.org/en/2.1.0/config/auth.html#admins
func encryptPasswd(passwd string) string {
	salt := utils.GenUUID()
	kd := pbkdf2.Key([]byte(passwd), []byte(salt), passwdIter, passwdSha1OutputLen, sha1.New)
	return fmt.Sprintf(pbkdf2Fmt, hex.EncodeToString(kd), salt, passwdIter)
}

// GenDefaultInitTaskRequest returns the default service init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig, azs []string,
	serviceUUID string, replicas int64, manageurl string, admin string, adminPass string) *containersvc.RunTaskOptions {

	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, manageurl, req.ServiceName, azs, replicas, admin, adminPass)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: InitContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		LogConfig: logConfig,
	}

	taskOpts := &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}

	return taskOpts
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
func GenInitTaskEnvKVPairs(region string, cluster string, manageurl string, service string,
	azs []string, replicas int64, admin string, adminPass string) []*common.EnvKeyValuePair {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_CouchDB}
	kvport := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_PORT, Value: strconv.Itoa(httpPort)}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetServiceInitOp}

	zones := ""
	for i, zone := range azs {
		if i == 0 {
			zones = zone
		} else {
			zones = zones + common.ENV_VALUE_SEPARATOR + zone
		}
	}
	kvazs := &common.EnvKeyValuePair{Name: common.ENV_AVAILABILITY_ZONES, Value: zones}

	kvadmin := &common.EnvKeyValuePair{Name: common.ENV_ADMIN, Value: admin}
	// TODO simply pass the password as env variable. The init task should fetch from the manage server.
	kvadminpass := &common.EnvKeyValuePair{Name: common.ENV_ADMIN_PASSWORD, Value: adminPass}

	domain := dns.GenDefaultDomainName(cluster)
	members := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	for i := int64(1); i < replicas; i++ {
		members = members + common.ENV_VALUE_SEPARATOR + dns.GenDNSName(utils.GenServiceMemberName(service, i), domain)
	}
	kvmembers := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MEMBERS, Value: members}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvservice, kvsvctype, kvport, kvop, kvazs, kvadmin, kvadminpass, kvmembers}
	return envkvs
}

const (
	pbkdf2Fmt        = "-pbkdf2-%s,%s,%d"
	zonePlacementFmt = "%s:%d"
	zonePlaceSep     = ","

	vmArgsConfigs = `
-name couchdb@%s

# All nodes must share the same magic cookie for distributed Erlang to work.
# Comment out this line if you synchronized the cookies by other means (using
# the ~/.erlang.cookie file, for example).
-setcookie monster

# Tell kernel and SASL not to log anything
-kernel error_logger silent
-sasl sasl_error_logger false

# Erlang kernel listen port
-kernel inet_dist_listen_min 9100
-kernel inet_dist_listen_max %d

# Use kernel poll functionality if supported by emulator
+K true

# Start a pool of asynchronous IO threads
+A 16

# Comment this line out to enable the interactive Erlang shell on startup
+Bd -noinput
`

	couchConfigs = `
[couchdb]
uuid = %s
database_dir = /data/couchdb
view_index_dir = /data/couchdb

[couch_peruser]
; peruser db is broken in 2.1.0, https://github.com/apache/couchdb/issues/749
;enable = true
;delete_dbs = true

[chttpd]
port = 5984
; has to listen on 0.0.0.0, couchdb could not bind dnsname.
bind_address = 0.0.0.0

require_valid_user = true

[httpd]
port = 5986
bind_address = 0.0.0.0
enable_cors = %s

WWW-Authenticate = Basic realm="administrator"

[couch_httpd_auth]
require_valid_user = true

; 'username = password'
[admins]
%s = %s
`

	clusterConfigs = `
#http://docs.couchdb.org/en/2.1.0/cluster/theory.html
[cluster]
q=%d
r=%d
w=%d
n=3
placement = %s
; placement = metro-dc-a:2,metro-dc-b:1
`

	corsConfigs = `
[cors]
credentials = %s
; List of origins separated by a comma, * means accept all
; Origins must include the scheme: http://example.com
; You can't set origins: * and credentials = true at the same time.
origins = %s
; List of accepted headers separated by a comma
headers = %s
; List of accepted methods
methods = %s
`

	cacertConfig = "cacert_file = /data/conf/ca-certificates.crt"

	sslConfigs = `
[daemons]
httpsd = {couch_httpd, start_link, [https]}

[ssl]
cert_file = /data/conf/couchdb.pem
key_file = /data/conf/privkey.pem
%s
`
)
