package shared

import (
	"os"
	"os/signal"
	"syscall"
)

// SignalHandler manages signal forwarding with force-kill on second signal.
// Returns a cleanup function.
// The onFirst callback receives the first SIGINT/SIGTERM.
// The onSecond callback is called on the second signal (force-kill).
func SignalHandler(onFirst func(os.Signal), onSecond func()) (cleanup func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigCount := 0
		for sig := range sigCh {
			sigCount++
			if sigCount >= 2 {
				onSecond()
				return
			}
			onFirst(sig)
		}
	}()
	return func() { signal.Stop(sigCh) }
}
