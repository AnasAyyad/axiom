package runtimecore

import "sort"

// ActiveGeneration is one persisted single-writer generation selection.
type ActiveGeneration struct {
	Key        MarketKey `json:"key"`
	Generation uint64    `json:"generation"`
}

// MarketViewsState is the complete restart state for committed views and gaps.
type MarketViewsState struct {
	Active []ActiveGeneration `json:"active_generations"`
	Views  []MarketViewInput  `json:"views"`
	Gaps   []ViewGap          `json:"gaps"`
}

// State returns a defensive canonically ordered restart snapshot.
func (views *MarketViews) State() MarketViewsState {
	views.mutex.RLock()
	defer views.mutex.RUnlock()
	state := MarketViewsState{}
	for identity, generation := range views.activeGenerations {
		key := views.keys[identity]
		state.Active = append(state.Active, ActiveGeneration{Key: key, Generation: generation})
	}
	for _, history := range views.history {
		for _, view := range history {
			state.Views = append(state.Views, inputForView(view))
		}
	}
	for _, gaps := range views.gaps {
		state.Gaps = append(state.Gaps, gaps...)
	}
	sortMarketViewsState(&state)
	return state
}

// RestoreMarketViews validates and restores one complete committed state.
func RestoreMarketViews(state MarketViewsState) (*MarketViews, error) {
	restored := NewMarketViews()
	sortMarketViewsState(&state)
	if err := restored.restoreActiveGenerations(state.Active); err != nil {
		return nil, err
	}
	for _, input := range state.Views {
		if err := validateMarketView(input); err != nil {
			return nil, runtimeError("market_view_restore_rejected", "view")
		}
		identity := marketKeyString(input.Key)
		active := restored.activeGenerations[identity]
		if active == 0 || input.ConnectionGeneration > active {
			return nil, runtimeError("market_view_restore_rejected", "generation")
		}
		history := restored.history[identity]
		if len(history) > 0 {
			prior := history[len(history)-1]
			if input.BookVersion != prior.Version()+1 ||
				input.ReceiveMonotonicNanos < prior.ReceiveMonotonicNanos() ||
				input.IngestOrdinal <= prior.IngestOrdinal() {
				return nil, runtimeError("market_view_restore_rejected", "order")
			}
		}
		view := MarketView{record: marketViewRecord(input)}
		restored.history[identity] = append(history, view)
		restored.latest[identity] = view
	}
	if err := restored.restoreGaps(state.Gaps); err != nil {
		return nil, err
	}
	return restored, nil
}

func (views *MarketViews) restoreActiveGenerations(activeGenerations []ActiveGeneration) error {
	for _, active := range activeGenerations {
		if !validMarketKey(active.Key) || active.Generation == 0 {
			return runtimeError("market_view_restore_rejected", "generation")
		}
		identity := marketKeyString(active.Key)
		if _, duplicate := views.activeGenerations[identity]; duplicate {
			return runtimeError("market_view_restore_rejected", "duplicate_generation")
		}
		views.activeGenerations[identity], views.keys[identity] = active.Generation, active.Key
	}
	return nil
}

func (views *MarketViews) restoreGaps(gaps []ViewGap) error {
	for _, gap := range gaps {
		identity := marketKeyString(gap.Key)
		if !validMarketKey(gap.Key) || gap.Generation == 0 || gap.Generation > views.activeGenerations[identity] ||
			gap.FirstMonotonicNanos == 0 || gap.LastMonotonicNanos < gap.FirstMonotonicNanos ||
			!validViewLabel(gap.Reason) {
			return runtimeError("market_view_restore_rejected", "gap")
		}
		prior := views.gaps[identity]
		if len(prior) > 0 && gap.FirstMonotonicNanos <= prior[len(prior)-1].LastMonotonicNanos {
			return runtimeError("market_view_restore_rejected", "gap_overlap")
		}
		views.gaps[identity] = append(prior, gap)
	}
	return nil
}

func inputForView(view MarketView) MarketViewInput {
	return MarketViewInput{Key: view.Key(), BookVersion: view.Version(),
		ConnectionGeneration: view.ConnectionGeneration(), ReceiveMonotonicNanos: view.ReceiveMonotonicNanos(),
		ReceiveUTC: view.ReceiveUTC(), IngestOrdinal: view.IngestOrdinal(), ClockOffset: view.ClockOffset(),
		ClockUncertainty: view.ClockUncertainty(), StateHash: view.StateHash(),
		CollectorInstance: view.CollectorInstance(), CollectorRegion: view.CollectorRegion()}
}

func sortMarketViewsState(state *MarketViewsState) {
	sort.Slice(state.Active, func(left, right int) bool {
		return lessMarketKey(state.Active[left].Key, state.Active[right].Key)
	})
	sort.Slice(state.Views, func(left, right int) bool {
		leftKey, rightKey := marketKeyString(state.Views[left].Key), marketKeyString(state.Views[right].Key)
		if leftKey == rightKey {
			return state.Views[left].BookVersion < state.Views[right].BookVersion
		}
		return lessMarketKey(state.Views[left].Key, state.Views[right].Key)
	})
	sort.Slice(state.Gaps, func(left, right int) bool {
		leftKey, rightKey := marketKeyString(state.Gaps[left].Key), marketKeyString(state.Gaps[right].Key)
		if leftKey == rightKey {
			return state.Gaps[left].FirstMonotonicNanos < state.Gaps[right].FirstMonotonicNanos
		}
		return lessMarketKey(state.Gaps[left].Key, state.Gaps[right].Key)
	})
}
