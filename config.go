package main

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/go-yaml/yaml"
)


type Configuration struct {
	Include string `yaml:"include"`
	Server GosuvServer `yaml:"server"`
	Client GosuvClient `yaml:"client"`
}

type GosuvServer struct {
	HttpServer HttpServer `yaml:"httpserver"`
	UnixServer UnixServer `yaml:"unixserver"`
	Auth       Auth  `yaml:"auth"`
	PidFile string `yaml:"pidfile"`
	Log     GosuvLog `yaml:"log"`
	MinFds	int `yaml:"minfds"`
	MinProcs int `yaml:"minprocs"`
}

type GosuvClient struct {
	ServerURL string `yaml:"server_url"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type HttpServer struct {
	Enabled  bool `yaml:"enabled"`
	Addr     string `yaml:"addr"`
}

type Auth struct {
	Enabled  bool   `yaml:"enabled"`
	User     string `yaml:"username"`
	Password string `yaml:"password"`
	IPFile   string `yaml:"ipfile"`
}

type UnixServer struct {
	Enabled bool `yaml:"enabled"`
	SockFile string `yaml:"sockfile"`
}

type GosuvLog struct {
	LogPath string `yaml:"logpath"`
	Level string `yaml:"level"`
	FileMax int `yaml:"filemax"`
	Backups  int   `yaml:"backups"`
}

func readConf(filename string) (c Configuration, err error) {
	// initial default value
	// 初始化配置文件 如果config.yml不存在的时候.
	c.Server.HttpServer.Enabled=false
	c.Server.HttpServer.Addr="127.0.0.1:11333"

	c.Server.UnixServer.Enabled=true
	c.Server.UnixServer.SockFile=".gosuv.sock"

	c.Server.Log.LogPath="logs"

	c.Client.ServerURL="unix://.gosuv.sock"
	c.Server.PidFile=".gosuv.pid"

	c.Server.Log.Backups=7
	c.Server.Log.Level="info"
	c.Server.Log.FileMax=10000

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		data = []byte("")
	}
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return
	}
	cfgDir := filepath.Dir(filename)
	if !IsDir(CfgDir) {
		os.MkdirAll(cfgDir, 0755)
	}
	data, _ = yaml.Marshal(c)
	err = ioutil.WriteFile(filename, data, 0640)


	return
}


/*
include: ./conf/programs.yml
# include主要是Programs的配置 看是一个文件还是多个文件.
server:
  httpserver:
    enabled: true
    addr: :11313
    httpauth:
      enabled: true
      username: abc
      password: abc
      ipfile: ./allow.list
  unix_http_server:
    enabled: true
    sockfile: ./.gosuv.sock
  pidfile: ./.gosuv.pid
  log:
    logpath: ./logs  # gosuv服务的日志会输出到 gosuv.log programs的日志会按program的名字存储在该目录中.
    level: info  # 只对gosuv服务的日志有效. program的日志是stdout stderr的日志内容.
    filemax: 50MB
    backups: 10
  minfds: 1024
  minprocs: 1024
client:
  server_url: http://:11313
  # 添加http://或者sock://做为不同的client方式.
  username:
  password:
 */
