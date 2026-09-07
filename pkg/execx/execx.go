package execx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
)

const (
	DefaultTimeout   = 5 * time.Minute
	DefaultMaxOutput = 32 << 20
	DefaultGrace     = 5 * time.Second
)

var (
	ErrNoArgv     = pkgerrors.New(pkgerrors.Invalid, "a spawn needs at least a program")
	ErrNotFound   = pkgerrors.New(pkgerrors.Unavailable, "the program is not on PATH")
	ErrNotStarted = pkgerrors.New(pkgerrors.Unavailable, "the program could not be started")
)

type Outcome uint8

const (
	Ran Outcome = iota
	Refused
	Unavailable
	TimedOut
)

func (o Outcome) String() string {
	switch o {
	case Refused:
		return "refused"
	case Unavailable:
		return "unavailable"
	case TimedOut:
		return "timed out"
	default:
		return "ran"
	}
}

type Policy struct {
	Timeout   time.Duration
	MaxOutput int64
	Dir       string

	// Env is the WHOLE environment. Empty means PATH and nothing else — see the
	// package doc on why inheriting is a credential in an artifact.
	Env  []string
	Path string

	Grace time.Duration
}

func (p Policy) withDefaults() Policy {
	if p.Timeout == 0 {
		p.Timeout = DefaultTimeout
	}
	if p.MaxOutput == 0 {
		p.MaxOutput = DefaultMaxOutput
	}
	if p.Grace == 0 {
		p.Grace = DefaultGrace
	}
	if p.Path == "" {
		p.Path = "/usr/local/bin:/usr/bin:/bin"
	}
	if len(p.Env) == 0 {
		p.Env = []string{"PATH=" + p.Path}
	}
	return p
}

type Result struct {
	Argv     []string
	Binary   string
	Outcome  Outcome
	ExitCode int
	Signal   string
	Reason   string

	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool

	StartedAt time.Time
	Duration  time.Duration
}

// Refuse records a spawn that never happened. decisions/0010: the gate fails
// before exec, and the refusal is the scope proof rather than an error.
func Refuse(argv []string, reason string) Result {
	return Result{Argv: append([]string(nil), argv...), Outcome: Refused, Reason: reason, ExitCode: -1}
}

func Spawn(ctx context.Context, argv []string, p Policy) (Result, error) {
	if len(argv) == 0 {
		return Result{}, ErrNoArgv
	}
	p = p.withDefaults()
	res := Result{Argv: append([]string(nil), argv...), ExitCode: -1}

	binary, err := exec.LookPath(argv[0])
	if err != nil {
		res.Outcome = Unavailable
		res.Reason = argv[0] + " is not on PATH"
		return res, pkgerrors.Wrap(err, pkgerrors.Unavailable, "execx: "+argv[0]).
			WithDetail("program", argv[0])
	}
	res.Binary = binary

	run, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	cmd := exec.Command(binary, argv[1:]...)
	cmd.Dir = p.Dir
	cmd.Env = p.Env
	// Its own process group, so the whole tree can be signalled. A tool that
	// forked and is killed by pid leaves children holding sockets.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out, errBuf bytes.Buffer
	cmd.Stdout = &capped{w: &out, limit: p.MaxOutput, hit: &res.StdoutTruncated}
	cmd.Stderr = &capped{w: &errBuf, limit: p.MaxOutput, hit: &res.StderrTruncated}

	res.StartedAt = time.Now()
	if err := cmd.Start(); err != nil {
		res.Outcome = Unavailable
		res.Reason = err.Error()
		res.Duration = time.Since(res.StartedAt)
		return res, pkgerrors.Wrap(err, pkgerrors.Unavailable, "execx: start "+argv[0])
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var killed bool
	select {
	case err = <-done:
	case <-run.Done():
		killed = true
		terminate(cmd.Process.Pid, p.Grace)
		err = <-done
	}

	res.Duration = time.Since(res.StartedAt)
	res.Stdout = out.Bytes()
	res.Stderr = errBuf.Bytes()

	var exitErr *exec.ExitError
	switch {
	case killed:
		res.Outcome = TimedOut
		res.Reason = "did not finish within " + p.Timeout.String()
		if errors.As(err, &exitErr) {
			res.Signal = signalOf(exitErr)
		}
		return res, nil
	case err == nil:
		res.Outcome = Ran
		res.ExitCode = 0
		return res, nil
	case errors.As(err, &exitErr):
		// A non-zero exit is an ANSWER. nuclei exits 1 when it finds nothing.
		res.Outcome = Ran
		res.ExitCode = exitErr.ExitCode()
		res.Signal = signalOf(exitErr)
		return res, nil
	}
	res.Outcome = Unavailable
	res.Reason = err.Error()
	return res, pkgerrors.Wrap(err, pkgerrors.Internal, "execx: wait "+argv[0])
}

// terminate signals the GROUP: SIGTERM, then SIGKILL after a grace period,
// because a tool that traps SIGTERM to flush its output should be allowed to.
func terminate(pid int, grace time.Duration) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(grace)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func signalOf(e *exec.ExitError) string {
	if status, ok := e.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return strings.TrimPrefix(status.Signal().String(), "signal: ")
	}
	return ""
}

// capped writes until the limit and then records that it stopped. It never
// errors: a tool that fills a pipe should be truncated, not killed by EPIPE,
// because the bytes it already produced are the record.
type capped struct {
	w     io.Writer
	limit int64
	n     int64
	hit   *bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.limit - c.n
	if room <= 0 {
		*c.hit = true
		return len(p), nil
	}
	if int64(len(p)) > room {
		*c.hit = true
		_, _ = c.w.Write(p[:room])
		c.n = c.limit
		return len(p), nil
	}
	c.n += int64(len(p))
	return c.w.Write(p)
}
