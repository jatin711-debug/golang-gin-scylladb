package utils

import (
	"os"
	"os/signal"
	"syscall"
)

// GracefulShutdown listens for termination signals
func GracefulShutdown() <-chan os.Signal {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	return stop
}