// Package domain holds the org and its members.
//
// **The reasoning lives one level up, in internal/org.**
//
// Every type is a value and every transition returns a new one; nothing mutates
// a receiver and nothing reads the clock. Same rules as identity's, for the same
// reasons.
package domain
