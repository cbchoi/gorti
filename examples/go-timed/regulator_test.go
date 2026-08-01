package main

import "testing"

func TestRegulatorTickTimestampIncludesLookahead(t *testing.T) {
	tests := []struct {
		name      string
		grantTime float64
		lookahead float64
		want      float64
	}{
		{name: "fast", grantTime: 3, lookahead: 0.5, want: 3.5},
		{name: "normal", grantTime: 3, lookahead: 1, want: 4},
		{name: "slow", grantTime: 3, lookahead: 2, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := regulator{lookahead: tt.lookahead}
			if got := r.tickTimestamp(tt.grantTime); got != tt.want {
				t.Fatalf("tickTimestamp(%v) = %v, want %v", tt.grantTime, got, tt.want)
			}
		})
	}
}
