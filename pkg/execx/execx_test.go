package execx_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/execx"
)

func sh(t *testing.T, script string) []string {
	t.Helper()
	return []string{"/bin/sh", "-c", script}
}

func TestARunIsARecordWithArgvTheBinaryAndTheBytes(t *testing.T) {
	res, err := execx.Spawn(context.Background(), sh(t, "printf out; printf err >&2; exit 0"), execx.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != execx.Ran || res.ExitCode != 0 {
		t.Errorf("outcome %v exit %d", res.Outcome, res.ExitCode)
	}
	if string(res.Stdout) != "out" || string(res.Stderr) != "err" {
		t.Errorf("stdout %q stderr %q — the streams are separate and both are the record", res.Stdout, res.Stderr)
	}
	// The resolved path, not the name asked for: which /bin/sh ran matters when
	// there are two on the PATH.
	if res.Binary == "" || res.Argv[0] != "/bin/sh" {
		t.Errorf("binary %q argv %v", res.Binary, res.Argv)
	}
	if res.Duration <= 0 || res.StartedAt.IsZero() {
		t.Error("no timing on the record")
	}
}

// nuclei exits 1 when it finds nothing. That is an answer, and a wrapper that
// returns it as an error makes every caller unwrap a Go error to read a number
// the process already reported.
func TestANonZeroExitIsDataAndNotAnError(t *testing.T) {
	res, err := execx.Spawn(context.Background(), sh(t, "printf found-nothing; exit 1"), execx.Policy{})
	if err != nil {
		t.Fatalf("a non-zero exit came back as an error: %v", err)
	}
	if res.Outcome != execx.Ran {
		t.Errorf("outcome %v, want ran", res.Outcome)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit %d, want 1", res.ExitCode)
	}
	if string(res.Stdout) != "found-nothing" {
		t.Error("output from a failing run was discarded")
	}
}

// CLAUDE.md's health noun: "a tool off PATH looks like silence". It must not.
func TestAToolOffPathIsUnavailableAndSaysWhich(t *testing.T) {
	res, err := execx.Spawn(context.Background(), []string{"definitely-not-a-real-tool", "-x"}, execx.Policy{})
	if err == nil {
		t.Fatal("a missing program did not report")
	}
	if !errors.IsKind(err, errors.Unavailable) {
		t.Errorf("kind %v, want Unavailable", errors.KindOf(err))
	}
	if res.Outcome != execx.Unavailable || !strings.Contains(res.Reason, "definitely-not-a-real-tool") {
		t.Errorf("record does not name the missing tool: %+v", res)
	}
}

func TestATimeoutKillsAndIsNotAnError(t *testing.T) {
	started := time.Now()
	res, err := execx.Spawn(context.Background(), sh(t, "sleep 30"),
		execx.Policy{Timeout: 300 * time.Millisecond, Grace: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("a timeout came back as an error: %v", err)
	}
	if res.Outcome != execx.TimedOut {
		t.Errorf("outcome %v, want timed out", res.Outcome)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("took %v — the process was not killed", took)
	}
	if !strings.Contains(res.Reason, "300ms") {
		t.Errorf("reason %q does not say what the bound was", res.Reason)
	}
}

// CommandContext signals the direct child. A tool that forked leaves its
// children running, holding sockets and writing to a pipe nobody reads.
func TestTheWholeProcessGroupIsKilled(t *testing.T) {
	// The shell backgrounds a sleeper and exits immediately. If only the direct
	// child were signalled, the grandchild would hold stdout open and the read
	// would block well past the timeout.
	script := "sleep 30 & printf started; wait"
	started := time.Now()
	res, err := execx.Spawn(context.Background(), sh(t, script),
		execx.Policy{Timeout: 400 * time.Millisecond, Grace: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("took %v — a grandchild survived and held the pipe", took)
	}
	if res.Outcome != execx.TimedOut {
		t.Errorf("outcome %v", res.Outcome)
	}
}

func TestEachStreamIsCappedIndependentlyAndSaysSo(t *testing.T) {
	script := "printf '%0.sx' $(seq 1 5000); printf '%0.sy' $(seq 1 10) >&2"
	res, err := execx.Spawn(context.Background(), sh(t, script), execx.Policy{MaxOutput: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != 100 || !res.StdoutTruncated {
		t.Errorf("stdout %d bytes truncated=%v, want 100 and true", len(res.Stdout), res.StdoutTruncated)
	}
	// A tool that fills stderr with warnings has not lost its findings, so the
	// caps are per stream.
	if res.StderrTruncated || len(res.Stderr) != 10 {
		t.Errorf("stderr %d bytes truncated=%v — the caps are not independent", len(res.Stderr), res.StderrTruncated)
	}
}

// A child that inherits this process's environment inherits DATABASE_URL, and a
// tool that prints its environment on error writes it into an artifact kept
// forever.
func TestTheEnvironmentIsBuiltAndNeverInherited(t *testing.T) {
	t.Setenv("OVERWATCH_SECRET_UNDER_TEST", "a-password")

	res, err := execx.Spawn(context.Background(), sh(t, "env"), execx.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.Stdout), "OVERWATCH_SECRET_UNDER_TEST") {
		t.Fatalf("the child inherited the parent environment:\n%s", res.Stdout)
	}
	if !strings.Contains(string(res.Stdout), "PATH=") {
		t.Error("no PATH — almost every tool needs one")
	}

	given, err := execx.Spawn(context.Background(), sh(t, "env"),
		execx.Policy{Env: []string{"PATH=/bin:/usr/bin", "TOOL_TOKEN=abc"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(given.Stdout), "TOOL_TOKEN=abc") {
		t.Error("an explicit Env was not used")
	}
}

// decisions/0010: the gate fails BEFORE exec, and the refusal is the scope proof
// rather than an error. An invocation has one shape whether or not it ran.
func TestARefusalIsARecordAndNotAnError(t *testing.T) {
	res := execx.Refuse([]string{"masscan", "-p1-65535", "198.51.100.0/24"}, "rule r2 excludes this range")

	if res.Outcome != execx.Refused {
		t.Errorf("outcome %v, want refused", res.Outcome)
	}
	if res.Reason == "" {
		t.Error("a refusal with no reason is not a scope proof")
	}
	if len(res.Argv) != 3 || res.Argv[0] != "masscan" {
		t.Errorf("argv %v — what was NOT run is the record", res.Argv)
	}
	if res.ExitCode != -1 || res.Binary != "" {
		t.Error("a refusal has no exit code and no binary, because nothing ran")
	}
}

func TestAnEmptyArgvIsRefusedBeforeAnythingHappens(t *testing.T) {
	if _, err := execx.Spawn(context.Background(), nil, execx.Policy{}); !errors.Is(err, execx.ErrNoArgv) {
		t.Errorf("gave %v, want ErrNoArgv", err)
	}
}

func TestACancelledContextStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()

	started := time.Now()
	res, err := execx.Spawn(ctx, sh(t, "sleep 30"), execx.Policy{Grace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("took %v — cancellation did not reach the process", took)
	}
	if res.Outcome != execx.TimedOut {
		t.Errorf("outcome %v", res.Outcome)
	}
}
