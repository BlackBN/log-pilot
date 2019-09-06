package main

import (
	"os/exec"
	"syscall"
	"time"

	"github.com/caicloud/nirvana/log"
)

const (
	waitingTime = 60
)

type AsyncCmd struct {
	cmd      *exec.Cmd
	waitDone chan struct{}
	finished bool
}

func WrapCmd(cmd *exec.Cmd) *AsyncCmd {
	return &AsyncCmd{
		cmd:      cmd,
		waitDone: make(chan struct{}),
		finished: false,
	}
}

func (ac *AsyncCmd) Start() error {
	if err := ac.cmd.Start(); err != nil {
		return err
	}

	go func(ac *AsyncCmd) {
		ac.cmd.Wait()
		close(ac.waitDone)
		ac.finished = true
	}(ac)

	return nil
}

func (ac *AsyncCmd) Stop() error {
	log.Infoln("Send TERM signal")
	if err := ac.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	select {
	case <-ac.waitDone:
		return nil
	case <-time.After(waitingTime * time.Second):
		log.Infoln("Kill Process")
		if err := ac.cmd.Process.Kill(); err != nil {
			return err
		}
	}

	<-ac.waitDone
	return nil
}

func (ac *AsyncCmd) Exited() bool {
	return ac.finished
}
