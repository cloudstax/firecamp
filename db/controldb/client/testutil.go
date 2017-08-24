package controldbcli

import (
	"net"
	"os"
	"strconv"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"

	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/db/controldb/server"
)

type TestControlDBServer struct {
	Testdir    string
	ListenPort int
	lis        net.Listener
	rpcserver  *grpc.Server
}

func (s *TestControlDBServer) RunControldbTestServer(cluster string) {
	var err error
	addr := "localhost:" + strconv.Itoa(s.ListenPort)

	// retry 3 times if listen fails.
	for i := 0; i < 3; i++ {
		s.lis, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}

		glog.Errorln("failed to listen on addr", addr, "error", err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		glog.Fatalln("failed to listen on addr", addr, "error", err)
	}

	dbserver, err := controldbserver.NewControlDBServer(s.Testdir, cluster)
	if err != nil {
		glog.Fatalln("NewControlDBServer error", err, "dataDir", s.Testdir, "cluster", cluster)
	}

	s.rpcserver = grpc.NewServer()
	pb.RegisterControlDBServiceServer(s.rpcserver, dbserver)

	s.rpcserver.Serve(s.lis)
}

func (s *TestControlDBServer) StopControldbTestServer() {
	if s.rpcserver != nil {
		s.rpcserver.Stop()
	}
	if s.lis != nil {
		s.lis.Close()
	}
	os.RemoveAll(s.Testdir)
}
