package run

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sshConnection "github.com/mmussett/extensions/SSH/connector/connection"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/test"
	"golang.org/x/crypto/ssh"
)

// fakeServer is an in-process SSH server with a few scripted commands:
//
//	echo-stdin   copies stdin to stdout
//	fail N       writes "boom" to stderr and exits with status N
//	sleep        blocks until the session is closed
//	anything     prints "ran: <cmd>"
type fakeServer struct {
	addr  string
	ln    net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func startFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "p" {
				return nil, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{addr: ln.Addr().String(), ln: ln}
	t.Cleanup(func() { ln.Close(); s.dropAll() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, nc)
			s.mu.Unlock()
			go s.serve(nc, cfg)
		}
	}()
	return s
}

func (s *fakeServer) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
}

func (s *fakeServer) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go handleSession(ch, chReqs)
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		cmd := string(req.Payload[4:])
		status := uint32(0)
		switch {
		case cmd == "echo-stdin":
			_, _ = io.Copy(ch, ch)
		case strings.HasPrefix(cmd, "fail "):
			n, _ := strconv.Atoi(strings.TrimPrefix(cmd, "fail "))
			_, _ = ch.Stderr().Write([]byte("boom"))
			status = uint32(n)
		case cmd == "sleep":
			for range reqs {
			}
			return
		default:
			_, _ = ch.Write([]byte("ran: " + cmd))
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, status)
		_, _ = ch.SendRequest("exit-status", false, b)
		return
	}
}

func newManager(t *testing.T, s *fakeServer) *sshConnection.SshSharedConfigManager {
	t.Helper()
	host, port, _ := net.SplitHostPort(s.addr)
	p, _ := strconv.Atoi(port)
	m, err := (&sshConnection.SshFactory{}).NewManager(map[string]interface{}{
		"name": "test", "host": host, "port": p, "user": "u", "password": "p",
		"publicKeyFlag": false, "hostKeyFlag": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr := m.(*sshConnection.SshSharedConfigManager)
	t.Cleanup(func() { _ = mgr.Stop() })
	return mgr
}

func eval(t *testing.T, in *Input) (*Output, error) {
	t.Helper()
	act := &MyActivity{logger: log.RootLogger(), activityName: "run"}
	tc := test.NewActivityContext(act.Metadata())
	tc.SetInputObject(in)
	if _, err := act.Eval(tc); err != nil {
		return nil, err
	}
	out := &Output{}
	if err := tc.GetOutputObject(out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func TestManyCommandsOnOneConnection(t *testing.T) {
	m := newManager(t, startFakeServer(t))
	for i := 0; i < 5; i++ {
		out, err := eval(t, &Input{Connection: m, Cmd: "hello " + strconv.Itoa(i), FailOnNonZeroExit: true})
		if err != nil {
			t.Fatalf("command %d: %v", i, err)
		}
		if out.StdOut != "ran: hello "+strconv.Itoa(i) || out.ExitCode != 0 {
			t.Fatalf("command %d: %+v", i, out)
		}
	}
}

func TestConcurrentCommands(t *testing.T) {
	m := newManager(t, startFakeServer(t))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := eval(t, &Input{Connection: m, Cmd: "c" + strconv.Itoa(i), FailOnNonZeroExit: true})
			if err == nil && out.StdOut != "ran: c"+strconv.Itoa(i) {
				err = io.ErrUnexpectedEOF
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStdinIsPassedThrough(t *testing.T) {
	m := newManager(t, startFakeServer(t))
	payload := `{"name":"x; rm -rf /","note":"$(whoami)"}`
	out, err := eval(t, &Input{Connection: m, Cmd: "echo-stdin", Stdin: payload, FailOnNonZeroExit: true})
	if err != nil || out.StdOut != payload {
		t.Fatalf("got %+v %v", out, err)
	}
}

func TestNonZeroExit(t *testing.T) {
	m := newManager(t, startFakeServer(t))

	_, err := eval(t, &Input{Connection: m, Cmd: "fail 3", FailOnNonZeroExit: true})
	var ae *activity.Error
	if err == nil || !asActivityError(err, &ae) || ae.Code() != ErrCodeExit || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("want SSH-4004 with stderr, got %v", err)
	}

	out, err := eval(t, &Input{Connection: m, Cmd: "fail 7", FailOnNonZeroExit: false})
	if err != nil || out.ExitCode != 7 || out.StdErr != "boom" {
		t.Fatalf("got %+v %v", out, err)
	}
}

func TestTimeout(t *testing.T) {
	m := newManager(t, startFakeServer(t))
	start := time.Now()
	_, err := eval(t, &Input{Connection: m, Cmd: "sleep", TimeoutSeconds: 1, FailOnNonZeroExit: true})
	var ae *activity.Error
	if err == nil || !asActivityError(err, &ae) || ae.Code() != ErrCodeTimeout {
		t.Fatalf("want SSH-4003, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
	// the connection is still usable afterwards
	if out, err := eval(t, &Input{Connection: m, Cmd: "after", FailOnNonZeroExit: true}); err != nil || out.StdOut != "ran: after" {
		t.Fatalf("after timeout: %+v %v", out, err)
	}
}

func TestReconnectAfterDrop(t *testing.T) {
	s := startFakeServer(t)
	m := newManager(t, s)
	if _, err := eval(t, &Input{Connection: m, Cmd: "before", FailOnNonZeroExit: true}); err != nil {
		t.Fatal(err)
	}
	s.dropAll()
	time.Sleep(100 * time.Millisecond)
	out, err := eval(t, &Input{Connection: m, Cmd: "after-drop", FailOnNonZeroExit: true})
	if err != nil || out.StdOut != "ran: after-drop" {
		t.Fatalf("expected transparent reconnect: %+v %v", out, err)
	}
}

func TestDefaultsKeepOriginalBehaviour(t *testing.T) {
	in := &Input{}
	_ = in.FromMap(map[string]interface{}{"SSH Connection": nil, "cmd": "x"})
	if !in.FailOnNonZeroExit || in.TimeoutSeconds != 0 {
		t.Fatalf("defaults changed: %+v", in)
	}

	// through a Flogo context, unmapped inputs are seeded from the metadata
	tc := test.NewActivityContext((&MyActivity{}).Metadata())
	if v := tc.GetInput("failOnNonZeroExit"); v != true {
		t.Fatalf("unmapped failOnNonZeroExit = %v, want true", v)
	}
}

func asActivityError(err error, target **activity.Error) bool {
	ae, ok := err.(*activity.Error)
	if ok {
		*target = ae
	}
	return ok
}
