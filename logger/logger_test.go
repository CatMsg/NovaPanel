package logger

import (
	"fmt"
	"sync"
	"testing"

	"github.com/op/go-logging"
)

func TestLoggerExistsBeforeExplicitInitialization(t *testing.T) {
	if GetLogger() == nil {
		t.Fatal("bootstrap logger is nil")
	}
}

func TestLogBufferConcurrentAccessAndLimit(t *testing.T) {
	InitLogger(logging.ERROR)
	logBufferMu.Lock()
	logBuffer = nil
	logBufferMu.Unlock()

	const writers = 8
	const messages = 40
	var waitGroup sync.WaitGroup
	waitGroup.Add(writers)
	for writer := range writers {
		go func() {
			defer waitGroup.Done()
			for message := range messages {
				addToBuffer("INFO", fmt.Sprintf("%d-%d", writer, message))
				_ = GetLogs(10, "info")
			}
		}()
	}
	waitGroup.Wait()

	if got := len(GetLogs(25, "info")); got != 25 {
		t.Fatalf("GetLogs returned %d rows, want 25", got)
	}
	if got := len(GetLogs(10_000, "info")); got != writers*messages {
		t.Fatalf("GetLogs returned %d rows, want %d available rows", got, writers*messages)
	}
}
