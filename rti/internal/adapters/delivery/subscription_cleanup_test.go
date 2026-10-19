package delivery

import "testing"

func closeTestSubscription(t *testing.T, closeSubscription func() error) {
	t.Helper()
	if err := closeSubscription(); err != nil {
		t.Errorf("close subscription: %v", err)
	}
}
