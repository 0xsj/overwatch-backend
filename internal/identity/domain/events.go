package domain

const (
	EventAccountCreated    = "identity.account.created"
	EventAccountActivated  = "identity.account.activated"
	EventAccountArchived   = "identity.account.archived"
	EventCredentialChanged = "identity.credential.changed"
	EventEmailChanged      = "identity.account.email_changed"
	EventCredentialRevoked = "identity.credential.revoked"
	EventSessionStarted    = "identity.session.started"
	EventSessionEnded      = "identity.session.ended"
)

type AccountCreated struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
}

type AccountActivated struct {
	AccountID string `json:"account_id"`
}

type AccountArchived struct {
	AccountID string `json:"account_id"`
}

type CredentialChanged struct {
	AccountID    string `json:"account_id"`
	CredentialID string `json:"credential_id"`
	Kind         string `json:"kind"`
}

type CredentialRevoked struct {
	AccountID    string `json:"account_id"`
	CredentialID string `json:"credential_id"`
	Kind         string `json:"kind"`
}

type SessionStarted struct {
	AccountID string `json:"account_id"`
	SessionID string `json:"session_id"`
}

type SessionEnded struct {
	AccountID string `json:"account_id"`
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"`
}

type EmailChanged struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}
