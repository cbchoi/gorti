package rtiv1

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	// Session authentication, ordered hydration, callback receipts, explicit
	// restore source, release denial, sync history and atomic name release.
	// MOM measurement maps preserve unavailable values without inventing zeros.
	// Time-enable callbacks append tags 70/71 without changing an existing RPC.
	// Save/restore failure callbacks append optional reason metadata at tag 2.
	// LogicalTimeValue adds an exact tagged value without changing service fields.
	// DDM snapshots retain inactive associations and their associating federate.
	// All logical-time scalars retain their tags beside exact message fields.
	// Unpublication adds optional attribute-set presence at declaration tag 6.
	// Unsubscription independently adds optional set presence at its own tag 6.
	// Historical baselines remain enforced by additive projection tests.
	// callback_report.proto uses the same go_package option as the other files;
	// Buf's M mappings select the current generated Go package directory.
	descriptorGoldenSHA256 = "d98e8aadcd1743fc3374bd79bd6c47f1373106d5d47ac44b250be8ff2ca62392"
	rpcGoldenSHA256        = "fd99e5c36f80c1455fa366405701f2921fe7a2eb9ab7845b68552afd51b70532"
	// Kept separately so every earlier projection still checks its original RPC surface.
	rpcCurrentGoldenSHA256 = "e5a95177a500e880e2cd65b83f5c856f92eec28add7498fac9a093f443484f97"
	// Explicit SDK callback negotiation and fenced local-delete add four RPCs.
	rpcOwnershipCallbackGoldenSHA256 = "58eac12e34a95e35a32a522a88b14a9a9a1d1f8787578d7cac4f4657cb36504d"
	rpcCallbackReportGoldenSHA256    = "e4eb60728fb47c121448b3bb7015de02a391579e702b2577bddd313eb25eaf2e"
)

func TestDescriptorAndRPCSurfaceGolden(t *testing.T) {
	checkDescriptorGolden(t, descriptorGoldenSHA256, rpcCallbackReportGoldenSHA256, nil)
}

// Removing only the reviewed additions must reproduce the previous descriptor
// byte-for-byte, including field numbers, types, optionality and RPC signatures.
func TestServiceCompletionWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "f1b6a47f318d550b18c5c922f4e907ab47e90f995d1f357d65115567d0921822", "8924ff7af84177264bac659960950849c9448b4f52dbdd421a452534586b6e67", projectBeforeServiceCompletion)
}

func projectBeforeServiceCompletion(file *descriptorpb.FileDescriptorProto) {
	projectBeforeLiveServices(file)
	var limits map[string]int32
	switch file.GetName() {
	case supportProtoFile:
		limits = map[string]int32{"GetObjectInstanceHandleRequest": 3, "GetObjectInstanceNameRequest": 3}
		file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool {
			return m.GetName() == "GetKnownObjectClassHandleRequest" || m.GetName() == "GetKnownObjectClassHandleResponse"
		})
	case declarationProtoFile:
		limits = map[string]int32{"SubObjAttrsRequest": 5, "SubInterRequest": 4}
		file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool { return m.GetName() == "SetAdvisorySwitchRequest" })
		file.EnumType = slices.DeleteFunc(file.EnumType, func(e *descriptorpb.EnumDescriptorProto) bool { return e.GetName() == "AdvisorySwitchKind" })
	case ddmProtoFile:
		limits = map[string]int32{"SubscribeOCAWithRegionsRequest": 6, "SubscribeICWithRegionsRequest": 5, "DDMSubscriptionEntry": 4, "DDMInteractionSubscriptionEntry": 3}
	}
	for _, message := range file.MessageType {
		if limit, ok := limits[message.GetName()]; ok {
			message.Field = slices.DeleteFunc(message.Field, func(f *descriptorpb.FieldDescriptorProto) bool { return f.GetNumber() > limit })
			message.OneofDecl = slices.DeleteFunc(message.OneofDecl, func(o *descriptorpb.OneofDescriptorProto) bool { return o.GetName() == "_active" })
		}
	}
	for _, service := range file.Service {
		service.Method = slices.DeleteFunc(service.Method, func(m *descriptorpb.MethodDescriptorProto) bool {
			return (file.GetName() == supportProtoFile && m.GetName() == "GetKnownObjectClassHandle") || (file.GetName() == declarationProtoFile && m.GetName() == "SetAdvisorySwitch")
		})
	}
}

func checkDescriptorGolden(t *testing.T, wantDescriptor, wantRPC string, project func(*descriptorpb.FileDescriptorProto)) {
	t.Helper()
	var files []protoreflect.FileDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if strings.HasPrefix(file.Path(), "rti/v1/") {
			files = append(files, file)
		}
		return true
	})
	slices.SortFunc(files, func(a, b protoreflect.FileDescriptor) int { return strings.Compare(a.Path(), b.Path()) })

	descriptorHash := sha256.New()
	var rpcSurface []string
	for _, file := range files {
		descriptor := protodesc.ToFileDescriptorProto(file)
		if project != nil && (wantDescriptor == descriptorBeforeCallbackReportSHA256 || wantDescriptor == descriptorBeforeGoPackageAlignmentSHA256) {
			project(descriptor)
		}
		if project != nil && wantDescriptor != descriptorBeforeCallbackReportSHA256 && wantDescriptor != descriptorBeforeGoPackageAlignmentSHA256 {
			projectBeforeCallbackReport(descriptor)
			projectBeforeCallbackRecovery(descriptor)
			if wantDescriptor == "f1b7677d6433548db6ef69ea0688376580616bd002393fe2a592a5875646275a" {
				project(descriptor)
			} else {
				projectBeforeDDMConveyedRegions(descriptor)
				if wantDescriptor != "cca85ee814fa5c39a39d93f54aca749c2cb05a9646df4f68f633c8783b61d8c8" {
					projectBeforeTransportationConfirmation(descriptor)
					if wantDescriptor != "8d37cdc552aebe505bdebc9f72b9d6b5aa29cfb13729210f446fef91d0443fcf" {
						projectBeforeCallbackInvocationEntryCapability(descriptor)
						if wantDescriptor != "20038e182cd5e540652417aa00f81065ed43989198245e73a56fdf735019deb7" {
							projectBeforeDDMRegionTemplate(descriptor)
						}
						if wantDescriptor != "3a4eeb6a6bdc0b834757b5c5661cc94ebf54a5fbd7cb680853cebd46fb3f71ce" && wantDescriptor != "20038e182cd5e540652417aa00f81065ed43989198245e73a56fdf735019deb7" {
							projectBeforeCallbackInvocationEntry(descriptor)
						}
					}
				}
				project(descriptor)
			}
		}
		// Projections can remove a newly introduced file, not only its fields.
		if descriptor.GetName() == "" {
			continue
		}
		data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(descriptor)
		if err != nil {
			t.Fatalf("marshal %s: %v", file.Path(), err)
		}
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(len(data)))
		descriptorHash.Write(size[:])
		descriptorHash.Write(data)
		for _, service := range descriptor.Service {
			for _, method := range service.Method {
				rpcSurface = append(rpcSurface, fmt.Sprintf("%s.%s/%s:%s>%s:cs=%t:ss=%t", descriptor.GetPackage(), service.GetName(), method.GetName(), strings.TrimPrefix(method.GetInputType(), "."), strings.TrimPrefix(method.GetOutputType(), "."), method.GetClientStreaming(), method.GetServerStreaming()))
			}
		}
	}
	slices.Sort(rpcSurface)
	descriptorGot := fmt.Sprintf("%x", descriptorHash.Sum(nil))
	rpcGot := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(rpcSurface, "\n"))))
	if descriptorGot != wantDescriptor {
		t.Errorf("descriptor SHA-256 = %s, want %s", descriptorGot, wantDescriptor)
	}
	if rpcGot != wantRPC {
		t.Errorf("RPC SHA-256 = %s, want %s", rpcGot, wantRPC)
	}
}

const declarationProtoFile = "rti/v1/declaration.proto"
