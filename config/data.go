package config

import "errors"

type ConfigApp struct {
	AppGroup string `yaml:"app_group"`
	AppName string `yaml:"app_name"`

	ServerAddr string `yaml:"server_addr"`
	ServerPort int `yaml:"server_port"`

    CacheRedisAddr string  `yaml:"cache_redis_addr"`
	CacheRedisUsername string  `yaml:"cache_redis_username"`
	CacheRedisPassword string  `yaml:"cache_redis_password"`
	CacheRedisDb int  `yaml:"cache_redis_db"`

	SkyapmOapGrpcAddr string `yaml:"skyapm_oap_grpc_addr"`
}


func (this *ConfigApp) Check() error {

	var xError error

	if len(this.AppGroup)<1{
		this.AppGroup="cheer-arch"
	}

	if len(this.AppName)<1{
		this.AppGroup="cheer-socks"
	}

	if len(this.ServerAddr)<1{
		this.AppGroup="0.0.0.0"
	}

	if this.ServerPort<1||this.ServerPort>65535{
		this.ServerPort=10080
	}


	if len(this.CacheRedisAddr) < 1 {
		xError = errors.New("invalid config node=[cache_redis_addr]")
		return xError
	}


	return xError

}