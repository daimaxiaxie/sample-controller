package signals

import (
	"context"
	"os"
	"os/signal"
)

var onlyOneHandler = make(chan struct{})

func SetupSignalHandler() context.Context {
	// panic when called twice
	close(onlyOneHandler)

	ch := make(chan os.Signal, 2)
	ctx, cancel := context.WithCancel(context.Background())
	signal.Notify(ch, shutdownSignals...)
	go func() {
		<-ch
		cancel()
		<-ch
		os.Exit(1)
	}()
	return ctx
}
