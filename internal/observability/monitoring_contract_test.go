package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMonitoringProvisioningParses(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	for _, relative := range []string{
		"monitoring/prometheus.yml", "monitoring/alerts.yml",
		"monitoring/grafana/provisioning/dashboards/axiom.yml",
		"monitoring/grafana/provisioning/datasources/prometheus.yml",
	} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		var document yaml.Node
		if err = yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", relative, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "monitoring/grafana/dashboards/axiom-operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dashboard struct {
		Title  string `json:"title"`
		Panels []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"panels"`
	}
	if err = json.Unmarshal(data, &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Title == "" || len(dashboard.Panels) < 8 {
		t.Fatalf("incomplete dashboard: %#v", dashboard)
	}
}
