package main

import (
	"errors"
	"fmt"
	"github.com/chwjbn/cheer-proxy-http/app"
	"github.com/chwjbn/cheer-proxy-http/cheerapp"
	"github.com/chwjbn/cheer-proxy-http/cheerlib"
	"github.com/chwjbn/cheer-proxy-http/config"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

var (
	gConfig *config.ConfigApp=nil
)

func loadConfig() error  {

	var xError error

	configFilePath := path.Join(cheerlib.ApplicationBaseDirectory(),"config", "config.yml")
	if !cheerlib.FileExists(configFilePath) {
		xError = errors.New("Lost Config File:" + configFilePath)
		return xError
	}

	var cfg config.ConfigApp

	//从文件中读取配置
	xError = config.ReadConfigFromFile(configFilePath, &cfg)
	if xError != nil {
		return xError
	}

	//从环境变量中读取配置
	xError = config.ReadConfigFromEnv(&cfg)
	if xError != nil {
		return xError
	}

	xError = cfg.Check()
	if xError != nil {
		return xError
	}

	gConfig=&cfg


	//设置应用信息
	cheerlib.SetGlobalAppInfo(gConfig.AppName, fmt.Sprintf("%s service",gConfig.AppName))

	// 最长日志保留时间
	cheerlib.SetGlobalCheerLogFileMaxAge(48 * time.Hour)

	return xError

}

func AppWork() error {

	var xError error

	if len(gConfig.SkyapmOapGrpcAddr)>0{
		os.Setenv("SKYAPM_APP_NAME", fmt.Sprintf("%s::%ss", gConfig.AppGroup,gConfig.AppName))
		os.Setenv("SKYAPM_OAP_GRPC_ADDR", gConfig.SkyapmOapGrpcAddr)
		cheerapp.StartSkyapm()
	}

	xError=app.RunApp(gConfig)

	if xError!=nil{
		cheerlib.StdError(fmt.Sprintf("app.RunApp with error:%s",xError.Error()))
	}


	return xError

}

func runApp()  {

	runtime.GOMAXPROCS(runtime.NumCPU())

	xServiceMgr, xServiceMgrErr := cheerapp.CreateServiceMgr(cheerlib.GetGlobalAppName(), cheerlib.GetGlobalAppDescription(), cheerlib.GetGlobalAppDescription(), AppWork)
	if xServiceMgrErr != nil {
		cheerlib.LogError("xServiceMgrErr=" + xServiceMgrErr.Error())
	}

	xRunArgs := os.Args

	if len(xRunArgs) > 1 {
		xRunArg := xRunArgs[1]
		if strings.Contains(xRunArg, "install") {
			xServiceMgr.Install()
			return
		}

		if strings.Contains(xRunArg, "remove") {
			xServiceMgr.StopService()
			xServiceMgr.Uninstall()
			return
		}
	}

	xServiceMgr.RunService()

}

func main()  {

	xError:=loadConfig()

	if xError!=nil{

		cheerlib.StdError(xError.Error())
		return
	}

	cheerlib.StdInfo(fmt.Sprintf("==================================================%s begin==================================================", cheerlib.GetGlobalAppDescription()))

	runApp()

	cheerlib.StdInfo(fmt.Sprintf("==================================================%s end==================================================", cheerlib.GetGlobalAppDescription()))
}