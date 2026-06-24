package gateway

// GatewayError is the base error type for all gateway-returned errors.
type GatewayError struct {
	Message      string
	StatusCode   int
	ResponseBody map[string]any
}

func (e *GatewayError) Error() string { return e.Message }

// PolicyDeniedError is returned when the gateway policy engine denies a request.
type PolicyDeniedError struct {
	GatewayError
	Reason   string
	Trace    map[string]any
	RecordID string
}

func (e *PolicyDeniedError) Error() string { return "policy denied: " + e.Reason }

// QuotaExceededError is returned when an org or workload quota has been reached.
type QuotaExceededError struct {
	GatewayError
	UpgradeURL string
	RetryAfter int
}

func (e *QuotaExceededError) Error() string { return e.GatewayError.Message }

// GatewayTimeoutError is returned when the connector execution times out.
type GatewayTimeoutError struct{}

func (e *GatewayTimeoutError) Error() string { return "connector execution timed out" }
