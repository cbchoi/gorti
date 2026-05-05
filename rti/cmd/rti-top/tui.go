// runTUI is the bubbletea entry point. Commit 1 ships a stub; commit
// 2 will replace it with the full MVU loop. Keeping the seam here so
// main.go's Status-smoke path is self-contained and unchanged across
// follow-up commits.

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cbchoi/gorti/rti/cmd/rti-top/internal/client"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

func runTUI(_ context.Context, _ *client.Client, st *rtiv1.StatusResponse, refresh time.Duration) error {
	// Phase-2 commit-1 placeholder. Status smoke succeeded — print
	// what we have and exit. Commit 2 replaces this with the
	// bubbletea Model + tea.NewProgram(...).Run() drive loop.
	fmt.Printf("rti-top: connected (rtid_version=%s uptime=%ds, refresh=%s)\n",
		st.GetRtidVersion(), st.GetUptimeSeconds(), refresh)
	fmt.Println("rti-top: TUI not yet wired (commit 1); use --smoke for status check.")
	return nil
}
