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
	b2CoherentSegmentSchema = "axiom.b2-coherent-segment.v1"
	b2CoherentManifestName  = "b2-coherent-manifest.json"
	b2CoherentSegmentEvery  = 5 * time.Minute
	b2CoherentSampleEvery   = 5 * time.Second
	b2MaximumDegradation    = 15 * time.Second
)

type b2RejectionCode string

const (
	b2RejectCaptureFailure      b2RejectionCode = "capture_failure"
	b2RejectMissing             b2RejectionCode = "missing"
	b2RejectPostTrigger         b2RejectionCode = "post_trigger"
	b2RejectGeneration          b2RejectionCode = "generation"
	b2RejectGap                 b2RejectionCode = "gap"
	b2RejectStale               b2RejectionCode = "stale"
	b2RejectUncertainty         b2RejectionCode = "uncertainty"
	b2RejectSkew                b2RejectionCode = "skew"
	b2RejectInterval            b2RejectionCode = "interval"
	b2RejectIdentity            b2RejectionCode = "identity"
	b2RejectConfiguration       b2RejectionCode = "configuration"
	b2RejectDuplicateMembership b2RejectionCode = "duplicate_membership"
)

var b2RejectionCodes = []b2RejectionCode{
	b2RejectCaptureFailure, b2RejectMissing, b2RejectPostTrigger, b2RejectGeneration,
	b2RejectGap, b2RejectStale, b2RejectUncertainty, b2RejectSkew, b2RejectInterval,
	b2RejectIdentity, b2RejectConfiguration, b2RejectDuplicateMembership,
}

type b2CoherentMemberEvidence struct {
	Key              runtimecore.MarketKey      `json:"key"`
	Reference        *runtimecore.ViewReference `json:"reference,omitempty"`
	ActiveGeneration uint64                     `json:"active_generation"`
	UnresolvedGap    bool                       `json:"unresolved_gap"`
}

type b2DegradationFact struct {
	DegradedSince       time.Time     `json:"degraded_since,omitempty"`
	Recovered           bool          `json:"recovered"`
	RecoveryDuration    time.Duration `json:"recovery_duration_nanos,omitempty"`
	RecoveryWithinLimit bool          `json:"recovery_within_limit,omitempty"`
	ExceededLimit       bool          `json:"exceeded_limit,omitempty"`
}

type b2CoherentSample struct {
	Sequence      uint64                     `json:"sequence"`
	Phase         string                     `json:"phase"`
	Pair          string                     `json:"pair"`
	SampledAt     time.Time                  `json:"sampled_at"`
	Policy        runtimecore.CoherentPolicy `json:"policy"`
	Trigger       runtimecore.AsOfTrigger    `json:"trigger"`
	CaptureFailed bool                       `json:"capture_failed,omitempty"`
	Members       []b2CoherentMemberEvidence `json:"members"`
	Outcome       string                     `json:"outcome"`
	RejectionCode b2RejectionCode            `json:"rejection_code,omitempty"`
	CoherentID    string                     `json:"coherent_identity,omitempty"`
	Degradation   b2DegradationFact          `json:"degradation"`
}

type b2CoherentSegment struct {
	SchemaVersion  string             `json:"schema_version"`
	Kind           string             `json:"kind"`
	Sequence       uint64             `json:"sequence"`
	StartedAt      time.Time          `json:"started_at"`
	EndedAt        time.Time          `json:"ended_at"`
	PreviousHash   string             `json:"previous_hash,omitempty"`
	RecordChecksum string             `json:"record_checksum"`
	Records        []b2CoherentSample `json:"records"`
	Hash           string             `json:"hash"`
}

type b2CoherentSegmentReference struct {
	Sequence       uint64 `json:"sequence"`
	Filename       string `json:"filename"`
	RecordCount    uint64 `json:"record_count"`
	RecordChecksum string `json:"record_checksum"`
	PreviousHash   string `json:"previous_hash,omitempty"`
	Hash           string `json:"hash"`
}

type b2CoherentManifest struct {
	SchemaVersion string                       `json:"schema_version"`
	Kind          string                       `json:"kind"`
	SourceCommit  string                       `json:"source_commit"`
	UpdatedAt     time.Time                    `json:"updated_at"`
	Segments      []b2CoherentSegmentReference `json:"segments"`
	Sequence      uint64                       `json:"sequence"`
	TerminalHash  string                       `json:"terminal_hash,omitempty"`
	SampleCount   uint64                       `json:"sample_count"`
}

type b2CoherentSegmentWriter struct {
	mutex        sync.Mutex
	root         string
	sourceCommit string
	write        func(string, any) error
	manifest     b2CoherentManifest
	pending      []b2CoherentSample
	window       time.Time
	nextSample   uint64
}

func newB2CoherentSegmentWriter(root, sourceCommit string, write func(string, any) error) (*b2CoherentSegmentWriter, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) || !validGitCommit(sourceCommit) || write == nil {
		return nil, errors.New("B2 coherent segment writer configuration invalid")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, errors.New("B2 coherent segment root must be empty")
	}
	return &b2CoherentSegmentWriter{root: root, sourceCommit: sourceCommit, write: write,
		manifest: b2CoherentManifest{SchemaVersion: b2CoherentSegmentSchema, Kind: "manifest", SourceCommit: sourceCommit}}, nil
}

func (writer *b2CoherentSegmentWriter) Append(sample b2CoherentSample) error {
	if writer == nil {
		return errors.New("B2 coherent segment writer unavailable")
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if sample.SampledAt.IsZero() || sample.SampledAt.Location() != time.UTC || !validB2Pair(sample.Pair) || !validB2SampleOutcome(sample) {
		return errors.New("B2 coherent sample invalid")
	}
	window := sample.SampledAt.Truncate(b2CoherentSegmentEvery)
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
	writer.pending = append(writer.pending, cloneB2CoherentSample(sample))
	return nil
}

func (writer *b2CoherentSegmentWriter) Flush() error {
	if writer == nil {
		return errors.New("B2 coherent segment writer unavailable")
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.flushLocked()
}

func (writer *b2CoherentSegmentWriter) Snapshot() (uint64, string, uint64) {
	if writer == nil {
		return 0, "", 0
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.manifest.Sequence, writer.manifest.TerminalHash, writer.manifest.SampleCount
}

func (writer *b2CoherentSegmentWriter) flushLocked() error {
	if len(writer.pending) == 0 {
		return nil
	}
	records := append([]b2CoherentSample(nil), writer.pending...)
	checksum, err := b2JSONHash(records)
	if err != nil {
		return err
	}
	segment := b2CoherentSegment{SchemaVersion: b2CoherentSegmentSchema, Kind: "segment",
		Sequence: writer.manifest.Sequence + 1, StartedAt: records[0].SampledAt,
		EndedAt: records[len(records)-1].SampledAt, PreviousHash: writer.manifest.TerminalHash,
		RecordChecksum: checksum, Records: records}
	segment.Hash, err = b2SegmentHash(segment)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("b2-coherent-%06d.json", segment.Sequence)
	path := filepath.Join(writer.root, filename)
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("B2 coherent segment path collision")
	}
	if err = writer.write(path, segment); err != nil {
		return err
	}
	reference := b2CoherentSegmentReference{Sequence: segment.Sequence, Filename: filename,
		RecordCount: uint64(len(records)), RecordChecksum: checksum, PreviousHash: segment.PreviousHash, Hash: segment.Hash}
	next := writer.manifest
	next.UpdatedAt = time.Now().UTC()
	next.Segments = append(append([]b2CoherentSegmentReference(nil), writer.manifest.Segments...), reference)
	next.Sequence, next.TerminalHash = segment.Sequence, segment.Hash
	next.SampleCount += uint64(len(records))
	if err = writer.write(filepath.Join(writer.root, b2CoherentManifestName), next); err != nil {
		return err
	}
	writer.manifest, writer.pending, writer.window = next, nil, time.Time{}
	return nil
}

func verifyB2CoherentSegments(root, sourceCommit string, expectedSequence uint64, expectedHash string) (b2CoherentManifest, error) {
	var manifest b2CoherentManifest
	if err := readStrictJSON(filepath.Join(root, b2CoherentManifestName), &manifest); err != nil {
		return manifest, err
	}
	if manifest.SchemaVersion != b2CoherentSegmentSchema || manifest.Kind != "manifest" || manifest.SourceCommit != sourceCommit ||
		manifest.Sequence != expectedSequence || manifest.TerminalHash != expectedHash || uint64(len(manifest.Segments)) != manifest.Sequence {
		return manifest, errors.New("B2 coherent manifest metadata mismatch")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return manifest, err
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".partial") {
			return manifest, errors.New("B2 coherent partial write retained")
		}
	}
	samples, prior, err := verifyB2CoherentSegmentFiles(root, manifest)
	if err != nil {
		return manifest, err
	}
	if samples != manifest.SampleCount || prior != manifest.TerminalHash {
		return manifest, errors.New("B2 coherent manifest terminal mismatch")
	}
	return manifest, nil
}

func verifyB2CoherentSegmentFiles(root string, manifest b2CoherentManifest) (uint64, string, error) {
	var prior string
	var samples uint64
	for index, reference := range manifest.Segments {
		if reference.Sequence != uint64(index+1) || reference.PreviousHash != prior || filepath.Base(reference.Filename) != reference.Filename {
			return 0, "", errors.New("B2 coherent manifest chain mismatch")
		}
		var segment b2CoherentSegment
		if err := readStrictJSON(filepath.Join(root, reference.Filename), &segment); err != nil {
			return 0, "", err
		}
		if segment.SchemaVersion != b2CoherentSegmentSchema || segment.Kind != "segment" || segment.Sequence != reference.Sequence ||
			segment.PreviousHash != prior || segment.RecordChecksum != reference.RecordChecksum || segment.Hash != reference.Hash ||
			uint64(len(segment.Records)) != reference.RecordCount || len(segment.Records) == 0 {
			return 0, "", errors.New("B2 coherent segment metadata mismatch")
		}
		checksum, checksumErr := b2JSONHash(segment.Records)
		hash, hashErr := b2SegmentHash(segment)
		if checksumErr != nil || hashErr != nil || checksum != segment.RecordChecksum || hash != segment.Hash {
			return 0, "", errors.New("B2 coherent segment checksum or hash mismatch")
		}
		for _, sample := range segment.Records {
			samples++
			if sample.Sequence != samples {
				return 0, "", errors.New("B2 coherent sample sequence mismatch")
			}
			if err := replayB2CoherentSample(sample); err != nil {
				return 0, "", err
			}
		}
		prior = segment.Hash
	}
	return samples, prior, nil
}

func replayB2CoherentSample(sample b2CoherentSample) error {
	identity, code := evaluateB2CoherentSample(sample)
	if sample.Outcome == "success" {
		if code != "" || identity != sample.CoherentID || sample.RejectionCode != "" {
			return errors.New("B2 coherent success replay mismatch")
		}
		if _, err := runtimecore.RestoreCoherentView(sample.CoherentID, sample.Policy, sample.Trigger, b2SampleReferences(sample)); err != nil {
			return errors.New("B2 coherent success restore mismatch")
		}
		return nil
	}
	if sample.Outcome != "rejected" || code == "" || code != sample.RejectionCode || sample.CoherentID != "" {
		return errors.New("B2 coherent rejection replay mismatch")
	}
	return nil
}

func evaluateB2CoherentSample(sample b2CoherentSample) (string, b2RejectionCode) {
	if sample.CaptureFailed {
		return "", b2RejectCaptureFailure
	}
	if sample.Policy != runtimecore.InitialB2CoherentPolicy() || !validB2Trigger(sample.Trigger) || len(sample.Members) != 2 {
		return "", b2RejectConfiguration
	}
	references, code := evaluateB2CoherentMembers(sample)
	if code != "" {
		return "", code
	}
	sortB2References(references)
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
		return "", b2RejectSkew
	}
	if latestStart.After(earliestEnd) {
		return "", b2RejectInterval
	}
	identity, err := b2JSONHash(references)
	if err != nil {
		return "", b2RejectIdentity
	}
	return identity, ""
}

func evaluateB2CoherentMembers(sample b2CoherentSample) ([]runtimecore.ViewReference, b2RejectionCode) {
	seen := make(map[string]struct{}, 2)
	references := make([]runtimecore.ViewReference, 0, 2)
	for _, member := range sample.Members {
		keyID, valid := b2MarketKeyID(member.Key)
		if !valid {
			return nil, b2RejectIdentity
		}
		if _, duplicate := seen[keyID]; duplicate {
			return nil, b2RejectDuplicateMembership
		}
		seen[keyID] = struct{}{}
		if member.Reference == nil {
			return nil, b2RejectMissing
		}
		reference := *member.Reference
		referenceID, referenceValid := b2MarketKeyID(reference.Key)
		if !referenceValid || referenceID != keyID || !validB2Reference(reference) {
			return nil, b2RejectIdentity
		}
		if reference.ReceiveMonotonicNanos > sample.Trigger.MonotonicNanos || reference.IngestOrdinal > sample.Trigger.IngestOrdinal {
			return nil, b2RejectPostTrigger
		}
		if member.ActiveGeneration == 0 || reference.ConnectionGeneration != member.ActiveGeneration {
			return nil, b2RejectGeneration
		}
		if member.UnresolvedGap {
			return nil, b2RejectGap
		}
		if sample.Trigger.MonotonicNanos-reference.ReceiveMonotonicNanos > uint64(sample.Policy.MaximumBookAge.Nanoseconds()) {
			return nil, b2RejectStale
		}
		if reference.ClockUncertainty > sample.Policy.MaximumClockUncertainty {
			return nil, b2RejectUncertainty
		}
		references = append(references, reference)
	}
	return references, ""
}

func validB2Reference(reference runtimecore.ViewReference) bool {
	views := runtimecore.NewMarketViews()
	if err := views.ActivateGeneration(reference.Key, reference.ConnectionGeneration); err != nil {
		return false
	}
	_, err := views.Publish(runtimecore.MarketViewInput(reference))
	return err == nil
}

func validB2Trigger(trigger runtimecore.AsOfTrigger) bool {
	return trigger.MonotonicNanos != 0 && trigger.IngestOrdinal != 0 && !trigger.UTC.IsZero() && trigger.UTC.Location() == time.UTC
}

func validB2Pair(pair string) bool { return pair == "BTCUSDT" || pair == "ETHUSDT" || pair == "ETHBTC" }

func validB2SampleOutcome(sample b2CoherentSample) bool {
	if sample.Policy.Version == "" || len(sample.Members) > 2 || (sample.Phase != "readiness" && sample.Phase != "official") {
		return false
	}
	if sample.Outcome == "success" {
		return sample.CoherentID != "" && sample.RejectionCode == ""
	}
	return sample.Outcome == "rejected" && sample.CoherentID == "" && validB2RejectionCode(sample.RejectionCode)
}

func validB2RejectionCode(code b2RejectionCode) bool {
	for _, candidate := range b2RejectionCodes {
		if code == candidate {
			return true
		}
	}
	return false
}

func b2SampleReferences(sample b2CoherentSample) []runtimecore.ViewReference {
	references := make([]runtimecore.ViewReference, 0, len(sample.Members))
	for _, member := range sample.Members {
		if member.Reference != nil {
			references = append(references, *member.Reference)
		}
	}
	sortB2References(references)
	return references
}

func sortB2References(references []runtimecore.ViewReference) {
	sort.Slice(references, func(left, right int) bool {
		leftID, _ := b2MarketKeyID(references[left].Key)
		rightID, _ := b2MarketKeyID(references[right].Key)
		return leftID < rightID
	})
}

func b2MarketKeyID(key runtimecore.MarketKey) (string, bool) {
	if key.Exchange == "" || !validQualificationLabel(key.Exchange) {
		return "", false
	}
	if _, err := domain.NewSpotInstrument(key.Instrument.Base, key.Instrument.Quote); err != nil {
		return "", false
	}
	return key.Exchange + ":" + key.Instrument.Symbol(), true
}

func b2JSONHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func b2SegmentHash(segment b2CoherentSegment) (string, error) {
	segment.Hash = ""
	return b2JSONHash(segment)
}

func cloneB2CoherentSample(sample b2CoherentSample) b2CoherentSample {
	sample.Members = append([]b2CoherentMemberEvidence(nil), sample.Members...)
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

type b2RejectionCounts struct {
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

func (counts *b2RejectionCounts) increment(code b2RejectionCode) {
	switch code {
	case b2RejectCaptureFailure:
		counts.CaptureFailure++
	case b2RejectMissing:
		counts.Missing++
	case b2RejectPostTrigger:
		counts.PostTrigger++
	case b2RejectGeneration:
		counts.Generation++
	case b2RejectGap:
		counts.Gap++
	case b2RejectStale:
		counts.Stale++
	case b2RejectUncertainty:
		counts.Uncertainty++
	case b2RejectSkew:
		counts.Skew++
	case b2RejectInterval:
		counts.Interval++
	case b2RejectIdentity:
		counts.Identity++
	case b2RejectConfiguration:
		counts.Configuration++
	case b2RejectDuplicateMembership:
		counts.DuplicateMembership++
	}
}

type b2PairSnapshot struct {
	Attempts              uint64            `json:"attempts"`
	Successes             uint64            `json:"successes"`
	Rejections            b2RejectionCounts `json:"rejections"`
	DegradedSince         time.Time         `json:"degraded_since,omitempty"`
	RecoveryCount         uint64            `json:"recovery_count"`
	RecoveryOver15Seconds uint64            `json:"recovery_over_15_seconds"`
	RecoveryP95Bucket     time.Duration     `json:"recovery_p95_bucket_nanos"`
	RecoveryMaximum       time.Duration     `json:"recovery_maximum_nanos"`
}

type b2PairTracker struct {
	snapshot       b2PairSnapshot
	recoveries     []time.Duration
	exceededActive bool
}

func (tracker *b2PairTracker) Observe(sample *b2CoherentSample) bool {
	tracker.snapshot.Attempts++
	if sample.Outcome == "rejected" {
		tracker.snapshot.Rejections.increment(sample.RejectionCode)
		if tracker.snapshot.DegradedSince.IsZero() {
			tracker.snapshot.DegradedSince = sample.SampledAt
		}
		sample.Degradation.DegradedSince = tracker.snapshot.DegradedSince
		if sample.SampledAt.Sub(tracker.snapshot.DegradedSince) > b2MaximumDegradation && !tracker.exceededActive {
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
	sample.Degradation = b2DegradationFact{DegradedSince: tracker.snapshot.DegradedSince, Recovered: true,
		RecoveryDuration: duration, RecoveryWithinLimit: duration <= b2MaximumDegradation, ExceededLimit: duration > b2MaximumDegradation}
	tracker.snapshot.RecoveryCount++
	tracker.recoveries = append(tracker.recoveries, duration)
	if duration > tracker.snapshot.RecoveryMaximum {
		tracker.snapshot.RecoveryMaximum = duration
	}
	if duration > b2MaximumDegradation && !tracker.exceededActive {
		tracker.snapshot.RecoveryOver15Seconds++
	}
	tracker.snapshot.RecoveryP95Bucket = b2RecoveryP95Bucket(tracker.recoveries)
	tracker.snapshot.DegradedSince = time.Time{}
	tracker.exceededActive = false
	return duration <= b2MaximumDegradation
}

func (tracker *b2PairTracker) Snapshot() b2PairSnapshot {
	if tracker == nil {
		return b2PairSnapshot{}
	}
	return tracker.snapshot
}

func b2RecoveryP95Bucket(values []time.Duration) time.Duration {
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
