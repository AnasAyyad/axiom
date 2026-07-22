package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"axiom/internal/storage/segments"
)

const tierAManifestSchemaVersion = "axiom.multi-exchange-dataset.v1"

// TierAMember binds one verified per-exchange manifest to its replay proof.
type TierAMember struct {
	Exchange     string              `json:"exchange"`
	DatasetID    string              `json:"dataset_id"`
	SessionID    string              `json:"session_id"`
	Revision     uint64              `json:"revision"`
	ManifestHash string              `json:"manifest_hash"`
	Verification DatasetVerification `json:"verification"`
	Coverage     ExchangeCoverage    `json:"coverage"`
}

// TierAManifest is an immutable verified multi-exchange qualification identity.
type TierAManifest struct {
	SchemaVersion  string                    `json:"schema_version"`
	DatasetID      string                    `json:"dataset_id"`
	QualityTier    string                    `json:"quality_tier"`
	CreatedAt      time.Time                 `json:"created_at"`
	Members        []TierAMember             `json:"members"`
	Compatibility  CompatibilityRequirements `json:"compatibility_requirements"`
	HiddenGapCount uint64                    `json:"hidden_gap_count"`
	Complete       bool                      `json:"complete"`
	Hash           string                    `json:"hash"`
}

// BuildTierAManifest verifies every child chain, raw/canonical link, and the
// combined dataset-wide ordinal sequence before assigning quality tier A.
func BuildTierAManifest(
	datasetID string,
	createdAt time.Time,
	roots map[string]string,
	manifests []DatasetManifest,
) (TierAManifest, error) {
	if !identifierPattern.MatchString(datasetID) || createdAt.IsZero() || createdAt.Location() != time.UTC ||
		len(manifests) < 2 || len(roots) != len(manifests) {
		return TierAManifest{}, recorderError("tier_a_input_invalid")
	}
	build := tierABuild{members: make([]TierAMember, 0, len(manifests)),
		seenExchanges: make(map[string]struct{}, len(manifests))}
	for _, manifest := range manifests {
		root, exists := roots[manifest.Exchange]
		if !exists {
			return TierAManifest{}, recorderError("tier_a_chain_invalid")
		}
		if err := build.add(root, manifest); err != nil {
			return TierAManifest{}, err
		}
	}
	sort.Slice(build.members, func(left, right int) bool { return build.members[left].Exchange < build.members[right].Exchange })
	sort.Slice(build.ordinals, func(left, right int) bool { return build.ordinals[left] < build.ordinals[right] })
	for index, ordinal := range build.ordinals {
		if ordinal == 0 || (index > 0 && ordinal != build.ordinals[index-1]+1) {
			return TierAManifest{}, recorderError("tier_a_hidden_gap")
		}
	}
	manifest := TierAManifest{SchemaVersion: tierAManifestSchemaVersion, DatasetID: datasetID,
		QualityTier: "A", CreatedAt: createdAt, Members: build.members,
		Compatibility: CompatibilityRequirements{MinimumReaderVersion: build.minimumReader,
			SchemaVersions: uniqueSorted(build.schemas), ParserVersions: uniqueSorted(build.parsers),
			NormalizationVersions: uniqueSorted(build.normalizers)},
		HiddenGapCount: 0, Complete: true}
	manifest.Hash = tierAManifestHash(manifest)
	if err := validateTierAManifest(manifest); err != nil {
		return TierAManifest{}, err
	}
	return manifest, nil
}

type tierABuild struct {
	members       []TierAMember
	ordinals      []uint64
	seenExchanges map[string]struct{}
	minimumReader string
	schemas       []string
	parsers       []string
	normalizers   []string
	failure       error
}

func (build *tierABuild) add(root string, manifest DatasetManifest) error {
	if manifest.SchemaVersion != datasetSchemaVersionV2 || manifest.QualityTier != "candidate" ||
		len(manifest.ExchangeCoverage) != 1 || !manifest.Complete || len(manifest.Gaps) != 0 {
		build.failure = recorderError("tier_a_manifest_ineligible")
		return build.failure
	}
	if _, duplicate := build.seenExchanges[manifest.Exchange]; duplicate {
		build.failure = recorderError("tier_a_exchange_duplicate")
		return build.failure
	}
	if VerifyManifestChain(root, manifest) != nil {
		build.failure = recorderError("tier_a_chain_invalid")
		return build.failure
	}
	verification, ordinals, err := verifyDatasetWithOrdinals(root, manifest)
	if err != nil {
		build.failure = recorderError("tier_a_linkage_invalid")
		return build.failure
	}
	coverage := cloneExchangeCoverage(manifest.ExchangeCoverage)[0]
	if coverage.Exchange != manifest.Exchange || coverage.RawRecordCount != verification.RecordCount ||
		coverage.CanonicalRecordCount != verification.RecordCount || !coverage.RawCanonicalLinkageComplete {
		build.failure = recorderError("tier_a_coverage_invalid")
		return build.failure
	}
	if build.minimumReader != "" && manifest.Compatibility.MinimumReaderVersion != build.minimumReader {
		build.failure = recorderError("tier_a_compatibility_mismatch")
		return build.failure
	}
	coverage.HiddenGapCount, coverage.Complete = 0, true
	build.members = append(build.members, TierAMember{Exchange: manifest.Exchange, DatasetID: manifest.DatasetID,
		SessionID: manifest.SessionID, Revision: manifest.Revision, ManifestHash: manifest.Hash,
		Verification: verification, Coverage: coverage})
	build.ordinals = append(build.ordinals, ordinals...)
	build.seenExchanges[manifest.Exchange], build.minimumReader = struct{}{}, manifest.Compatibility.MinimumReaderVersion
	build.schemas, build.parsers = append(build.schemas, coverage.SchemaVersions...), append(build.parsers, coverage.ParserVersions...)
	build.normalizers = append(build.normalizers, coverage.NormalizationVersions...)
	return nil
}

// WriteTierAManifest atomically retains one qualified aggregate under root.
func WriteTierAManifest(root string, manifest TierAManifest) (string, error) {
	if err := validateTierAManifest(manifest); err != nil || !filepath.IsAbs(root) ||
		filepath.Clean(root) == string(filepath.Separator) {
		return "", recorderError("tier_a_manifest_invalid")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "directory_create", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", recorderError("tier_a_manifest_invalid")
	}
	name := manifest.DatasetID + ".tier-a.json"
	partial, final := filepath.Join(root, name+".partial"), filepath.Join(root, name)
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "manifest_create", err)
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "manifest_sync", err)
	}
	if err = os.Rename(partial, final); err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "manifest_rename", err)
	}
	directory, err := os.Open(root)
	if err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "directory_open", err)
	}
	err, closeErr = directory.Sync(), directory.Close()
	if err != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "directory_sync", err)
	}
	if closeErr != nil {
		return "", recorderIOError("tier_a_manifest_write_failed", "directory_close", closeErr)
	}
	return final, nil
}

// ReadTierAManifest strictly decodes and validates one qualified aggregate.
func ReadTierAManifest(path string) (TierAManifest, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumEventBytes {
		return TierAManifest{}, recorderError("tier_a_manifest_unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return TierAManifest{}, recorderError("tier_a_manifest_unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maximumEventBytes+1))
	decoder.DisallowUnknownFields()
	var manifest TierAManifest
	if err = decoder.Decode(&manifest); err != nil {
		return TierAManifest{}, recorderError("tier_a_manifest_invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF || validateTierAManifest(manifest) != nil {
		return TierAManifest{}, recorderError("tier_a_manifest_invalid")
	}
	return manifest, nil
}

// ValidateTierAManifest validates an in-memory immutable Tier A aggregate.
func ValidateTierAManifest(manifest TierAManifest) error { return validateTierAManifest(manifest) }

func (recorder *Recorder) nextGenerationCoverage(rows []segments.WireRow) map[uint64]GenerationCoverage {
	next := make(map[uint64]GenerationCoverage, len(recorder.generationCoverage))
	for generation, coverage := range recorder.generationCoverage {
		next[generation] = coverage
	}
	for _, row := range rows {
		coverage := next[row.ConnectionGeneration]
		receivedAt := time.Unix(0, row.ReceivedAtUnixNano).UTC()
		if coverage.ConnectionGeneration == 0 {
			coverage = GenerationCoverage{ConnectionGeneration: row.ConnectionGeneration,
				FirstOrdinal: row.IngestOrdinal, CoverageStart: receivedAt}
		}
		coverage.LastOrdinal, coverage.CoverageEnd = row.IngestOrdinal, receivedAt
		coverage.RecordCount++
		next[row.ConnectionGeneration] = coverage
	}
	return next
}

func (recorder *Recorder) exchangeCoverage(
	references []SegmentReference,
	rawCount, canonicalCount uint64,
	generations map[uint64]GenerationCoverage,
) ExchangeCoverage {
	coverage := ExchangeCoverage{Exchange: recorder.exchange, CollectorInstance: recorder.profile.Instance,
		CollectorRegion: recorder.profile.Region, RawRecordCount: rawCount, CanonicalRecordCount: canonicalCount,
		RawCanonicalLinkageComplete: rawCount > 0 && rawCount == canonicalCount,
		HiddenGapCount:              0, Complete: len(recorder.gaps) == 0 && rawCount == canonicalCount}
	var schemas, parsers, normalizers []string
	for index, reference := range references {
		spec := reference.Manifest.Spec
		if index == 0 || spec.StartedAt.Before(coverage.CoverageStart) {
			coverage.CoverageStart = spec.StartedAt
		}
		if index == 0 || spec.EndedAt.After(coverage.CoverageEnd) {
			coverage.CoverageEnd = spec.EndedAt
		}
		if coverage.FirstOrdinal == 0 || spec.FirstOrdinal < coverage.FirstOrdinal {
			coverage.FirstOrdinal = spec.FirstOrdinal
		}
		if spec.LastOrdinal > coverage.LastOrdinal {
			coverage.LastOrdinal = spec.LastOrdinal
		}
		schemas = append(schemas, spec.SchemaVersion)
		if reference.Kind == "canonical" {
			parsers = append(parsers, spec.ParserVersion)
			normalizers = append(normalizers, spec.NormalizationVersion)
		}
	}
	coverage.SchemaVersions, coverage.ParserVersions = uniqueSorted(schemas), uniqueSorted(parsers)
	coverage.NormalizationVersions = uniqueSorted(normalizers)
	for _, generation := range generations {
		coverage.GenerationHistory = append(coverage.GenerationHistory, generation)
	}
	sort.Slice(coverage.GenerationHistory, func(left, right int) bool {
		return coverage.GenerationHistory[left].ConnectionGeneration < coverage.GenerationHistory[right].ConnectionGeneration
	})
	return coverage
}

func validateTierAManifest(manifest TierAManifest) error {
	if manifest.SchemaVersion != tierAManifestSchemaVersion || !identifierPattern.MatchString(manifest.DatasetID) ||
		manifest.QualityTier != "A" || manifest.CreatedAt.IsZero() || manifest.CreatedAt.Location() != time.UTC ||
		len(manifest.Members) < 2 || manifest.HiddenGapCount != 0 || !manifest.Complete ||
		manifest.Hash != tierAManifestHash(manifest) || !validCompatibility(manifest.Compatibility) {
		return recorderError("tier_a_manifest_invalid")
	}
	for index, member := range manifest.Members {
		if !identifierPattern.MatchString(member.Exchange) || !identifierPattern.MatchString(member.DatasetID) ||
			!identifierPattern.MatchString(member.SessionID) || member.Revision == 0 || !validDigest(member.ManifestHash) ||
			member.Verification.RecordCount == 0 || !validDigest(member.Verification.ReplaySHA256) ||
			!validTierAMemberCoverage(member) ||
			(index > 0 && manifest.Members[index-1].Exchange >= member.Exchange) {
			return recorderError("tier_a_manifest_invalid")
		}
	}
	return nil
}

func validTierAMemberCoverage(member TierAMember) bool {
	compatibility := CompatibilityRequirements{MinimumReaderVersion: "tier-a-reader.v1",
		SchemaVersions:        append([]string(nil), member.Coverage.SchemaVersions...),
		ParserVersions:        append([]string(nil), member.Coverage.ParserVersions...),
		NormalizationVersions: append([]string(nil), member.Coverage.NormalizationVersions...)}
	manifest := DatasetManifest{Exchange: member.Exchange, RawRecordCount: member.Verification.RecordCount,
		CanonicalCount: member.Verification.RecordCount, Complete: true, Compatibility: &compatibility}
	return validateExchangeCoverage(manifest, member.Coverage) == nil
}

func tierAManifestHash(manifest TierAManifest) string {
	manifest.Hash = ""
	encoded, _ := json.Marshal(manifest)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func cloneExchangeCoverage(values []ExchangeCoverage) []ExchangeCoverage {
	result := append([]ExchangeCoverage(nil), values...)
	for index := range result {
		result[index].GenerationHistory = append([]GenerationCoverage(nil), result[index].GenerationHistory...)
		result[index].SchemaVersions = append([]string(nil), result[index].SchemaVersions...)
		result[index].ParserVersions = append([]string(nil), result[index].ParserVersions...)
		result[index].NormalizationVersions = append([]string(nil), result[index].NormalizationVersions...)
	}
	return result
}

func cloneCompatibility(value CompatibilityRequirements) CompatibilityRequirements {
	value.SchemaVersions = append([]string(nil), value.SchemaVersions...)
	value.ParserVersions = append([]string(nil), value.ParserVersions...)
	value.NormalizationVersions = append([]string(nil), value.NormalizationVersions...)
	return value
}

func validCompatibility(value CompatibilityRequirements) bool {
	return identifierPattern.MatchString(value.MinimumReaderVersion) && len(value.SchemaVersions) > 0 &&
		len(value.ParserVersions) > 0 && len(value.NormalizationVersions) > 0 &&
		sortedUniqueNonempty(value.SchemaVersions) && sortedUniqueNonempty(value.ParserVersions) &&
		sortedUniqueNonempty(value.NormalizationVersions)
}

func sortedUniqueNonempty(values []string) bool {
	for index, value := range values {
		if value == "" || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return len(values) > 0
}
