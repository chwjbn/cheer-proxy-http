package app

import (
	"errors"
	"fmt"
	"github.com/chwjbn/cheer-proxy-http/app/appservice"
	"github.com/chwjbn/cheer-proxy-http/cheerlib"
	"github.com/chwjbn/cheer-proxy-http/config"
	"github.com/gin-gonic/gin"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type ProxyNextContext struct {
	NextDailer proxy.Dialer
	KeepAlive bool
	TargetHostPort string
}

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


func (this *ProxyApp)onSubRequest(ctx *gin.Context)  {

	xToken:=ctx.DefaultQuery("token","")

    xUrl:=fmt.Sprintf("https://vernus.abc123.vip/xapi/api/proxy/sub-http?token=%s",xToken)

    xHttpClient:=http.Client{}

    xResp,xRespErr:=xHttpClient.Get(xUrl)

    if xRespErr!=nil{
    	ctx.String(http.StatusGone,"system busy.")
		return
	}

	io.Copy(ctx.Writer,xResp.Body)
}

func (this *ProxyApp)getProxyNextContext(ctx *gin.Context) (*ProxyNextContext,error)  {

	var xError error

	xRequest:=ctx.Request

	xAuthVal:=xRequest.Header.Get("Proxy-Authorization")
	if len(xAuthVal)<1{
		xError=errors.New(fmt.Sprintf("Request From=[%s] Lost Header=[Proxy-Authorization]",xRequest.RemoteAddr))
		return nil,xError
	}

	xAuthUser:=cheerlib.WebHttpBasicAuthDecode(xAuthVal)
	if xAuthUser==nil{
		xError=errors.New(fmt.Sprintf("Request From=[%s] With Invalid Proxy HttpBasicAuth",xRequest.RemoteAddr))
		return nil,xError
	}

	cheerlib.StdInfo(fmt.Sprintf("Request From=[%s] With Auth(username=[%s],password=[%s])",xRequest.RemoteAddr,xAuthUser.Username,xAuthUser.Password))

	xProxyAcc,xProxyAccErr:=this.mDataSvc.GetProxyAccount(xAuthUser.Username)
	if xProxyAccErr!=nil||len(xProxyAcc.AccountId)<1{
		xError=errors.New(fmt.Sprintf("Request From=[%s] With Invalid Proxy Username",xRequest.RemoteAddr))
		return nil,xError
	}

	xProxyNode,xProxyNodeErr:=this.mDataSvc.GetProxyNode(xAuthUser.Password)
	if xProxyNodeErr!=nil||len(xProxyNode.NodeId)<1{
		xError=errors.New(fmt.Sprintf("Request From=[%s] With Invalid Proxy Password",xRequest.RemoteAddr))
		return nil,xError
	}

	xProxyDailer,xProxyDailerErr:=proxy.SOCKS5("tcp",
		fmt.Sprintf("%s:%d",xProxyNode.ServerAddr,xProxyNode.ServerPort),
		&proxy.Auth{User: xProxyNode.Username,Password: xProxyNode.Password},
		proxy.Direct,
	)

	if xProxyDailerErr!=nil{
		xError=errors.New(fmt.Sprintf("Request From=[%s] With ProxyDailerError:%s",xRequest.RemoteAddr,xProxyDailerErr.Error()))
		return nil,xError
	}

	xProxyNextContext:=ProxyNextContext{}
	xProxyNextContext.NextDailer=xProxyDailer

	xProxyNextContext.KeepAlive=false
	if strings.EqualFold(strings.ToLower(xRequest.Header.Get("Proxy-Connection")),"keep-alive"){
		xProxyNextContext.KeepAlive=true
	}

	xDestHostPort := xRequest.Header.Get("Host")
	if len(xDestHostPort)<1{
		xDestHostPort=xRequest.URL.Host
	}

	var xDefaultPort uint64=80
	if strings.EqualFold(xRequest.URL.Scheme, "https") {
		xDefaultPort=443
	}

	xDestHostPort,xParseErr:=ParseHostPort(xDestHostPort,xDefaultPort)
	if xParseErr!=nil{
		xError=errors.New(fmt.Sprintf("Request From=[%s] With HostPort ParseError:%s",xRequest.RemoteAddr,xParseErr.Error()))
		return nil,xError
	}

	xProxyNextContext.TargetHostPort=xDestHostPort

	return &xProxyNextContext,nil
}


//处理HTTPS
func (this *ProxyApp)processProxyConnectHttpRequest(ctx *gin.Context)  {

	xProxyDailer,xProxyDailerErr:=this.getProxyNextContext(ctx)
	if xProxyDailerErr!=nil{
		cheerlib.LogWarn(xProxyDailerErr.Error())
		cheerlib.StdInfo(xProxyDailerErr.Error())
		ctx.AbortWithStatus(http.StatusProxyAuthRequired)
		return
	}

	xRequest:=ctx.Request

	cheerlib.StdInfo(fmt.Sprintf("processProxyConnectHttpRequest Request From=[%s] getProxyNextContext KeepAlive=[%v] Target=[%s]",
		ctx.Request.RemoteAddr,
		xProxyDailer.KeepAlive,
		xProxyDailer.TargetHostPort))

	xTargetConn,xTargetConnErr:=xProxyDailer.NextDailer.Dial("tcp",xProxyDailer.TargetHostPort)
	if xTargetConnErr!=nil{

		cheerlib.StdInfo(fmt.Sprintf("processProxyConnectHttpRequest Request From=[%s] With NextDailer Error:%s",ctx.Request.RemoteAddr,xTargetConnErr.Error()))
		ctx.AbortWithStatus(http.StatusBadGateway)
		return
	}

	xSrcConn,_,xSrcConnErr:=ctx.Writer.Hijack()
	if xSrcConnErr!=nil{

		cheerlib.StdInfo(fmt.Sprintf("processProxyConnectHttpRequest Request From=[%s] With Hijack Error:%s",ctx.Request.RemoteAddr,xSrcConnErr.Error()))
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}


	xRespData:=fmt.Sprintf("HTTP/%d.%d %03d %s\n\n",xRequest.ProtoMajor, xRequest.ProtoMinor, http.StatusOK, "Connection established")
	fmt.Fprint(xSrcConn,xRespData)

	processConnRelay(xSrcConn,xTargetConn)

	if !xProxyDailer.KeepAlive{
		xSrcConn.Close()
		xTargetConn.Close()
	}
}


//处理HTTP
func (this *ProxyApp)processProxyPlainHttpRequest(ctx *gin.Context)  {

	xProxyDailer,xProxyDailerErr:=this.getProxyNextContext(ctx)
	if xProxyDailerErr!=nil{
		cheerlib.LogWarn(xProxyDailerErr.Error())
		cheerlib.StdInfo(xProxyDailerErr.Error())
		ctx.AbortWithStatus(http.StatusProxyAuthRequired)
		return
	}

	cheerlib.StdInfo(fmt.Sprintf("processProxyPlainHttpRequest Request From=[%s] getProxyNextContext KeepAlive=[%v] Target=[%s]",
		ctx.Request.RemoteAddr,
		xProxyDailer.KeepAlive,
		xProxyDailer.TargetHostPort))

	xRequest:=ctx.Request

	xHttpHost := xRequest.Header.Get("Host")
	if len(xHttpHost)<1{
		xHttpHost=xRequest.URL.Host
	}
	if len(xHttpHost)>0{
		xRequest.Host=xHttpHost
	}

	xRequest.RequestURI = ""
	removeHopByHopHeaders(xRequest.Header)
	removeExtraHTTPHostPort(xRequest)

	if len(xRequest.Host)<1||len(xRequest.URL.Host)<1{
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	//HTTP连接
	xTransport:=http.Transport{}
	xTransport.Dial=xProxyDailer.NextDailer.Dial

	xHttpClient:=http.Client{Transport: &xTransport}
	xResp,xRespErr:=xHttpClient.Do(xRequest)
	if xRespErr!=nil{
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if xProxyDailer.KeepAlive {
		xResp.Header.Set("Proxy-Connection", "keep-alive")
		xResp.Header.Set("Connection", "keep-alive")
		xResp.Header.Set("Keep-Alive", "timeout=4")
	}

	xResp.Close = !xProxyDailer.KeepAlive

	xSrcConn,_,xSrcConnErr:=ctx.Writer.Hijack()
	if xSrcConnErr!=nil{

		cheerlib.StdInfo(fmt.Sprintf("processProxyPlainHttpRequest Request From=[%s] With Hijack Error:%s",ctx.Request.RemoteAddr,xSrcConnErr.Error()))

		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	xResp.Write(xSrcConn)

	if !xProxyDailer.KeepAlive{
		xSrcConn.Close()
	}
}

func (this *ProxyApp)onProxyRequest(ctx *gin.Context)  {

	if strings.EqualFold(ctx.Request.Method,http.MethodConnect){
		this.processProxyConnectHttpRequest(ctx)
		return
	}

	this.processProxyPlainHttpRequest(ctx)
}

func (this *ProxyApp) runService() error {

	var xError error

	xError=this.runWebProxyService()

	return xError
}


func (this *ProxyApp)runWebProxyService() error  {

	var xError error

	xServerHostPort:=fmt.Sprintf("%s:%d",this.mConfig.ServerAddr,this.mConfig.ServerPort)

	gin.SetMode(gin.DebugMode)
	xRouter := gin.Default()
	xRouter.SetTrustedProxies([]string{"127.0.0.1"})

	xRouter.GET("/proxy/sub",this.onSubRequest)
	xRouter.NoRoute(this.onProxyRequest)
	xRouter.NoMethod(this.onProxyRequest)

	go func() {
		xRouter.Run(xServerHostPort)
	}()


	return xError

}

func processConnRelay(localConn net.Conn,remoteConn net.Conn)  {

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
}

// removeHopByHopHeaders remove hop-by-hop header
func removeHopByHopHeaders(header http.Header) {
	// Strip hop-by-hop header based on RFC:
	// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html#sec13.5.1
	// https://www.mnot.net/blog/2011/07/11/what_proxies_must_do

	header.Del("Proxy-Connection")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Authorization")
	header.Del("TE")
	header.Del("Trailers")
	header.Del("Transfer-Encoding")
	header.Del("Upgrade")

	connections := header.Get("Connection")
	header.Del("Connection")
	if len(connections) == 0 {
		return
	}
	for _, h := range strings.Split(connections, ",") {
		header.Del(strings.TrimSpace(h))
	}
}

// removeExtraHTTPHostPort remove extra host port (example.com:80 --> example.com)
// It resolves the behavior of some HTTP servers that do not handle host:80 (e.g. baidu.com)
func removeExtraHTTPHostPort(req *http.Request) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	if pHost, port, err := net.SplitHostPort(host); err == nil && port == "80" {
		host = pHost
	}

	req.Host = host
	req.URL.Host = host
}

func ParseHostPort(rawHost string, defaultPort uint64) (string, error) {
	port := defaultPort
	host, rawPort, err := net.SplitHostPort(rawHost)
	if err != nil {
		if addrError, ok := err.(*net.AddrError); ok && strings.Contains(addrError.Err, "missing port") {
			host = rawHost
		} else {
			return "", err
		}
	} else if len(rawPort) > 0 {
		intPort, err := strconv.ParseUint(rawPort, 0, 16)
		if err != nil {
			return "", err
		}
		port = intPort
	}

	return fmt.Sprintf("%s:%d",host,port), nil
}