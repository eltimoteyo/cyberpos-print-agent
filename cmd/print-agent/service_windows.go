//go:build windows

package main

import (
	"fmt"
	"log"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

type printAgentService struct{}

func (s *printAgentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	stopCh := make(chan struct{})
	go runAgent(stopCh)

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				close(stopCh)
				changes <- svc.Status{State: svc.StopPending}
				return false, 0
			default:
			}
		}
	}
}

func runAsService() bool {
	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatalf("failed to detect session type: %v", err)
	}
	return !isInteractive
}

func runService(name string, isDebug bool) {
	evtlog, err := eventlog.Open(name)
	if err != nil {
		// Try to register the source if it doesn't exist
		_ = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
		evtlog, _ = eventlog.Open(name)
	}
	if evtlog != nil {
		defer evtlog.Close()
		_ = evtlog.Info(1, fmt.Sprintf("%s service starting", name))
	}

	err = svc.Run(name, &printAgentService{})
	if err != nil {
		log.Fatalf("%s service failed: %v", name, err)
	}
}
