package model

// AutomaticResignAction is the explicit FDD declaration; empty means absent.
func (f *FOM) AutomaticResignAction() string { return f.automaticResignAction }

func (f *FOM) WithAutomaticResignAction(action string) *FOM {
	cloned := *f
	cloned.automaticResignAction = action
	return &cloned
}
