package app

import (
	"fmt"
	"github.com/chwjbn/cheer-proxy-http/app/appservice"
	"github.com/chwjbn/cheer-proxy-http/cheerlib"
	"github.com/chwjbn/cheer-proxy-http/config"
	"github.com/gin-gonic/gin"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"strings"
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


func (this *ProxyApp)onProxyRequest(ctx *gin.Context)  {

	xRequest:=ctx.Request

	xAuthVal:=xRequest.Header.Get("Proxy-Authorization")
	if len(xAuthVal)<1{
		ctx.String(http.StatusProxyAuthRequired,"please provide correct auth info(251).")
		return
	}

	xAuthUser:=cheerlib.WebHttpBasicAuthDecode(xAuthVal)
	if xAuthUser==nil{
		ctx.String(http.StatusProxyAuthRequired,"please provide correct auth info(252).")
		return
	}

	cheerlib.StdInfo(fmt.Sprintf("Auth(Username=[%s],Password=[%s])",xAuthUser.Username,xAuthUser.Password))

	xProxyAcc,xProxyAccErr:=this.mDataSvc.GetProxyAccount(xAuthUser.Username)
	if xProxyAccErr!=nil||len(xProxyAcc.AccountId)<1{
		ctx.String(http.StatusProxyAuthRequired,"please provide correct auth info(253).")
		return
	}


	xProxyNode,xProxyNodeErr:=this.mDataSvc.GetProxyNode(xAuthUser.Password)
	if xProxyNodeErr!=nil||len(xProxyNode.NodeId)<1{
		ctx.String(http.StatusProxyAuthRequired,"please provide correct auth info(254).")
		return
	}

	xProxyDailer,xProxyDailerErr:=proxy.SOCKS5("tcp",
		fmt.Sprintf("%s:%d",xProxyNode.ServerAddr,xProxyNode.ServerPort),
		&proxy.Auth{User: xProxyNode.Username,Password: xProxyNode.Password},
		proxy.Direct,
	)

	if xProxyDailerErr!=nil{
		ctx.String(http.StatusServiceUnavailable,"backend node busy.")
		return
	}


	xIsKeepAlive:=false
	if strings.EqualFold(strings.ToLower(xRequest.Header.Get("Proxy-Connection")),"keep-alive"){
		xIsKeepAlive=true
	}

	xDestHost := xRequest.Header.Get("Host")
	if len(xDestHost)<1{
		xDestHost=xRequest.URL.Host
	}

	if len(xDestHost)>0{
		xRequest.Host=xDestHost
	}


	xRequest.RequestURI = ""

	removeHopByHopHeaders(xRequest.Header)
	removeExtraHTTPHostPort(xRequest)

    if len(xRequest.Host)<1||len(xRequest.URL.Host)<1{
		ctx.String(http.StatusBadRequest,"invalid request.")
		return
	}

	xSrcConn,_,xSrcConnErr:=ctx.Writer.Hijack()
	if xSrcConnErr!=nil{
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	xLogData:=fmt.Sprintf("Begin HttpMethod=[%s] To xDestHost=[%s]",xRequest.Method,xDestHost)
	cheerlib.StdInfo(xLogData)
	cheerlib.LogInfo(xLogData)

	//HTTPS隧道连接
	if strings.EqualFold(xRequest.Method,http.MethodConnect){

		xTargetConn,xTargetConnErr:=xProxyDailer.Dial("tcp",xDestHost)

		if xTargetConnErr!=nil{
			xRespData:=fmt.Sprintf("HTTP/%d.%d %03d %s\n\n",xRequest.ProtoMajor, xRequest.ProtoMinor, http.StatusBadGateway, "Bad gateway")
			fmt.Fprint(xSrcConn,xRespData)
			xSrcConn.Close()
			return
		}

		xRespData:=fmt.Sprintf("HTTP/%d.%d %03d %s\n\n",xRequest.ProtoMajor, xRequest.ProtoMinor, http.StatusOK, "Connection established")
		fmt.Fprint(xSrcConn,xRespData)

		processConnRelay(xSrcConn,xTargetConn)

		return
	}


	//HTTP连接
	xTransport:=http.Transport{}
	xTransport.Dial=xProxyDailer.Dial

	xHttpClient:=http.Client{Transport: &xTransport}
	xResp,xRespErr:=xHttpClient.Do(xRequest)
	if xRespErr!=nil{
		ctx.String(http.StatusBadGateway,"backend node is busy now.")
		return
	}


	if xIsKeepAlive {
		xResp.Header.Set("Proxy-Connection", "keep-alive")
		xResp.Header.Set("Connection", "keep-alive")
		xResp.Header.Set("Keep-Alive", "timeout=4")
	}

	xResp.Close = !xIsKeepAlive
	xResp.Write(xSrcConn)
	xSrcConn.Close()
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

	localConn.Close()
	remoteConn.Close()
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