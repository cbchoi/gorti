package session_test

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
)

func TestHostFollowsLiveMembershipNotRestoredHandle(t *testing.T) {
	mgr, members, _, token := setup(t)
	mgr.RegisterWithHost(members.id, token, "reported.example")
	check := func(want string) {
		t.Helper()
		release, err := members.AcquireMember("f", members.id.Handle)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		host, known, err := mgr.Host("f", members.id.Handle)
		if err != nil || host != want || known != (want != "") {
			t.Fatalf("host=%q known=%t err=%v, want %q", host, known, err, want)
		}
	}
	check("reported.example")
	members.id.Handle = 8
	check("reported.example")
	members.id.Incarnation++
	check("")
	newToken, err := session.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(members.id, newToken)
	check("")
	mgr.RegisterWithHost(members.id, newToken, "replacement.example")
	check("replacement.example")
	members.id.Generation++
	check("")
	mgr.RegisterWithHost(members.id, newToken, "next.example")
	check("next.example")
	mgr.Close()
	check("")
}
