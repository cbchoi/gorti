package m36spec

import "testing"

// S1516.1 11.4.1: MOM lifecycle reflections and removals have no time or tag.
func TestApplicableTimeSupportMOMUntimedUntaggedLifecycle(t *testing.T) {
	m, declarations, out := newMOMStack(t)
	ctx := driveLifecycle(t, m, declarations)
	if err := m.FederateJoined(ctx, fedName, 2, "review-subject", "review-type"); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateResigned(ctx, fedName, 2); err != nil {
		t.Fatal(err)
	}
	reflections, removals := 0, 0
	for _, event := range out.SentTo(1) {
		if reflection := event.GetReflect(); reflection != nil {
			reflections++
			if reflection.LogicalTime != nil || reflection.LogicalTimeValue != nil || len(reflection.UserSuppliedTag) != 0 {
				t.Fatalf("MOM reflection carries a delivery timestamp or tag: %+v", reflection)
			}
		}
		if removal := event.GetRemove(); removal != nil {
			removals++
			if removal.LogicalTime != nil || removal.LogicalTimeValue != nil || len(removal.UserSuppliedTag) != 0 {
				t.Fatalf("MOM removal carries a delivery timestamp or tag: %+v", removal)
			}
		}
	}
	if reflections != 2 || removals != 1 {
		t.Fatalf("expected two concrete reflections and one removal, got %d/%d", reflections, removals)
	}
}
