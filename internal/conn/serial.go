package conn

import (
	"sort"

	"go.bug.st/serial"
)

// SerialConfig is a COM / tty port setup.
type SerialConfig struct {
	Port     string
	Baud     int
	DataBits int
	Parity   string // none, even, odd
	StopBits string // 1, 2
	LineEnd  string // cr, lf, crlf: what Enter sends
}

// SerialPorts lists the ports present right now.
func SerialPorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil || ports == nil {
		return []string{}
	}
	sort.Strings(ports)
	return ports
}

type serialSession struct {
	port    serial.Port
	lineEnd string
	stop    once
}

// OpenSerial opens a port (9600 8N1 unless set otherwise).
func OpenSerial(c SerialConfig, h Handlers) (Session, error) {
	mode := &serial.Mode{BaudRate: c.Baud, DataBits: c.DataBits, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	if mode.BaudRate == 0 {
		mode.BaudRate = 9600
	}
	if mode.DataBits == 0 {
		mode.DataBits = 8
	}
	switch c.Parity {
	case "even":
		mode.Parity = serial.EvenParity
	case "odd":
		mode.Parity = serial.OddParity
	}
	if c.StopBits == "2" {
		mode.StopBits = serial.TwoStopBits
	}
	p, err := serial.Open(c.Port, mode)
	if err != nil {
		return nil, err
	}
	s := &serialSession{port: p, lineEnd: c.LineEnd}
	go pump(p, h, func() { s.Close() })
	return s, nil
}

func (s *serialSession) Write(b []byte) (int, error) {
	if _, err := s.port.Write(Enter(b, s.lineEnd)); err != nil {
		return 0, err
	}
	return len(b), nil
}
func (s *serialSession) Resize(int, int) error { return nil }
func (s *serialSession) Close() error          { return s.stop.do(s.port.Close) }
