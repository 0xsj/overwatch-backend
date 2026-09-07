// Package domain holds the target and its rules.
//
// **The reasoning lives one level up, in internal/target.**
//
// Every type is a value, every transition returns a new one, and nothing reads
// the clock — the instant is a parameter, because every interesting case is a
// boundary. Same rules as identity's and org's, for the same reasons.
package domain
