package mom

import (
	"strings"
	"testing"
)

func TestServiceGroupCatalog(t *testing.T) {
	seen := make(map[string]uint64)
	for group, names := range serviceGroupNames {
		for _, name := range strings.Fields(names) {
			if _, exists := seen[name]; !exists {
				seen[name] = uint64(group)
			}
			if got, ok := ServiceGroup(name); !ok || got != seen[name] {
				t.Fatalf("ServiceGroup(%q) = %d, %v; want %d, true", name, got, ok, seen[name])
			}
		}
	}
	for _, name := range []string{"", "unknownService", " sendInteraction", "sendInteraction ", "SendInteraction"} {
		if got, ok := ServiceGroup(name); ok || got != 0 {
			t.Fatalf("unknown ServiceGroup(%q) = %d, %v", name, got, ok)
		}
	}
}

func TestServiceGroupLookupDoesNotAllocate(t *testing.T) {
	for _, name := range []string{"updateAttributeValues", "sendInteraction", "unknownService"} {
		if allocations := testing.AllocsPerRun(100, func() { ServiceGroup(name) }); allocations != 0 {
			t.Errorf("ServiceGroup(%q) allocated %g times per lookup", name, allocations)
		}
	}
}

func BenchmarkServiceGroupLookup(b *testing.B) {
	for _, name := range []string{"updateAttributeValues", "sendInteraction", "unknownService"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ServiceGroup(name)
			}
		})
	}
}
