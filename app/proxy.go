package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/chwjbn/cheer-socks/cheerlib"
	"github.com/chwjbn/cheer-socks/config"
	"github.com/chwjbn/trojanx"
	"github.com/chwjbn/trojanx/metadata"
	"github.com/chwjbn/trojanx/protocol"
	"github.com/chwjbn/xclash/adapter/outbound"
	"github.com/chwjbn/xclash/constant"
	"github.com/chwjbn/xclash/transport/socks5"
	"io"
	"net"
	"path"
	"sync"
)

type ProxyApp struct {
	mConfig *config.ConfigApp
}

func RunApp(cfg *config.ConfigApp) error  {

	var xError error
	proxyApp:=new(ProxyApp)
	proxyApp.mConfig=cfg


	xError=proxyApp.runService()

    return xError
}

func (this *ProxyApp) runService() error {

	var xError error

	xError=this.runTrojanService()

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
			Host:   "www.baidu.com",
			Port:   80,
		},
	}

	xServer:=trojanx.New(context.Background(),&xServerCnf)

	xServer.ConnectHandler = func(ctx context.Context) bool {
		return true
	}

	xServer.AuthenticationHandler = func(ctx context.Context, hash string) bool {
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


	xLogInfo:=fmt.Sprintf("++++++++begin srcConnCtx from=[%s] to=[%s]",srcMeta.SourceAddress(),srcMeta.RemoteAddress())
	cheerlib.LogInfo(xLogInfo)
	cheerlib.StdInfo(xLogInfo)


	xNextProxyOpt:=outbound.Socks5Option{}
	xNextProxyOpt.UDP=false
	xNextProxyOpt.SkipCertVerify=true

	xNextProxyOpt.Server="dreambali.abc123.vip"
	xNextProxyOpt.Port=65534
	xNextProxyOpt.UserName="64e6ece473a8056df8de728f"
	xNextProxyOpt.Password=""

	//xNextProxyOpt.Server="127.0.0.1"
	//xNextProxyOpt.Port=10808
	//xNextProxyOpt.UserName=""
	//xNextProxyOpt.Password=""

	xNextProxy:=outbound.NewSocks5(xNextProxyOpt)

	xNextConn,xNextConnErr:=xNextProxy.DialContext(context.Background(),srcMeta)

	if xNextConnErr!=nil{
		cheerlib.LogError(fmt.Sprintf("NextProxy.DialContext Error:%s",xNextConnErr.Error()))
		return
	}

	this.processConnRelay(xNextConn,srcConn)

	xLogInfo=fmt.Sprintf("--------end srcConnCtx from=[%s] to=[%s]",srcMeta.SourceAddress(),srcMeta.RemoteAddress())
	cheerlib.LogInfo(xLogInfo)
	cheerlib.StdInfo(xLogInfo)

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

