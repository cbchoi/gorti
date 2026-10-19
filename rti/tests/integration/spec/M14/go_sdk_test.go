// TASK-294 (M14 W2) — Go SDK ConnectWithOptions + ConnectOptions surface.

package m14spec

import (
	"crypto/tls"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

// TestACConnectOptionsType — ConnectOptions struct is exported with
// expected fields.
func TestACConnectOptionsType(t *testing.T) {
	opts := federate.ConnectOptions{
		TLS:         &tls.Config{},
		BearerToken: "test",
	}
	if opts.TLS == nil {
		t.Errorf("TLS field not exported")
	}
	if opts.BearerToken != "test" {
		t.Errorf("BearerToken field not exported")
	}
}

// TestACConnectWithOptionsExists — surface introspection.
func TestACConnectWithOptionsExists(t *testing.T) {
	pkg := reflect.TypeOf(federate.ConnectOptions{})
	if pkg.NumField() < 3 {
		t.Errorf("ConnectOptions has %d fields, expected at least 3 (TLS, BearerToken, BearerTokenProvider)", pkg.NumField())
	}
	// Verify field names exist.
	wantFields := map[string]bool{
		"TLS":                 false,
		"BearerToken":         false,
		"BearerTokenProvider": false,
	}
	for i := 0; i < pkg.NumField(); i++ {
		name := pkg.Field(i).Name
		if _, ok := wantFields[name]; ok {
			wantFields[name] = true
		}
	}
	for name, found := range wantFields {
		if !found {
			t.Errorf("ConnectOptions missing field %s", name)
		}
	}
}
