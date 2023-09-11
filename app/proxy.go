package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/chwjbn/cheer-socks/app/appservice"
	"github.com/chwjbn/cheer-socks/cheerlib"
	"github.com/chwjbn/cheer-socks/config"
	"github.com/chwjbn/trojanx"
	"github.com/chwjbn/trojanx/metadata"
	"github.com/chwjbn/trojanx/protocol"
	"github.com/chwjbn/xclash/adapter/outbound"
	"github.com/chwjbn/xclash/constant"
	"github.com/chwjbn/xclash/transport/socks5"
	"github.com/gin-gonic/gin"
	"io"
	"net"
	"path"
	"sync"
)

type ProxyApp struct {
	mConfig *config.ConfigApp
	mDataSvc *appservice.DataService
}

func RunApp(cfg *config.ConfigApp) error  {

	var xError error
	proxyApp:=new(ProxyApp)
	proxyApp.mConfig=cfg

	proxyApp.mDataSvc,xError=appservice.NewDataService(appservice.DataCacheConfig{
		CacheRedisAddr: cfg.CacheRedisAddr,
		CacheRedisUser: cfg.CacheRedisUser,
		CacheRedisPwd: cfg.CacheRedisPwd,
		CacheRedisDb: cfg.CacheRedisDb,
	})

	if xError!=nil{
		return xError
	}

	xError=proxyApp.runService()

    return xError
}

func (this *ProxyApp)onWebApiIndex(ctx *gin.Context)  {
	ctx.Redirect(301,"https://www.baidu.com")
}

func (this *ProxyApp)onWebApiOpenAccountSub(ctx *gin.Context)  {

	xAccToken,_:=ctx.GetQuery("token")
	if len(xAccToken)<1{
		ctx.String(200,"invalid request")
		return
	}

	xAccData,xErr:=this.mDataSvc.GetProxyAccount(xAccToken)
	if xErr!=nil{
		ctx.String(200,"invalid request")
		return
	}

	ctx.String(200,xAccData.SubUrlData)
}

func (this *ProxyApp) runService() error {

	var xError error

	xError=this.runWebApiService()
	if xError!=nil{
		return xError
	}

	xError=this.runTrojanService()

	return xError
}

func (this *ProxyApp)runWebApiService() error  {

	var xError error

	gin.SetMode(gin.DebugMode)
	xRouter := gin.Default()
	xRouter.SetTrustedProxies([]string{"127.0.0.1"})
	xRouter.GET("/open/account/sub",this.onWebApiOpenAccountSub)
	xRouter.GET("/",this.onWebApiIndex)

	xServerHostPort:=fmt.Sprintf("%s:%d",this.mConfig.HttpServerAddr,this.mConfig.HttpServerPort)
	go func() {
		xRouter.Run(xServerHostPort)
	}()

	return xError

}

func (this *ProxyApp) runTrojanService() error  {

	var xError error

	xCertFilePath := path.Join(cheerlib.ApplicationBaseDirectory(),"config", "cert.pem")
	if !cheerlib.FileExists(xCertFilePath) {
		xError = errors.New("Lost cert File:" + xCertFilePath)
		return xError
	}

	xKeyFilePath := path.Join(cheerlib.ApplicationBaseDirectory(),"config", "key.pem")
	if !cheerlib.FileExists(xCertFilePath) {
		xError = errors.New("Lost key File:" + xKeyFilePath)
		return xError
	}

	xServerCnf:=trojanx.Config{
		Host: this.mConfig.ServerAddr,
		Port: this.mConfig.ServerPort,
		TLSConfig: &trojanx.TLSConfig{
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
			CertificateFiles: []trojanx.CertificateFileConfig{
				{PublicKeyFile: xCertFilePath, PrivateKeyFile: xKeyFilePath},
			},
		},
		ReverseProxyConfig: &trojanx.ReverseProxyConfig{
			Scheme: "http",
			Host:   "127.0.0.1",
			Port:   this.mConfig.HttpServerPort,
		},
	}

	xServer:=trojanx.New(context.Background(),&xServerCnf)

	xServer.ConnectHandler = func(ctx context.Context) bool {
		return true
	}

	xServer.AuthenticationHandler = func(ctx context.Context, hash string) bool {

		if len(hash)<1{
			return false
		}

		xAccData,xErr:=this.mDataSvc.GetProxyInbound(hash)
		if xErr!=nil{
			return false
		}

		if len(xAccData.InboundId)<1{
			return false
		}

		return true
	}

	xServer.ErrorHandler = func(ctx context.Context, err error) {
		cheerlib.LogError(fmt.Sprintf("Trojan Server With Error:%s",err.Error()))
	}
	
	xServer.ForwardHandler= func(ctx context.Context, hash string, request protocol.Request) error {

		var xError error

		xSrcMeta:=metadata.FromContext(ctx)

		xSrcAddrHost,xSrcAddrPort,xSrcAddrErr:=net.SplitHostPort(xSrcMeta.RemoteAddr.String())

		if xSrcAddrErr!=nil{
			xError=errors.New("error src address")
			return xError
		}


		xDesMeta:=constant.Metadata{
			NetWork: constant.TCP,
			Type: constant.SOCKS5,
			SrcIP: net.ParseIP(xSrcAddrHost),
			SrcPort: xSrcAddrPort,
			DstPort: fmt.Sprintf("%d",request.DescriptionPort),

		}


		if request.AddressType==protocol.AddressTypeIPv4{
			xDesMeta.AddrType=socks5.AtypIPv4
			xDesMeta.DstIP=net.ParseIP(request.DescriptionAddress)
			xDesMeta.DNSMode=constant.DNSNormal
		}

		if request.AddressType==protocol.AddressTypeDomain{
			xDesMeta.AddrType=socks5.AtypDomainName
			xDesMeta.Host=request.DescriptionAddress
			xDesMeta.DNSMode=constant.DNSMapping
		}

		if request.AddressType==protocol.AddressTypeIPv6{
			xDesMeta.AddrType=socks5.AtypIPv6
			xDesMeta.DstIP=net.ParseIP(request.DescriptionAddress)
			xDesMeta.DNSMode=constant.DNSNormal
		}

		this.processSocks5Conn(&xDesMeta,xSrcMeta.SrcConn,hash)

		return xError

	}

	xServerErr:=xServer.Run()
	xError=errors.New(fmt.Sprintf("Trojan Server Run With Error:%s",xServerErr.Error()))

	return xError
}

func (this *ProxyApp)processSocks5Conn(srcMeta *constant.Metadata,srcConn net.Conn,userHash string)  {

	xLogInfo:=""

	xNode:=this.mDataSvc.GetProxyNodeByInboundPwd(userHash)

	if len(xNode.NodeId)<1{
		xLogInfo=fmt.Sprintf("!!!!!!!!!!no route for srcConnCtx userHash=[%s] from=[%s] to=[%s]",userHash,srcMeta.SourceAddress(),srcMeta.RemoteAddress())
		cheerlib.LogError(xLogInfo)
		cheerlib.StdError(xLogInfo)
		return
	}

	xLogInfo=fmt.Sprintf("@@@@@@@@@@route srcConnCtx userHash=[%s] from=[%s] to=[%s] to node=[%s]",userHash,srcMeta.SourceAddress(),srcMeta.RemoteAddress(),xNode.NodeId)
	cheerlib.LogInfo(xLogInfo)
	cheerlib.StdInfo(xLogInfo)

	xNextProxyOpt:=outbound.Socks5Option{}
	xNextProxyOpt.UDP=false
	xNextProxyOpt.SkipCertVerify=true

	xNextProxyOpt.Server=xNode.ServerAddr
	xNextProxyOpt.Port=xNode.ServerPort
	xNextProxyOpt.UserName=xNode.Username
	xNextProxyOpt.Password=xNode.Password

	xNextProxy:=outbound.NewSocks5(xNextProxyOpt)

	xNextConn,xNextConnErr:=xNextProxy.DialContext(context.Background(),srcMeta)

	if xNextConnErr!=nil{
		cheerlib.LogError(fmt.Sprintf("NextProxy.DialContext Error:%s",xNextConnErr.Error()))
		return
	}

	this.processConnRelay(xNextConn,srcConn)
}

func (this *ProxyApp)processConnRelay(localConn net.Conn,remoteConn net.Conn)  {

	var xWaitGroup sync.WaitGroup
	xWaitGroup.Add(2)

	go func() {
		defer xWaitGroup.Done()
		io.Copy(localConn, remoteConn)
	}()

	go func() {
		defer xWaitGroup.Done()
		io.Copy(remoteConn, localConn)
	}()

	xWaitGroup.Wait()

	localConn.Close()
	remoteConn.Close()
}

