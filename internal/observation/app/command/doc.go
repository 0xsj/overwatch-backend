// Package command turns an artifact's bytes into observations.
//
// **Nothing here authorises.** Extraction is called by `run`'s executor, which
// runs behind a gate the composition root already applied when the run was
// started.
package command
