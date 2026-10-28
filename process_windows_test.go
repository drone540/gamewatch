//go:build windows

package main

import (
	"sync"
	"testing"
)

// TestScanWindowsRegistersCallbackOnce guards against the callback leak that
// caused "fatal error: too many callback functions". scanWindows runs every few
// seconds, so registering a new Go callback for each scan eventually exhausts
// the runtime's callback table.
func TestScanWindowsRegistersCallbackOnce(t *testing.T) {
	oldOnce := enumWindowsCallbackOnce
	oldAddr := enumWindowsCallbackAddr
	oldNew := newWindowsCallback
	t.Cleanup(func() {
		enumWindowsCallbackOnce = oldOnce
		enumWindowsCallbackAddr = oldAddr
		newWindowsCallback = oldNew
	})

	enumWindowsCallbackOnce = new(sync.Once)
	enumWindowsCallbackAddr = 0

	registrations := 0
	newWindowsCallback = func(fn any) uintptr {
		registrations++
		return oldNew(fn)
	}

	for i := 0; i < 5; i++ {
		if _, _, _ = scanWindows(); activeWindowScan != nil {
			t.Fatal("scan state was not cleared after EnumWindows")
		}
	}

	if registrations != 1 {
		t.Fatalf("callback registered %d times, want 1", registrations)
	}
	if enumWindowsCallbackAddr == 0 {
		t.Fatal("callback address was not stored")
	}
}
