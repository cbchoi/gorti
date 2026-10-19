package composition

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestG13FDDDelaySwitchImmutableAcrossExtension(t *testing.T) {
	for _, setting := range []string{"absent", "false", "true"} {
		t.Run(setting, func(t *testing.T) {
			ctx := context.Background()
			xml := `<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>`
			if setting != "absent" {
				xml += `<switches><delaySubscriptionEvaluation isEnabled="` + setting + `"/></switches>`
			}
			xml += `</objectModel>`
			repo := NewFOMRepository()
			handle, err := repo.Load(ctx, []core.FOMModule{{Path: "creation.xml", XML: []byte(xml)}})
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor("fed", handle)
			before, _ := handle.(core.FOMSubscriptionEvaluation).DelaySubscriptionEvaluation()
			opposite := "true"
			if before {
				opposite = "false"
			}
			additional := []core.FOMModule{{Path: "change.xml", XML: []byte(`<objectModel><switches><delaySubscriptionEvaluation isEnabled="` + opposite + `"/></switches></objectModel>`)}}
			if _, err := repo.PrepareExtension(ctx, "fed", additional); err == nil {
				t.Fatal("join FOM can change effective creation-time switch")
			}
			after, err := repo.Get(ctx, "fed")
			if err != nil || after != handle {
				t.Fatal("failed extension changed live FDD", err)
			}
		})
	}
}

func TestG13FDDDelaySwitchImmutableAcrossLiveExtension(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base, err := repo.Load(ctx, []core.FOMModule{{Path: "base.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", base)
	additional := []core.FOMModule{{Path: "switch.xml", XML: []byte(`<objectModel><switches><delaySubscriptionEvaluation isEnabled="true"/></switches></objectModel>`)}}
	if _, err := repo.PrepareLiveExtension(ctx, "fed", additional); err == nil {
		t.Fatal("live join extension bypasses immutable creation switch")
	}
	if current, err := repo.Get(ctx, "fed"); err != nil || current != base {
		t.Fatal("rejected live extension changed federation model", err)
	}
}
