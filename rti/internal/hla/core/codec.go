package core

// Codec is one HLA Evolved type encoder/decoder per IEEE 1516.2-2010 §4.
// Concrete codecs are produced by rti/pkg/encoding via CodecFactory.
type Codec interface {
	// Encode serializes v according to this type's encoding rule.
	Encode(v any) ([]byte, error)

	// Decode parses bytes from b. Returns the decoded value and the number of
	// bytes consumed. n is needed for nested decoding of composite types.
	Decode(b []byte) (v any, n int, err error)

	// OctetBoundary returns the alignment requirement for this type, in octets,
	// per IEEE 1516.2-2010 §4. Used by composite codecs to compute padding.
	OctetBoundary() int
}

// CodecFactory builds Codec implementations from FOM data type descriptors.
// The concrete dataType type lives in rti/pkg/fom/model and is opaque here.
type CodecFactory interface {
	For(dt any) (Codec, error)
}
