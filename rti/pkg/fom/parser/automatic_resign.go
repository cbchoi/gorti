package parser

import "strings"

type xmlAutomaticResignSwitch struct {
	Action   *string    `xml:"resignAction,attr"`
	Content  string     `xml:",chardata"`
	Children []struct{} `xml:",any"`
}

func mergeAutomaticResign(path, base string, raw *xmlAdvisorySwitches) (string, []Diagnostic) {
	if raw == nil || len(raw.AutomaticResign) == 0 {
		return base, nil
	}
	reject := func() (string, []Diagnostic) {
		return base, []Diagnostic{{Code: "FOM-017", ModulePath: path, Message: "invalid, duplicate or conflicting automaticResignAction"}}
	}
	if len(raw.AutomaticResign) != 1 {
		return reject()
	}
	value := raw.AutomaticResign[0]
	if strings.TrimSpace(value.Content) != "" || len(value.Children) != 0 {
		return reject()
	}
	action := "NoAction"
	if value.Action != nil {
		action = strings.TrimSpace(*value.Action)
	}
	switch action {
	case "UnconditionallyDivestAttributes", "DeleteObjects", "CancelPendingOwnershipAcquisitions", "DeleteObjectsThenDivest", "CancelThenDeleteThenDivest", "NoAction":
	default:
		return reject()
	}
	if base != "" && base != action {
		return reject()
	}
	return action, nil
}
