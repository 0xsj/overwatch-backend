package provenance

const (
	FieldRequest     = "request_id"
	FieldCorrelation = "correlation_id"
	FieldCausation   = "causation_id"
	FieldOrigin      = "origin"
	FieldDepth       = "depth"
	FieldAttempt     = "attempt"
	FieldActor       = "actor"
	FieldOnBehalfOf  = "on_behalf_of"
	FieldTenant      = "tenant"
	FieldTraceparent = "traceparent"
)

const MaxIDLength = 128

const MaxDepth = 32
