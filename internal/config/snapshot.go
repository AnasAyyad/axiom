package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"axiom/internal/domain"
)

// Source identifies where an accepted configuration revision originated.
type Source string

// Supported auditable configuration sources.
const (
	SourceDefault     Source = "default"
	SourceFile        Source = "file"
	SourceEnvironment Source = "environment"
	SourceAdmin       Source = "admin"
)

// Snapshot is an immutable validated configuration revision.
type Snapshot struct {
	configuration Configuration
	hash          string
	identifier    domain.ConfigurationSnapshotID
	createdAt     domain.EventTime
	source        Source
	actor         string
}

// NewSnapshot validates, clones, hashes, and records a configuration revision.
func NewSnapshot(configuration Configuration, source Source, actor string, clock domain.Clock) (Snapshot, error) {
	if !validSource(source) || !validActor(actor) || clock == nil {
		return Snapshot{}, configError("invalid_configuration", "snapshot_origin")
	}
	if err := Validate(configuration); err != nil {
		return Snapshot{}, err
	}
	return buildSnapshot(configuration, source, actor, clock)
}

func buildSnapshot(configuration Configuration, source Source, actor string, clock domain.Clock) (Snapshot, error) {
	canonical, err := json.Marshal(configuration)
	if err != nil {
		return Snapshot{}, configError("invalid_configuration", "serialization")
	}
	digest := sha256.Sum256(canonical)
	hash := hex.EncodeToString(digest[:])
	identifier, err := domain.NewConfigurationSnapshotID(hash[:24])
	if err != nil {
		return Snapshot{}, configError("invalid_configuration", "snapshot_id")
	}
	createdAt := clock.Now()
	if err := createdAt.Validate(); err != nil {
		return Snapshot{}, configError("invalid_configuration", "snapshot_time")
	}
	return Snapshot{
		configuration: cloneConfiguration(configuration), hash: hash, identifier: identifier,
		createdAt: createdAt, source: source, actor: actor,
	}, nil
}

// Configuration returns a defensive copy of the exact accepted graph.
func (snapshot Snapshot) Configuration() Configuration {
	return cloneConfiguration(snapshot.configuration)
}

// Hash returns the lower-case SHA-256 hash of canonical configuration JSON.
func (snapshot Snapshot) Hash() string { return snapshot.hash }

// ID returns the stable hash-derived snapshot identifier.
func (snapshot Snapshot) ID() domain.ConfigurationSnapshotID { return snapshot.identifier }

// CreatedAt returns the UTC and monotonic acceptance timestamp.
func (snapshot Snapshot) CreatedAt() domain.EventTime { return snapshot.createdAt }

// Source returns the accepted revision's origin class.
func (snapshot Snapshot) Source() Source { return snapshot.source }

// Actor returns the non-secret actor identifier recorded at acceptance.
func (snapshot Snapshot) Actor() string { return snapshot.actor }

// Metadata is the immutable audit record for one published snapshot.
type Metadata struct {
	Revision  uint64           `json:"revision"`
	ID        string           `json:"id"`
	Hash      string           `json:"hash"`
	CreatedAt domain.EventTime `json:"created_at"`
	Source    Source           `json:"source"`
	Actor     string           `json:"actor"`
	Changes   []string         `json:"changes"`
}

func (snapshot Snapshot) metadata() Metadata {
	return Metadata{
		Revision: snapshot.configuration.Revision, ID: snapshot.identifier.String(), Hash: snapshot.hash,
		CreatedAt: snapshot.createdAt, Source: snapshot.source, Actor: snapshot.actor,
	}
}

func cloneConfiguration(configuration Configuration) Configuration {
	cloned := configuration
	cloned.Assets = append([]domain.Asset(nil), configuration.Assets...)
	cloned.Instruments = append([]Instrument(nil), configuration.Instruments...)
	cloned.Exchanges = append([]ExchangeConfiguration(nil), configuration.Exchanges...)
	for index := range cloned.Exchanges {
		cloned.Exchanges[index].Instruments = append([]Instrument(nil), configuration.Exchanges[index].Instruments...)
		cloned.Exchanges[index].CandleIntervals = append([]string(nil), configuration.Exchanges[index].CandleIntervals...)
	}
	cloned.Trend.Parameters = append([]StrategyParameter(nil), configuration.Trend.Parameters...)
	for index := range cloned.Trend.Parameters {
		cloned.Trend.Parameters[index].ModelDependencies = append([]string(nil), configuration.Trend.Parameters[index].ModelDependencies...)
	}
	cloned.MeanReversion.Parameters = append([]StrategyParameter(nil), configuration.MeanReversion.Parameters...)
	for index := range cloned.MeanReversion.Parameters {
		cloned.MeanReversion.Parameters[index].ModelDependencies = append([]string(nil), configuration.MeanReversion.Parameters[index].ModelDependencies...)
	}
	cloned.Triangular.Cycles = append([]string(nil), configuration.Triangular.Cycles...)
	cloned.Triangular.Parameters = append([]StrategyParameter(nil), configuration.Triangular.Parameters...)
	for index := range cloned.Triangular.Parameters {
		cloned.Triangular.Parameters[index].ModelDependencies = append([]string(nil), configuration.Triangular.Parameters[index].ModelDependencies...)
	}
	cloned.Capabilities = append([]CapabilityDisposition(nil), configuration.Capabilities...)
	cloned.Secrets = append([]SecretReference(nil), configuration.Secrets...)
	return cloned
}

func validSource(source Source) bool {
	switch source {
	case SourceDefault, SourceFile, SourceEnvironment, SourceAdmin:
		return true
	default:
		return false
	}
}

func validActor(actor string) bool {
	return actor != "" && len(actor) <= 120 && !strings.ContainsAny(actor, "\r\n\x00")
}

// EqualConfiguration reports whether snapshots contain the exact same graph.
func (snapshot Snapshot) EqualConfiguration(other Snapshot) bool {
	return snapshot.hash == other.hash
}

// Age reports elapsed wall duration since acceptance for diagnostics only.
func (snapshot Snapshot) Age(now time.Time) time.Duration {
	return now.UTC().Sub(snapshot.createdAt.UTC)
}
