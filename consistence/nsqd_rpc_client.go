package consistence

import (
	"github.com/absolute8511/nsq/nsqd"
	"github.com/valyala/gorpc"
	"time"
)

const (
	RPC_TIMEOUT       = time.Duration(time.Second * 10)
	RPC_TIMEOUT_SHORT = time.Duration(time.Second * 5)
)

type NsqdRpcClient struct {
	remote  string
	timeout time.Duration
	d       *gorpc.Dispatcher
	c       *gorpc.Client
	dc      *gorpc.DispatcherClient
}

func convertRpcError(err error, errInterface interface{}) *CoordErr {
	if err != nil {
		return NewCoordErr(err.Error(), CoordNetErr)
	}
	if errInterface == nil {
		return nil
	}
	coordErr, ok := errInterface.(*CoordErr)
	if ok {
		if coordErr != nil && coordErr.HasError() {
			return coordErr
		}
	} else {
		return NewCoordErr("Not an Invalid CoordErr", CoordCommonErr)
	}
	return nil
}

func NewNsqdRpcClient(addr string, timeout time.Duration) (*NsqdRpcClient, error) {
	c := gorpc.NewTCPClient(addr)
	c.RequestTimeout = timeout
	c.Start()
	d := gorpc.NewDispatcher()
	d.AddService("NsqdCoordRpcServer", &NsqdCoordRpcServer{})

	return &NsqdRpcClient{
		remote:  addr,
		timeout: timeout,
		d:       d,
		c:       c,
		dc:      d.NewServiceClient("NsqdCoordRpcServer", c),
	}, nil
}

func (self *NsqdRpcClient) Close() {
	self.c.Stop()
}

func (self *NsqdRpcClient) Reconnect() error {
	self.c.Stop()
	self.c = gorpc.NewTCPClient(self.remote)
	self.c.RequestTimeout = self.timeout
	self.dc = self.d.NewServiceClient("NsqdCoordRpcServer", self.c)
	self.c.Start()
	return nil
}

func (self *NsqdRpcClient) CallWithRetry(method string, arg interface{}) (interface{}, error) {
	for {
		reply, err := self.dc.Call(method, arg)
		if err != nil && err.(*gorpc.ClientError).Connection {
			coordLog.Infof("rpc connection closed, error: %v", err)
			err = self.Reconnect()
			if err != nil {
				return reply, err
			}
		} else {
			if err != nil {
				coordLog.Infof("rpc call %v error: %v", method, err)
			}
			return reply, err
		}
	}
}

func (self *NsqdRpcClient) NotifyTopicLeaderSession(epoch EpochType, topicInfo *TopicPartitionMetaInfo, leaderSession *TopicLeaderSession, joinSession string) *CoordErr {
	var rpcInfo RpcTopicLeaderSession
	rpcInfo.LookupdEpoch = epoch
	rpcInfo.TopicLeaderSession = leaderSession.Session
	rpcInfo.TopicLeaderSessionEpoch = leaderSession.LeaderEpoch
	if leaderSession.LeaderNode != nil {
		rpcInfo.LeaderNode = *leaderSession.LeaderNode
	}
	rpcInfo.JoinSession = joinSession
	rpcInfo.TopicName = topicInfo.Name
	rpcInfo.TopicPartition = topicInfo.Partition
	retErr, err := self.CallWithRetry("NotifyTopicLeaderSession", &rpcInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) NotifyAcquireTopicLeader(epoch EpochType, topicInfo *TopicPartitionMetaInfo) *CoordErr {
	var rpcInfo RpcAcquireTopicLeaderReq
	rpcInfo.LookupdEpoch = epoch
	rpcInfo.TopicName = topicInfo.Name
	rpcInfo.TopicPartition = topicInfo.Partition
	rpcInfo.TopicEpoch = topicInfo.Epoch
	rpcInfo.LeaderNodeID = topicInfo.Leader
	retErr, err := self.CallWithRetry("NotifyAcquireTopicLeader", &rpcInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) UpdateTopicInfo(epoch EpochType, topicInfo *TopicPartitionMetaInfo) *CoordErr {
	var rpcInfo RpcAdminTopicInfo
	rpcInfo.LookupdEpoch = epoch
	rpcInfo.TopicPartitionMetaInfo = *topicInfo
	retErr, err := self.CallWithRetry("UpdateTopicInfo", &rpcInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) EnableTopicWrite(epoch EpochType, topicInfo *TopicPartitionMetaInfo) *CoordErr {
	var rpcInfo RpcAdminTopicInfo
	rpcInfo.LookupdEpoch = epoch
	rpcInfo.TopicPartitionMetaInfo = *topicInfo
	retErr, err := self.CallWithRetry("EnableTopicWrite", &rpcInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) DisableTopicWrite(epoch EpochType, topicInfo *TopicPartitionMetaInfo) *CoordErr {
	var rpcInfo RpcAdminTopicInfo
	rpcInfo.LookupdEpoch = epoch
	rpcInfo.TopicPartitionMetaInfo = *topicInfo
	retErr, err := self.CallWithRetry("DisableTopicWrite", &rpcInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) IsTopicWriteDisabled(topicInfo *TopicPartitionMetaInfo) bool {
	var rpcInfo RpcAdminTopicInfo
	rpcInfo.TopicPartitionMetaInfo = *topicInfo
	ret, err := self.CallWithRetry("IsTopicWriteDisabled", &rpcInfo)
	if err != nil {
		return false
	}
	return ret.(bool)
}

func (self *NsqdRpcClient) GetTopicStats(topic string) (*NodeTopicStats, error) {
	stat, err := self.CallWithRetry("GetTopicStats", topic)
	return stat.(*NodeTopicStats), err
}

func (self *NsqdRpcClient) UpdateChannelOffset(leaderSession *TopicLeaderSession, info *TopicPartitionMetaInfo, channel string, offset ChannelConsumerOffset) *CoordErr {
	var updateInfo RpcChannelOffsetArg
	updateInfo.TopicName = info.Name
	updateInfo.TopicPartition = info.Partition
	updateInfo.TopicEpoch = info.Epoch
	updateInfo.TopicLeaderSessionEpoch = leaderSession.LeaderEpoch
	updateInfo.TopicLeaderSession = leaderSession.Session
	updateInfo.Channel = channel
	updateInfo.ChannelOffset = offset
	retErr, err := self.CallWithRetry("UpdateChannelOffset", &updateInfo)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) PutMessage(leaderSession *TopicLeaderSession, info *TopicPartitionMetaInfo, log CommitLogData, message *nsqd.Message) *CoordErr {
	var putData RpcPutMessage
	putData.LogData = log
	putData.TopicName = info.Name
	putData.TopicPartition = info.Partition
	putData.TopicMessage = message
	putData.TopicEpoch = info.Epoch
	putData.TopicLeaderSessionEpoch = leaderSession.LeaderEpoch
	putData.TopicLeaderSession = leaderSession.Session
	retErr, err := self.CallWithRetry("PutMessage", &putData)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) PutMessages(leaderSession *TopicLeaderSession, info *TopicPartitionMetaInfo, log CommitLogData, messages []*nsqd.Message) *CoordErr {
	var putData RpcPutMessages
	putData.LogData = log
	putData.TopicName = info.Name
	putData.TopicPartition = info.Partition
	putData.TopicMessages = messages
	putData.TopicEpoch = info.Epoch
	putData.TopicLeaderSessionEpoch = leaderSession.LeaderEpoch
	putData.TopicLeaderSession = leaderSession.Session
	retErr, err := self.CallWithRetry("PutMessages", &putData)
	return convertRpcError(err, retErr)
}

func (self *NsqdRpcClient) GetLastCommitLogID(topicInfo *TopicPartitionMetaInfo) (int64, *CoordErr) {
	var req RpcCommitLogReq
	req.TopicName = topicInfo.Name
	req.TopicPartition = topicInfo.Partition
	var retErr CoordErr
	ret, err := self.CallWithRetry("GetLastCommitLogID", &req)
	return ret.(int64), convertRpcError(err, &retErr)
}

func (self *NsqdRpcClient) GetCommitLogFromOffset(topicInfo *TopicPartitionMetaInfo, offset int64) (int64, CommitLogData, *CoordErr) {
	var req RpcCommitLogReq
	req.LogOffset = offset
	req.TopicName = topicInfo.Name
	req.TopicPartition = topicInfo.Partition
	var rsp *RpcCommitLogRsp
	rspVar, err := self.CallWithRetry("GetCommitLogFromOffset", &req)
	rsp = rspVar.(*RpcCommitLogRsp)
	return rsp.LogOffset, rsp.LogData, convertRpcError(err, &rsp.ErrInfo)
}

func (self *NsqdRpcClient) PullCommitLogsAndData(topic string, partition int,
	startOffset int64, num int) ([]CommitLogData, [][]byte, error) {
	var r RpcPullCommitLogsReq
	r.TopicName = topic
	r.TopicPartition = partition
	r.StartLogOffset = startOffset
	r.LogMaxNum = num
	var ret *RpcPullCommitLogsRsp
	retVar, err := self.CallWithRetry("PullCommitLogsAndData", &r)
	ret = retVar.(*RpcPullCommitLogsRsp)
	return ret.Logs, ret.DataList, err
}

func (self *NsqdRpcClient) CallRpcTest(data string) (string, *CoordErr) {
	var req RpcTestReq
	req.Data = data
	var ret *RpcTestRsp
	retVar, err := self.CallWithRetry("TestRpcError", &req)
	ret = retVar.(*RpcTestRsp)
	return ret.RspData, convertRpcError(err, ret.RetErr)
}

func (self *NsqdRpcClient) CallRpcTesttimeout(data string) error {
	_, err := self.CallWithRetry("TestRpcTimeout", "req")
	return err
}
