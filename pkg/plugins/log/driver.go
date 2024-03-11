package firecampdockerlog

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/jazzl0ver/firecamp/pkg/plugins/log/awslogs"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fifo"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/log"
	"github.com/jazzl0ver/firecamp/pkg/plugins"
)

const (
	getMemberMaxWaitSeconds   = int64(60)
	getMemberRetryWaitSeconds = int64(2)

	regionKey    = "awslogs-region"
	logStreamKey = "awslogs-stream"
)

// LogDriver defines the docker log driver interface.
type LogDriver interface {
	StartLogging(file string, logCtx logger.Info) error
	StopLogging(file string) error
	GetCapability() logger.Capability
}

type driver struct {
	region  string
	cluster string

	mu     sync.Mutex
	logs   map[string]*logPair
	logger logger.Logger
}

type logPair struct {
	l      logger.Logger
	stream io.ReadCloser
	info   logger.Info
}

func newDriver(region string, cluster string) *driver {
	return &driver{
		region:  region,
		cluster: cluster,
		logs:    make(map[string]*logPair),
	}
}

func (d *driver) StartLogging(file string, logCtx logger.Info) error {
	d.mu.Lock()
	if _, exists := d.logs[file]; exists {
		d.mu.Unlock()
		return fmt.Errorf("logger for %q already exists", file)
	}
	d.mu.Unlock()

	// Get the service member.
	// The volume plugin creates a local file to record the service member.
	// The volume plugin will delete the local file when the container is shut down.
	// The log plugin should be called after the volume plugin. If this is not the case,
	// there might be a corner case that the log plugin may get the stale service member.
	// The corner case is: the node is restarted and leaves the service member file undeleted.
	// After restart, the volume plugin assigns a different member to the local node. If StartLogging
	// is called before that, it will get the old service member. This only mixes the member's log,
	// we could still recover the correct log, as the service container logs its member at startup.
	//
	// The other option is: construct dbIns and find the service member that is owned by the local instance.
	// While, finding the service member requires to list all members and compare the
	// member's ContainerInstanceID, as the volume plugin will assign one member to the local
	// instance. This may waste the resources if the service has like 100 members.
	// This option may hit one corner case. The member could be updated when listing all service
	// members. Listing may get 2 members (one is stale) on the local instance.

	// the serviceuuid shoud be set in logCtx.Config map.
	serviceUUID := logCtx.Config[common.LogServiceUUIDKey]
	if len(serviceUUID) == 0 {
		logrus.WithField("id", logCtx.ContainerID).Errorf("ServiceUUID is not set in the log config %s", logCtx)
		return errors.New("ServiceUUID is not set in the log config")
	}

	// get hostname
	hostname, err := logCtx.Hostname()
	if err != nil {
		return errors.Wrapf(err, "error getting hostname")
	}

	// get service member name. For service with a single member, the service could directly pass this field.
	memberName, ok := logCtx.Config[common.LogServiceMemberKey]
	if !ok {
		memberName, err = firecampplugin.GetPluginServiceMember(serviceUUID, getMemberMaxWaitSeconds, getMemberRetryWaitSeconds)
		if err != nil {
			logrus.WithField("id", logCtx.ContainerID).WithField("ServiceUUID", serviceUUID).WithError(err).Error("GetPluginServiceMember error")
			return errors.Wrapf(err, "error GetPluginServiceMember, serviceUUID: %s", serviceUUID)
		}
	}

	// logCtx.Config should already set the logGroup.
	logCtx.Config[regionKey] = d.region
	logCtx.Config[logStreamKey] = cloudlog.GenServiceMemberLogStreamName(memberName, hostname, logCtx.ContainerID)

	logrus.WithField("id", logCtx.ContainerID).WithField("ServiceUUID", serviceUUID).WithField("ServiceMember", memberName).Infoln("start logging, logger info", logCtx)

	if logCtx.LogPath == "" {
		logCtx.LogPath = filepath.Join("/var/log/docker", logCtx.ContainerID)
	}
	if err := os.MkdirAll(filepath.Dir(logCtx.LogPath), 0755); err != nil {
		return errors.Wrap(err, "error setting up logger dir")
	}

	logrus.WithField("id", logCtx.ContainerID).WithField("file", file).WithField("logpath", logCtx.LogPath).Debugf("Start logging")
	f, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

	l, err := awslogs.New(logCtx)
	if err != nil {
		return errors.Wrap(err, "error creating awslogs logger")
	}

	d.mu.Lock()
	lf := &logPair{l, f, logCtx}
	d.logs[file] = lf
	d.mu.Unlock()

	go consumeLog(lf)
	return nil
}

func (d *driver) StopLogging(file string) error {
	logrus.WithField("file", file).Debugf("Stop logging")
	d.mu.Lock()
	lf, ok := d.logs[file]
	if ok {
		lf.stream.Close()
		delete(d.logs, file)
	}
	d.mu.Unlock()
	return nil
}

func (d *driver) GetCapability() logger.Capability {
	return logger.Capability{ReadLogs: false}
}

func consumeLog(lf *logPair) {
	dec := protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
	defer dec.Close()

	var buf logdriver.LogEntry
	for {
		if err := dec.ReadMsg(&buf); err != nil {
			if err == io.EOF {
				logrus.WithField("id", lf.info.ContainerID).WithError(err).Debug("shutting down log logger")
				lf.stream.Close()
				return
			}
			dec = protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
		}

		// TODO define our own message to include like container instance id, hostname.
		var msg logger.Message
		msg.Line = buf.Line
		msg.Source = buf.Source
		msg.Partial = buf.Partial
		msg.Timestamp = time.Unix(0, buf.TimeNano)

		if err := lf.l.Log(&msg); err != nil {
			logrus.WithField("id", lf.info.ContainerID).WithError(err).WithField("message", msg).Error("error writing log message")
			fmt.Printf("error")
		}

		buf.Reset()
	}
}
