package core

// RTIObjectDiscoveryRecorder records successful discovery delivery by another
// RTI service, so object knowledge and save images use the same authoritative
// history. It must not be called before delivery has been accepted.
type RTIObjectDiscoveryRecorder interface {
	RecordRTIObjectDiscovery(FederationName, FederateHandle, ObjectHandle) error
}
