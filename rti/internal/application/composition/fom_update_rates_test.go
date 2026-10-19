package composition

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func updateRateModule(name, rate string) core.FOMModule {
	return core.FOMModule{Path: name + ".xml", XML: []byte(fmt.Sprintf(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><updateRates><updateRate><name>%s</name><rate>%s</rate><semantics>test rate</semantics></updateRate></updateRates></objectModel>`, name, rate))}
}

func TestFOMUpdateRatesRetainedThroughProductionLoad(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	h, err := repo.Load(ctx, []core.FOMModule{updateRateModule("Fast", "12.5"), updateRateModule("Slow", ".25")})
	if err != nil {
		t.Fatal(err)
	}
	lookup, ok := h.(interface{ UpdateRateValue(string) (float64, bool) })
	if !ok {
		t.Fatal("production FOM discards update-rate declarations: lookup unavailable")
	}
	for name, want := range map[string]float64{"Fast": 12.5, "Slow": .25} {
		got, found := lookup.UpdateRateValue(name)
		if !found || got != want {
			t.Errorf("rate %q = %v, %v; want %v", name, got, found, want)
		}
	}
	if _, found := lookup.UpdateRateValue("Unknown"); found {
		t.Fatal("unknown rate resolved")
	}
	if _, found := lookup.UpdateRateValue("fast"); found {
		t.Fatal("rate lookup lost case sensitivity")
	}
	if _, found := h.LookupObjectClass("HLAobjectRoot"); !found {
		t.Fatal("rate loading lost MIM")
	}
}

func TestFOMUpdateRateConflictsRejectBeforeRepositoryMutation(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base := updateRateModule("Fast", "12.5")
	h, err := repo.Load(ctx, []core.FOMModule{base})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("live", h)
	digest, _ := repo.DigestFor("live")
	_, err = repo.Load(ctx, []core.FOMModule{base, updateRateModule("Fast", "25")})
	if err == nil {
		t.Fatal("conflicting named rates silently accepted")
	}
	if !strings.Contains(err.Error(), "Fast.xml") {
		t.Errorf("missing module path: %v", err)
	}
	got, _ := repo.Get(ctx, "live")
	afterDigest, _ := repo.DigestFor("live")
	if got != h || afterDigest != digest {
		t.Fatal("failed validation changed live repository")
	}
}

func TestFOMUpdateRateInvalidDecimalRejected(t *testing.T) {
	for _, rate := range []string{"", "NaN", "INF", "+Inf", "1e2", "0x1p2", "1_000", "1.2.3", strings.Repeat("9", 400), "0." + strings.Repeat("0", 400) + "1"} {
		t.Run(rate, func(t *testing.T) {
			_, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{updateRateModule("Rate", rate)})
			if err == nil {
				t.Fatal("invalid or unrepresentable decimal accepted")
			}
		})
	}
}

func TestFOMUpdateRatesDecimalEquivalence(t *testing.T) {
	for _, tc := range []struct {
		rate, equivalent string
		want             float64
	}{
		{"1", "1.000", 1},
		{"0.25", "+00.250", .25},
		{"+0012.500", "12.5", 12.5},
		{".5", "0.500", .5},
		{"5.", "005.0", 5},
	} {
		t.Run(tc.rate, func(t *testing.T) {
			h, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{
				updateRateModule("Rate", tc.rate),
				updateRateModule("Rate", tc.rate),
				updateRateModule("Rate", tc.equivalent),
			})
			if err != nil {
				t.Fatal(err)
			}
			lookup, ok := h.(interface{ UpdateRateValue(string) (float64, bool) })
			if !ok {
				t.Fatal("equivalent rate declarations lost lookup")
			}
			if got, found := lookup.UpdateRateValue("Rate"); !found || got != tc.want {
				t.Fatalf("equivalent rates resolved to %v, %v; want %v", got, found, tc.want)
			}
		})
	}
	t.Run("reverse-equivalence", func(t *testing.T) {
		_, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{updateRateModule("Rate", "12.5"), updateRateModule("Rate", "+0012.500")})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestFOMUpdateRateNonpositiveRejectedBeforeRepositoryMutation(t *testing.T) {
	for _, rate := range []string{"0", "-0.000", "-0.25", "-1"} {
		t.Run(rate, func(t *testing.T) {
			ctx := context.Background()
			repo := NewFOMRepository()
			base := updateRateModule("Fast", "12.5")
			h, err := repo.Load(ctx, []core.FOMModule{base})
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor("live", h)
			digest, ok := repo.DigestFor("live")
			if !ok {
				t.Fatal("baseline digest unavailable")
			}
			invalid := updateRateModule("Invalid", rate)
			for _, tc := range []struct {
				name    string
				modules []core.FOMModule
			}{
				{"invalid-only", []core.FOMModule{invalid}},
				{"valid-first", []core.FOMModule{base, invalid}},
				{"invalid-first", []core.FOMModule{invalid, base}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					candidate, err := repo.Load(ctx, tc.modules)
					if candidate != nil || err == nil {
						t.Fatalf("nonpositive rate admitted: %v %v", candidate, err)
					}
					for _, text := range []string{"FOM-016", invalid.Path, "greater than zero"} {
						if !strings.Contains(err.Error(), text) {
							t.Errorf("missing %q in rejection: %v", text, err)
						}
					}
					got, err := repo.Get(ctx, "live")
					afterDigest, ok := repo.DigestFor("live")
					if err != nil || got != h || !ok || afterDigest != digest {
						t.Fatal("nonpositive admission changed live repository")
					}
				})
			}
		})
	}
}
