package sdkstate

import "testing"

func TestFederateIdentityUsesPackedLowWord(t *testing.T) {
	for _, test := range []struct {
		name               string
		handle, generation uint64
		valid              bool
	}{
		{"zero", 0, 0, false},
		{"first legacy identity", 1, 0, true},
		{"generation without identity", 1 << 32, 1, false},
		{"first generation identity", 1<<32 | 1, 1, true},
		{"maximum low word", 1<<32 | 0xffffffff, 1, true},
		{"maximum high word", 0xffffffff00000001, 0xffffffff, true},
		{"different generation", 1<<32 | 1, 2, false},
		{"generation cannot fit", 1, 1 << 32, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validFederate(test.handle, test.generation); got != test.valid {
				t.Fatalf("validFederate(%#x, %#x) = %v, want %v", test.handle, test.generation, got, test.valid)
			}
		})
	}
}
