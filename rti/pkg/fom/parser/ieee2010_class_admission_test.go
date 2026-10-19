package parser_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// Source: IEEE 1516.1-2010 4.1.4-4.1.4.1, PDF pp. 47-49;
// IEEE 1516.2-2010 Table 50, PDF p. 92. Only name-only ancestors
// are scaffolding; a regular declaration's local member set cannot grow.
func TestIEEE2010CompleteClassMemberAdmission(t *testing.T) {
	for _, kind := range []string{testObjectKind, testInteractionKind} {
		for parseName, parse := range classAdmissionParsers() {
			for _, tc := range []struct {
				name         string
				before, next []string
			}{
				{"disjoint", []string{"A"}, []string{"B"}},
				{"superset", []string{"A"}, []string{"A", "B"}},
				{"subset", []string{"A", "B"}, []string{"A"}},
				{"remove_all", []string{"A"}, nil},
				{"grow_empty_complete", nil, []string{"A"}},
			} {
				t.Run(kind+"/"+parseName+"/"+tc.name, func(t *testing.T) {
					base := admissionModule(kind, "base.xml", admissionClass(kind, "Shared", true, tc.before, ""))
					next := admissionModule(kind, "next.xml", admissionClass(kind, "Shared", true, tc.next, "")+admissionClass(kind, "NewClass", true, []string{"Valid"}, ""))
					before, err := parse([]parser.Module{base})
					if err != nil || before.FOM == nil || len(before.Diagnostics) != 0 {
						t.Fatalf("invalid baseline: %v %+v", err, before)
					}
					result, err := parse([]parser.Module{base, next})
					if err != nil || result.FOM != nil || !result.HasCode("FOM-014") {
						t.Fatalf("changed complete member set accepted: %v %+v", err, result)
					}
					for _, diagnostic := range result.Diagnostics {
						if diagnostic.Code == "FOM-014" && (diagnostic.ModulePath != "next.xml" || !strings.Contains(diagnostic.Message, "Shared")) {
							t.Fatalf("wrong conflict location: %+v", diagnostic)
						}
					}
					after, err := parse([]parser.Module{base})
					if err != nil || !reflect.DeepEqual(before, after) {
						t.Fatalf("failed admission changed the original model: %v %+v", err, after)
					}
				})
			}
		}
	}
}

func TestIEEE2010ScaffoldingCompletionAndNewClasses(t *testing.T) {
	for _, kind := range []string{testObjectKind, testInteractionKind} {
		for parseName, parse := range classAdmissionParsers() {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reverse=%t", kind, parseName, reverse), func(t *testing.T) {
					complete := admissionModule(kind, "complete.xml", admissionClass(kind, "Shared", true, []string{"A", "B"}, ""))
					scaffold := admissionModule(kind, "subclass.xml", admissionClass(kind, "Shared", false, nil, admissionClass(kind, "Child", true, []string{"C"}, ""))+admissionClass(kind, "Sibling", true, []string{"D"}, ""))
					modules := []parser.Module{complete, scaffold}
					if reverse {
						modules[0], modules[1] = modules[1], modules[0]
					}
					result, err := parse(modules)
					if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
						t.Fatalf("legal class/scaffolding merge rejected: %v %+v", err, result)
					}
					root := "HLAobjectRoot"
					if kind == testInteractionKind {
						root = "HLAinteractionRoot"
					}
					want := map[string][]string{root: nil, root + ".Shared": {"A", "B"}, root + ".Shared.Child": {"C"}, root + ".Sibling": {"D"}}
					if got := admissionMembers(result.FOM.(*model.FOM), kind); !reflect.DeepEqual(got, want) {
						t.Fatalf("class membership changed: got %+v want %+v", got, want)
					}
					growth := admissionModule(kind, "growth.xml", admissionClass(kind, "Shared", true, []string{"A", "B", "E"}, ""))
					result, err = parse(append(modules, growth))
					if err != nil || result.FOM != nil || !result.HasCode("FOM-014") {
						t.Fatalf("completed scaffold still allowed member growth: %v %+v", err, result)
					}
				})
			}
		}
	}
}

func TestIEEE2010IdenticalMemberSetsIgnoreDeclarationOrder(t *testing.T) {
	for _, kind := range []string{testObjectKind, testInteractionKind} {
		for parseName, parse := range classAdmissionParsers() {
			t.Run(kind+"/"+parseName, func(t *testing.T) {
				first := admissionModule(kind, "first.xml", admissionClass(kind, "Shared", true, []string{"A", "B"}, ""))
				next := admissionModule(kind, "next.xml", admissionClass(kind, "Shared", true, []string{"B", "A"}, ""))
				baseline, err := parse([]parser.Module{first})
				if err != nil || baseline.FOM == nil {
					t.Fatalf("baseline: %v %+v", err, baseline)
				}
				result, err := parse([]parser.Module{first, next})
				if err != nil || !reflect.DeepEqual(result, baseline) {
					t.Fatalf("equivalent set changed initial declaration order: %v %+v", err, result)
				}
			})
		}
	}
}

func TestIEEE2010ClassDescriptionsDoNotRejectMerge(t *testing.T) {
	for _, kind := range []string{testObjectKind, testInteractionKind} {
		for _, target := range []string{testClassMetadata, "member", testUpdateCondition} {
			if kind == testInteractionKind && target == testUpdateCondition {
				continue
			}
			t.Run(kind+"/"+target, func(t *testing.T) {
				testFDDDescriptions(t, kind+"_"+target, func(description string) string {
					fragment := admissionClass(kind, "Shared", true, []string{"A"}, "")
					tag, closeTag := "semantics", "</dataType>"
					if target == testClassMetadata {
						closeTag = "<name>Shared</name>"
					} else if target == testUpdateCondition {
						tag = "updateCondition"
					}
					fragment = strings.Replace(fragment, closeTag, closeTag+"<"+tag+">"+description+"</"+tag+">", 1)
					module := admissionModule(kind, "descriptive.xml", fragment)
					return strings.TrimSuffix(strings.TrimPrefix(string(module.XML), "<objectModel>"), "</objectModel>")
				}, func(f *model.FOM) model.OptionalString {
					if kind == testObjectKind {
						for _, c := range f.ObjectClasses() {
							if c.Name == "Shared" {
								if c.Sharing != "PublishSubscribe" || len(c.Attributes) != 1 || c.Attributes[0].Name != "A" || c.Attributes[0].DataType != "HLAinteger32BE" {
									t.Fatalf("object declaration changed: %+v", c)
								}
								if target == testClassMetadata {
									return c.Semantics
								}
								if target == testUpdateCondition {
									return c.Attributes[0].UpdateCondition
								}
								return c.Attributes[0].Semantics
							}
						}
					} else {
						for _, c := range f.InteractionClasses() {
							if c.Name == "Shared" {
								if c.Sharing != "PublishSubscribe" || c.Order != "Receive" || c.Transportation != "HLAreliable" || len(c.Parameters) != 1 || c.Parameters[0].Name != "A" || c.Parameters[0].DataType != "HLAinteger32BE" {
									t.Fatalf("interaction declaration changed: %+v", c)
								}
								if target == testClassMetadata {
									return c.Semantics
								}
								return c.Parameters[0].Semantics
							}
						}
					}
					t.Fatal("Shared class missing")
					return model.OptionalString{}
				})
			})
		}
	}
}

func TestIEEE2010ClassOperationalConflictsStillReject(t *testing.T) {
	for _, kind := range []string{testObjectKind, testInteractionKind} {
		for parseName, parse := range classAdmissionParsers() {
			base := admissionModule(kind, "base.xml", admissionClass(kind, "Shared", true, []string{"A"}, ""))
			xml := string(base.XML)
			if kind == testObjectKind {
				xml = strings.Replace(xml, "</attribute>", "<transportation>HLAreliable</transportation><order>Receive</order></attribute>", 1)
			}
			xml = strings.Replace(xml, "<order>Receive</order>", "<order>Receive</order><dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions>", 1)
			xml = strings.Replace(xml, "</objectModel>", "<dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension><dimension><name>Y</name><upperBound>100</upperBound></dimension></dimensions></objectModel>", 1)
			base.XML = []byte(xml)
			for _, tc := range []struct{ name, before, after string }{
				{"datatype", "HLAinteger32BE", "HLAinteger64BE"},
				{"transportation", "HLAreliable", "HLAbestEffort"},
				{"order", "<order>Receive</order>", "<order>TimeStamp</order>"},
				{"dimensions", "<dimension>X</dimension><dimension>Y</dimension>", "<dimension>Y</dimension>"},
				{"sharing", "PublishSubscribe", "Publish"},
			} {
				t.Run(kind+"/"+parseName+"/"+tc.name, func(t *testing.T) {
					baseline, err := parse([]parser.Module{base})
					if err != nil || baseline.FOM == nil || len(baseline.Diagnostics) != 0 {
						t.Fatalf("invalid baseline: %v %+v", err, baseline)
					}
					changed := strings.Replace(xml, tc.before, tc.after, 1)
					if changed == xml {
						t.Fatal("conflict fixture unchanged")
					}
					changed = strings.Replace(changed, "<name>Shared</name>", "<name>Shared</name><semantics>new description</semantics>", 1)
					result, err := parse([]parser.Module{base, {Path: "conflict.xml", XML: []byte(changed)}})
					if err != nil || result.FOM != nil || !result.HasCode("FOM-014") {
						t.Fatalf("operational conflict hidden by description: %v %+v", err, result)
					}
				})
			}
			t.Run(kind+"/"+parseName+"/dimension_order", func(t *testing.T) {
				changed := strings.Replace(xml, "<dimension>X</dimension><dimension>Y</dimension>", "<dimension>Y</dimension><dimension>X</dimension>", 1)
				baseline, err := parse([]parser.Module{base})
				if err != nil || baseline.FOM == nil {
					t.Fatalf("baseline: %v %+v", err, baseline)
				}
				result, err := parse([]parser.Module{base, {Path: "reordered.xml", XML: []byte(changed)}})
				if err != nil || !reflect.DeepEqual(result, baseline) {
					t.Fatalf("dimension set changed: %v %+v", err, result)
				}
			})
		}
	}
}

func classAdmissionParsers() map[string]func([]parser.Module) (parser.Result, error) {
	return map[string]func([]parser.Module) (parser.Result, error){"parse": parser.Parse, "additive": parser.ParseAdditive}
}

func admissionModule(kind, path, classes string) parser.Module {
	collection, class, root := "objects", "objectClass", "HLAobjectRoot"
	if kind == testInteractionKind {
		collection, class, root = "interactions", "interactionClass", "HLAinteractionRoot"
	}
	return parser.Module{Path: path, XML: []byte(fmt.Sprintf(`<objectModel><%s><%s><name>%s</name>%s</%s></%s></objectModel>`, collection, class, root, classes, class, collection))}
}

func admissionClass(kind, name string, complete bool, members []string, children string) string {
	class, member, properties := "objectClass", "attribute", ""
	if kind == testInteractionKind {
		class, member = "interactionClass", "parameter"
	}
	if complete {
		properties = "<sharing>PublishSubscribe</sharing>"
		if kind == testInteractionKind {
			properties += "<transportation>HLAreliable</transportation><order>Receive</order>"
		}
	}
	for _, name := range members {
		properties += fmt.Sprintf(`<%s><name>%s</name><dataType>HLAinteger32BE</dataType></%s>`, member, name, member)
	}
	return fmt.Sprintf(`<%s><name>%s</name>%s%s</%s>`, class, name, properties, children, class)
}

func admissionMembers(f *model.FOM, kind string) map[string][]string {
	got := make(map[string][]string)
	if kind == testObjectKind {
		for _, c := range f.ObjectClasses() {
			got[c.Key()] = nil
			for _, a := range c.Attributes {
				got[c.Key()] = append(got[c.Key()], a.Name)
			}
		}
	} else {
		for _, c := range f.InteractionClasses() {
			got[c.Key()] = nil
			for _, p := range c.Parameters {
				got[c.Key()] = append(got[c.Key()], p.Name)
			}
		}
	}
	return got
}

const (
	testInteractionKind = "interaction"
	testObjectKind      = "object"
	testClassMetadata   = "class"
	testUpdateCondition = "update_condition"
	testReliableYes     = "Yes"
)
