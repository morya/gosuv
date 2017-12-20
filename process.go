package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/cihub/seelog"
	"github.com/codeskyblue/kexec"
	"github.com/kennygrant/sanitize"
)

type Process struct {
	*FSM       `json:"-"`
	Program    `json:"program"`
	cmd        *kexec.KCommand
	Stdout     *QuickLossBroadcastWriter `json:"-"`
	Stderr     *QuickLossBroadcastWriter `json:"-"`
	Output     *QuickLossBroadcastWriter `json:"-"`
	OutputFile *os.File                  `json:"-"`
	stopC      chan syscall.Signal
	retryLeft  int
	Status     string `json:"status"`

	mu sync.Mutex
}

// FIXME(ssx): maybe need to return error
func (p *Process) buildCommand() *kexec.KCommand {
	cmd := kexec.CommandString(p.Command)
	logDir := filepath.Join(Cfg.Server.Log.LogPath, sanitize.Name(p.Name))
	if !IsDir(logDir) {
		os.MkdirAll(logDir, 0755)
	}

	var err error
	var foutOut, foutErr io.Writer

	outFile := filepath.Join(logDir, "output.log")

	if p.LogDisable {
		log.Infof("disabled stdout and stderr log")
		foutOut = ioutil.Discard
		foutErr = ioutil.Discard
	} else {
		p.OutputFile, err = os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Warnf("[%s] create stdout log failed: %+v", p.Name, err)
			foutOut = ioutil.Discard
			foutErr = ioutil.Discard
		} else {
			if p.StderrOnly {
				log.Infof("[%s] disabled stdout log", p.Name)
				foutOut = ioutil.Discard
			} else {
				foutOut = p.OutputFile
			}
			foutErr = p.OutputFile
		}
	}

	cmd.Stdout = io.MultiWriter(p.Stdout, p.Output, foutOut)
	cmd.Stderr = io.MultiWriter(p.Stderr, p.Output, foutErr)

	cmd.Env = os.Environ()
	environ := map[string]string{}
	if p.User != "" {
		if !IsRoot() {
			log.Warnf("[%s] detect not root, can not switch user", p.Name)
		} else if err := cmd.SetUser(p.User); err != nil {
			log.Warnf("[%s] change user to %s failed, %v", p.Name, p.User, err)
		} else {
			var homeDir string
			switch runtime.GOOS {
			case "linux":
				// FIXME(ssx): maybe there is a better way
				homeDir = "/home/" + p.User
			case "darwin":
				homeDir = "/Users/" + p.User
			}
			cmd.Env = append(cmd.Env, "HOME="+homeDir, "USER="+p.User)
			environ["HOME"] = homeDir
			environ["USER"] = p.User
		}
	}
	cmd.Env = append(cmd.Env, p.Environ...)
	mapping := func(key string) string {
		val := os.Getenv(key)
		if val != "" {
			return val
		}
		return environ[key]
	}
	cmd.Dir = os.Expand(p.Dir, mapping)
	if strings.HasPrefix(cmd.Dir, "~") {
		cmd.Dir = mapping("HOME") + cmd.Dir[1:]
	}
	log.Infof("[%s] use dir: %s", p.Name, cmd.Dir)
	return cmd
}

func (p *Process) waitNextRetry() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SetState(RetryWait)
	if p.retryLeft <= 0 {
		p.retryLeft = p.StartRetries
		p.SetState(Fatal)
		return
	}
	p.retryLeft -= 1
	select {
	case <-time.After(2 * time.Second): // TODO: need put it into Program
		log.Warnf("[%s] retry start program,left times: %+v", p.Name, p.retryLeft)
		p.startCommand()
	case <-p.stopC:
		log.Infof("[%s] try to  stop command", p.Name)
		p.stopCommand()
	}

}

func (p *Process) resetRetry() {
	timer := time.NewTimer(time.Minute * 1)
	<-timer.C
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.State() == Running && p.retryLeft < p.StartRetries {
		log.Tracef("[%s] reset retry from %+v to %+v", p.Name, p.retryLeft, p.StartRetries)
		p.retryLeft = p.StartRetries
	}
}

func (p *Process) stopCommand() {

	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.SetState(Stopped)

	if p.cmd == nil {
		log.Infof("[%s] not found command", p.Name)
		return
	}

	p.SetState(Stopping)

	if p.cmd.Process != nil {
		p.cmd.Process.Signal(syscall.SIGTERM)
	}

	select {
	case <-GoFunc(p.cmd.Wait):
		log.Infof("[%s] program quit normally", p.Name)
	case <-time.After(time.Duration(p.StopTimeout) * time.Second):
		log.Infof("[%s] program terminate all", p.Name)
		p.cmd.Terminate(syscall.SIGKILL)
	}

	err := p.cmd.Wait()

	prefixStr := "\n--- GOSUV LOG " + time.Now().Format("2006-01-02 15:04:05")
	if err == nil {
		io.WriteString(p.cmd.Stderr, fmt.Sprintf("%s exit success ---\n\n", prefixStr))
	} else {
		io.WriteString(p.cmd.Stderr, fmt.Sprintf("%s exit fail %v ---\n\n", prefixStr, err))
	}
	if p.OutputFile != nil {
		p.OutputFile.Close()
		p.OutputFile = nil
	}
	p.cmd = nil
}

func (p *Process) IsRunning() bool {
	return p.State() == Running || p.State() == RetryWait
}

func (p *Process) startCommand() {

	log.Infof("[%s] start cmd: %s", p.Name, p.Command)
	p.cmd = p.buildCommand()

	p.SetState(Running)
	log.Tracef("[%s] state is %v", p.Name, p.Status)
	if err := p.cmd.Start(); err != nil {
		log.Warnf("[%s] program start failed: %v", p.Name, err)
		p.SetState(Fatal)
		return
	}

	//重置retry次数
	go p.resetRetry()

	go func() {
		errC := GoFunc(p.cmd.Wait)
		startTime := time.Now()
		select {
		case <-errC:
			// if p.cmd.Wait() returns, it means program and its sub process all quited. no need to kill again
			// func Wait() will only return when program session finish.
			log.Warnf("[%s] program finished, time used %v", p.Name, time.Since(startTime))
			if time.Since(startTime) < time.Duration(p.StartSeconds)*time.Second {
				if p.retryLeft == p.StartRetries { // If first time quit so fast, just set to fatal
					log.Infof("[%s] program exit too quick, sleep 100ms", p.Name)
					time.Sleep(time.Microsecond * 100)
				}
			}
			p.waitNextRetry()
		case <-p.stopC:
			log.Infof("[%s] recv stop command", p.Name)
			p.stopCommand()
		}
	}()
}

func NewProcess(pg Program) *Process {
	outputBufferSize := 24 * 1024 // 24K
	pr := &Process{
		FSM:       NewFSM(Stopped),
		Program:   pg,
		stopC:     make(chan syscall.Signal),
		retryLeft: pg.StartRetries,
		Status:    string(Stopped),
		Output:    NewQuickLossBroadcastWriter(outputBufferSize),
		Stdout:    NewQuickLossBroadcastWriter(outputBufferSize),
		Stderr:    NewQuickLossBroadcastWriter(outputBufferSize),
	}
	pr.StateChange = func(_, newStatus FSMState) {
		pr.Status = string(newStatus)
		// TODO: status need to filter with config, not hard coded.
		if newStatus == Fatal {
			go pr.Program.RunNotification()
		}
	}
	if pr.StartSeconds <= 0 {
		pr.StartSeconds = 2
	}
	if pr.StopTimeout <= 0 {
		pr.StopTimeout = 3
	}

		pr.AddHandler(Stopped, StartEvent, func() {
		pr.retryLeft = pr.StartRetries
		pr.startCommand()
	})
	pr.AddHandler(Fatal, StartEvent, pr.startCommand)

	pr.AddHandler(Running, StopEvent, func() {
		select {
		case pr.stopC <- syscall.SIGTERM:
		case <-time.After(200 * time.Millisecond):
		}
	}).AddHandler(Running, RestartEvent, func() {
		go func() {
			pr.Operate(StopEvent)
			// TODO: start laterly ?
			time.Sleep(1 * time.Second)
			pr.Operate(StartEvent)
		}()
	})
	return pr
}
