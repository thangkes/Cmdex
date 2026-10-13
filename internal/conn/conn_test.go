package conn

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTelnetNegotiationAndData(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		// WILL ECHO, DO NAWS(31), a subnegotiation, then text with an escaped 255
		c.Write([]byte{tIAC, tWILL, optEcho, tIAC, tDO, 31, tIAC, tSB, 24, 1, tIAC, tSE})
		c.Write([]byte("login: \xff\xffok"))
		buf := make([]byte, 64)
		var all []byte
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		for len(all) < 9 {
			n, err := c.Read(buf)
			all = append(all, buf[:n]...)
			if err != nil {
				break
			}
		}
		got <- all
	}()

	var mu sync.Mutex
	var out bytes.Buffer
	s, err := OpenTelnet("127.0.0.1", ln.Addr().(*net.TCPAddr).Port, Handlers{Data: func(b []byte) { mu.Lock(); out.Write(b); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	time.Sleep(200 * time.Millisecond)
	s.Write([]byte("bob\r"))
	srv := <-got
	want := []byte{tIAC, tDO, optEcho, tIAC, tWONT, 31, 'b', 'o', 'b', '\r', '\n'}
	if !bytes.Equal(srv, want) {
		t.Fatalf("server got % x, want % x", srv, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if out.String() != "login: \xffok" {
		t.Fatalf("client shows %q", out.String())
	}
}

func TestTelnetReaderSplitAcrossReads(t *testing.T) {
	// IAC at the end of one read and its verb in the next
	pr, pw := io.Pipe()
	var replies [][]byte
	r := &telnetReader{r: pr, reply: func(b ...byte) { replies = append(replies, b) }}
	go func() {
		pw.Write([]byte{'a', tIAC})
		pw.Write([]byte{tDO, optSGA, 'b'})
		pw.Close()
	}()
	all, _ := io.ReadAll(r)
	if string(all) != "ab" || len(replies) != 1 || !bytes.Equal(replies[0], []byte{tIAC, tWILL, optSGA}) {
		t.Fatalf("%q %v", all, replies)
	}
}

func TestEnter(t *testing.T) {
	if string(Enter([]byte("show ver\r"), "crlf")) != "show ver\r\n" || string(Enter([]byte("a\r"), "lf")) != "a\n" || string(Enter([]byte("a\r"), "cr")) != "a\r" {
		t.Fatal("line endings")
	}
}
