package parser

import (
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type xmlAdvisorySwitches struct {
	AutoProvide     []xmlAdvisorySwitch        `xml:"autoProvide"`
	Regions         []xmlAdvisorySwitch        `xml:"conveyRegionDesignatorSets"`
	Producer        []xmlAdvisorySwitch        `xml:"conveyProducingFederate"`
	Services        []xmlAdvisorySwitch        `xml:"serviceReporting"`
	Exceptions      []xmlAdvisorySwitch        `xml:"exceptionReporting"`
	Delay           []xmlAdvisorySwitch        `xml:"delaySubscriptionEvaluation"`
	AutomaticResign []xmlAutomaticResignSwitch `xml:"automaticResignAction"`
	Object          []xmlAdvisorySwitch        `xml:"objectClassRelevanceAdvisory"`
	Attribute       []xmlAdvisorySwitch        `xml:"attributeRelevanceAdvisory"`
	Scope           []xmlAdvisorySwitch        `xml:"attributeScopeAdvisory"`
	Interaction     []xmlAdvisorySwitch        `xml:"interactionRelevanceAdvisory"`
}
type xmlAdvisorySwitch struct {
	Enabled  *string    `xml:"isEnabled,attr"`
	Content  string     `xml:",chardata"`
	Children []struct{} `xml:",any"`
}

func mergeAdvisoryDeclarations(path string, base []model.AdvisorySwitch, raw *xmlAdvisorySwitches) ([]model.AdvisorySwitch, []Diagnostic) {
	if raw == nil {
		return base, nil
	}
	var incoming []model.AdvisorySwitch
	var diags []Diagnostic
	reject := func(message string) {
		diags = append(diags, Diagnostic{Code: "FOM-017", ModulePath: path, Message: message})
	}
	for _, group := range []struct {
		name   string
		values []xmlAdvisorySwitch
	}{
		{"autoProvide", raw.AutoProvide}, {"conveyRegionDesignatorSets", raw.Regions}, {"conveyProducingFederate", raw.Producer},
		{"serviceReporting", raw.Services}, {"exceptionReporting", raw.Exceptions}, {"delaySubscriptionEvaluation", raw.Delay},
		{"objectClassRelevanceAdvisory", raw.Object}, {"attributeRelevanceAdvisory", raw.Attribute}, {"attributeScopeAdvisory", raw.Scope}, {"interactionRelevanceAdvisory", raw.Interaction},
	} {
		if len(group.values) > 1 {
			reject(fmt.Sprintf("duplicate advisory switch %q", group.name))
			continue
		}
		for _, value := range group.values {
			if strings.TrimSpace(value.Content) != "" || len(value.Children) != 0 {
				reject(fmt.Sprintf("advisory switch %q cannot contain content", group.name))
				continue
			}
			// switchType.isEnabled defaults to false in the supplied XSD.
			// This applies only to a present element, never an absent switch.
			enabled := false
			if value.Enabled != nil {
				switch strings.TrimSpace(*value.Enabled) {
				case "true", "1":
					enabled = true
				case "false", "0":
				default:
					reject(fmt.Sprintf("advisory switch %q has invalid xs:boolean isEnabled", group.name))
					continue
				}
			}
			incoming = append(incoming, model.AdvisorySwitch{Name: group.name, Enabled: enabled})
		}
	}
	out, err := model.MergeAdvisorySwitches(base, incoming)
	if err != nil {
		reject(err.Error())
	}
	return out, diags
}
