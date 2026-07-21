package research

import "time"

// Window is one immutable chronological research interval [Start, End).
type Window struct {
	Name  string    `json:"name"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Observation is one ordered exact strategy-return observation.
type Observation struct {
	At            time.Time `json:"at"`
	Return        string    `json:"return"`
	Asset         string    `json:"asset"`
	Regime        string    `json:"regime"`
	HoldingClass  string    `json:"holding_class"`
	FalseBreakout bool      `json:"false_breakout"`
}

// ChronologicalSplit owns non-overlapping train, validation, and final windows.
type ChronologicalSplit struct {
	Train      Window `json:"train"`
	Validation Window `json:"validation"`
	FinalTest  Window `json:"final_test"`
}

// Validate proves chronological, non-overlapping, explicitly named windows.
func (split ChronologicalSplit) Validate() error {
	if !validWindow(split.Train, "train") || !validWindow(split.Validation, "validation") ||
		!validWindow(split.FinalTest, "final_test") || split.Train.End.After(split.Validation.Start) ||
		split.Validation.End.After(split.FinalTest.Start) {
		return researchError("research_split_invalid")
	}
	return nil
}

// Partition assigns each ordered observation without future normalization.
func (split ChronologicalSplit) Partition(observations []Observation) (map[string][]Observation, error) {
	if err := split.Validate(); err != nil {
		return nil, err
	}
	result := map[string][]Observation{"train": {}, "validation": {}, "final_test": {}}
	var prior time.Time
	for _, observation := range observations {
		if observation.At.Location() != time.UTC || (!prior.IsZero() && !observation.At.After(prior)) {
			return nil, researchError("research_observation_order_invalid")
		}
		prior = observation.At
		switch {
		case contains(split.Train, observation.At):
			result["train"] = append(result["train"], observation)
		case contains(split.Validation, observation.At):
			result["validation"] = append(result["validation"], observation)
		case contains(split.FinalTest, observation.At):
			result["final_test"] = append(result["final_test"], observation)
		}
	}
	return result, nil
}

// WalkForwardFold contains sample indexes for one chronological evaluation.
type WalkForwardFold struct {
	TrainStart      int `json:"train_start"`
	TrainEnd        int `json:"train_end"`
	ValidationStart int `json:"validation_start"`
	ValidationEnd   int `json:"validation_end"`
	TestStart       int `json:"test_start"`
	TestEnd         int `json:"test_end"`
}

// WalkForward creates deterministic expanding-window folds.
func WalkForward(samples, initialTrain, validation, test, step int) ([]WalkForwardFold, error) {
	if samples <= 0 || initialTrain <= 0 || validation <= 0 || test <= 0 || step <= 0 ||
		initialTrain+validation+test > samples {
		return nil, researchError("walk_forward_invalid")
	}
	var folds []WalkForwardFold
	for trainEnd := initialTrain; trainEnd+validation+test <= samples; trainEnd += step {
		folds = append(folds, WalkForwardFold{TrainStart: 0, TrainEnd: trainEnd,
			ValidationStart: trainEnd, ValidationEnd: trainEnd + validation,
			TestStart: trainEnd + validation, TestEnd: trainEnd + validation + test})
	}
	return folds, nil
}

func validWindow(window Window, name string) bool {
	return window.Name == name && window.Start.Location() == time.UTC && window.End.Location() == time.UTC && window.End.After(window.Start)
}

func contains(window Window, value time.Time) bool {
	return !value.Before(window.Start) && value.Before(window.End)
}
