package controldbcli

import (
	"io"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

const (
	maxRetryCount           = 3
	sleepSecondsBeforeRetry = 2
)

// ControlDBCli implements db interface and talks to ControlDBServer
type ControlDBCli struct {
	// address is ip:port
	addr string

	cliLock *sync.Mutex
	cli     *pbclient
}

type pbclient struct {
	// whether the connection is good
	isConnGood bool
	conn       *grpc.ClientConn
	dbcli      pb.ControlDBServiceClient
}

func NewControlDBCli(address string) *ControlDBCli {
	c := &ControlDBCli{
		addr:    address,
		cliLock: &sync.Mutex{},
		cli:     &pbclient{isConnGood: false},
	}

	c.connect()
	return c
}

func (c *ControlDBCli) getCli() *pbclient {
	if c.cli.isConnGood {
		return c.cli
	}

	// the current cli.isConnGood is false, connect again
	return c.connect()
}

func (c *ControlDBCli) connect() *pbclient {
	c.cliLock.Lock()
	defer c.cliLock.Unlock()

	// checkk isConnGood again, as another request may hold the lock and set up the connection
	if c.cli.isConnGood {
		return c.cli
	}

	// TODO support tls
	conn, err := grpc.Dial(c.addr, grpc.WithInsecure())
	if err != nil {
		glog.Errorln("grpc dial error", err, "address", c.addr)
		return c.cli
	}

	cli := &pbclient{
		isConnGood: true,
		conn:       conn,
		dbcli:      pb.NewControlDBServiceClient(conn),
	}

	c.cli = cli
	return c.cli
}

func (c *ControlDBCli) markClientFailed(cli *pbclient) (isClientChanged bool) {
	c.cliLock.Lock()
	defer c.cliLock.Unlock()

	if !c.cli.isConnGood {
		// the current connection is marked as failed, no need to mark again
		glog.V(1).Infoln("the current connection is already marked as failed", c.cli, cli)
		return false
	}

	if c.cli != cli {
		// the current connection is good and the failed cli is not the same with the current cli.
		// this means some other request already reconnects to the server.
		glog.V(1).Infoln("the current connection", c.cli, "is good, the failed connection is", cli)
		return true
	}

	// the failed cli is the same with the current cli, mark it failed
	c.cli.isConnGood = false
	// close the connection
	c.cli.conn.Close()
	return false
}

func (c *ControlDBCli) markClientFailedAndSleep(cli *pbclient) {
	isClientChanged := c.markClientFailed(cli)
	if !isClientChanged {
		// the current cli is marked as failed, wait some time before retry
		time.Sleep(sleepSecondsBeforeRetry * time.Second)
	}
}

func (c *ControlDBCli) checkAndConvertError(err error) error {
	// grpc defines the error codes in /grpcsrc/codes/codes.go.
	// if server side returns the application-level error, grpc will return error with
	// code = codes.Unknown, desc = applicationError.Error(), see /grpcsrc/rpc_util/toRPCError()
	switch grpc.ErrorDesc(err) {
	case db.StrErrDBInternal:
		return db.ErrDBInternal
	case db.StrErrDBInvalidRequest:
		return db.ErrDBInvalidRequest
	case db.StrErrDBRecordNotFound:
		return db.ErrDBRecordNotFound
	case db.StrErrDBConditionalCheckFailed:
		return db.ErrDBConditionalCheckFailed
	}
	return err
}

func (c *ControlDBCli) CreateSystemTables(ctx context.Context) error {
	return nil
}

func (c *ControlDBCli) SystemTablesReady(ctx context.Context) (tableStatus string, ready bool, err error) {
	return db.TableStatusActive, true, nil
}

func (c *ControlDBCli) DeleteSystemTables(ctx context.Context) error {
	return nil
}

func (c *ControlDBCli) CreateDevice(ctx context.Context, dev *common.Device) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	pbdev := controldb.GenPbDevice(dev)
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateDevice(ctx, pbdev)
		if err == nil {
			glog.Infoln("created device", pbdev, "requuid", requuid)
			return nil
		}

		// error
		glog.Errorln("CreateDevice error", err, "device", pbdev, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetDevice(ctx context.Context, clusterName string, deviceName string) (dev *common.Device, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.DeviceKey{
		ClusterName: clusterName,
		DeviceName:  deviceName,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbdev, err := cli.dbcli.GetDevice(ctx, key)
		if err == nil {
			glog.Infoln("got device", pbdev, "requuid", requuid)
			return controldb.GenDbDevice(pbdev), nil
		}

		// error
		glog.Errorln("GetDevice error", err, key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteDevice(ctx context.Context, clusterName string, deviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.DeviceKey{
		ClusterName: clusterName,
		DeviceName:  deviceName,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.DeleteDevice(ctx, key)
		if err == nil {
			glog.Infoln("deleted device", key, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteDevice error", err, key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) listDevices(ctx context.Context, clusterName string, cli *pbclient,
	req *pb.ListDeviceRequest, requuid string) (devs []*common.Device, err error) {
	stream, err := cli.dbcli.ListDevices(ctx, req)
	if err != nil {
		glog.Errorln("ListDevices error", err, "cluster", clusterName, "requuid", requuid)
		return nil, err
	}

	for {
		pbdev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			glog.Errorln("list one device error", err, "cluster", clusterName, "requuid", requuid)
			return nil, err
		}

		// get one device
		dev := controldb.GenDbDevice(pbdev)
		devs = append(devs, dev)
		glog.V(1).Infoln("list one device", dev, "total", len(devs), "requuid", requuid)
	}

	if len(devs) > 0 {
		glog.Infoln("list", len(devs), "devices, last device is", devs[len(devs)-1], "requuid", requuid)
	} else {
		glog.Infoln("cluster", clusterName, "has no devices, requuid", requuid)
	}
	return devs, nil
}

func (c *ControlDBCli) ListDevices(ctx context.Context, clusterName string) (devs []*common.Device, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	req := &pb.ListDeviceRequest{
		ClusterName: clusterName,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		devs, err = c.listDevices(ctx, clusterName, cli, req, requuid)
		if err == nil {
			return devs, nil
		}

		glog.Errorln("ListDevices error", err, req, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) CreateService(ctx context.Context, svc *common.Service) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	pbsvc := controldb.GenPbService(svc)
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateService(ctx, pbsvc)
		if err == nil {
			glog.Infoln("created service", pbsvc, "requuid", requuid)
			return nil
		}

		glog.Errorln("CreateService error", err, "service", pbsvc, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetService(ctx context.Context, clusterName string, serviceName string) (svc *common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.ServiceKey{
		ClusterName: clusterName,
		ServiceName: serviceName,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbsvc, err := cli.dbcli.GetService(ctx, key)
		if err == nil {
			glog.Infoln("get service", pbsvc, "requuid", requuid)
			return controldb.GenDbService(pbsvc), nil
		}

		glog.Errorln("GetService error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteService(ctx context.Context, clusterName string, serviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.ServiceKey{
		ClusterName: clusterName,
		ServiceName: serviceName,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbsvc, err := cli.dbcli.DeleteService(ctx, key)
		if err == nil {
			glog.Infoln("delete service", pbsvc, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteService error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) listServices(ctx context.Context, clusterName string, cli *pbclient,
	req *pb.ListServiceRequest, requuid string) (svcs []*common.Service, err error) {
	stream, err := cli.dbcli.ListServices(ctx, req)
	if err != nil {
		glog.Errorln("ListServices error", err, "cluster", clusterName, "requuid", requuid)
		return nil, err
	}

	for {
		pbsvc, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			glog.Errorln("list one service error", err, "cluster", clusterName, "requuid", requuid)
			return nil, err
		}

		// get one service
		svc := controldb.GenDbService(pbsvc)
		svcs = append(svcs, svc)
		glog.V(1).Infoln("list one service", svc, "total", len(svcs), "requuid", requuid)
	}

	if len(svcs) > 0 {
		glog.Infoln("list", len(svcs), "services, last service is", svcs[len(svcs)-1], "requuid", requuid)
	} else {
		glog.Infoln("cluster", clusterName, "has no service, requuid", requuid)
	}
	return svcs, nil
}

func (c *ControlDBCli) ListServices(ctx context.Context, clusterName string) (svcs []*common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	req := &pb.ListServiceRequest{
		ClusterName: clusterName,
	}

	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		svcs, err = c.listServices(ctx, clusterName, cli, req, requuid)
		if err == nil {
			return svcs, nil
		}

		glog.Errorln("ListServices error", err, "cluster", clusterName, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	pbattr, err := controldb.GenPbServiceAttr(attr)
	if err != nil {
		return err
	}

	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateServiceAttr(ctx, pbattr)
		if err == nil {
			glog.Infoln("created service attr", pbattr, "requuid", requuid)
			return nil
		}

		glog.Errorln("CreateServiceAttr error", err, "serviceAttr", pbattr, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	pboldAttr, err := controldb.GenPbServiceAttr(oldAttr)
	if err != nil {
		return err
	}
	pbnewAttr, err := controldb.GenPbServiceAttr(newAttr)
	if err != nil {
		return err
	}

	req := &pb.UpdateServiceAttrRequest{
		OldAttr: pboldAttr,
		NewAttr: pbnewAttr,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.UpdateServiceAttr(ctx, req)
		if err == nil {
			glog.Infoln("UpdateServiceAttr from", oldAttr, "to", newAttr, "requuid", requuid)
			return nil
		}

		glog.Errorln("UpdateServiceAttr error", err, "old attr", oldAttr, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetServiceAttr(ctx context.Context, serviceUUID string) (attr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbAttr, err := cli.dbcli.GetServiceAttr(ctx, key)
		if err == nil {
			glog.Infoln("get service attr", pbAttr, "requuid", requuid)
			return controldb.GenDbServiceAttr(pbAttr)
		}

		glog.Errorln("GetServiceAttr error", err, "service", serviceUUID, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteServiceAttr(ctx context.Context, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbAttr, err := cli.dbcli.DeleteServiceAttr(ctx, key)
		if err == nil {
			glog.Infoln("delete service attr", pbAttr, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteServiceAttr error", err, "service", serviceUUID, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) CreateServiceMember(ctx context.Context, member *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	pbmember := controldb.GenPbServiceMember(member)
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateServiceMember(ctx, pbmember)
		if err == nil {
			glog.Infoln("created serviceMember", pbmember, "requuid", requuid)
			return nil
		}

		glog.Errorln("CreateServiceMember error", err, "serviceMember", pbmember, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) UpdateServiceMember(ctx context.Context, oldMember *common.ServiceMember, newMember *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	req := &pb.UpdateServiceMemberRequest{
		OldMember: controldb.GenPbServiceMember(oldMember),
		NewMember: controldb.GenPbServiceMember(newMember),
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.UpdateServiceMember(ctx, req)
		if err == nil {
			glog.Infoln("UpdateServiceMember from", oldMember, "to", newMember, "requuid", requuid)
			return nil
		}

		glog.Errorln("UpdateServiceMember error", err, "old serviceMember", oldMember, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) (member *common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: memberIndex,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbmember, err := cli.dbcli.GetServiceMember(ctx, key)
		if err == nil {
			glog.Infoln("get serviceMember", pbmember, "requuid", requuid)
			return controldb.GenDbServiceMember(pbmember), nil
		}

		glog.Errorln("GetServiceMember error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: memberIndex,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbmember, err := cli.dbcli.DeleteServiceMember(ctx, key)
		if err == nil {
			glog.Infoln("delete serviceMember", pbmember, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteServiceMember error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) listServiceMembers(ctx context.Context, serviceUUID string, cli *pbclient,
	req *pb.ListServiceMemberRequest, requuid string) (members []*common.ServiceMember, err error) {
	stream, err := cli.dbcli.ListServiceMembers(ctx, req)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	for {
		pbmember, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			glog.Errorln("list one serviceMember error", err, "serviceUUID", serviceUUID, "requuid", requuid)
			return nil, err
		}

		member := controldb.GenDbServiceMember(pbmember)
		members = append(members, member)
		glog.V(1).Infoln("list one serviceMember", member, "total", len(members), "requuid", requuid)
	}

	if len(members) > 0 {
		glog.Infoln("list", len(members), "serviceMembers, last serviceMember is", members[len(members)-1], "requuid", requuid)
	} else {
		glog.Infoln("service has no serviceMember", serviceUUID, "requuid", requuid)
	}
	return members, nil
}

func (c *ControlDBCli) ListServiceMembers(ctx context.Context, serviceUUID string) (members []*common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	req := &pb.ListServiceMemberRequest{
		ServiceUUID: serviceUUID,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		members, err = c.listServiceMembers(ctx, serviceUUID, cli, req, requuid)
		if err == nil {
			return members, nil
		}

		glog.Errorln("ListServiceMembers error", err, "serviceUUID", serviceUUID, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	pbcfg := controldb.GenPbConfigFile(cfg)
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateConfigFile(ctx, pbcfg)
		if err == nil {
			glog.Infoln("created config file", pbcfg, "requuid", requuid)
			return nil
		}

		glog.Errorln("CreateConfigFile error", err, "config file", pbcfg, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetConfigFile(ctx context.Context, serviceUUID string, fileID string) (cfg *common.ConfigFile, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileID,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbcfg, err := cli.dbcli.GetConfigFile(ctx, key)
		if err == nil {
			glog.Infoln("get config file", pbcfg, "requuid", requuid)
			return controldb.GenDbConfigFile(pbcfg), nil
		}

		glog.Errorln("GetConfigFile error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteConfigFile(ctx context.Context, serviceUUID string, fileID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileID,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbcfg, err := cli.dbcli.DeleteConfigFile(ctx, key)
		if err == nil {
			glog.Infoln("delete config file", pbcfg, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteConfigFile error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) CreateServiceStaticIP(ctx context.Context, serviceip *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	pbserviceip := controldb.GenPbServiceStaticIP(serviceip)
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.CreateServiceStaticIP(ctx, pbserviceip)
		if err == nil {
			glog.Infoln("created serviceStaticIP", pbserviceip, "requuid", requuid)
			return nil
		}

		glog.Errorln("CreateServiceStaticIP error", err, "serviceStaticIP", pbserviceip, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) UpdateServiceStaticIP(ctx context.Context, oldIP *common.ServiceStaticIP, newIP *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	req := &pb.UpdateServiceStaticIPRequest{
		OldIP: controldb.GenPbServiceStaticIP(oldIP),
		NewIP: controldb.GenPbServiceStaticIP(newIP),
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		_, err = cli.dbcli.UpdateServiceStaticIP(ctx, req)
		if err == nil {
			glog.Infoln("UpdateServiceStaticIP from", oldIP, "to", newIP, "requuid", requuid)
			return nil
		}

		glog.Errorln("UpdateServiceStaticIP error", err, "old serviceStaticIP", oldIP, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}

func (c *ControlDBCli) GetServiceStaticIP(ctx context.Context, staticIP string) (serviceip *common.ServiceStaticIP, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	key := &pb.ServiceStaticIPKey{
		StaticIP: staticIP,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbserviceip, err := cli.dbcli.GetServiceStaticIP(ctx, key)
		if err == nil {
			glog.Infoln("get serviceStaticIP", pbserviceip, "requuid", requuid)
			return controldb.GenDbServiceStaticIP(pbserviceip), nil
		}

		glog.Errorln("GetServiceStaticIP error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return nil, c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return nil, err
}

func (c *ControlDBCli) DeleteServiceStaticIP(ctx context.Context, staticIP string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	var err error
	key := &pb.ServiceStaticIPKey{
		StaticIP: staticIP,
	}
	for i := 0; i < maxRetryCount; i++ {
		cli := c.getCli()
		pbserviceip, err := cli.dbcli.DeleteServiceStaticIP(ctx, key)
		if err == nil {
			glog.Infoln("delete serviceStaticIP", pbserviceip, "requuid", requuid)
			return nil
		}

		glog.Errorln("DeleteServiceStaticIP error", err, "key", key, "requuid", requuid)
		if grpc.Code(err) == codes.Unknown {
			// not grpc layer error code, directly return
			return c.checkAndConvertError(err)
		}
		// grpc error, retry it
		c.markClientFailedAndSleep(cli)
	}
	return err
}
