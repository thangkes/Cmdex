package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/thangkes/cmdex/internal/viewer"
)

// Cmdex's other windows (package viewer): the shared log window and the
// notes window, each a child process started here and tied to this one by
// its standard input and output.

type childProc struct {
	kind  string
	out   *viewer.Writer
	in    io.Closer // its input: closing it closes the window
	wait  func()    // returns when it has ended
	kill  func()
	ready bool
}

func (c *childProc) send(m viewer.Msg) error { return c.out.Send(m) }

// close asks the window to close, and makes sure it does.
func (c *childProc) close() {
	c.in.Close()
	go func() {
		time.Sleep(3 * time.Second)
		c.kill()
	}()
}

// startChild starts "cmdex --kind". Its messages go to onMsg; onEnd runs
// once it has ended. max bounds one message from it.
func startChild(kind string, max int, onMsg func(*childProc, viewer.Msg), onEnd func(*childProc)) (*childProc, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "--"+kind)
	cmd.Env = childEnv()
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &childProc{kind: kind, out: viewer.NewWriter(in), in: in, wait: func() { cmd.Wait() }, kill: func() { cmd.Process.Kill() }}
	go runChild(c, out, max, onMsg, onEnd)
	return c, nil
}

func runChild(c *childProc, out io.Reader, max int, onMsg func(*childProc, viewer.Msg), onEnd func(*childProc)) {
	viewer.Read(out, max, func(m viewer.Msg) { onMsg(c, m) })
	c.wait()
	onEnd(c)
}

// childEnv is a window's environment: nothing that points it at the
// database (it never opens one).
func childEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(e), "CMDEX_") {
			env = append(env, e)
		}
	}
	return env
}

// callMethod runs one of the App's methods for a window: only those listed
// in allowed. It returns the first result as JSON, or the error.
func (a *App) callMethod(allowed map[string]bool, name string, args []json.RawMessage) (json.RawMessage, error) {
	if !allowed[name] {
		return nil, fmt.Errorf("%s is not available here", name)
	}
	m := reflect.ValueOf(a).MethodByName(name)
	if !m.IsValid() {
		return nil, fmt.Errorf("no method %s", name)
	}
	t := m.Type()
	if len(args) != t.NumIn() {
		return nil, fmt.Errorf("%s takes %d arguments", name, t.NumIn())
	}
	in := make([]reflect.Value, len(args))
	for i := range args {
		p := reflect.New(t.In(i))
		if err := json.Unmarshal(args[i], p.Interface()); err != nil {
			return nil, errors.New("bad argument: " + err.Error())
		}
		in[i] = p.Elem()
	}
	outs := m.Call(in)
	var result any
	for _, o := range outs {
		if o.Type() == reflect.TypeOf((*error)(nil)).Elem() {
			if !o.IsNil() {
				return nil, o.Interface().(error)
			}
			continue
		}
		result = o.Interface()
	}
	return json.Marshal(result)
}
