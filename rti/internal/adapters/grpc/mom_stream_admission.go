package grpc

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConfirmedStreamMembershipAdmitted is for a transport admission wrapper that
// already holds the member lease and has checked the authenticated identity and
// open generation. It prevents validateOpen from recursively entering that lease.
func ConfirmedStreamMembershipAdmitted(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmedObjectMembershipValidatedKey{}, true)
}

// LocalLRCMOMFrameAdmission is an optional transport capability on membership
// composition. Ordinary guard/leaser capabilities remain independently intact.
type LocalLRCMOMFrameAdmission interface {
	AdmitLocalLRCFrame(context.Context, core.FederationName, core.FederateHandle, *rtiv1.LocalLRCRequest) (context.Context, func(), bool, error)
}

type LocalLRCMOMOpenAdmission interface {
	AdmitLocalLRCOpen(context.Context, core.FederationName, core.FederateHandle, uint64) (func(), error)
}

func (f *localLRCFrameFencer) acquireFrame(ctx context.Context, frame *rtiv1.LocalLRCRequest) (context.Context, localLRCFrameLease, error) {
	if f.momAdmission != nil {
		admitted, release, handled, err := f.momAdmission.AdmitLocalLRCFrame(ctx, f.fed, f.handle, frame)
		if err != nil {
			if release != nil {
				release()
			}
			return ctx, localLRCFrameLease{}, err
		}
		if handled {
			if admitted == nil || release == nil {
				if release != nil {
					release()
				}
				return ctx, localLRCFrameLease{}, status.Error(codes.Internal, "MOM LocalLRC admission returned no scope lease")
			}
			return admitted, localLRCFrameLease{release: release, management: true}, nil
		}
		if release != nil {
			release()
			return ctx, localLRCFrameLease{}, status.Error(codes.Internal, "ordinary LocalLRC frame received a special admission lease")
		}
	}
	lease, err := f.acquire(ctx)
	return ctx, lease, err
}
