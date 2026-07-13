package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

func deterministicPayload(seed, channel string, index int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", seed, channel, index)))
	return hex.EncodeToString(digest[:])[:16]
}

func encodeInteger32BE(value int) ([]byte, error) {
	if value < 0 || uint64(value) > uint64(^uint32(0)>>1) {
		return nil, errors.New("HLAinteger32BE value is outside the non-negative int32 range")
	}
	encoded := make([]byte, 4)
	binary.BigEndian.PutUint32(encoded, uint32(value))
	return encoded, nil
}

func decodeInteger32BE(encoded []byte) (int, error) {
	if len(encoded) != 4 {
		return 0, fmt.Errorf("HLAinteger32BE length = %d, want 4", len(encoded))
	}
	value := binary.BigEndian.Uint32(encoded)
	if value > uint32(^uint32(0)>>1) {
		return 0, errors.New("HLAinteger32BE value is negative")
	}
	return int(value), nil
}

func encodeASCIIString(value string) ([]byte, error) {
	for index := range value {
		if value[index] > 0x7f {
			return nil, errors.New("HLAASCIIstring contains a non-ASCII byte")
		}
	}
	encoded := make([]byte, 4+len(value))
	binary.BigEndian.PutUint32(encoded, uint32(len(value)))
	copy(encoded[4:], value)
	return encoded, nil
}

func decodeASCIIString(encoded []byte) (string, error) {
	if len(encoded) < 4 {
		return "", fmt.Errorf("HLAASCIIstring length = %d, want at least 4", len(encoded))
	}
	length := binary.BigEndian.Uint32(encoded[:4])
	if uint64(length) != uint64(len(encoded)-4) {
		return "", fmt.Errorf("HLAASCIIstring prefix = %d, payload length = %d", length, len(encoded)-4)
	}
	for _, value := range encoded[4:] {
		if value > 0x7f {
			return "", errors.New("HLAASCIIstring contains a non-ASCII byte")
		}
	}
	return string(encoded[4:]), nil
}

func federationSeed(seed string) uint64 {
	digest := sha256.Sum256([]byte(seed))
	return binary.BigEndian.Uint64(digest[:8])
}
