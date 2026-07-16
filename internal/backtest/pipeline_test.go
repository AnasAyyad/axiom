package backtest

import "testing"

func TestOperationalPipelineFailsClosedWithoutA9A10Dependencies(t *testing.T) {
	if _, err := NewPipelineProcessor(PipelineDependencies{}); err == nil {
		t.Fatal("incomplete operational pipeline was enabled")
	}
}
