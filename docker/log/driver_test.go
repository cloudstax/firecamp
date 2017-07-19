package openmanagedockerlogs

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	"github.com/tonistiigi/fifo"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/utils"
)

func TestLogDriver(t *testing.T) {
	uuid := utils.GenUUID()
	region := "us-east-1"
	containerID := "test-ContainerID"
	group := "testgroup-" + uuid
	stream := "teststream"

	ctx := context.Background()
	d := NewDriver()

	basePath := "/tmp/logdriver" + uuid
	err := os.MkdirAll(basePath, 0700)
	if err != nil {
		t.Fatalf("create the log basePath %s error %s", basePath, err)
	}
	defer os.RemoveAll(basePath)

	fpath := filepath.Join(basePath, containerID)

	w, err := fifo.OpenFifo(ctx, fpath, syscall.O_CREAT|syscall.O_WRONLY|syscall.O_NONBLOCK, 0700)
	if err != nil {
		t.Fatalf("OpenFifo %s error %s", fpath, err)
	}
	defer w.Close()
	defer os.Remove(fpath)

	logCtx := logger.Info{
		ContainerID: containerID,
		LogPath:     basePath,
		Config:      make(map[string]string),
	}

	logCtx.Config["awslogs-region"] = region
	logCtx.Config["awslogs-group"] = group
	logCtx.Config["awslogs-stream"] = stream
	logCtx.Config["awslogs-create-group"] = "true"

	err = d.StartLogging(fpath, logCtx)
	if err != nil {
		t.Fatalf("StartLogging error %s, fpath %s", err, fpath)
	}
	defer d.StopLogging(fpath)
	defer deleteLogGroup(t, region, group)

	// negative case: StartLogging again
	err = d.StartLogging(fpath, logCtx)
	if err == nil {
		t.Fatalf("StartLogging error %s, fpath %s", err, fpath)
	}

	// new logEntry encoder
	encoder := logdriver.NewLogEntryEncoder(w)

	// write out 3 logs
	logEntry := newLogEntry("test0" + uuid)
	err = encoder.Encode(logEntry)
	if err != nil {
		t.Fatalf("write to fifo file %s error %s", fpath, err)
	}

	logEntry = newLogEntry("test1" + uuid)
	err = encoder.Encode(logEntry)
	if err != nil {
		t.Fatalf("write to fifo file %s error %s", fpath, err)
	}

	logEntry = newLogEntry("test2" + uuid)
	err = encoder.Encode(logEntry)
	if err != nil {
		t.Fatalf("write to fifo file %s error %s", fpath, err)
	}

	// wait till aws logs publish the logs to cloud watch.
	time.Sleep(time.Duration(7) * time.Second)

	// stop logging
	err = d.StopLogging(fpath)
	if err != nil {
		t.Fatalf("StopLogging error %s, fpath %s", err, fpath)
	}

	// read back from cloud watch
	awscli := cloudwatchlogs.New(session.New(), aws.NewConfig().WithRegion(region))
	getlogReq := &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String(group),
		LogStreamName: aws.String(stream),
	}
	logevs, err := awscli.GetLogEvents(getlogReq)
	if err != nil {
		t.Fatalf("GetLogEvents group %s stream %s error %s", group, stream, err)
	}
	if len(logevs.Events) != 3 {
		t.Fatalf("expect 3 log events, get %d", len(logevs.Events))
	}
	for i, ev := range logevs.Events {
		msg := "test" + strconv.Itoa(i) + uuid
		if *ev.Message != msg {
			t.Fatalf("expect message %s, get %s, group %s stream %s", msg, *ev, group, stream)
		}
	}

}

func deleteLogGroup(t *testing.T, region string, group string) {
	// delete the log group
	awscli := cloudwatchlogs.New(session.New(), aws.NewConfig().WithRegion(region))
	delReq := &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(group),
	}
	_, err := awscli.DeleteLogGroup(delReq)
	if err != nil {
		t.Fatalf("DeleteLogGroup %s error %s", group, err)
	}
}

func newLogEntry(line string) *logdriver.LogEntry {
	return &logdriver.LogEntry{
		Line:     []byte(line),
		Source:   "container",
		Partial:  false,
		TimeNano: time.Now().UnixNano(),
	}
}
