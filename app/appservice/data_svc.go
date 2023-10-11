package appservice

import (
	"errors"
	"fmt"
	"github.com/chwjbn/cheer-proxy-http/app/appmodel"
	"github.com/chwjbn/cheer-proxy-http/cheerlib"
	"github.com/gomodule/redigo/redis"
	"strings"
	"time"
)

const (
	xRedisProxyVersionKey="cheer-proxy-version"
	xRedisProxyNodeKey="cheer-proxy-node"        //cheer-proxy-node:node-id=>{}
	xRedisProxyInboundKey="cheer-proxy-inbound"   //cheer-proxy-inbound:md5(sha224Pwd)
	xRedisProxyAccountKey="cheer-proxy-account"   //cheer-proxy-account:token
	xRedisProxyInboundIndexKey="cheer-proxy-inbound-index"  //cheer-proxy-inbound-index:inbound-id
)

type DataCacheConfig struct {
	CacheRedisAddr string `json:"cache_redis_addr"`
	CacheRedisUser string `json:"cache_redis_user"`
	CacheRedisPwd string  `json:"cache_redis_pwd"`
	CacheRedisDb int  `json:"cache_redis_db"`
}

type DataService struct {
	mConfig *DataCacheConfig
	mRedisPool *redis.Pool
}

func NewDataService(cnf DataCacheConfig) (*DataService,error)  {

	xSvc:=new(DataService)
	xSvc.mConfig=&cnf

	xErr:=xSvc.initRedisCache()
	if xErr!=nil{
		return nil, xErr
	}

	return xSvc,nil

}

func (s *DataService) initRedisCache() error  {

	var err error

	xRedisDialOptions:=[]redis.DialOption{
		redis.DialConnectTimeout(5*time.Second),
		redis.DialDatabase(s.mConfig.CacheRedisDb),
		redis.DialKeepAlive(5*time.Second),
	}

	if len(s.mConfig.CacheRedisUser)>0{
		xRedisDialOptions=append(xRedisDialOptions, redis.DialUsername(s.mConfig.CacheRedisUser))
	}

	if len(s.mConfig.CacheRedisPwd)>0{
		xRedisDialOptions=append(xRedisDialOptions, redis.DialPassword(s.mConfig.CacheRedisPwd))
	}

	s.mRedisPool=&redis.Pool{
		MaxIdle: 3,
		MaxActive: 9,
		IdleTimeout: 18 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", s.mConfig.CacheRedisAddr, xRedisDialOptions...)
		},
	}

	xRedisCheckConn:=s.mRedisPool.Get()
	defer xRedisCheckConn.Close()

	_,xPingErr:=xRedisCheckConn.Do("PING")

	if xPingErr!=nil{
		err=errors.New(fmt.Sprintf("redis to [%s] ping with error:%s",s.mConfig.CacheRedisAddr,xPingErr.Error()))
	}

	return err
}

func (s *DataService)GetProxyVersion() string  {

	xDataKey:=xRedisProxyVersionKey
	xDataVal,xErr:=s.GetRedisData(xDataKey)

	if xErr!=nil{
		return ""
	}

	return xDataVal

}

func (s *DataService)GetProxyNodeByInboundPwd(inboundPwd string) (appmodel.ProxyNode)  {

	xNodeData:=appmodel.ProxyNode{}

	xInboundData,xErr:=s.GetProxyInbound(inboundPwd)
	if xErr!=nil{
		return xNodeData
	}

	if len(xInboundData.InboundId)<1{
		return xNodeData
	}

	xIndexMin:=0
	xIndexMax:=len(xInboundData.NodePool)-1

	xLastIndexData:=s.GetProxyInboundNodeIndex(xInboundData.InboundId)
	if xLastIndexData.LastTime<1{
		xLastIndexData.LastIndex=-1
	}

	if strings.EqualFold(xInboundData.BalanceType,"poll_static"){
		xLastIndexData.LastIndex=xIndexMin
		xLastIndexData.LastTime=time.Now().Unix()
	}

	if strings.EqualFold(xInboundData.BalanceType,"poll_random"){
		xNewIndex:=xLastIndexData.LastIndex+1
		xLastIndexData.LastIndex=xNewIndex
		xLastIndexData.LastTime=time.Now().Unix()
	}

	if strings.EqualFold(xInboundData.BalanceType,"poll_minute"){

		var xTimeDiff int64=1000*60
		xTimeNow:=time.Now().Unix()
		if xTimeNow-xLastIndexData.LastTime>xTimeDiff{
			xNewIndex:=xLastIndexData.LastIndex+1
			xLastIndexData.LastIndex=xNewIndex
			xLastIndexData.LastTime=time.Now().Unix()
		}
	}

	if strings.EqualFold(xInboundData.BalanceType,"poll_hour"){

		var xTimeDiff int64=1000*60*60
		xTimeNow:=time.Now().Unix()
		if xTimeNow-xLastIndexData.LastTime>xTimeDiff{
			xNewIndex:=xLastIndexData.LastIndex+1
			xLastIndexData.LastIndex=xNewIndex
			xLastIndexData.LastTime=time.Now().Unix()
		}
	}

	if strings.EqualFold(xInboundData.BalanceType,"poll_day"){

		var xTimeDiff int64=1000*60*60*24
		xTimeNow:=time.Now().Unix()
		if xTimeNow-xLastIndexData.LastTime>xTimeDiff{
			xNewIndex:=xLastIndexData.LastIndex+1
			xLastIndexData.LastIndex=xNewIndex
			xLastIndexData.LastTime=time.Now().Unix()
		}
	}

	if xLastIndexData.LastIndex<xIndexMin||xLastIndexData.LastIndex>xIndexMax{
		xLastIndexData.LastIndex=xIndexMin
		xLastIndexData.LastTime=time.Now().Unix()
	}


	xMarkIndex:=xLastIndexData.LastIndex

	for  {
		xBoundNodeData:=xInboundData.NodePool[xLastIndexData.LastIndex]
		xTestNodeData,xTestErr:=s.GetProxyNode(xBoundNodeData.NodeId)
		if xTestErr!=nil{
			break
		}

		if strings.EqualFold(xTestNodeData.Status,"online"){
			xNodeData=xTestNodeData
			break
		}

		xLastIndexData.LastIndex=xLastIndexData.LastIndex+1
		xLastIndexData.LastTime=time.Now().Unix()

		if xLastIndexData.LastIndex<xIndexMin||xLastIndexData.LastIndex>xIndexMax{
			xLastIndexData.LastIndex=xIndexMin
			xLastIndexData.LastTime=time.Now().Unix()
		}

		//跑了一个轮回
		if xLastIndexData.LastIndex==xMarkIndex{
			break
		}
	}

	//记录时间
	s.SetProxyInboundNodeIndex(xInboundData.InboundId,xLastIndexData,60*60*24*3)

	return xNodeData

}

func (s *DataService)GetProxyInbound(sha224Pwd string) (appmodel.ProxyInbound,error) {

	xConfigVersion:=s.GetProxyVersion()
	if len(xConfigVersion)<1{
		return appmodel.ProxyInbound{},errors.New("invalid proxy config version")
	}

	xDataKey:=fmt.Sprintf("%s:%s:%s",xRedisProxyInboundKey,xConfigVersion,cheerlib.EncryptMd5(sha224Pwd))

	xDataVal,xErr:=s.GetRedisData(xDataKey)

	if xErr!=nil{
		return appmodel.ProxyInbound{}, xErr
	}

	xData:=appmodel.ProxyInbound{}

	xErr=cheerlib.TextStructFromJson(&xData,xDataVal)
	if xErr!=nil{
		return appmodel.ProxyInbound{}, errors.New(fmt.Sprintf("GetProxyInbound ProxyInbound data with error:%s",xErr.Error()))
	}

	return xData,nil
}

func (s *DataService)GetProxyNode(nodeId string)(appmodel.ProxyNode,error)  {

	xConfigVersion:=s.GetProxyVersion()
	if len(xConfigVersion)<1{
		return appmodel.ProxyNode{},errors.New("invalid proxy config version")
	}


	xDataKey:=fmt.Sprintf("%s:%s:%s",xRedisProxyNodeKey,xConfigVersion,nodeId)

	xDataVal,xErr:=s.GetRedisData(xDataKey)

	if xErr!=nil{
		return appmodel.ProxyNode{}, xErr
	}

	xData:=appmodel.ProxyNode{}

	xErr=cheerlib.TextStructFromJson(&xData,xDataVal)
	if xErr!=nil{
		return appmodel.ProxyNode{}, errors.New(fmt.Sprintf("decode ProxyNode data with error:%s",xErr.Error()))
	}

	return xData,nil
}

func (s *DataService)GetProxyAccount(accountToken string)(appmodel.ProxyAccount,error)  {

	xConfigVersion:=s.GetProxyVersion()
	if len(xConfigVersion)<1{
		return appmodel.ProxyAccount{},errors.New("invalid proxy config version")
	}


	xDataKey:=fmt.Sprintf("%s:%s:%s",xRedisProxyAccountKey,xConfigVersion,accountToken)
	xDataVal,xErr:=s.GetRedisData(xDataKey)

	xData:=appmodel.ProxyAccount{}

	if xErr!=nil{
		return xData, xErr
	}

	xErr=cheerlib.TextStructFromJson(&xData,xDataVal)
	if xErr!=nil{
		return xData, errors.New(fmt.Sprintf("decode ProxyAccount data with error:%s",xErr.Error()))
	}

	return xData,nil
}

func (s *DataService)GetProxyInboundNodeIndex(inboundId string) appmodel.ProxyInboundIndex  {

	xIndexData:=appmodel.ProxyInboundIndex{}

	xConfigVersion:=s.GetProxyVersion()
	if len(xConfigVersion)<1{
		return xIndexData
	}

	xDataKey:=fmt.Sprintf("%s:%s:%s",xRedisProxyInboundIndexKey,xConfigVersion,inboundId)

	xDataVal,xErr:=s.GetRedisData(xDataKey)
	if xErr!=nil{
		return xIndexData
	}

	if len(xDataVal)<1{
		return xIndexData
	}

	xErr=cheerlib.TextStructFromJson(&xIndexData,xDataVal)
	if xErr!=nil{
		xIndexData=appmodel.ProxyInboundIndex{}
	}

	return xIndexData

}

func (s *DataService)SetProxyInboundNodeIndex(inboundId string,indexVal appmodel.ProxyInboundIndex,timeSec int64){

	xConfigVersion:=s.GetProxyVersion()
	if len(xConfigVersion)<1{
		return
	}

	xDataKey:=fmt.Sprintf("%s:%s:%s",xRedisProxyInboundIndexKey,xConfigVersion,inboundId)
	xDataVal:=cheerlib.TextStructToJson(indexVal)

	xErr:=s.SetRedisData(xDataKey,xDataVal,timeSec)

	if xErr!=nil{
		xLogInfo:=fmt.Sprintf("SetProxyInboundNodeIndex with error:%s",xErr.Error())
		cheerlib.LogError(xLogInfo)
		cheerlib.StdError(xLogInfo)
	}

}


func (s *DataService) GetRedisData(dataKey string)(string,error){

	xRedisConn:=s.mRedisPool.Get()
	defer xRedisConn.Close()

	xData,xErr:=redis.String(xRedisConn.Do("GET",dataKey))

	if xErr!=nil{
		return "", errors.New(fmt.Sprintf("redis GET datakey=[%s] with error:%s",dataKey,xErr.Error()))
	}

	return xData,nil
}

func (s *DataService) SetRedisData(dataKey string,dataVal string,timeOutSec int64) error  {

	xRedisConn:=s.mRedisPool.Get()
	defer xRedisConn.Close()

	_,xErr:=xRedisConn.Do("SETEX",dataKey,timeOutSec,dataVal)
	if xErr!=nil{
		return errors.New(fmt.Sprintf("redis SETEX datakey=[%s] with error:%s",dataKey,xErr.Error()))
	}

	return nil
}

func (s *DataService) DelRedisData(dataKey string) error  {

	xRedisConn:=s.mRedisPool.Get()
	defer xRedisConn.Close()

	_,xErr:=xRedisConn.Do("DEL",dataKey)
	if xErr!=nil{
		return errors.New(fmt.Sprintf("redis DEL datakey=[%s] with error:%s",dataKey,xErr.Error()))
	}

	return nil
}

