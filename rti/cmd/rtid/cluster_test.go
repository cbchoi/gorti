// M15 cut-2 cluster wiring tests.

package main

import "testing"

func TestParseClusterPeers_Empty(t *testing.T) {
	got, err := parseClusterPeers("")
	if err != nil {
		t.Fatalf("empty input: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty input: got %d entries, want 0", len(got))
	}
}

func TestParseClusterPeers_OnePeer(t *testing.T) {
	got, err := parseClusterPeers("node-b=127.0.0.1:8443")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["node-b"] != "127.0.0.1:8443" {
		t.Errorf("got %v, want node-b=127.0.0.1:8443", got)
	}
}

func TestParseClusterPeers_MultiplePeers(t *testing.T) {
	got, err := parseClusterPeers("node-b=127.0.0.1:8443, node-c=127.0.0.1:8444")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got["node-b"] != "127.0.0.1:8443" || got["node-c"] != "127.0.0.1:8444" {
		t.Errorf("got %v", got)
	}
}

func TestParseClusterPeers_InvalidFormat(t *testing.T) {
	cases := []string{
		"node-b",            // no =
		"=127.0.0.1:8443",   // empty id
		"node-b=",           // empty address
	}
	for _, in := range cases {
		if _, err := parseClusterPeers(in); err == nil {
			t.Errorf("parseClusterPeers(%q) = nil err, want error", in)
		}
	}
}

func TestParseClusterPeers_DuplicateRejected(t *testing.T) {
	_, err := parseClusterPeers("node-b=addr1,node-b=addr2")
	if err == nil {
		t.Errorf("duplicate node-id: want error, got nil")
	}
}
