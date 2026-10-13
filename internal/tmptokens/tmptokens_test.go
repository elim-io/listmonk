package tmptokens

import (
	"testing"
	"time"
)

func count() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(tokens)
}

func TestNoCleanerUnlessStarted(t *testing.T) {
	// Importing the package starts no timer: on Cloud Run, Clean() is called
	// by Cloud Scheduler. An expired token stays until Clean() runs.
	Set("expired", time.Millisecond, 1)
	time.Sleep(20 * time.Millisecond)
	if count() != 1 {
		t.Fatal("the expired token was removed without Clean()")
	}

	Clean()
	if count() != 0 {
		t.Fatal("Clean() kept the expired token")
	}
}

func TestStartCleaner(t *testing.T) {
	Set("expired", time.Millisecond, 1)
	Set("live", time.Hour, 2)

	StartCleaner(10 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	if _, err := Get("live"); err != nil {
		t.Fatal("the cleaner removed a live token")
	}
	mu.RLock()
	_, ok := tokens["expired"]
	mu.RUnlock()
	if ok {
		t.Fatal("the cleaner kept the expired token")
	}
}
