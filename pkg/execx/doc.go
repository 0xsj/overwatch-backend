// Package execx runs other people's programs and turns each run into a record.
//
// Overwatch's premise is that it runs the tools a researcher already runs —
// subfinder, httpx, nuclei, nmap. So os/exec is unavoidable, and this package
// exists because the five things that go wrong with it are the five things this
// product cannot afford to get wrong.
//
// # There is no shell, and that is the safety property
//
// [Spawn] takes **argv as a slice**. There is no string to parse, so there is
// nothing to escape and nothing to inject:
//
//	execx.Spawn(ctx, []string{"subfinder", "-d", domain}, p)     the OS parses nothing
//	sh -c "subfinder -d " + domain                               domain = "x; rm -rf /"
//
// A target's name is attacker-influenced by definition. STACK.md calls this
// "argv slice, no shell — the runner's whole safety property", and it is the
// reason this package has no convenience that takes a command line.
//
// # The environment is built, never inherited
//
// A child that inherits this process's environment inherits DATABASE_URL. Its
// output is kept verbatim as an artifact, so a tool that prints its environment
// on error writes a database password into the record this product exists to
// keep — permanently, and in the one place designed never to be deleted.
//
// So [Policy.Env] is the whole environment. Empty means PATH and nothing else,
// which is what almost every tool actually needs.
//
// # A non-zero exit is data, not an error
//
// nuclei exits 1 when it finds nothing. httpx exits non-zero on an unreachable
// host. Those are answers, and a wrapper that returns them as errors makes every
// caller unwrap a Go error to read a number the process already reported.
//
// [Result.ExitCode] carries it and [Result.Outcome] says whether it is
// meaningful. An error from Spawn means the run did not happen or could not be
// observed — never that the program disagreed with you.
//
// # Outcome is the vocabulary the journal was missing
//
//	ran          the process finished. ExitCode is meaningful
//	refused      never spawned — a rule said no, and that is a RECORD
//	unavailable  could not start: not on PATH, not executable
//	timed out    killed, because it did not finish in time
//
// decisions/0014 records that internal/journal has no outcome column because
// nothing carried the vocabulary. This is that vocabulary, and it is deliberately
// the same set the Logs surface facets on.
//
// **Refused is a first-class result rather than an error.** decisions/0010's
// spawn gate fails before exec, and "a tool was not run because rule r3 said so"
// is the scope proof — the thing a client's report cites. [Refuse] builds that
// record without a process, so an invocation has ONE shape whether or not
// anything ran.
//
// # A tool off PATH looks like silence
//
// CLAUDE.md's `health` noun says exactly this, and it is why [Spawn] resolves
// the binary with [exec.LookPath] and returns [ErrNotFound] rather than a
// generic failure. A scheduled tool that silently stopped existing is the
// failure nobody notices, because nothing appears to be wrong.
//
// [Result.Binary] records the resolved path, not the name asked for. Which
// `httpx` ran matters when there are two on the PATH.
//
// # The whole process group is killed, not the child
//
// CommandContext sends a signal to the direct child. A tool that forked leaves
// its children running, holding sockets and writing to a pipe nobody reads.
//
// So the child is started in its own process group and the **group** is
// signalled: SIGTERM first, then SIGKILL after a grace period, because a tool
// that traps SIGTERM to flush its output should be allowed to.
//
// # Output is capped per stream, and the cap is recorded
//
// A tool pointed at something large emits gigabytes. Both streams are bounded
// and [Result.Truncated] says whether the bound was reached — a fact about the
// artifact, not something to infer from a length. Truncation is per stream,
// because a tool that fills stderr with warnings has not lost its findings.
//
// # Deliberately absent
//
// **Retry.** A tool that failed is an invocation that failed, and re-running it
// is a second invocation with its own record. Hiding that inside one call would
// make the coverage claim wrong.
//
// **Streaming output.** Every consumer today wants the bytes to hash and store.
// A reader arrives with the first tool whose output does not fit in memory, and
// it changes Result's shape rather than adding a parameter.
//
// **The scope gate.** Whether a tool may touch a target is decisions/0010, and
// it is a domain rule over targets — this package cannot see a target. [Refuse]
// is the shape of the answer, not the answer.
package execx
