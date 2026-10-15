package run

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/log"
	"golang.org/x/crypto/ssh"
)

// Flogo seeds unmapped inputs from these struct values, so they must carry the defaults.
var activityMd = activity.ToMetadata(&Input{FailOnNonZeroExit: true}, &Output{})

// Error codes surface in the flow's error handler as $error.code.
const (
	ErrCodeSession = "SSH-4002"
	ErrCodeTimeout = "SSH-4003"
	ErrCodeExit    = "SSH-4004"
)

const stderrInError = 2048

func init() {
	_ = activity.Register(&MyActivity{}, New)
}

// New creates a new activity
func New(ctx activity.InitContext) (activity.Activity, error) {
	return &MyActivity{logger: log.ChildLogger(ctx.Logger(), "SSH-activity-run"), activityName: "run"}, nil
}

// MyActivity runs a command on the SSH server of the selected connection.
type MyActivity struct {
	logger       log.Logger
	activityName string
}

// Metadata implements activity.Activity.Metadata
func (*MyActivity) Metadata() *activity.Metadata {
	return activityMd
}

type sessionOpener interface {
	NewSession() (*ssh.Session, error)
}

func openSession(input *Input) (*ssh.Session, error) {
	if o, ok := input.Connection.(sessionOpener); ok {
		return o.NewSession()
	}
	if c, ok := input.Connection.GetConnection().(*ssh.Client); ok && c != nil {
		return c.NewSession()
	}
	return nil, errors.New("the SSH connection is not available")
}

// Eval implements activity.Activity.Eval
func (a *MyActivity) Eval(context activity.Context) (done bool, err error) {
	input := &Input{}
	if err = context.GetInputObject(input); err != nil {
		return false, err
	}
	if strings.TrimSpace(input.Cmd) == "" {
		return false, errors.New("cmd is required")
	}

	session, err := openSession(input)
	if err != nil {
		return false, activity.NewError(err.Error(), ErrCodeSession, nil)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if input.Stdin != "" {
		session.Stdin = strings.NewReader(input.Stdin)
	}

	start := time.Now()
	if err = session.Start(input.Cmd); err != nil {
		return false, activity.NewError("start command: "+err.Error(), ErrCodeSession, nil)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- session.Wait() }()

	var timeout <-chan time.Time
	if input.TimeoutSeconds > 0 {
		timer := time.NewTimer(time.Duration(input.TimeoutSeconds) * time.Second)
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case err = <-waitErr:
	case <-timeout:
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		var partial map[string]interface{}
		// the output buffers are only safe to read once Wait has returned
		select {
		case <-waitErr:
			partial = map[string]interface{}{"stdOut": stdout.String(), "stdErr": stderr.String()}
		case <-time.After(2 * time.Second):
		}
		return false, activity.NewError(fmt.Sprintf("command timed out after %d s", input.TimeoutSeconds), ErrCodeTimeout, partial)
	}

	output := &Output{
		StdOut:     stdout.String(),
		StdErr:     stderr.String(),
		DurationMs: int(time.Since(start).Milliseconds()),
	}
	if err != nil {
		var exitErr *ssh.ExitError
		var missing *ssh.ExitMissingError
		switch {
		case errors.As(err, &exitErr):
			output.ExitCode = exitErr.ExitStatus()
		case errors.As(err, &missing):
			output.ExitCode = -1
		default:
			return false, activity.NewError("command failed: "+err.Error(), ErrCodeSession, nil)
		}
	}
	a.logger.Debugf("SSH command finished: exit=%d stdout=%dB stderr=%dB in %d ms",
		output.ExitCode, len(output.StdOut), len(output.StdErr), output.DurationMs)

	if output.ExitCode != 0 && input.FailOnNonZeroExit {
		msg := strings.TrimSpace(output.StdErr)
		if len(msg) > stderrInError {
			msg = "..." + msg[len(msg)-stderrInError:]
		}
		return false, activity.NewError(fmt.Sprintf("command exited with status %d: %s", output.ExitCode, msg), ErrCodeExit,
			output.ToMap())
	}

	if err = context.SetOutputObject(output); err != nil {
		return false, err
	}
	return true, nil
}
