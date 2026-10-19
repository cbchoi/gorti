package federate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type sdkRuntimeOutcome struct {
	Identity  string `json:"identity"`
	Success   bool   `json:"success"`
	Exception string `json:"exception"`
}

func (o *sdkRuntimeOutcome) UnmarshalJSON(encoded []byte) error {
	var fields struct {
		Identity  *string `json:"identity"`
		Success   *bool   `json:"success"`
		Exception *string `json:"exception"`
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if err := d.Decode(&fields); err != nil {
		return err
	}
	if fields.Identity == nil || fields.Success == nil || fields.Exception == nil {
		return errors.New("federate: incomplete finished callback outcome")
	}
	o.Identity, o.Success, o.Exception = *fields.Identity, *fields.Success, *fields.Exception
	return nil
}

// Recovery carriers are not application events. In particular, translating a
// saved grant, discovery or ownership body must not apply its effects again.
type callbackOutcomeRecovery struct{ CallbackInfo }

func (callbackOutcomeRecovery) isFederateEvent() {}

func callbackIdentity(receipt []byte) string {
	if len(receipt) == 0 {
		return ""
	}
	identity := sha256.Sum256(receipt)
	return hex.EncodeToString(identity[:])
}

func validCallbackIdentity(identity string) bool {
	decoded, err := hex.DecodeString(identity)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == identity
}

func callbackRecoveryInfo(wire *rtiv1.FederateEvent) (CallbackInfo, error) {
	info := CallbackInfo{identity: callbackIdentity(wire.CallbackReceipt)}
	if recovery := wire.GetCallbackRecovery(); recovery != nil {
		info.recovered = true
		if len(recovery.InvocationIdentity) != sha256.Size || (recovery.Completed && !recovery.OutcomeOnly) {
			return info, errors.New("federate: invalid callback recovery identity or phase")
		}
		info.identity = hex.EncodeToString(recovery.InvocationIdentity)
		info.outcomeOnly, info.completed = recovery.OutcomeOnly, recovery.Completed
		if recovery.Completed {
			if wire.Event != nil || len(wire.CallbackReceipt) != 0 || wire.OwnershipReceipt != nil {
				return info, errors.New("federate: completed callback marker carries an invocation capability")
			}
		} else if wire.Event == nil || len(wire.CallbackReceipt) != sha256.Size {
			return info, errors.New("federate: callback recovery carrier requires a body and fresh receipt")
		}
	}
	return info, nil
}

func (f *Federate) reconcileCompletedCallback(identity string) {
	f.genericCallbackMu.Lock()
	delete(f.genericCallbacks, identity)
	for i := 0; i < len(f.genericCallbackCompleted); {
		if f.genericCallbackCompleted[i] == identity {
			f.genericCallbackCompleted = append(f.genericCallbackCompleted[:i], f.genericCallbackCompleted[i+1:]...)
		} else {
			i++
		}
	}
	f.genericCallbackMu.Unlock()
	for i := 0; i < len(f.runtimePending); {
		if f.runtimePending[i].identity == identity {
			f.removeRuntimePending(f.runtimePending[i].id)
		} else {
			i++
		}
	}
}

func (f *Federate) recoveredOutcome(event Event) (sdkRuntimeOutcome, error) {
	info, _ := runtimeInfo(event)
	f.genericCallbackMu.Lock()
	defer f.genericCallbackMu.Unlock()
	entry := f.genericCallbacks[info.identity]
	if !info.outcomeOnly || entry == nil || !entry.complete {
		return sdkRuntimeOutcome{}, errors.New("federate: recovery carrier has no known finished callback outcome")
	}
	// A new receipt can retry the result, but never reopens application admission.
	previousReceipt := entry.reportedReceipt
	if entry.event != nil {
		previousReceipt = entry.event.(interface{ CallbackReceipt() []byte }).CallbackReceipt()
	}
	if !bytes.Equal(event.(interface{ CallbackReceipt() []byte }).CallbackReceipt(), previousReceipt) {
		entry.event, entry.reported = event, false
		entry.reportedReceipt = nil
		for i := 0; i < len(f.genericCallbackCompleted); {
			if f.genericCallbackCompleted[i] == info.identity {
				f.genericCallbackCompleted = append(f.genericCallbackCompleted[:i], f.genericCallbackCompleted[i+1:]...)
			} else {
				i++
			}
		}
	}
	return sdkRuntimeOutcome{Identity: info.identity, Success: entry.success, Exception: entry.exception}, nil
}
