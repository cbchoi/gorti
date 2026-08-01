package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type role string

const (
	rolePublisher  role = "publisher"
	roleSubscriber role = "subscriber"

	receiveOrderTransportProfileSchema = "gorti.receive-order-transport/v1"
)

type receiveOrderTransportProfile struct {
	SchemaVersion          string `json:"schema_version"`
	Transport              string `json:"receive_order_transport"`
	LocalLRCQueueCapacity  int    `json:"local_lrc_queue_capacity"`
	LocalLRCAckEvery       int    `json:"local_lrc_ack_every"`
	LocalLRCBatchSize      int    `json:"local_lrc_batch_size"`
	CallbackRepresentation string `json:"callback_representation"`
}

type config struct {
	Role                      role
	Address                   string
	Federation                string
	FOMPath                   string
	Seed                      string
	Count                     int
	OperationWarmup           int
	OutputDir                 string
	Timeout                   time.Duration
	ReceiveOrder              bool
	AllowGrantBeforeCallbacks bool
	TMAdvanceOnly             bool
	LocalLRC                  bool
	Confirmed                 bool
	LocalLRCQueueCapacity     int
	LocalLRCAckEvery          int
	LocalLRCBatchSize         int
	CallbackRepresentation    string
	WorkloadPlan              string
	WorkloadPlanSHA256        string
	CompactSummary            bool
	TransportConfigPath       string
	ObjectClass               string
	InteractionClass          string
	ObjectName                string
	ParticipantCount          int
	ParticipantIndex          int
}

func (cfg config) devstoneMode() bool {
	return cfg.WorkloadPlan != "" || cfg.CompactSummary
}

func (cfg config) compactReceiveOrderWorkload() bool {
	return cfg.CompactSummary && cfg.ReceiveOrder && cfg.WorkloadPlan != ""
}

func parseConfig(args []string) (config, error) {
	profile := receiveOrderTransportProfile{
		SchemaVersion:          receiveOrderTransportProfileSchema,
		Transport:              "local-lrc",
		LocalLRCQueueCapacity:  1024,
		LocalLRCAckEvery:       32,
		LocalLRCBatchSize:      32,
		CallbackRepresentation: "names",
	}
	configPath, err := transportConfigPath(args)
	if err != nil {
		return config{}, err
	}
	if configPath != "" {
		profile, err = loadReceiveOrderTransportProfile(configPath, profile)
		if err != nil {
			return config{}, err
		}
	}
	cfg := config{
		LocalLRCQueueCapacity:  profile.LocalLRCQueueCapacity,
		LocalLRCAckEvery:       profile.LocalLRCAckEvery,
		LocalLRCBatchSize:      profile.LocalLRCBatchSize,
		CallbackRepresentation: profile.CallbackRepresentation,
		TransportConfigPath:    configPath,
	}
	var roleValue string
	set := flag.NewFlagSet("gorti-go-fair", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&cfg.TransportConfigPath, "transport-config", configPath, "receive-order transport JSON profile")
	set.StringVar(&cfg.TransportConfigPath, "config", configPath, "alias for --transport-config")
	set.StringVar(&roleValue, "role", "", "publisher or subscriber")
	set.StringVar(&cfg.Address, "address", "127.0.0.1:8442", "rtid host:port")
	set.StringVar(&cfg.Federation, "federation", "", "federation name")
	set.StringVar(&cfg.FOMPath, "fom", "", "caller-supplied FOM XML path")
	set.StringVar(&cfg.Seed, "seed", "", "caller-supplied deterministic payload seed")
	set.IntVar(&cfg.Count, "count", 0, "caller-supplied iteration count")
	set.IntVar(&cfg.OperationWarmup, "operation-warmup", 0, "unmeasured receive-order iterations before VERIFY_MEASURE")
	set.StringVar(&cfg.OutputDir, "output", "", "output directory")
	set.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "per-phase timeout")
	set.BoolVar(&cfg.ReceiveOrder, "receive-order", false, "use receive-order OM traffic without time management")
	set.BoolVar(
		&cfg.AllowGrantBeforeCallbacks,
		"allow-grant-before-callbacks",
		false,
		"permit TAG before timestamped callback completion while still requiring both",
	)
	set.BoolVar(&cfg.TMAdvanceOnly, "tm-advance-only", false, "benchmark TAR/TAG without OM traffic")
	set.BoolVar(&cfg.LocalLRC, "local-lrc", false, "explicitly use the queued LocalLRC receive-order transport")
	set.BoolVar(&cfg.Confirmed, "confirmed", false, "wait for the RTI server result on each receive-order OM call")
	set.IntVar(&cfg.LocalLRCQueueCapacity, "local-lrc-queue", profile.LocalLRCQueueCapacity, "maximum unacknowledged LocalLRC operations")
	set.IntVar(&cfg.LocalLRCAckEvery, "local-lrc-ack-every", profile.LocalLRCAckEvery, "requested LocalLRC cumulative ACK interval")
	set.IntVar(&cfg.LocalLRCBatchSize, "local-lrc-batch-size", profile.LocalLRCBatchSize, "requested LocalLRC operations per transport frame")
	set.StringVar(&cfg.CallbackRepresentation, "callback-representation", profile.CallbackRepresentation, "callback maps use names or handles")
	set.StringVar(&cfg.WorkloadPlan, "workload-plan", "", "DEVStone-HLA binary workload plan")
	set.StringVar(
		&cfg.WorkloadPlanSHA256,
		"workload-plan-sha256",
		"",
		"expected lowercase SHA-256 of the DEVStone-HLA binary workload plan",
	)
	set.StringVar(&cfg.ObjectClass, "object-class", "VerifierEntity", "federation object class under test")
	set.StringVar(
		&cfg.InteractionClass,
		"interaction-class",
		"VerifierMessage",
		"federation interaction class under test",
	)
	set.StringVar(
		&cfg.ObjectName,
		"object-name",
		"CommercialRtiVerifierEntity",
		"reserved object instance name under test",
	)
	set.BoolVar(&cfg.CompactSummary, "compact-summary", false, "write only the accepted participant summary")
	set.IntVar(&cfg.ParticipantCount, "participant-count", 2, "federates in the shared federation")
	set.IntVar(&cfg.ParticipantIndex, "participant-index", -1, "publisher is 0; subscribers are 1..count-1")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(set.Args(), " "))
	}
	cfg.Role = role(strings.ToLower(strings.TrimSpace(roleValue)))
	if cfg.Role != rolePublisher && cfg.Role != roleSubscriber {
		return config{}, errors.New("--role must be publisher or subscriber")
	}
	if cfg.TMAdvanceOnly && cfg.ReceiveOrder {
		return config{}, errors.New("--tm-advance-only forbids --receive-order")
	}
	if cfg.ParticipantCount < 2 {
		return config{}, errors.New("--participant-count must be at least 2")
	}
	if cfg.ParticipantIndex < 0 {
		if cfg.Role == rolePublisher {
			cfg.ParticipantIndex = 0
		} else {
			cfg.ParticipantIndex = 1
		}
	}
	if (cfg.Role == rolePublisher && cfg.ParticipantIndex != 0) ||
		(cfg.Role == roleSubscriber &&
			(cfg.ParticipantIndex < 1 || cfg.ParticipantIndex >= cfg.ParticipantCount)) {
		return config{}, errors.New("--participant-index is outside the role/count range")
	}
	if strings.TrimSpace(cfg.Address) == "" || strings.Contains(cfg.Address, "://") {
		return config{}, errors.New("--address must be a host:port without a URI scheme")
	}
	if strings.TrimSpace(cfg.Federation) == "" {
		return config{}, errors.New("--federation is required")
	}
	if strings.TrimSpace(cfg.FOMPath) == "" {
		return config{}, errors.New("--fom is required")
	}
	if cfg.Seed == "" {
		return config{}, errors.New("--seed is required")
	}
	if strings.TrimSpace(cfg.ObjectClass) == "" {
		return config{}, errors.New("--object-class is required")
	}
	if strings.TrimSpace(cfg.InteractionClass) == "" {
		return config{}, errors.New("--interaction-class is required")
	}
	if strings.TrimSpace(cfg.ObjectName) == "" {
		return config{}, errors.New("--object-name is required")
	}
	if cfg.Count < 1 {
		return config{}, errors.New("--count must be at least 1")
	}
	if cfg.OperationWarmup < 0 {
		return config{}, errors.New("--operation-warmup must not be negative")
	}
	if cfg.OperationWarmup > 0 && !cfg.ReceiveOrder {
		return config{}, errors.New("--operation-warmup requires --receive-order")
	}
	if strings.TrimSpace(cfg.OutputDir) == "" {
		return config{}, errors.New("--output is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("--timeout must be positive")
	}
	if cfg.LocalLRC && cfg.Confirmed {
		return config{}, errors.New("--local-lrc and --confirmed are mutually exclusive")
	}
	if cfg.LocalLRC && !cfg.ReceiveOrder {
		return config{}, errors.New("--local-lrc requires --receive-order")
	}
	if cfg.Confirmed && !cfg.ReceiveOrder {
		return config{}, errors.New("--confirmed requires --receive-order")
	}
	transport := profile.Transport
	if cfg.LocalLRC {
		transport = "local-lrc"
	}
	if cfg.Confirmed {
		transport = "confirmed"
	}
	switch transport {
	case "local-lrc":
		cfg.LocalLRC, cfg.Confirmed = cfg.ReceiveOrder, false
	case "confirmed":
		cfg.LocalLRC, cfg.Confirmed = false, cfg.ReceiveOrder
	default:
		return config{}, errors.New("receive_order_transport must be local-lrc or confirmed")
	}
	if cfg.LocalLRCQueueCapacity < 1 {
		return config{}, errors.New("--local-lrc-queue must be at least 1")
	}
	if cfg.LocalLRCAckEvery < 1 || uint64(cfg.LocalLRCAckEvery) > uint64(^uint32(0)) {
		return config{}, errors.New("--local-lrc-ack-every must fit in a positive uint32")
	}
	switch cfg.LocalLRCBatchSize {
	case 32, 64, 128, 256:
	default:
		return config{}, errors.New("--local-lrc-batch-size must be 32, 64, 128, or 256")
	}
	if cfg.CallbackRepresentation != "names" && cfg.CallbackRepresentation != "handles" {
		return config{}, errors.New("--callback-representation must be names or handles")
	}
	if cfg.WorkloadPlan != "" && !cfg.ReceiveOrder {
		return config{}, errors.New("--workload-plan requires --receive-order")
	}
	if cfg.WorkloadPlanSHA256 != "" {
		if cfg.WorkloadPlan == "" {
			return config{}, errors.New("--workload-plan-sha256 requires --workload-plan")
		}
		if len(cfg.WorkloadPlanSHA256) != sha256.Size*2 || strings.ToLower(cfg.WorkloadPlanSHA256) != cfg.WorkloadPlanSHA256 {
			return config{}, errors.New("--workload-plan-sha256 must be a 64-character lowercase SHA-256")
		}
		if _, err := hex.DecodeString(cfg.WorkloadPlanSHA256); err != nil {
			return config{}, errors.New("--workload-plan-sha256 must be a 64-character lowercase SHA-256")
		}
	}
	if cfg.CompactSummary && cfg.WorkloadPlan == "" {
		return config{}, errors.New("--compact-summary requires --workload-plan")
	}
	if cfg.CompactSummary && cfg.WorkloadPlanSHA256 == "" {
		return config{}, errors.New("--compact-summary requires --workload-plan-sha256")
	}
	cfg.FOMPath, err = filepath.Abs(cfg.FOMPath)
	if err != nil {
		return config{}, fmt.Errorf("resolve FOM path: %w", err)
	}
	cfg.OutputDir, err = filepath.Abs(cfg.OutputDir)
	if err != nil {
		return config{}, fmt.Errorf("resolve output directory: %w", err)
	}
	if cfg.WorkloadPlan != "" {
		cfg.WorkloadPlan, err = filepath.Abs(cfg.WorkloadPlan)
		if err != nil {
			return config{}, fmt.Errorf("resolve workload plan path: %w", err)
		}
	}
	return cfg, nil
}

func (cfg config) federateName() string {
	if cfg.Role == rolePublisher {
		return publisherName
	}
	return cfg.subscriberFederateName(cfg.ParticipantIndex)
}

func (cfg config) subscriberFederateName(index int) string {
	if cfg.ParticipantCount == 2 {
		return subscriberName
	}
	return fmt.Sprintf("%s-%d", subscriberName, index)
}

func (cfg config) registersSynchronization(label string) bool {
	if cfg.ParticipantCount > 2 {
		return cfg.Role == rolePublisher
	}
	if label == readySync {
		return cfg.Role == roleSubscriber
	}
	return cfg.Role == rolePublisher
}

func transportConfigPath(args []string) (string, error) {
	var path string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "--transport-config=") || strings.HasPrefix(arg, "--config=") {
			path = strings.TrimSpace(arg[strings.IndexByte(arg, '=')+1:])
		} else if arg == "--transport-config" || arg == "--config" {
			index++
			if index >= len(args) {
				return "", errors.New("--transport-config requires a path")
			}
			path = strings.TrimSpace(args[index])
		}
	}
	if path == "" && slicesContainTransportConfig(args) {
		return "", errors.New("--transport-config requires a non-empty path")
	}
	return path, nil
}

func slicesContainTransportConfig(args []string) bool {
	for _, arg := range args {
		if arg == "--transport-config" || arg == "--config" ||
			strings.HasPrefix(arg, "--transport-config=") || strings.HasPrefix(arg, "--config=") {
			return true
		}
	}
	return false
}

func loadReceiveOrderTransportProfile(
	path string,
	defaults receiveOrderTransportProfile,
) (receiveOrderTransportProfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return receiveOrderTransportProfile{}, fmt.Errorf("open transport config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	profile := defaults
	profile.SchemaVersion = ""
	profile.Transport = ""
	if err := decoder.Decode(&profile); err != nil {
		return receiveOrderTransportProfile{}, fmt.Errorf("decode transport config: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return receiveOrderTransportProfile{}, errors.New("decode transport config: trailing JSON value")
	}
	if profile.SchemaVersion != receiveOrderTransportProfileSchema {
		return receiveOrderTransportProfile{}, fmt.Errorf(
			"transport config schema_version must be %q", receiveOrderTransportProfileSchema,
		)
	}
	return profile, nil
}
