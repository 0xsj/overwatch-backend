// Package mail sends the few messages this product sends, and refuses to be
// anything more.
//
// Verification and password reset are the whole list. Both exist because
// decisions/0017 makes activation the gate an account passes before it can sign
// in, and a link that arrives at an address is the only proof the address is
// real — the same argument internal/identity gives for a deliberately shallow
// email parser.
//
// # A link to the wrong host silently does nothing
//
// [New] refuses to start without a base URL. The failure mode it prevents is the
// quiet one: a verification mail that arrives, looks right, and points at
// localhost from somebody else's laptop. Nothing errors, nobody is told, and the
// account never activates.
//
// So the base URL is required at construction rather than defaulted, and the
// process does not boot without it.
//
// # The transport is stdlib and the seam is one interface
//
// net/smtp is enough for a handful of transactional messages: dial, optional
// STARTTLS, authenticate, send. A provider SDK arrives when somebody needs
// delivery receipts or a template editor, and it arrives behind [Sender] rather
// than through this package.
//
// [Memory] is the second implementation and is not a test double — it is what
// makes running with no infrastructure a supported mode, the same argument
// pkg/postgres's memory adapters make. It keeps what it was given so a test can
// assert on a link rather than on the fact that Send returned nil.
//
// # Nothing here is retried
//
// A send that fails returns an Unavailable error and stops. Retrying belongs to
// whatever decided the message was worth sending — pkg/outbox already retries
// with backoff and burial, and a second retry loop inside this package would
// multiply against it.
//
// # An address is not validated here
//
// internal/identity already parsed it, deliberately shallowly, on the grounds
// that the only proof an address works is a message arriving at it. Parsing it
// again to a different standard would mean two answers to one question, and the
// stricter one would refuse deliverable addresses this system has already
// accepted.
//
// # Deliberately absent
//
// **Templates as files.** Two messages do not earn a template language, an
// embed directory or a rendering step. They are Go string literals with the
// values interpolated, which is greppable and cannot drift from its arguments.
// A third message is not the trigger; a designer editing copy is.
//
// **HTML.** A verification mail is one sentence and a URL. HTML doubles the
// surface — a text part, an HTML part, a multipart boundary — and buys nothing
// a link does not already do.
//
// **Attachments, queues, bulk send, unsubscribe.** This is transactional mail to
// somebody who asked for it. Anything with a list behind it is a different
// product with different law attached.
package mail
