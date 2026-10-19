package grpc

// MOMServiceExceptionName reuses the transport's standard exception identities
// without turning gRPC statuses or error prose into purported IEEE names.
func MOMServiceExceptionName(err error) string {
	if err == nil {
		return ""
	}
	if name, known := specExceptionName(err); known {
		return name
	}
	return rtiInternalErrorName
}

const (
	rtiInternalErrorName = "RTIinternalError"
)
