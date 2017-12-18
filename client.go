package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	log "github.com/cihub/seelog"
	_ "github.com/shurcooL/vfsgen"
	"github.com/urfave/cli"
)

type Client struct {
	UnixClient  bool
	HTTPClient  bool
	Addr        string
	User        string
	Password    string
	Action      map[string]ActionMap
	ProgramFile string
	UnixHTTP    http.Client
}

type ActionMap struct {
	Uri    string
	Method string
}

func NewClient() *Client {

	unixServer := strings.HasPrefix(Cfg.Client.ServerURL, "unix://")
	httpServer := strings.HasPrefix(Cfg.Client.ServerURL, "http://")
	if !(unixServer || httpServer) {
		fmt.Printf("Please check client configure, ex: unix://%s or http://ip:port\n", DefaultSockFile)
		log.Criticalf("client config is error , %v\n", Cfg.Client.ServerURL)
		os.Exit(-1)
	}
	clientAddr := strings.Split(Cfg.Client.ServerURL, "//")
	addr := clientAddr[1]

	cl := &Client{
		UnixClient: unixServer,
		HTTPClient: httpServer,
		User:       Cfg.Client.Username,
		Password:   Cfg.Client.Password,
	}

	if CfgDir == "" {
		cl.ProgramFile = filepath.Join("./", DefaultProgramFile)
	} else {
		cl.ProgramFile = filepath.Join(CfgDir, DefaultProgramFile)
	}

	if httpServer {
		addrs := strings.Split(addr, ":")
		ipAddr := addrs[0]
		portAddr := addrs[1]
		if ipAddr == "" {
			cl.Addr = "http://127.0.0.1" + ":" + portAddr
		}
	} else if unixServer {
		//sockfile
		cl.Addr = "http://unix"
		sockFile := addr
		if sockFile == "" {
			sockFile = DefaultSockFile
		}
		cl.UnixHTTP = http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", sockFile)
				},
			},
		}
	} else {
		fmt.Println("notice: only can use 'kill' command")
	}

	//注册Client的方法
	// <actions> <rui> <method>
	actions := make(map[string]ActionMap)
	actions["shutdown"] = ActionMap{Uri: "/api/shutdown", Method: "POST"}
	actions["reload"] = ActionMap{Uri: "/api/reload", Method: "POST"}

	actions["status"] = ActionMap{Uri: "/api/status", Method: "GET"}
	actions["addProgram"] = ActionMap{Uri: "/api/programs", Method: "POST"}
	actions["getProgramStatus"] = ActionMap{Uri: "/api/programs", Method: "GET"}

	actions["getProgram"] = ActionMap{Uri: "/api/programs/", Method: "GET"}
	actions["delProgram"] = ActionMap{Uri: "/api/programs/", Method: "DELETE"}
	actions["programs"] = ActionMap{Uri: "/api/programs/", Method: "POST"}

	cl.Action = actions

	return cl
}

/*
programs相关指令
*/

// server status
func actionStatus(c *cli.Context) error {

	request, _ := http.NewRequest(cl.Action["status"].Method, cl.Addr+cl.Action["status"].Uri, nil)
	request.SetBasicAuth(cl.User, cl.Password)

	var resp *http.Response
	var err error
	if cl.UnixClient {
		resp, err = cl.UnixHTTP.Do(request)
	} else {
		resp, err = http.DefaultClient.Do(request)
	}
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var serverStatus JSONResponse
	err = json.Unmarshal(body, &serverStatus)
	if err != nil {
		return errors.New("json loads error: " + string(body))
	}

	if serverStatus.Status != 0 {
		return fmt.Errorf("server status is %+v,%+v\n", serverStatus.Status, serverStatus.Value)
	}
	fmt.Println(serverStatus.Value)
	return nil
}

//programs status
func actionProgramStatus(c *cli.Context) error {

	var programs = make([]struct {
		Program Program `json:"program"`
		Status  string  `json:"status"`
	}, 0)

	request, _ := http.NewRequest(cl.Action["getProgramStatus"].Method, cl.Addr+cl.Action["getProgramStatus"].Uri, nil)
	request.SetBasicAuth(cl.User, cl.Password)

	var resp *http.Response
	var err error

	if cl.UnixClient {
		resp, err = cl.UnixHTTP.Do(request)
	} else {
		resp, err = http.DefaultClient.Do(request)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(body, &programs)
	if err != nil {
		return errors.New("json loads error: " + string(body))
	}

	format := "%-23s\t%-8s\n"
	fmt.Printf(format, "PROGRAM NAME", "STATUS")
	for _, p := range programs {
		fmt.Printf(format, p.Program.Name, p.Status)
	}
	return nil
}

func actionStart(c *cli.Context) (err error) {

	name := c.Args().First()

	success, err := programOperate("start", name)
	if err != nil {
		return
	}
	if success {
		fmt.Println(name, "Started")
	} else {
		fmt.Println(name, "Start failed")
	}
	return nil
}

func actionStop(c *cli.Context) (err error) {
	name := c.Args().First()
	success, err := programOperate("stop", name)
	if err != nil {
		return
	}
	if !success {
		fmt.Println(name, "Stop failed")
	}
	return nil
}

// cmd: <start|stop>
func programOperate(cmd, name string) (success bool, err error) {

	request, _ := http.NewRequest(cl.Action["programs"].Method, cl.Addr+cl.Action["programs"].Uri+name+"/"+cmd, nil)
	request.SetBasicAuth(Cfg.Server.Auth.User, Cfg.Server.Auth.Password)

	var resp *http.Response
	if cl.UnixClient {
		resp, err = cl.UnixHTTP.Do(request)
	} else {
		resp, err = http.DefaultClient.Do(request)
	}
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var v = struct {
		Status int `json:"status"`
	}{}

	err = json.Unmarshal(body, &v)
	if err != nil {
		return false, errors.New("json loads error: " + string(body))
	}

	success = v.Status == 0
	return success, nil
}

/*
gosuv server相关操作指令
*/

func actionShutdown(c *cli.Context) error {

	ret, err := postForm(cl.Addr+cl.Action["shutdown"].Uri, nil)

	if err != nil {
		return err
	}
	fmt.Println(ret.Value)
	return nil
}

func actionRestart(c *cli.Context) error {

	fmt.Println("shutdown server...")

	actionShutdown(c)

	fmt.Println("start server..")

	ser := NewSer()

	ser.startServer(false)

	return nil
}

func actionReload(c *cli.Context) error {
	ret, err := postForm(cl.Addr+cl.Action["reload"].Uri, nil)
	if err != nil {
		return err
	}
	fmt.Println(ret.Value)
	return nil
}

/*
	命令行
*/

//查看gosuv版本
func actionVersion(c *cli.Context) error {
	fmt.Printf("gosuv version %s\n", Version)
	return nil
}

//测试配置文件
func actionConfigTest(c *cli.Context) error {
	if _, _, err := newSupervisorHandler(); err != nil {
		return err
	}
	fmt.Println("test is successful")
	return nil
}

//编辑programs.yml配置文件
func actionEdit(c *cli.Context) error {
	cmd := exec.Command("vim", cl.ProgramFile)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

//查看gosuv server状态 使用server的配置.
func actionServerStatus() error {

	request, _ := http.NewRequest("GET", cl.Addr+"/api/status", nil)
	request.SetBasicAuth(Cfg.Server.Auth.User, Cfg.Server.Auth.Password)

	var resp *http.Response
	var err error
	if cl.UnixClient {
		resp, err = cl.UnixHTTP.Do(request)
	} else if cl.HTTPClient {
		resp, err = http.DefaultClient.Do(request)
	} else {
		return fmt.Errorf("no client configure %+v\n", cl)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var ret JSONResponse
	err = json.Unmarshal(body, &ret)
	if err != nil {
		return errors.New("json loads error: " + string(body))
	}
	if ret.Status != 0 {
		return fmt.Errorf("%v", ret.Value)
	}
	return nil
}

func postForm(urlPath string, data url.Values) (r *JSONResponse, err error) {

	request, err := http.NewRequest("POST", urlPath, strings.NewReader(data.Encode()))
	if err != nil {
		return r, err
	}
	request.SetBasicAuth(cl.User, cl.Password)

	var resp *http.Response

	if cl.UnixClient {
		resp, err = cl.UnixHTTP.Do(request)
	} else {
		resp, err = http.DefaultClient.Do(request)
	}
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(body, &r)
	if err != nil {
		return r, fmt.Errorf("POST %v %v", strconv.Quote(urlPath), string(body))
	}
	return r, nil
}

// 从pid file读取pid进行kill操作.
func actionKill(c *cli.Context) error {

	//if actionServerStatus() != nil {
	//	fmt.Println("server is not running.")
	//	return nil
	//}

	pidFile := Cfg.Server.PidFile

	if Cfg.Server.PidFile == "" {
		pidFile = DefaultPidFile
	}

	fi, err := os.Open(pidFile)
	if err != nil {
		return err
	}

	defer fi.Close()

	fd, err := ioutil.ReadAll(fi)
	if err != nil {
		return err
	}

	pid, err := strconv.Atoi(string(fd))
	if err != nil {
		return err
	}

	if err := syscall.Kill(pid, syscall.SIGQUIT); err != nil {
		return err
	}

	return nil

}
