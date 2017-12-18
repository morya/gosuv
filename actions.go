package main

import (
	"fmt"
	//"log"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/facebookgo/atomicfile"
	"github.com/goji/httpauth"
	"github.com/urfave/cli"
	"strconv"
)

type Server struct {
	HTTPServer bool
	UnixServer bool
	SockFile   string
	PidFile    string
	Auth       bool
	AuthUser   string
	AuthPasswd string
	IPFile     string
	HTTPAddr   string
}

var (
	errNotConfigured = errors.New("pidfile not configured")
)

func NewSer() Server {
	return Server{
		Cfg.Server.HttpServer.Enabled,
		Cfg.Server.UnixServer.Enabled,
		Cfg.Server.UnixServer.SockFile,
		Cfg.Server.PidFile,
		Cfg.Server.Auth.Enabled,
		Cfg.Server.Auth.User,
		Cfg.Server.Auth.Password,
		Cfg.Server.Auth.IPFile,
		Cfg.Server.HttpServer.Addr,
	}
}
func actionStartServer(c *cli.Context) error {

	foregroud := c.Bool("foreground")

	ser := NewSer()

	// 检查是否已经有服务启动了.
	if actionServerStatus() == nil {
		return fmt.Errorf("server is already running\n")
	}

	return ser.startServer(foregroud)

}

// start server , HTTP or Unix
func (s *Server) startServer(foregroud bool) error {
	// start http server
	// 依赖的变量
	httpAddr := s.HTTPAddr
	if strings.HasPrefix(s.HTTPAddr, ":") {
		httpAddr = "0.0.0.0" + s.HTTPAddr
	}

	var listenAddr string
	if s.UnixServer {
		listenAddr = s.SockFile
	} else if s.HTTPServer {
		listenAddr = httpAddr
	} else {
		listenAddr = ""
	}

	//日志目录添加
	logC := Cfg.Server.Log
	logFile := filepath.Join(logC.LogPath, DefaultGoSuvLogFile)
	if err := os.MkdirAll(logC.LogPath, os.FileMode(0755)); err != nil {
		log.Criticalf("mkdir log path %s failed. %+v", logC.LogPath, err)
		return err
	}

	syncLogger(logFile, logC.Level, logC.FileMax, logC.Backups)

	//日志设置
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("create file %s failed: %v", logFile, err)
	}

	suv, hdlr, err := newSupervisorHandler()
	if err != nil {
		return err
	}

	//添加密码验证
	if s.Auth {
		hdlr = httpauth.SimpleBasicAuth(s.AuthUser, s.AuthPasswd)(hdlr)
	}

	http.Handle("/", hdlr)

	// 直接启动
	if foregroud {
		log.Info("----------- start server -----------")
		suv.AutoStartPrograms()
		if s.UnixServer {
			unixListener, err := net.Listen("unix", listenAddr)
			if err != nil {
				log.Critical(err)
				return err
			}
			log.Infof("sock file  %v", listenAddr)
			log.Critical(http.Serve(unixListener, nil))
		} else if s.HTTPServer {
			log.Infof("server listen on %v", listenAddr)
			log.Critical(http.ListenAndServe(listenAddr, nil))
		}
		return errors.New("server listen nothing ,exit .")

	} else {
		cmd := exec.Command(os.Args[0], "-c", CfgFile, "start-server", "-f")
		cmd.Stdout = logFd
		cmd.Stderr = logFd

		err = cmd.Start()
		if err != nil {
			return err
		}

		// 写pidfile
		if err := writePidFile(cmd.Process.Pid, s.PidFile); err != nil {
			return fmt.Errorf("write pid file faild. ", s.PidFile, err)
		}

		select {
		case err = <-GoFunc(cmd.Wait):
			return fmt.Errorf("server started failed,check log %s, %v\n", logFile, err)
		case <-time.After(200 * time.Millisecond):
			fmt.Printf("server started, listening  %s\n", listenAddr)
		}
	}
	return nil
}

func writePidFile(pid int, pidfile string) error {
	if pidfile == "" {
		return errNotConfigured
	}

	if err := os.MkdirAll(filepath.Dir(pidfile), os.FileMode(0755)); err != nil {
		return err
	}

	file, err := atomicfile.New(pidfile, os.FileMode(0644))
	if err != nil {
		return fmt.Errorf("error opening pidfile %s: %s", pidfile, err)
	}
	defer file.Close() // in case we fail before the explicit close

	_, err = fmt.Fprintf(file, "%d", pid)
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return nil
}

func syncLogger(logFile string, logLevel string, logFileMax int, logBackups int) {

	var LogConfig = `
<seelog minlevel="` + logLevel + `">
    <outputs formatid="common">
        <rollingfile type="size" filename="` + logFile + `" maxsize="` + strconv.Itoa(logFileMax) + `" maxrolls="` + strconv.Itoa(logBackups) + `"/>
    </outputs>
    <formats>
        <format id="colored"  format="%Time %EscM(46)%Level%EscM(49) %Msg%n%EscM(0)"/>
        <format id="common" format="%Date/%Time [%LEV] [%File:%Line] %Msg%n" />
        <format id="critical" format="%Date/%Time %File:%Line %Func %Msg%n" />
    </formats>
</seelog>
`

	logger, _ := log.LoggerFromConfigAsBytes([]byte(LogConfig))
	log.UseLogger(logger)
}
