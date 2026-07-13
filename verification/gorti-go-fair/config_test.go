package main

import (
	"strings"
	"testing"
)

func TestConfigRequiresFairComparisonInputs(t *testing.T) {
	base := []string{"--role=publisher", "--federation=f", "--fom=f.xml", "--seed=1516", "--count=2", "--output=out"}
	if _, err := parseConfig(base); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"--federation=f", "--fom=f.xml", "--seed=1516", "--count=2", "--output=out"} {
		args := make([]string, 0, len(base)-1)
		for _, value := range base {
			if value != required {
				args = append(args, value)
			}
		}
		if _, err := parseConfig(args); err == nil {
			t.Fatalf("missing %s was accepted", strings.Split(required, "=")[0])
		}
	}
}

func TestConfigUsesPublisherAndSubscriberActors(t *testing.T) {
	for _, actor := range []string{"publisher", "subscriber"} {
		cfg, err := parseConfig([]string{
			"--role=" + actor, "--federation=f", "--fom=f.xml", "--seed=s", "--count=1", "--output=out",
		})
		if err != nil || string(cfg.Role) != actor {
			t.Fatalf("role %s: cfg=%+v err=%v", actor, cfg, err)
		}
	}
}
