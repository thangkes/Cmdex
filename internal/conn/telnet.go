package conn

import (
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Telnet protocol bytes (RFC 854).
const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tSE   = 240

	optEcho = 1
	optSGA  = 3 // suppress go-ahead
)

type telnet struct {
	conn net.Conn
	wmu  sync.Mutex
	stop once
}

// OpenTelnet connects to host:port (23 by default). Telnet is not encrypted.
func OpenTelnet(host string, port int, h Handlers) (Session, error) {
	if port == 0 {
		port = 23
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 15*time.Second)
	if err != nil {
		return nil, err
	}
	t := &telnet{conn: c}
	r := &telnetReader{r: c, reply: t.reply}
	go pump(r, h, func() { t.Close() })
	return t, nil
}

// Write sends typed bytes: a literal 255 is doubled, and Enter (CR) becomes
// CR LF as the protocol asks.
func (t *telnet) Write(p []byte) (int, error) {
	out := make([]byte, 0, len(p)+4)
	for _, b := range p {
		switch b {
		case tIAC:
			out = append(out, tIAC, tIAC)
		case '\r':
			out = append(out, '\r', '\n')
		default:
			out = append(out, b)
		}
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if _, err := t.conn.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *telnet) reply(b ...byte) {
	t.wmu.Lock()
	t.conn.Write(b)
	t.wmu.Unlock()
}

func (t *telnet) Resize(int, int) error { return nil }
func (t *telnet) Close() error          { return t.stop.do(t.conn.Close) }

// telnetReader removes option negotiation from the stream and answers it:
// the server may echo and suppress go-ahead; everything else is refused.
type telnetReader struct {
	r     io.Reader
	reply func(...byte)
	state int // 0 data, 1 after IAC, 2 after IAC DO/DONT/WILL/WONT, 3 in SB, 4 IAC inside SB
	verb  byte
	buf   []byte
}

func (t *telnetReader) Read(p []byte) (int, error) {
	for {
		if cap(t.buf) < len(p) {
			t.buf = make([]byte, len(p))
		}
		n, err := t.r.Read(t.buf[:len(p)])
		out := 0
		for _, b := range t.buf[:n] {
			switch t.state {
			case 0:
				if b == tIAC {
					t.state = 1
				} else {
					p[out] = b
					out++
				}
			case 1:
				switch b {
				case tIAC:
					p[out] = tIAC
					out++
					t.state = 0
				case tDO, tDONT, tWILL, tWONT:
					t.verb, t.state = b, 2
				case tSB:
					t.state = 3
				default:
					t.state = 0
				}
			case 2:
				t.answer(t.verb, b)
				t.state = 0
			case 3:
				if b == tIAC {
					t.state = 4
				}
			case 4:
				if b == tSE {
					t.state = 0
				} else {
					t.state = 3
				}
			}
		}
		if out > 0 || err != nil {
			return out, err
		}
	}
}

func (t *telnetReader) answer(verb, opt byte) {
	switch verb {
	case tDO:
		if opt == optSGA {
			t.reply(tIAC, tWILL, opt)
		} else {
			t.reply(tIAC, tWONT, opt)
		}
	case tWILL:
		if opt == optEcho || opt == optSGA {
			t.reply(tIAC, tDO, opt)
		} else {
			t.reply(tIAC, tDONT, opt)
		}
	}
}
