package controldbserver

import (
	"net"
	"os"
	"os/signal"
	"strconv"

	"github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/cloudstax/firecamp/common"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

type controlserver struct {
	rpcserver *grpc.Server
	// pass back the rpc server serve status
	rpcServeRes chan error

	dbserver *ControlDBServer
}

func (s *controlserver) tearDown() {
	s.rpcserver.GracefulStop()
	s.dbserver.Stop()
}

func (s *controlserver) serve(lis net.Listener) {
	s.rpcServeRes <- s.rpcserver.Serve(lis)
}

// StartControlDBServer initializes and starts the controldb server
func StartControlDBServer(cluster string, dataDir string, tlsEnabled bool, certFile, keyFile string) {
	// sanity check, controldb data dir should exist
	exist, err := utils.IsDirExist(dataDir)
	if err != nil {
		glog.Fatalln("check controldb data dir error", err, dataDir)
	}
	if !exist {
		glog.Fatalln("controldb data dir not exist", dataDir)
	}

	// listen on all ips, as controldb runs inside the container
	addr := ":" + strconv.Itoa(common.ControlDBServerPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		glog.Fatalln("failed to listen on addr", addr, "error", err)
	}

	var opts []grpc.ServerOption
	if tlsEnabled {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			glog.Fatalln("Failed to generate credentials %v", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	dbserver, err := NewControlDBServer(dataDir, cluster)
	if err != nil {
		glog.Fatalln("NewControlDBServer error", err, "dataDir", dataDir, "cluster", cluster)
	}

	rpcserver := grpc.NewServer(opts...)
	pb.RegisterControlDBServiceServer(rpcserver, dbserver)

	s := &controlserver{
		rpcserver:   rpcserver,
		rpcServeRes: make(chan error),
		dbserver:    dbserver,
	}

	go s.serve(lis)

	err = <-s.rpcServeRes
	if err != nil {
		glog.Fatalln("failed to serve: %v", err)
	}

	c := make(chan os.Signal, 1)
	//signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGSTOP, syscall.SIGTERM)
	signal.Notify(c)

	// Block until a signal is received.
	sg := <-c
	s.tearDown()
	lis.Close()
	glog.Fatalln("Got signal:", sg)
}
