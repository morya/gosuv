# gosuv

gosuv是golang版的supervisor的进程管理程序,类似python版的superviosr. gosuv有安装部署简单,使用便利等优点. 

wiki地址: 

## 当前版本 

```
gosuv version 201712041615
```

## 功能列表

* [x] 命令行
    * [x] server控制
        * [x] 启动 start-server
        * [x] 关闭 shutdown
        * [x] 查看状态 status-server
        * [x] 重启 restart-server
        * [x] 杀进程 kill 
        * [x] 重载 reload 
        * [x] 配置检查 conftest
    * [x] programs控制
        * [x] 启动 start <program name>
        * [x] 停止 stop <program name>
        * [x] 编辑 edit 
        * [x] 状态 status
* [x] web页面控制
  * [x] Start, Stop, Tail, Reload
  * [x] Realtime log
	* [x] Add program support
	* [x] Edit support
	* [x] Delete support
	* [x] Memory and CPU monitor
* [x] 日志管理
    * [x] gosuv server日志
    * [x] programs标准/错误输出日志
    * [x] gosuv server日志切割
    * [ ] programs 日志切割
* [x] HTTP Server
* [x] Unix Sock Server
* [x] 基本的用户密码验证
* [x] 静态文件编译入bin
* [ ] 授权ip列表
* [ ] shell界面
* [x] 平滑关闭
* [ ] 首次启动失败,边界处理

## 总体介绍 

gosuv是一个集server和client一体的命令行. server又分为HTTP和Unix两种分别对应web和sock管理两种方式. 并且提供了web操作界面. 

主要的功能是为管理进程提供守护服务,当被管理的服务异常退出的时候再尝试拉起.



## Quick start

### 编译安装

```
cd ${git dir}

bash hack/build.sh
```
编译成功将gosuv文件放置到bin目录下


### 默认启动服务

```
$./gosuv start-server
server started, listening  .gosuv.sock.
```

在当前没有config的情况下会产生默认的config.yml在当前目录.默认使用sock的管理方式. 

### 查看server状态


```
$./gosuv status-server
server is running
```

表示服务提供正常

### 添加Programs

默认没有programs的配置文件. 

创建programs.yml到当前目录.

```
- name: redis-test # programs的名字唯一
  command: redis-server --port 6679
  environ: []
  directory: /tmp
  start_auto: true     #代表gosuv启动的时候默认启动该进程
  start_retries: 3  # 1分钟内的重启次数, 1分钟内重启成功,会重新计数. 所以不建议设置太大 如果太大容易造成永远retry. 还有优化的空间.
  user: work  #指定用户启动, 但是非root不用指定用户
  redirect_stderr: true  # 把 stderr 重定向到 stdout，默认 false
  log_disable: false # 是否禁用屏幕输出 默认为false ,如果标准输出和错误输出太多可以关闭.
```

PS: programs的日志没有切割功能,所以如果标准输出内容太多,可以使用log_disable : true 关闭

### 启动program

重新加载配置

```
$ ./gosuv reload
load config success
```

查看状态
```
$ ./gosuv status
PROGRAM NAME           	STATUS
redis-test             	running
```

### 关闭program

```
$ ./gosuv stop redis-test
```

## 高级用法

### 开发使用场景

* 开发使用场景,可以开启HTTP WEB的方式. 

* 可以在web上面添加programs

* 每个gosuv可以管理多个进程. 

### 线上服务场景

* 建议使用unix server的方式.(减少端口占用) 

* 1个gosuv管理一个进程服务. 

* 可以添加授权等操作. 

### 配置文件说明

```
include: ./conf/programs.yml #指定programs文件, 这个版本不支持. 当前版本还是默认和主配置文件同一个目录,文件名programs.yml固定 
server:
  httpserver:        ## http api 
    enabled: false   ## 是否启用 如果httpserver启动优先级大于unixserver
    addr: :11333     ## ip:port, :port的意思是bind all 0.0.0.0
  unixserver:        ## unix api
    enabled: true    ## 默认启动 
    sockfile: .gosuv.sock  ## sock file位置,默认当前目录.gosuv.sock
  auth:              ## 权限
    enabled: true    ## 是否启动
    username: abc    ## 用户名
    password: abc    ## 密码
    ipfile: ""       ## ip授权列表 这版本暂时未支持
  pidfile: .gosuv.pid  ## gosuv pid文件,默认当前目录.gosuv.pid
  log:
    logpath: logs  ## 日志存在目录 会存储gosuv.log 和各个programs(被管理进程的屏幕输出)
    level: info      ## 日志级别
    filemax: 10000    ## 每个日志文件大小
    backups: 10      ## 切割保留的日志数量
  minfds: 1024       ## 可以打开的文件描述符的最小值 暂不支持
  minprocs: 1024     ## 可以打开的进程数的最小值 暂不支持
client:              ## client配置, 可以独立于server使用和配置. 
  server_url: unix://.gosuv.sock ## url的配置 两种格式 unix://file.sock 和http://ip:port 例如:  unix:///tmp/gosuv.sock 或者http://127.0.0.1:8181 与server的方式相对应
  username: abc      ## server要求的用户名
  password: abc      ## server要求的密码
```

PS: programs的日志没有切割功能,这里的日志切割配置只管理了gosuv.log本身的日志

### 命令行说明

```
$./gosuv -h
NAME:
   gosuv - golang supervisor

USAGE:
   gosuv [global options] command [command options] [arguments...]

VERSION:
   201711232023

AUTHOR:
   op

COMMANDS:
     start-server       Start supervisor and run in background 启动gosuv 并放到后台,如果要在前台使用,可以添加 -f 
     status, st         Show program status  查看programs的状态
     status-server      Show server status   查看server的状态
     start              Start program
     stop               Stop program
     reload             Reload config file
     shutdown           Shutdown server    优雅关闭,会先关闭programs再退出.
     kill               kill stop server by pid file.  kill进程通过pid
     restart-server     restart server    重启server
     conftest, t        Test if config file is valid
     edit               Edit config file  
     version, v         Show version
     help, h            Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --conf value, -c value  config file (default: "config.yml")
   --help, -h              show help
   --version, -v           print the version

```
### web界面管理

![gosuv web](docs/gosuv.gif)

### 静态文件编译

./hack/install.sh

可以将res下面的静态文件编译到bin文件中.这样安装部署只有一个bin文件,不需要单独部署静态文件. 

## 注意事项

####  kill与kill -9 
    
1. kill <gosuv pid> 默认发送的是SIGQUIT指令,可以被gosuv获取到信号,所以会平滑的退出所有托管的进程.
2. kill -9 <gosuv pid> 发送的是SIGKILL指令,是不可以被gosuv获取到信息,所以所有托管的进程会被系统托管,gosuv本身退出. 进程服务可能还能正常提供服务. 

Linux Singal http://colobu.com/2015/10/09/Linux-Signals/

####  配置修改时间点

1. 如果gosuv正在提供服务,修改了其中的client的连接方式等会导致无法正常使用API或者cmd. 所以建议shutdown后再进行配置的修改再启动生效.

#### 重启次数

重启次数是在一分钟内的次数,如果超过一分钟,重启次数会进行重置.所以不建议一分钟类重启次数过多,可能会导致无限重启的情况,因为重启后的每隔1分钟就会被重置. 

## Design

HTTP is follow the RESTFul guide.

Get or Update program

`<GET|PUT> /api/programs/:name`

Add new program

`POST /api/programs`

Del program

`DELETE /api/programs/:name`

## State

Only 4 states. [ref](http://supervisord.org/subprocess.html#process-states)

## 声明

代码重构源于github.com/codeskyblue/gosuv 有时间提交贡献代码.


