// Package command writes checks and their chains.
//
// **Nothing here authorises.** The caller's org role is checked at the
// composition root — `owner` or `admin` to write, membership to read — because
// this package cannot see org and must not learn the ladder.
package command
