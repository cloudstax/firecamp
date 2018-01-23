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
	"github.com/cloudstax/firecamp/docker/log/awslogs"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fifo"
)

const (
	regionKey         = "awslogs-region"
	logGroupKey       = "awslogs-group"
	logStreamKey      = "awslogs-stream"
	logCreateGroupKey = "awslogs-create-group"
)

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

	logrus.Errorln("start logging, logger info", logCtx)

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
