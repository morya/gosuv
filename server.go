package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gosuv/gops"

	log "github.com/cihub/seelog"

	"github.com/codeskyblue/kexec"
	"github.com/go-yaml/yaml"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "github.com/shurcooL/vfsgen"
)

//var mutex sync.Mutex

func init() {
	http.Handle("/res/", http.StripPrefix("/res/", http.FileServer(Assets)))
}

type Supervisor struct {
	ConfigDir string

	names   []string // order of programs
	pgMap   map[string]Program
	procMap map[string]*Process
	mu      sync.Mutex
	eventB  *WriteBroadcaster
}

func newSupervisorHandler() (suv *Supervisor, hdlr http.Handler, err error) {
	suv = &Supervisor{
		ConfigDir: CfgDir,
		pgMap:     make(map[string]Program, 0),
		procMap:   make(map[string]*Process, 0),
		eventB:    NewWriteBroadcaster(4 * 1024),
	}
	if err = suv.loadDB(); err != nil {
		return
	}
	suv.catchExitSignal()

	r := mux.NewRouter()
	r.HandleFunc("/", suv.hIndex)
	r.HandleFunc("/settings/{name}", suv.hSetting)

	r.HandleFunc("/api/status", suv.hStatus)
	r.HandleFunc("/api/shutdown", suv.hShutdown).Methods("POST")
	r.HandleFunc("/api/reload", suv.hReload).Methods("POST")

	r.HandleFunc("/api/programs", suv.hGetProgramList).Methods("GET")
	r.HandleFunc("/api/programs/{name}", suv.hGetProgram).Methods("GET")
	r.HandleFunc("/api/programs/{name}", suv.hDelProgram).Methods("DELETE")
	r.HandleFunc("/api/programs/{name}", suv.hUpdateProgram).Methods("PUT")
	r.HandleFunc("/api/programs", suv.hAddProgram).Methods("POST")
	r.HandleFunc("/api/programs/{name}/start", suv.hStartProgram).Methods("POST")
	r.HandleFunc("/api/programs/{name}/stop", suv.hStopProgram).Methods("POST")

	r.HandleFunc("/ws/events", suv.wsEvents)
	r.HandleFunc("/ws/logs/{name}", suv.wsLog)
	r.HandleFunc("/ws/perfs/{name}", suv.wsPerf)

	r.HandleFunc("/webhooks/{name}/{category}", suv.hWebhook).Methods("POST")

	return suv, r, nil
}

func (s *Supervisor) AutoStartPrograms() {
	for _, proc := range s.procMap {
		if proc.Program.StartAuto {
			log.Infof("[%s] auto start", proc.Name)
			proc.Operate(StartEvent)
		}
	}
}

func (s *Supervisor) programs() []Program {
	pgs := make([]Program, 0, len(s.names))
	for _, name := range s.names {
		pgs = append(pgs, s.pgMap[name])
	}
	return pgs
}

func (s *Supervisor) procs() []*Process {
	ps := make([]*Process, 0, len(s.names))
	for _, name := range s.names {
		ps = append(ps, s.procMap[name])
	}
	return ps
}

func (s *Supervisor) programPath() string {
	return filepath.Join(s.ConfigDir, DefaultProgramFile)
}

func (s *Supervisor) newProcess(pg Program) *Process {
	p := NewProcess(pg)
	origFunc := p.StateChange
	p.StateChange = func(oldState, newState FSMState) {
		s.broadcastEvent(fmt.Sprintf("[%s] state: %s -> %s", p.Name, string(oldState), string(newState)))
		origFunc(oldState, newState)
	}

	log.Tracef("new process: %+v", p)
	return p
}

func (s *Supervisor) broadcastEvent(event string) {
	s.eventB.Write([]byte(event))
}

func (s *Supervisor) addStatusChangeListener(c chan string) {
	sChan := s.eventB.NewChanString(fmt.Sprintf("%d", time.Now().UnixNano()))
	go func() {
		for msg := range sChan {
			c <- msg
		}
	}()
}

// Send Stop signal and wait program stops
func (s *Supervisor) stopAndWait(name string) error {
	p, ok := s.procMap[name]
	if !ok {
		return errors.New("no such program")
	}
	if !p.IsRunning() {
		return nil
	}
	c := make(chan string, 0)
	s.addStatusChangeListener(c)
	p.Operate(StopEvent)
	for {
		select {
		case <-c:
			if !p.IsRunning() {
				return nil
			}
		case <-time.After(1 * time.Second): // In case some event not catched
			if !p.IsRunning() {
				return nil
			}
		}
	}
}

// 添加或者更新program
func (s *Supervisor) addOrUpdateProgram(pg Program) error {
	// defer s.broadcastEvent(pg.Name + " add or update")
	if err := pg.Check(); err != nil {
		return err
	}
	origPg, ok := s.pgMap[pg.Name]
	if ok {
		if reflect.DeepEqual(origPg, pg) {
			return nil
		}
		s.broadcastEvent(pg.Name + " update")
		log.Info("update:", pg.Name)
		origProc := s.procMap[pg.Name]
		isRunning := origProc.IsRunning()
		go func() {
			s.stopAndWait(origProc.Name)
			newProc := s.newProcess(pg)
			s.procMap[pg.Name] = newProc
			s.pgMap[pg.Name] = pg
			if isRunning {
				newProc.Operate(StartEvent)
			}
			s.saveDB()
		}()
	} else {
		s.names = append(s.names, pg.Name)
		s.pgMap[pg.Name] = pg
		s.procMap[pg.Name] = s.newProcess(pg)
		s.broadcastEvent(pg.Name + " added")
	}
	return nil
}

// Check
// - Yaml format
// - Duplicated program
func (s *Supervisor) readConfigFromDB() (pgs []Program, err error) {
	data, err := ioutil.ReadFile(s.programPath())
	if err != nil {
		data = []byte("")
	}
	pgs = make([]Program, 0)
	if err = yaml.Unmarshal(data, &pgs); err != nil {
		return nil, err
	}
	visited := map[string]bool{}
	for _, pg := range pgs {
		if visited[pg.Name] {
			return nil, fmt.Errorf("duplicated program name: %s", pg.Name)
		}
		visited[pg.Name] = true
	}
	return
}

func (s *Supervisor) loadDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pgs, err := s.readConfigFromDB()
	if err != nil {
		return err
	}
	// add or update program
	visited := map[string]bool{}
	names := make([]string, 0, len(pgs))
	for _, pg := range pgs {
		names = append(names, pg.Name)
		visited[pg.Name] = true
		s.addOrUpdateProgram(pg)
	}
	s.names = names
	// delete not exists program
	for _, pg := range s.pgMap {
		if visited[pg.Name] {
			continue
		}
		s.removeProgram(pg.Name)
	}
	return nil
}

func (s *Supervisor) saveDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := yaml.Marshal(s.programs())
	if err != nil {
		return err
	}
	return ioutil.WriteFile(s.programPath(), data, 0644)
}

func (s *Supervisor) removeProgram(name string) {
	names := make([]string, 0, len(s.names))
	for _, pName := range s.names {
		if pName == name {
			continue
		}
		names = append(names, pName)
	}
	s.names = names
	log.Infof("stop before delete program: %s", name)
	s.stopAndWait(name)
	delete(s.procMap, name)
	delete(s.pgMap, name)
	s.broadcastEvent(name + " deleted")
}

type WebConfig struct {
	User    string
	Version string
}

func (s *Supervisor) renderHTML(w http.ResponseWriter, name string, data interface{}) {
	file, err := Assets.Open(name + ".html")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	body, _ := ioutil.ReadAll(file)

	if data == nil {
		wc := WebConfig{}
		wc.Version = Version
		user, err := user.Current()
		if err == nil {
			wc.User = user.Username
		}
		data = wc
	}
	w.Header().Set("Content-Type", "text/html")
	template.Must(template.New("t").Delims("[[", "]]").Parse(string(body))).Execute(w, data)
}

type JSONResponse struct {
	Status int         `json:"status"`
	Value  interface{} `json:"value"`
}

func (s *Supervisor) renderJSON(w http.ResponseWriter, data JSONResponse) {
	w.Header().Set("Content-Type", "application/json")
	bytes, _ := json.Marshal(data)
	w.Write(bytes)
}

func (s *Supervisor) hIndex(w http.ResponseWriter, r *http.Request) {
	s.renderHTML(w, "index", nil)
}

func (s *Supervisor) hSetting(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	s.renderHTML(w, "setting", map[string]string{
		"Name": name,
	})
}

func (s *Supervisor) hStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(map[string]interface{}{
		"status": 0,
		"value":  "server is running",
	})
	w.Write(data)
}

func (s *Supervisor) hShutdown(w http.ResponseWriter, r *http.Request) {

	//s.CloseAndCleanWithLock()

	s.Close()
	s.CleanFile()

	s.renderJSON(w, JSONResponse{
		Status: 0,
		Value:  "gosuv server has been shutdown",
	})
	go func() {
		time.Sleep(500 * time.Microsecond)
		os.Exit(0)
	}()
}

func (s *Supervisor) hReload(w http.ResponseWriter, r *http.Request) {
	err := s.loadDB()
	log.Info("reload config file")
	if err == nil {
		s.renderJSON(w, JSONResponse{
			Status: 0,
			Value:  "load config success",
		})
	} else {
		s.renderJSON(w, JSONResponse{
			Status: 1,
			Value:  err.Error(),
		})
	}
}

func (s *Supervisor) hGetProgramList(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.procs())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Supervisor) hGetProgram(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	proc, ok := s.procMap[name]
	if !ok {
		s.renderJSON(w, JSONResponse{
			Status: 1,
			Value:  "program not exists",
		})
		return
	} else {
		s.renderJSON(w, JSONResponse{
			Status: 0,
			Value:  proc,
		})
	}
}

func (s *Supervisor) hAddProgram(w http.ResponseWriter, r *http.Request) {
	retries, err := strconv.Atoi(r.FormValue("retries"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	pg := Program{
		Name:         r.FormValue("name"),
		Command:      r.FormValue("command"),
		Dir:          r.FormValue("dir"),
		User:         r.FormValue("user"),
		StartAuto:    r.FormValue("autostart") == "on",
		StartRetries: retries,
	}
	if pg.Dir == "" {
		pg.Dir = "/"
	}
	if err := pg.Check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var data []byte
	if _, ok := s.pgMap[pg.Name]; ok {
		data, _ = json.Marshal(map[string]interface{}{
			"status": 1,
			"error":  fmt.Sprintf("Program %s already exists", strconv.Quote(pg.Name)),
		})
	} else {
		if err := s.addOrUpdateProgram(pg); err != nil {
			data, _ = json.Marshal(map[string]interface{}{
				"status": 1,
				"error":  err.Error(),
			})
		} else {
			s.saveDB()
			data, _ = json.Marshal(map[string]interface{}{
				"status": 0,
			})
		}
	}
	w.Write(data)
}

func (s *Supervisor) hUpdateProgram(w http.ResponseWriter, r *http.Request) {
	// name := mux.Vars(r)["name"]
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	pg := Program{}
	err := json.NewDecoder(r.Body).Decode(&pg)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": 1,
			"error":  err.Error(),
		})
		return
	}
	err = s.addOrUpdateProgram(pg)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": 2,
			"error":  err.Error(),
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      0,
		"description": "program updated",
	})
}

func (s *Supervisor) hDelProgram(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	w.Header().Set("Content-Type", "application/json")
	var data []byte
	if _, ok := s.pgMap[name]; !ok {
		data, _ = json.Marshal(map[string]interface{}{
			"status": 1,
			"error":  fmt.Sprintf("Program %s not exists", strconv.Quote(name)),
		})
	} else {
		s.removeProgram(name)
		s.saveDB()
		data, _ = json.Marshal(map[string]interface{}{
			"status": 0,
		})
	}
	w.Write(data)
}

func (s *Supervisor) hStartProgram(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	proc, ok := s.procMap[name]
	var data []byte
	if !ok {
		data, _ = json.Marshal(map[string]interface{}{
			"status": 1,
			"error":  fmt.Sprintf("Process %s not exists", strconv.Quote(name)),
		})
	} else {
		proc.Operate(StartEvent)
		data, _ = json.Marshal(map[string]interface{}{
			"status": 0,
			"name":   name,
		})
	}
	w.Write(data)
}

func (s *Supervisor) hStopProgram(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	proc, ok := s.procMap[name]
	var data []byte
	if !ok {
		data, _ = json.Marshal(map[string]interface{}{
			"status": 1,
			"error":  fmt.Sprintf("Process %s not exists", strconv.Quote(name)),
		})
	} else {
		proc.Operate(StopEvent)
		data, _ = json.Marshal(map[string]interface{}{
			"status": 0,
			"name":   name,
		})
	}
	w.Write(data)
}

func (s *Supervisor) hWebhook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name, category := vars["name"], vars["category"]
	proc, ok := s.procMap[name]
	if !ok {
		http.Error(w, fmt.Sprintf("proc %s not exist", strconv.Quote(name)), http.StatusForbidden)
		return
	}
	hook := proc.Program.WebHook
	if category == "github" {
		gh := hook.Github
		_ = gh.Secret
		isRunning := proc.IsRunning()
		s.stopAndWait(name)
		go func() {
			cmd := kexec.CommandString(hook.Command)
			cmd.Dir = proc.Program.Dir
			cmd.Stdout = proc.Output
			cmd.Stderr = proc.Output
			err := GoTimeout(cmd.Run, time.Duration(hook.Timeout)*time.Second)
			if err == ErrGoTimeout {
				cmd.Terminate(syscall.SIGTERM)
			}
			if err != nil {
				log.Warnf("webhook command error: %v", err)
				// Trigger pushover notification
			}
			if isRunning {
				proc.Operate(StartEvent)
			}
		}()
		io.WriteString(w, "success triggered")
	} else {
		log.Warnf("unknown webhook category: %v", category)
	}
}

var upgrader = websocket.Upgrader{}

func (s *Supervisor) wsEvents(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Info("upgrade:", err)
		return
	}
	defer c.Close()

	ch := make(chan string, 0)
	s.addStatusChangeListener(ch)
	go func() {
		_, _ = <-ch // ignore the history messages
		for message := range ch {
			// Question: type 1 ?
			c.WriteMessage(1, []byte(message))
		}
		// s.eventB.RemoveListener(ch)
	}()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Info("read:", mt, err)
			break
		}
		log.Info("recv: %v %s", mt, message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Info("write:", err)
			break
		}
	}
}

func (s *Supervisor) wsLog(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	log.Info(name)
	proc, ok := s.procMap[name]
	if !ok {
		log.Info("No such process")
		// TODO: raise error here?
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Info("upgrade:", err)
		return
	}
	defer c.Close()

	for data := range proc.Output.NewChanString(r.RemoteAddr) {
		err := c.WriteMessage(1, []byte(data))
		if err != nil {
			proc.Output.CloseWriter(r.RemoteAddr)
			break
		}
	}
}

// Performance
func (s *Supervisor) wsPerf(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Info("upgrade:", err)
		return
	}
	defer c.Close()

	name := mux.Vars(r)["name"]
	proc, ok := s.procMap[name]
	if !ok {
		log.Info("No such process")
		// TODO: raise error here?
		return
	}
	for {
		// c.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if proc.cmd == nil || proc.cmd.Process == nil {
			log.Info("process not running")
			return
		}
		pid := proc.cmd.Process.Pid
		ps, err := gops.NewProcess(pid)
		if err != nil {
			break
		}
		mainPinfo, err := ps.ProcInfo()
		if err != nil {
			break
		}
		pi := ps.ChildrenProcInfo(true)
		pi.Add(mainPinfo)

		err = c.WriteJSON(pi)
		if err != nil {
			break
		}
		time.Sleep(700 * time.Millisecond)
	}
}

func (s *Supervisor) Close() {
	for _, proc := range s.procMap {
		if err := s.stopAndWait(proc.Name); err != nil {
			log.Warnf("[%s] program stop  failed.", proc.Name)
		} else {
			log.Infof("[%s] program stop  successed.", proc.Name)
		}
	}

	log.Info("all server closed")
}

func (s *Supervisor) catchExitSignal() {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM)
	go func() {
		for sig := range sigC {
			if sig == syscall.SIGHUP {
				log.Info("Receive SIGHUP, just ignore")
				continue
			}

			//if sig == syscall.SIGINT {
			//	log.Info("Receive SIGINT, just ignore")
			//	continue
			//}

			log.Infof("Receive signal: %v, stopping all running process", sig)
			//	s.CloseAndCleanWithLock()
			s.Close()
			s.CleanFile()
			time.Sleep(500 * time.Microsecond)
			os.Exit(0)
		}
	}()
}

func (s *Supervisor) CleanFile() {
	//删除sock file
	if _, err := os.Stat(Cfg.Server.UnixServer.SockFile); !os.IsNotExist(err) {
		if err := os.Remove(Cfg.Server.UnixServer.SockFile); err != nil {
			log.Info("exit... remove sock file failed. %+v", err)
		}
	} else {
		log.Info(Cfg.Server.UnixServer.SockFile, " not found.")
	}

	//删除pid file
	if _, err := os.Stat(Cfg.Server.PidFile); !os.IsNotExist(err) {
		if err := os.Remove(Cfg.Server.PidFile); err != nil {
			log.Infof("exit... remove pid file failed. %+v", err)
		}
	} else {
		log.Infof("exit... remove pid file %s not found. %+v", Cfg.Server.PidFile, err)
	}
}
