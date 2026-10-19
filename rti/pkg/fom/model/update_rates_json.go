package model

import (
	"encoding/json"
	"fmt"
)

type updateRateJSON UpdateRate

// MarshalJSON retains the exact source decimal alongside the legacy runtime
// number. Older snapshots without ExactRate still decode as before.
func (r UpdateRate) MarshalJSON() ([]byte, error) {
	if r.exactRate == "" {
		return json.Marshal(updateRateJSON(r))
	}
	decimal, err := r.DecimalString()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		updateRateJSON
		ExactRate string
	}{updateRateJSON(r), decimal})
}

func (r *UpdateRate) UnmarshalJSON(data []byte) error {
	var value *struct {
		updateRateJSON
		ExactRate *string
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	next := UpdateRate(value.updateRateJSON)
	if value.ExactRate != nil {
		exact, err := NewUpdateRate(next.Name, *value.ExactRate, next.Semantics)
		if err != nil {
			return err
		}
		if exact.Rate != next.Rate {
			return fmt.Errorf("update rate %q differs from its JSON exact decimal", next.Name)
		}
		next.exactRate = exact.exactRate
	}
	*r = next
	return nil
}
