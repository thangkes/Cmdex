package conn

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalShellRunsACommand(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real shell")
	}
	var mu sync.Mutex
	var out bytes.Buffer
	closed := make(chan struct{})
	s, err := OpenLocal(100, 30, Handlers{
		Data:   func(b []byte) { mu.Lock(); out.Write(b); mu.Unlock() },
		Closed: func(error) { close(closed) },
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // shell start-up
	s.Write([]byte("echo cmdex-$((40+2))-ok\r"))
	if strings.Contains(strings.ToLower(shellName()), "powershell") || strings.Contains(strings.ToLower(shellName()), "pwsh") {
		s.Write([]byte("echo ('cmdex-' + (40+2) + '-ok')\r"))
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := strings.Contains(out.String(), "cmdex-42-ok")
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	got := out.String()
	mu.Unlock()
	if !strings.Contains(got, "cmdex-42-ok") {
		t.Fatalf("output: %q", got)
	}
	s.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Closed was not reported")
	}
}

func shellName() string { n, _ := LocalShell(); return n }
