// 状态机
// - 状态比喻成 数据结构
// - 事件比喻成 用户输入
// - 状态转移则是函数调用
// 如此依赖写成函数，也就是
// type FSM struct {
// 	State 			FSMState
// 	TransformFuncs  map[FSMState] func()
// }

// func (f *FSM) UserAction(action FSMAction) {
// 	...
// }

package main

import (
	"os"
	"sync"

	log "github.com/cihub/seelog"
)

// Only 4 states now is enough
var (
	Running   = FSMState("running")
	Stopped   = FSMState("stopped")
	Fatal     = FSMState("fatal")
	RetryWait = FSMState("retry wait")
	Stopping  = FSMState("stopping")

	StartEvent   = FSMEvent("start")
	StopEvent    = FSMEvent("stop")
	RestartEvent = FSMEvent("restart")
)

type FSMState string
type FSMEvent string
type FSMHandler func()

type FSM struct {
	mu       sync.Mutex
	state    FSMState
	handlers map[FSMState]map[FSMEvent]FSMHandler

	StateChange func(oldState, newState FSMState)
}

func (f *FSM) AddHandler(state FSMState, event FSMEvent, hdlr FSMHandler) *FSM {
	_, ok := f.handlers[state]
	if !ok {
		f.handlers[state] = make(map[FSMEvent]FSMHandler)
	}

	if _, ok = f.handlers[state][event]; ok {
		log.Criticalf("set twice for state(%s) event(%s) exit", state, event)
		os.Exit(-1)
	}

	f.handlers[state][event] = hdlr
	return f
}

func (f *FSM) State() FSMState {
	return f.state
}

func (f *FSM) SetState(newState FSMState) {
	if f.StateChange != nil {
		f.StateChange(f.state, newState)
	}
	f.state = newState
}

func (f *FSM) Operate(event FSMEvent) FSMState {
	f.mu.Lock()
	defer f.mu.Unlock()

	eventMap := f.handlers[f.State()]
	if eventMap == nil {
		return f.State()
	}
	if fn, ok := eventMap[event]; ok {
		fn()
	}
	return f.State()
}

func NewFSM(initState FSMState) *FSM {
	return &FSM{
		state:    initState,
		handlers: make(map[FSMState]map[FSMEvent]FSMHandler),
	}
}
