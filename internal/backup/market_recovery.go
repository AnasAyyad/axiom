package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var marketExchangePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// MarketSegmentReference is the authoritative file identity recovered from
// the restored PostgreSQL market_data_segments catalogue.
type MarketSegmentReference struct {
	ID       string `json:"id"`
	Exchange string `json:"exchange"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
}

// MarketRecoveryEvidence seals the complete ready-segment file inventory.
type MarketRecoveryEvidence struct {
	VerifiedSegments uint64 `json:"verified_segments"`
	InventoryHash    string `json:"inventory_hash"`
}

type verifiedMarketSegment struct {
	ID          string `json:"id"`
	Exchange    string `json:"exchange"`
	StoragePath string `json:"storage_path"`
	SHA256      string `json:"sha256"`
}

// VerifyMarketRecovery proves every ready database segment exists inside the
// restored market-data root and has its authoritative checksum. Current
// recorders store either directly below the root or below an exchange folder;
// exactly one of those locations must exist for each reference.
func VerifyMarketRecovery(root string, references []MarketSegmentReference) (MarketRecoveryEvidence, error) {
	cleanRoot, err := canonicalDirectory(root)
	if err != nil {
		return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_root_invalid")
	}
	ordered := append([]MarketSegmentReference(nil), references...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].ID != ordered[right].ID {
			return ordered[left].ID < ordered[right].ID
		}
		if ordered[left].Exchange != ordered[right].Exchange {
			return ordered[left].Exchange < ordered[right].Exchange
		}
		return ordered[left].Path < ordered[right].Path
	})
	verified := make([]verifiedMarketSegment, 0, len(ordered))
	seenIDs := make(map[string]struct{}, len(ordered))
	seenPaths := make(map[string]struct{}, len(ordered))
	for _, reference := range ordered {
		if !validMarketReference(reference) {
			return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_reference_invalid")
		}
		if _, duplicate := seenIDs[reference.ID]; duplicate {
			return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_reference_duplicate")
		}
		seenIDs[reference.ID] = struct{}{}
		storagePath, filePath, err := locateMarketSegment(cleanRoot, reference)
		if err != nil {
			return MarketRecoveryEvidence{}, err
		}
		if _, duplicate := seenPaths[storagePath]; duplicate {
			return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_path_duplicate")
		}
		seenPaths[storagePath] = struct{}{}
		digest, err := marketFileDigest(filePath)
		if err != nil || digest != reference.SHA256 {
			return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_checksum_failed")
		}
		verified = append(verified, verifiedMarketSegment{ID: reference.ID,
			Exchange: reference.Exchange, StoragePath: storagePath, SHA256: reference.SHA256})
	}
	payload, err := json.Marshal(verified)
	if err != nil {
		return MarketRecoveryEvidence{}, fmt.Errorf("market_recovery_inventory_invalid")
	}
	digest := sha256.Sum256(payload)
	return MarketRecoveryEvidence{VerifiedSegments: uint64(len(verified)),
		InventoryHash: hex.EncodeToString(digest[:])}, nil
}

func validMarketReference(reference MarketSegmentReference) bool {
	return validArtifactMetadata(reference.ID, 256) && marketExchangePattern.MatchString(reference.Exchange) &&
		artifactNamePattern.MatchString(reference.Path) && filepath.Base(reference.Path) == reference.Path &&
		!strings.Contains(reference.Path, `\`) && validHash(reference.SHA256)
}

func locateMarketSegment(root string, reference MarketSegmentReference) (string, string, error) {
	candidates := []string{reference.Path, filepath.Join(reference.Exchange, reference.Path)}
	var storagePath, resolvedPath string
	for _, relative := range candidates {
		candidate := filepath.Join(root, relative)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("market_recovery_file_invalid")
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !pathContains(root, resolved) {
			return "", "", fmt.Errorf("market_recovery_path_escape")
		}
		info, err = os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("market_recovery_file_invalid")
		}
		if resolvedPath != "" {
			return "", "", fmt.Errorf("market_recovery_path_ambiguous")
		}
		storagePath, resolvedPath = filepath.ToSlash(relative), resolved
	}
	if resolvedPath == "" {
		return "", "", fmt.Errorf("market_recovery_file_missing")
	}
	return storagePath, resolvedPath, nil
}

func marketFileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
