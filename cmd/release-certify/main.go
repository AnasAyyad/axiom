package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/certification"
	"axiom/internal/security"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release_certification-certify:", err)
		os.Exit(1)
	}
}

func run(_ context.Context, output io.Writer) error {
	if os.Getenv("AXIOM_RELEASE_CERTIFICATION_ENABLED") != "1" {
		return fmt.Errorf("final certification is default-off")
	}
	var candidate certification.Candidate
	if err := certification.ReadStrictJSON(os.Getenv("AXIOM_RELEASE_CERTIFICATION_CANDIDATE_FILE"), &candidate); err != nil {
		return err
	}
	var trust certification.TrustStore
	if err := certification.ReadStrictJSON(os.Getenv("AXIOM_RELEASE_CERTIFICATION_TRUSTED_REVIEWERS_FILE"), &trust); err != nil {
		return err
	}
	info := buildinfo.Current()
	if info.Dirty || info.Commit != candidate.SourceSHA {
		return fmt.Errorf("final certification build identity mismatch")
	}
	now := time.Now().UTC()
	if err := certification.ValidateCandidate(candidate, trust, now); err != nil {
		return err
	}
	key, err := signingKey(os.Getenv("AXIOM_RELEASE_CERTIFICATION_SIGNING_KEY_FILE"))
	if err != nil {
		return err
	}
	validUntil := now.Add(30 * 24 * time.Hour)
	if candidate.ValidUntil.Before(validUntil) {
		validUntil = candidate.ValidUntil
	}
	verdict, err := certification.NewReleaseVerdict(candidate, key, now, validUntil)
	if err != nil {
		return err
	}
	if _, err = certification.WriteVerdictNoReplace(os.Getenv("AXIOM_RELEASE_CERTIFICATION_VERDICT_DIRECTORY"), verdict); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output,
		"candidate_id=%s source_sha=%s state=%s certified=%t evidence_hash=%s\n",
		verdict.CandidateID, verdict.SourceSHA, verdict.State, verdict.Certified, verdict.EvidenceHash)
	return err
}

func signingKey(path string) (ed25519.PrivateKey, error) {
	value, err := security.ReadSecretFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("release_certification release signing key invalid")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
