package backtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"sort"

	"axiom/internal/domain"
)

// ConfidenceTier classifies the strongest claim supported by run inputs.
type ConfidenceTier string

// Supported confidence tiers from strongest to weakest.
const (
	ConfidenceA ConfidenceTier = "A"
	ConfidenceB ConfidenceTier = "B"
	ConfidenceC ConfidenceTier = "C"
	ConfidenceD ConfidenceTier = "D"
)

// ModelNamespace prevents aggregation of incompatible counterfactual worlds.
type ModelNamespace struct {
	ID              string `json:"id"`
	MarketContext   string `json:"market_context"`
	LiquidityDomain string `json:"liquidity_domain"`
	FeeDomain       string `json:"fee_domain"`
	LatencyDomain   string `json:"latency_domain"`
	FillDomain      string `json:"fill_domain"`
}

// Comparable rejects results whose modeled worlds are not identical.
func (namespace ModelNamespace) Comparable(other ModelNamespace) bool {
	return namespace.valid() && other.valid() && namespace == other
}

func (namespace ModelNamespace) valid() bool {
	return namespace.ID != "" && namespace.MarketContext != "" && namespace.LiquidityDomain != "" &&
		namespace.FeeDomain != "" && namespace.LatencyDomain != "" && namespace.FillDomain != ""
}

// DatasetDescriptor identifies one manifest revision without relying on its
// non-unique human dataset ID.
type DatasetDescriptor struct {
	DatasetID            string         `json:"dataset_id"`
	ManifestHash         string         `json:"manifest_hash"`
	Revision             uint64         `json:"revision"`
	SourceCommit         string         `json:"source_commit"`
	SchemaVersion        string         `json:"schema_version"`
	ParserVersion        string         `json:"parser_version"`
	NormalizationVersion string         `json:"normalization_version"`
	SegmentHashes        []string       `json:"segment_hashes"`
	RecordCount          uint64         `json:"record_count"`
	GapCount             uint64         `json:"gap_count"`
	LowDensitySegments   uint64         `json:"low_density_segments"`
	Complete             bool           `json:"complete"`
	Confidence           ConfidenceTier `json:"confidence"`
}

// RequireDecisionGrade rejects degraded or unusable feed evidence.
func (descriptor DatasetDescriptor) RequireDecisionGrade() error {
	if descriptor.Confidence != ConfidenceA && descriptor.Confidence != ConfidenceB {
		return backtestError("dataset_not_decision_grade")
	}
	return nil
}

// Clone returns a defensive descriptor copy.
func (descriptor DatasetDescriptor) Clone() DatasetDescriptor {
	descriptor.SegmentHashes = append([]string(nil), descriptor.SegmentHashes...)
	return descriptor
}

// BuildIdentity fixes toolchain and architecture inputs to deterministic runs.
type BuildIdentity struct {
	GoVersion       string   `json:"go_version"`
	Architecture    string   `json:"architecture"`
	OperatingSystem string   `json:"operating_system"`
	BuildFlags      []string `json:"build_flags"`
	GoSumHash       string   `json:"go_sum_hash"`
	PNPMLockHash    string   `json:"pnpm_lock_hash"`
}

// CurrentBuildIdentity creates a sorted immutable build identity.
func CurrentBuildIdentity(flags []string, goSumHash, pnpmLockHash string) BuildIdentity {
	flags = append([]string(nil), flags...)
	sort.Strings(flags)
	return BuildIdentity{GoVersion: runtime.Version(), Architecture: runtime.GOARCH,
		OperatingSystem: runtime.GOOS, BuildFlags: flags, GoSumHash: goSumHash, PNPMLockHash: pnpmLockHash}
}

// RunManifest is the complete immutable reproducibility identity for one run.
type RunManifest struct {
	RunID                domain.RunID      `json:"run_id"`
	Mode                 string            `json:"mode"`
	CodeCommit           string            `json:"code_commit"`
	Build                BuildIdentity     `json:"build"`
	Dataset              DatasetDescriptor `json:"dataset"`
	ConfigurationHash    string            `json:"configuration_hash"`
	ResearchGenerationID string            `json:"research_generation_id,omitempty"`
	Seed                 string            `json:"seed"`
	SchedulerVersion     string            `json:"scheduler_version"`
	SerializationVersion string            `json:"serialization_version"`
	Models               ModelNamespace    `json:"models"`
	StartingBalanceHash  string            `json:"starting_balance_hash"`
}

// CanonicalHash validates and hashes the canonical manifest representation.
func (manifest RunManifest) CanonicalHash() (string, error) {
	if err := manifest.validate(); err != nil {
		return "", err
	}
	manifest.Dataset = manifest.Dataset.Clone()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", backtestError("manifest_encode_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (manifest RunManifest) validate() error {
	if manifest.RunID.Value() == "" || !supportedMode(manifest.Mode) || manifest.CodeCommit == "" ||
		manifest.Build.GoVersion == "" || manifest.Build.Architecture == "" || manifest.Build.OperatingSystem == "" ||
		manifest.ConfigurationHash == "" || manifest.Seed == "" || manifest.SchedulerVersion == "" ||
		manifest.SerializationVersion == "" || manifest.StartingBalanceHash == "" || !manifest.Models.valid() {
		return backtestError("manifest_invalid")
	}
	if !validCommit(manifest.CodeCommit) || !validHash(manifest.ConfigurationHash) ||
		!validHash(manifest.StartingBalanceHash) || !validHash(manifest.Dataset.ManifestHash) ||
		!validHash(manifest.Build.GoSumHash) || !validHash(manifest.Build.PNPMLockHash) {
		return backtestError("manifest_invalid")
	}
	return validateDatasetDescriptor(manifest.Dataset)
}

func validateDatasetDescriptor(descriptor DatasetDescriptor) error {
	if descriptor.DatasetID == "" || descriptor.Revision == 0 || descriptor.SourceCommit == "" ||
		descriptor.SchemaVersion == "" || descriptor.ParserVersion == "" || descriptor.NormalizationVersion == "" ||
		descriptor.RecordCount == 0 || len(descriptor.SegmentHashes) == 0 || !validConfidence(descriptor.Confidence) {
		return backtestError("dataset_descriptor_invalid")
	}
	if !validHash(descriptor.ManifestHash) || !validCommit(descriptor.SourceCommit) {
		return backtestError("dataset_descriptor_invalid")
	}
	for _, hash := range descriptor.SegmentHashes {
		if !validHash(hash) {
			return backtestError("dataset_descriptor_invalid")
		}
	}
	return nil
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validCommit(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && (len(decoded) == 20 || len(decoded) == sha256.Size)
}

func validConfidence(tier ConfidenceTier) bool {
	return tier == ConfidenceA || tier == ConfidenceB || tier == ConfidenceC || tier == ConfidenceD
}

func supportedMode(mode string) bool {
	return mode == "backtest" || mode == "replay" || mode == "paper" || mode == "shadow"
}
