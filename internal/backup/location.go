package backup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocationEvidence records filesystem identities without exposing device paths.
type LocationEvidence struct {
	DestinationMount string   `json:"destination_mount"`
	DestinationID    string   `json:"destination_filesystem_id"`
	ProtectedIDs     []string `json:"protected_filesystem_ids"`
}

type mountRecord struct {
	id, point string
}

// ValidateIndependentDestination proves the backup root is on a dedicated
// non-root mount and not on a database, market-data, or staging filesystem.
func ValidateIndependentDestination(destination string, protected []string) (LocationEvidence, error) {
	if len(protected) < 2 {
		return LocationEvidence{}, fmt.Errorf("backup_location_protected_set_invalid")
	}
	mounts, err := currentMounts()
	if err != nil {
		return LocationEvidence{}, err
	}
	destinationPath, err := canonicalDirectory(destination)
	if err != nil {
		return LocationEvidence{}, fmt.Errorf("backup_destination_invalid")
	}
	destinationMount, found := containingMount(destinationPath, mounts)
	if !found || destinationMount.point == string(filepath.Separator) {
		return LocationEvidence{}, fmt.Errorf("backup_destination_not_independent_mount")
	}
	evidence := LocationEvidence{DestinationMount: destinationMount.point,
		DestinationID: destinationMount.id, ProtectedIDs: make([]string, 0, len(protected))}
	seen := make(map[string]struct{}, len(protected))
	for _, candidate := range protected {
		protectedPath, pathErr := canonicalDirectory(candidate)
		if pathErr != nil {
			return LocationEvidence{}, fmt.Errorf("backup_protected_location_invalid")
		}
		protectedMount, ok := containingMount(protectedPath, mounts)
		if !ok || protectedMount.id == destinationMount.id ||
			pathContains(protectedPath, destinationPath) || pathContains(destinationPath, protectedPath) {
			return LocationEvidence{}, fmt.Errorf("backup_destination_filesystem_not_independent")
		}
		if _, duplicate := seen[protectedMount.id]; !duplicate {
			evidence.ProtectedIDs = append(evidence.ProtectedIDs, protectedMount.id)
			seen[protectedMount.id] = struct{}{}
		}
	}
	return evidence, nil
}

func currentMounts() ([]mountRecord, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("backup_mount_inventory_unavailable")
	}
	defer file.Close()
	mounts := make([]mountRecord, 0, 32)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 || !strings.Contains(fields[2], ":") {
			return nil, fmt.Errorf("backup_mount_inventory_invalid")
		}
		point := unescapeMountField(fields[4])
		if !filepath.IsAbs(point) {
			return nil, fmt.Errorf("backup_mount_inventory_invalid")
		}
		mounts = append(mounts, mountRecord{id: fields[2], point: filepath.Clean(point)})
	}
	if scanner.Err() != nil || len(mounts) == 0 {
		return nil, fmt.Errorf("backup_mount_inventory_unavailable")
	}
	return mounts, nil
}

func canonicalDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) == string(filepath.Separator) {
		return "", fmt.Errorf("path_invalid")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("path_invalid")
	}
	return filepath.Clean(resolved), nil
}

func containingMount(path string, mounts []mountRecord) (mountRecord, bool) {
	var result mountRecord
	for _, mount := range mounts {
		if pathContains(mount.point, path) && len(mount.point) > len(result.point) {
			result = mount
		}
	}
	return result, result.point != ""
}

func pathContains(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func unescapeMountField(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}
