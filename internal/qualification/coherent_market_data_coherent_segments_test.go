package qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

const (
	coherentMarketDataCoherentSegmentSchema = "axiom.coherent_market_data-coherent-segment.v1"
	coherentMarketDataCoherentManifestName  = "coherent_market_data-coherent-manifest.json"
	coherentMarketDataCoherentSegmentEvery  = 5 * time.Minute
	coherentMarketDataCoherentSampleEvery   = 5 * time.Second
	coherentMarketDataMaximumDegradation    = 15 * time.Second
)

type coherentMarketDataRejectionCode string

const (
	coherentMarketDataRejectCaptureFailure      coherentMarketDataRejectionCode = "capture_failure"
	coherentMarketDataRejectMissing             coherentMarketDataRejectionCode = "missing"
	coherentMarketDataRejectPostTrigger         coherentMarketDataRejectionCode = "post_trigger"
	coherentMarketDataRejectGeneration          coherentMarketDataRejectionCode = "generation"
	coherentMarketDataRejectGap                 coherentMarketDataRejectionCode = "gap"
	coherentMarketDataRejectStale               coherentMarketDataRejectionCode = "stale"
	coherentMarketDataRejectUncertainty         coherentMarketDataRejectionCode = "uncertainty"
	coherentMarketDataRejectSkew                coherentMarketDataRejectionCode = "skew"
	coherentMarketDataRejectInterval            coherentMarketDataRejectionCode = "interval"
	coherentMarketDataRejectIdentity            coherentMarketDataRejectionCode = "identity"
	coherentMarketDataRejectConfiguration       coherentMarketDataRejectionCode = "configuration"
	coherentMarketDataRejectDuplicateMembership coherentMarketDataRejectionCode = "duplicate_membership"
)

var coherentMarketDataRejectionCodes = []coherentMarketDataRejectionCode{
	coherentMarketDataRejectCaptureFailure, coherentMarketDataRejectMissing, coherentMarketDataRejectPostTrigger, coherentMarketDataRejectGeneration,
	coherentMarketDataRejectGap, coherentMarketDataRejectStale, coherentMarketDataRejectUncertainty, coherentMarketDataRejectSkew, coherentMarketDataRejectInterval,
	coherentMarketDataRejectIdentity, coherentMarketDataRejectConfiguration, coherentMarketDataRejectDuplicateMembership,
}

type coherentMarketDataCoherentMemberEvidence struct {
	Key              runtimecore.MarketKey      `json:"key"`
	Reference        *runtimecore.ViewReference `json:"reference,omitempty"`
	ActiveGeneration uint64                     `json:"active_generation"`
	UnresolvedGap    bool                       `json:"unresolved_gap"`
}

type coherentMarketDataDegradationFact struct {
	DegradedSince       time.Time     `json:"degraded_since,omitempty"`
	Recovered           bool          `json:"recovered"`
	RecoveryDuration    time.Duration `json:"recovery_duration_nanos,omitempty"`
	RecoveryWithinLimit bool          `json:"recovery_within_limit,omitempty"`
	ExceededLimit       bool          `json:"exceeded_limit,omitempty"`
}

type coherentMarketDataCoherentSample struct {
	Sequence      uint64                                     `json:"sequence"`
	Phase         string                                     `json:"phase"`
	Pair          string                                     `json:"pair"`
	SampledAt     time.Time                                  `json:"sampled_at"`
	Policy        runtimecore.CoherentPolicy                 `json:"policy"`
	Trigger       runtimecore.AsOfTrigger                    `json:"trigger"`
	CaptureFailed bool                                       `json:"capture_failed,omitempty"`
	Members       []coherentMarketDataCoherentMemberEvidence `json:"members"`
	Outcome       string                                     `json:"outcome"`
	RejectionCode coherentMarketDataRejectionCode            `json:"rejection_code,omitempty"`
	CoherentID    string                                     `json:"coherent_identity,omitempty"`
	Degradation   coherentMarketDataDegradationFact          `json:"degradation"`
}

type coherentMarketDataCoherentSegment struct {
	SchemaVersion  string                             `json:"schema_version"`
	Kind           string                             `json:"kind"`
	Sequence       uint64                             `json:"sequence"`
	StartedAt      time.Time                          `json:"started_at"`
	EndedAt        time.Time                          `json:"ended_at"`
	PreviousHash   string                             `json:"previous_hash,omitempty"`
	RecordChecksum string                             `json:"record_checksum"`
	Records        []coherentMarketDataCoherentSample `json:"records"`
	Hash           string                             `json:"hash"`
}

type coherentMarketDataCoherentSegmentReference struct {
	Sequence       uint64 `json:"sequence"`
	Filename       string `json:"filename"`
	RecordCount    uint64 `json:"record_count"`
	RecordChecksum string `json:"record_checksum"`
	PreviousHash   string `json:"previous_hash,omitempty"`
	Hash           string `json:"hash"`
}

type coherentMarketDataCoherentManifest struct {
	SchemaVersion string                                       `json:"schema_version"`
	Kind          string                                       `json:"kind"`
	SourceCommit  string                                       `json:"source_commit"`
	UpdatedAt     time.Time                                    `json:"updated_at"`
	Segments      []coherentMarketDataCoherentSegmentReference `json:"segments"`
	Sequence      uint64                                       `json:"sequence"`
	TerminalHash  string                                       `json:"terminal_hash,omitempty"`
	SampleCount   uint64                                       `json:"sample_count"`
}

type coherentMarketDataCoherentSegmentWriter struct {
	mutex        sync.Mutex
	root         string
	sourceCommit string
	write        func(string, any) error
	manifest     coherentMarketDataCoherentManifest
	pending      []coherentMarketDataCoherentSample
	window       time.Time
	nextSample   uint64
}

func newCoherentMarketDataCoherentSegmentWriter(root, sourceCommit string, write func(string, any) error) (*coherentMarketDataCoherentSegmentWriter, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) || !validGitCommit(sourceCommit) || write == nil {
		return nil, errors.New("coherent market data coherent segment writer configuration invalid")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, errors.New("coherent market data coherent segment root must be empty")
	}
	return &coherentMarketDataCoherentSegmentWriter{root: root, sourceCommit: sourceCommit, write: write,
		manifest: coherentMarketDataCoherentManifest{SchemaVersion: coherentMarketDataCoherentSegmentSchema, Kind: "manifest", SourceCommit: sourceCommit}}, nil
}

func (writer *coherentMarketDataCoherentSegmentWriter) Append(sample coherentMarketDataCoherentSample) error {
	if writer == nil {
		return errors.New("coherent market data coherent segment writer unavailable")
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if sample.SampledAt.IsZero() || sample.SampledAt.Location() != time.UTC || !validCoherentMarketDataPair(sample.Pair) || !validCoherentMarketDataSampleOutcome(sample) {
		return errors.New("coherent market data coherent sample invalid")
	}
	window := sample.SampledAt.Truncate(coherentMarketDataCoherentSegmentEvery)
	if !writer.window.IsZero() && !window.Equal(writer.window) {
		if err := writer.flushLocked(); err != nil {
			return err
		}
	}
	if writer.window.IsZero() {
		writer.window = window
	}
	writer.nextSample++
	sample.Sequence = writer.nextSample
	writer.pending = append(writer.pending, cloneCoherentMarketDataCoherentSample(sample))
	return nil
}

func (writer *coherentMarketDataCoherentSegmentWriter) Flush() error {
	if writer == nil {
		return errors.New("coherent market data coherent segment writer unavailable")
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.flushLocked()
}

func (writer *coherentMarketDataCoherentSegmentWriter) Snapshot() (uint64, string, uint64) {
	if writer == nil {
		return 0, "", 0
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.manifest.Sequence, writer.manifest.TerminalHash, writer.manifest.SampleCount
}

func (writer *coherentMarketDataCoherentSegmentWriter) flushLocked() error {
	if len(writer.pending) == 0 {
		return nil
	}
	records := append([]coherentMarketDataCoherentSample(nil), writer.pending...)
	checksum, err := coherentMarketDataJSONHash(records)
	if err != nil {
		return err
	}
	segment := coherentMarketDataCoherentSegment{SchemaVersion: coherentMarketDataCoherentSegmentSchema, Kind: "segment",
		Sequence: writer.manifest.Sequence + 1, StartedAt: records[0].SampledAt,
		EndedAt: records[len(records)-1].SampledAt, PreviousHash: writer.manifest.TerminalHash,
		RecordChecksum: checksum, Records: records}
	segment.Hash, err = coherentMarketDataSegmentHash(segment)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("coherent_market_data-coherent-%06d.json", segment.Sequence)
	path := filepath.Join(writer.root, filename)
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("coherent market data coherent segment path collision")
	}
	if err = writer.write(path, segment); err != nil {
		return err
	}
	reference := coherentMarketDataCoherentSegmentReference{Sequence: segment.Sequence, Filename: filename,
		RecordCount: uint64(len(records)), RecordChecksum: checksum, PreviousHash: segment.PreviousHash, Hash: segment.Hash}
	next := writer.manifest
	next.UpdatedAt = time.Now().UTC()
	next.Segments = append(append([]coherentMarketDataCoherentSegmentReference(nil), writer.manifest.Segments...), reference)
	next.Sequence, next.TerminalHash = segment.Sequence, segment.Hash
	next.SampleCount += uint64(len(records))
	if err = writer.write(filepath.Join(writer.root, coherentMarketDataCoherentManifestName), next); err != nil {
		return err
	}
	writer.manifest, writer.pending, writer.window = next, nil, time.Time{}
	return nil
}

func verifyCoherentMarketDataCoherentSegments(root, sourceCommit string, expectedSequence uint64, expectedHash string) (coherentMarketDataCoherentManifest, error) {
	var manifest coherentMarketDataCoherentManifest
	if err := readStrictJSON(filepath.Join(root, coherentMarketDataCoherentManifestName), &manifest); err != nil {
		return manifest, err
	}
	if manifest.SchemaVersion != coherentMarketDataCoherentSegmentSchema || manifest.Kind != "manifest" || manifest.SourceCommit != sourceCommit ||
		manifest.Sequence != expectedSequence || manifest.TerminalHash != expectedHash || uint64(len(manifest.Segments)) != manifest.Sequence {
		return manifest, errors.New("coherent market data coherent manifest metadata mismatch")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return manifest, err
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".partial") {
			return manifest, errors.New("coherent market data coherent partial write retained")
		}
	}
	samples, prior, err := verifyCoherentMarketDataCoherentSegmentFiles(root, manifest)
	if err != nil {
		return manifest, err
	}
	if samples != manifest.SampleCount || prior != manifest.TerminalHash {
		return manifest, errors.New("coherent market data coherent manifest terminal mismatch")
	}
	return manifest, nil
}

func verifyCoherentMarketDataCoherentSegmentFiles(root string, manifest coherentMarketDataCoherentManifest) (uint64, string, error) {
	var prior string
	var samples uint64
	for index, reference := range manifest.Segments {
		if reference.Sequence != uint64(index+1) || reference.PreviousHash != prior || filepath.Base(reference.Filename) != reference.Filename {
			return 0, "", errors.New("coherent market data coherent manifest chain mismatch")
		}
		var segment coherentMarketDataCoherentSegment
		if err := readStrictJSON(filepath.Join(root, reference.Filename), &segment); err != nil {
			return 0, "", err
		}
		if segment.SchemaVersion != coherentMarketDataCoherentSegmentSchema || segment.Kind != "segment" || segment.Sequence != reference.Sequence ||
			segment.PreviousHash != prior || segment.RecordChecksum != reference.RecordChecksum || segment.Hash != reference.Hash ||
			uint64(len(segment.Records)) != reference.RecordCount || len(segment.Records) == 0 {
			return 0, "", errors.New("coherent market data coherent segment metadata mismatch")
		}
		checksum, checksumErr := coherentMarketDataJSONHash(segment.Records)
		hash, hashErr := coherentMarketDataSegmentHash(segment)
		if checksumErr != nil || hashErr != nil || checksum != segment.RecordChecksum || hash != segment.Hash {
			return 0, "", errors.New("coherent market data coherent segment checksum or hash mismatch")
		}
		for _, sample := range segment.Records {
			samples++
			if sample.Sequence != samples {
				return 0, "", errors.New("coherent market data coherent sample sequence mismatch")
			}
			if err := replayCoherentMarketDataCoherentSample(sample); err != nil {
				return 0, "", err
			}
		}
		prior = segment.Hash
	}
	return samples, prior, nil
}

func replayCoherentMarketDataCoherentSample(sample coherentMarketDataCoherentSample) error {
	identity, code := evaluateCoherentMarketDataCoherentSample(sample)
	if sample.Outcome == "success" {
		if code != "" || identity != sample.CoherentID || sample.RejectionCode != "" {
			return errors.New("coherent market data coherent success replay mismatch")
		}
		if _, err := runtimecore.RestoreCoherentView(sample.CoherentID, sample.Policy, sample.Trigger, coherentMarketDataSampleReferences(sample)); err != nil {
			return errors.New("coherent market data coherent success restore mismatch")
		}
		return nil
	}
	if sample.Outcome != "rejected" || code == "" || code != sample.RejectionCode || sample.CoherentID != "" {
		return errors.New("coherent market data coherent rejection replay mismatch")
	}
	return nil
}

func evaluateCoherentMarketDataCoherentSample(sample coherentMarketDataCoherentSample) (string, coherentMarketDataRejectionCode) {
	if sample.CaptureFailed {
		return "", coherentMarketDataRejectCaptureFailure
	}
	if sample.Policy != runtimecore.InitialCoherentMarketDataCoherentPolicy() || !validCoherentMarketDataTrigger(sample.Trigger) || len(sample.Members) != 2 {
		return "", coherentMarketDataRejectConfiguration
	}
	references, code := evaluateCoherentMarketDataCoherentMembers(sample)
	if code != "" {
		return "", code
	}
	sortCoherentMarketDataReferences(references)
	minimum, maximum := references[0].ReceiveMonotonicNanos, references[0].ReceiveMonotonicNanos
	latestStart := references[0].ReceiveUTC.Add(references[0].ClockOffset - references[0].ClockUncertainty)
	earliestEnd := references[0].ReceiveUTC.Add(references[0].ClockOffset + references[0].ClockUncertainty)
	for _, reference := range references[1:] {
		if reference.ReceiveMonotonicNanos < minimum {
			minimum = reference.ReceiveMonotonicNanos
		}
		if reference.ReceiveMonotonicNanos > maximum {
			maximum = reference.ReceiveMonotonicNanos
		}
		start := reference.ReceiveUTC.Add(reference.ClockOffset - reference.ClockUncertainty)
		end := reference.ReceiveUTC.Add(reference.ClockOffset + reference.ClockUncertainty)
		if start.After(latestStart) {
			latestStart = start
		}
		if end.Before(earliestEnd) {
			earliestEnd = end
		}
	}
	if maximum-minimum > uint64(sample.Policy.MaximumInterBookSkew.Nanoseconds()) {
		return "", coherentMarketDataRejectSkew
	}
	if latestStart.After(earliestEnd) {
		return "", coherentMarketDataRejectInterval
	}
	identity, err := coherentMarketDataJSONHash(references)
	if err != nil {
		return "", coherentMarketDataRejectIdentity
	}
	return identity, ""
}

func evaluateCoherentMarketDataCoherentMembers(sample coherentMarketDataCoherentSample) ([]runtimecore.ViewReference, coherentMarketDataRejectionCode) {
	seen := make(map[string]struct{}, 2)
	references := make([]runtimecore.ViewReference, 0, 2)
	for _, member := range sample.Members {
		keyID, valid := coherentMarketDataMarketKeyID(member.Key)
		if !valid {
			return nil, coherentMarketDataRejectIdentity
		}
		if _, duplicate := seen[keyID]; duplicate {
			return nil, coherentMarketDataRejectDuplicateMembership
		}
		seen[keyID] = struct{}{}
		if member.Reference == nil {
			return nil, coherentMarketDataRejectMissing
		}
		reference := *member.Reference
		referenceID, referenceValid := coherentMarketDataMarketKeyID(reference.Key)
		if !referenceValid || referenceID != keyID || !validCoherentMarketDataReference(reference) {
			return nil, coherentMarketDataRejectIdentity
		}
		if reference.ReceiveMonotonicNanos > sample.Trigger.MonotonicNanos || reference.IngestOrdinal > sample.Trigger.IngestOrdinal {
			return nil, coherentMarketDataRejectPostTrigger
		}
		if member.ActiveGeneration == 0 || reference.ConnectionGeneration != member.ActiveGeneration {
			return nil, coherentMarketDataRejectGeneration
		}
		if member.UnresolvedGap {
			return nil, coherentMarketDataRejectGap
		}
		if sample.Trigger.MonotonicNanos-reference.ReceiveMonotonicNanos > uint64(sample.Policy.MaximumBookAge.Nanoseconds()) {
			return nil, coherentMarketDataRejectStale
		}
		if reference.ClockUncertainty > sample.Policy.MaximumClockUncertainty {
			return nil, coherentMarketDataRejectUncertainty
		}
		references = append(references, reference)
	}
	return references, ""
}

func validCoherentMarketDataReference(reference runtimecore.ViewReference) bool {
	views := runtimecore.NewMarketViews()
	if err := views.ActivateGeneration(reference.Key, reference.ConnectionGeneration); err != nil {
		return false
	}
	_, err := views.Publish(runtimecore.MarketViewInput(reference))
	return err == nil
}

func validCoherentMarketDataTrigger(trigger runtimecore.AsOfTrigger) bool {
	return trigger.MonotonicNanos != 0 && trigger.IngestOrdinal != 0 && !trigger.UTC.IsZero() && trigger.UTC.Location() == time.UTC
}

func validCoherentMarketDataPair(pair string) bool {
	return pair == "BTCUSDT" || pair == "ETHUSDT" || pair == "ETHBTC"
}

func validCoherentMarketDataSampleOutcome(sample coherentMarketDataCoherentSample) bool {
	if sample.Policy.Version == "" || len(sample.Members) > 2 || (sample.Phase != "readiness" && sample.Phase != "official") {
		return false
	}
	if sample.Outcome == "success" {
		return sample.CoherentID != "" && sample.RejectionCode == ""
	}
	return sample.Outcome == "rejected" && sample.CoherentID == "" && validCoherentMarketDataRejectionCode(sample.RejectionCode)
}

func validCoherentMarketDataRejectionCode(code coherentMarketDataRejectionCode) bool {
	for _, candidate := range coherentMarketDataRejectionCodes {
		if code == candidate {
			return true
		}
	}
	return false
}

func coherentMarketDataSampleReferences(sample coherentMarketDataCoherentSample) []runtimecore.ViewReference {
	references := make([]runtimecore.ViewReference, 0, len(sample.Members))
	for _, member := range sample.Members {
		if member.Reference != nil {
			references = append(references, *member.Reference)
		}
	}
	sortCoherentMarketDataReferences(references)
	return references
}

func sortCoherentMarketDataReferences(references []runtimecore.ViewReference) {
	sort.Slice(references, func(left, right int) bool {
		leftID, _ := coherentMarketDataMarketKeyID(references[left].Key)
		rightID, _ := coherentMarketDataMarketKeyID(references[right].Key)
		return leftID < rightID
	})
}

func coherentMarketDataMarketKeyID(key runtimecore.MarketKey) (string, bool) {
	if key.Exchange == "" || !validQualificationLabel(key.Exchange) {
		return "", false
	}
	if _, err := domain.NewSpotInstrument(key.Instrument.Base, key.Instrument.Quote); err != nil {
		return "", false
	}
	return key.Exchange + ":" + key.Instrument.Symbol(), true
}

func coherentMarketDataJSONHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func coherentMarketDataSegmentHash(segment coherentMarketDataCoherentSegment) (string, error) {
	segment.Hash = ""
	return coherentMarketDataJSONHash(segment)
}

func cloneCoherentMarketDataCoherentSample(sample coherentMarketDataCoherentSample) coherentMarketDataCoherentSample {
	sample.Members = append([]coherentMarketDataCoherentMemberEvidence(nil), sample.Members...)
	for index := range sample.Members {
		if sample.Members[index].Reference != nil {
			reference := *sample.Members[index].Reference
			sample.Members[index].Reference = &reference
		}
	}
	return sample
}

func readStrictJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}

type coherentMarketDataRejectionCounts struct {
	CaptureFailure      uint64 `json:"capture_failure"`
	Missing             uint64 `json:"missing"`
	PostTrigger         uint64 `json:"post_trigger"`
	Generation          uint64 `json:"generation"`
	Gap                 uint64 `json:"gap"`
	Stale               uint64 `json:"stale"`
	Uncertainty         uint64 `json:"uncertainty"`
	Skew                uint64 `json:"skew"`
	Interval            uint64 `json:"interval"`
	Identity            uint64 `json:"identity"`
	Configuration       uint64 `json:"configuration"`
	DuplicateMembership uint64 `json:"duplicate_membership"`
}

func (counts *coherentMarketDataRejectionCounts) increment(code coherentMarketDataRejectionCode) {
	switch code {
	case coherentMarketDataRejectCaptureFailure:
		counts.CaptureFailure++
	case coherentMarketDataRejectMissing:
		counts.Missing++
	case coherentMarketDataRejectPostTrigger:
		counts.PostTrigger++
	case coherentMarketDataRejectGeneration:
		counts.Generation++
	case coherentMarketDataRejectGap:
		counts.Gap++
	case coherentMarketDataRejectStale:
		counts.Stale++
	case coherentMarketDataRejectUncertainty:
		counts.Uncertainty++
	case coherentMarketDataRejectSkew:
		counts.Skew++
	case coherentMarketDataRejectInterval:
		counts.Interval++
	case coherentMarketDataRejectIdentity:
		counts.Identity++
	case coherentMarketDataRejectConfiguration:
		counts.Configuration++
	case coherentMarketDataRejectDuplicateMembership:
		counts.DuplicateMembership++
	}
}

type coherentMarketDataPairSnapshot struct {
	Attempts              uint64                            `json:"attempts"`
	Successes             uint64                            `json:"successes"`
	Rejections            coherentMarketDataRejectionCounts `json:"rejections"`
	DegradedSince         time.Time                         `json:"degraded_since,omitempty"`
	RecoveryCount         uint64                            `json:"recovery_count"`
	RecoveryOver15Seconds uint64                            `json:"recovery_over_15_seconds"`
	RecoveryP95Bucket     time.Duration                     `json:"recovery_p95_bucket_nanos"`
	RecoveryMaximum       time.Duration                     `json:"recovery_maximum_nanos"`
}

type coherentMarketDataPairTracker struct {
	snapshot       coherentMarketDataPairSnapshot
	recoveries     []time.Duration
	exceededActive bool
}

func (tracker *coherentMarketDataPairTracker) Observe(sample *coherentMarketDataCoherentSample) bool {
	tracker.snapshot.Attempts++
	if sample.Outcome == "rejected" {
		tracker.snapshot.Rejections.increment(sample.RejectionCode)
		if tracker.snapshot.DegradedSince.IsZero() {
			tracker.snapshot.DegradedSince = sample.SampledAt
		}
		sample.Degradation.DegradedSince = tracker.snapshot.DegradedSince
		if sample.SampledAt.Sub(tracker.snapshot.DegradedSince) > coherentMarketDataMaximumDegradation && !tracker.exceededActive {
			tracker.exceededActive = true
			tracker.snapshot.RecoveryOver15Seconds++
			sample.Degradation.ExceededLimit = true
			return false
		}
		return true
	}
	tracker.snapshot.Successes++
	if tracker.snapshot.DegradedSince.IsZero() {
		return true
	}
	duration := sample.SampledAt.Sub(tracker.snapshot.DegradedSince)
	sample.Degradation = coherentMarketDataDegradationFact{DegradedSince: tracker.snapshot.DegradedSince, Recovered: true,
		RecoveryDuration: duration, RecoveryWithinLimit: duration <= coherentMarketDataMaximumDegradation, ExceededLimit: duration > coherentMarketDataMaximumDegradation}
	tracker.snapshot.RecoveryCount++
	tracker.recoveries = append(tracker.recoveries, duration)
	if duration > tracker.snapshot.RecoveryMaximum {
		tracker.snapshot.RecoveryMaximum = duration
	}
	if duration > coherentMarketDataMaximumDegradation && !tracker.exceededActive {
		tracker.snapshot.RecoveryOver15Seconds++
	}
	tracker.snapshot.RecoveryP95Bucket = coherentMarketDataRecoveryP95Bucket(tracker.recoveries)
	tracker.snapshot.DegradedSince = time.Time{}
	tracker.exceededActive = false
	return duration <= coherentMarketDataMaximumDegradation
}

func (tracker *coherentMarketDataPairTracker) Snapshot() coherentMarketDataPairSnapshot {
	if tracker == nil {
		return coherentMarketDataPairSnapshot{}
	}
	return tracker.snapshot
}

func coherentMarketDataRecoveryP95Bucket(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	value := ordered[(len(ordered)*95+99)/100-1]
	for _, bound := range []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute} {
		if value <= bound {
			return bound
		}
	}
	return time.Duration(1<<63 - 1)
}
