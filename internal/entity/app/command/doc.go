// Package command assembles the graph and records rulings on it.
//
// **Two subscribers and two writes.** The subscribers are what make fragments
// and attributions appear at all — decisions/0036 — and they run one outbox
// delivery behind the run that produced the observations.
package command
