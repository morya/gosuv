package main

import (
	"errors"
	"fmt"
	"os/user"

	"gosuv/pushover"

	log "github.com/cihub/seelog"
)

func (p *Program) Check() error {
	if p.Name == "" {
		return errors.New("Program name empty")
	}
	if p.Command == "" {
		return errors.New("Program command empty")
	}
	return nil
}

func (p *Program) RunNotification() {
	po := p.Notifications.Pushover
	if po.ApiKey != "" && len(po.Users) > 0 {
		for _, user := range po.Users {
			err := pushover.Notify(pushover.Params{
				Token:   po.ApiKey,
				User:    user,
				Title:   "gosuv",
				Message: fmt.Sprintf("%s change to fatal", p.Name),
			})
			if err != nil {
				log.Warnf("pushover error: %v", err)
			}
		}
	}
}

func IsRoot() bool {
	u, err := user.Current()
	return err == nil && u.Username == "root"
}
