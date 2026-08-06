package service

import (
	"sync"
	"testing"
)

func TestDataVersionIsMonotonicUnderConcurrentUpdates(t *testing.T) {
	lastUpdate.Store(0)
	initial := CurrentDataVersion()

	const updates = 32
	var waitGroup sync.WaitGroup
	waitGroup.Add(updates)
	for range updates {
		go func() {
			defer waitGroup.Done()
			markDataUpdated()
		}()
	}
	waitGroup.Wait()

	if got := CurrentDataVersion(); got <= initial {
		t.Fatalf("version = %d, want greater than %d", got, initial)
	}
}
