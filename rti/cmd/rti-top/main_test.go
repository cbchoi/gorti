// Boot-time validation tests for rti-top — checks the PINNED
// [100ms, 60s] refresh range from docs/rtid-tui.md §2.4.

package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateRefresh_Bounds(t *testing.T) {
	cases := []struct {
		name    string
		d       time.Duration
		wantErr bool
		wantMsg string
	}{
		{"below_min", 50 * time.Millisecond, true, "below the minimum"},
		{"at_min", 100 * time.Millisecond, false, ""},
		{"middle", 1 * time.Second, false, ""},
		{"at_max", 60 * time.Second, false, ""},
		{"above_max", 90 * time.Second, true, "above the maximum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRefresh(tc.d)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("validateRefresh(%s) err = %v; want err? %v", tc.d, err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("validateRefresh(%s) err = %q; want substring %q", tc.d, err.Error(), tc.wantMsg)
			}
		})
	}
}
