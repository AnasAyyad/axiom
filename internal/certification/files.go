package certification

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maximumInputBytes = 20 << 20

// ReadStrictJSON reads one bounded regular JSON file with no unknown fields.
func ReadStrictJSON(path string, destination any) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("certification_input_invalid")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("certification_input_invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("certification_input_unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, info) || !info.Mode().IsRegular() ||
		info.Size() <= 0 || info.Size() > maximumInputBytes {
		return fmt.Errorf("certification_input_invalid")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maximumInputBytes))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return fmt.Errorf("certification_input_invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("certification_input_trailing_data")
	}
	return nil
}

// WriteVerdictNoReplace writes and fsyncs one immutable release verdict.
func WriteVerdictNoReplace(root string, verdict ReleaseVerdict) (string, error) {
	if !filepath.IsAbs(root) || filepath.Base(verdict.CandidateID) != verdict.CandidateID ||
		verdict.State != CertifiedReleaseState || !verdict.Certified {
		return "", fmt.Errorf("release_verdict_write_rejected")
	}
	directoryInfo, err := os.Lstat(root)
	if err != nil || directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() ||
		directoryInfo.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("release_verdict_directory_failed")
	}
	path := filepath.Join(root, "v1-release-verdict-"+verdict.CandidateID+".json")
	payload, err := json.MarshalIndent(verdict, "", "  ")
	if err != nil {
		return "", fmt.Errorf("release_verdict_encode_failed")
	}
	payload = append(payload, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
	if err != nil {
		return "", fmt.Errorf("release_verdict_exists_or_unwritable")
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("release_verdict_write_failed")
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("release_verdict_sync_failed")
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("release_verdict_close_failed")
	}
	directory, err := os.Open(root)
	if err != nil {
		return "", fmt.Errorf("release_verdict_directory_failed")
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return "", fmt.Errorf("release_verdict_sync_failed")
	}
	return path, nil
}
