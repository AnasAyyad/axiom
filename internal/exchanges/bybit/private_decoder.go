package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// ErrDemoPrivateEvent is the generic fail-closed Demo private-stream error.
var ErrDemoPrivateEvent = errors.New("bybit_demo_private_event_rejected")

type demoPrivateDecoder struct {
	account sandbox.AccountID
	epoch   uint64
	lookup  sandbox.SubmissionLookup
	now     func() time.Time
}

type decodedDemoPrivateEvent struct {
	event         sandbox.PrivateEvent
	submission    sandbox.Submission
	needsBackfill bool
}

type canonicalDemoCoin struct {
	Coin          string `json:"coin"`
	WalletBalance string `json:"wallet_balance"`
	Locked        string `json:"locked"`
}

type demoPrivateEnvelope struct {
	ID           string          `json:"id"`
	Topic        string          `json:"topic"`
	CreationTime int64           `json:"creationTime"`
	Data         json.RawMessage `json:"data"`
}

func newDemoPrivateDecoder(
	account sandbox.AccountID,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	now func() time.Time,
) (*demoPrivateDecoder, error) {
	if account == "" || epoch == 0 || lookup == nil || now == nil {
		return nil, ErrDemoPrivateEvent
	}
	return &demoPrivateDecoder{
		account: account,
		epoch:   epoch,
		lookup:  lookup,
		now:     now,
	}, nil
}

func (decoder *demoPrivateDecoder) decode(
	ctx context.Context,
	body []byte,
) (decodedDemoPrivateEvent, error) {
	var envelope demoPrivateEnvelope
	if strictDecode(body, &envelope) != nil ||
		envelope.ID == "" || envelope.CreationTime <= 0 ||
		len(envelope.Data) == 0 {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	switch envelope.Topic {
	case "order.spot":
		return decoder.decodeOrders(ctx, envelope, body)
	case "execution.spot":
		return decoder.decodeExecutions(ctx, envelope)
	case "wallet":
		event, err := decoder.decodeWallet(envelope, body)
		return decodedDemoPrivateEvent{event: event}, err
	default:
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
}

func (decoder *demoPrivateDecoder) decodeOrders(
	ctx context.Context,
	envelope demoPrivateEnvelope,
	body []byte,
) (decodedDemoPrivateEvent, error) {
	var orders []demoOrderPayload
	if strictDecode(envelope.Data, &orders) != nil || len(orders) != 1 {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	order := orders[0]
	submission, found, err := decoder.lookup.SubmissionByClientOrderID(
		ctx,
		decoder.account,
		decoder.epoch,
		order.OrderLinkID,
	)
	if err != nil || !found || !demoOrderMatches(order, submission) {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	cumulative, err := domain.ParseQuantity(order.CumulativeQuantity)
	if err != nil {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	zero, _ := domain.ParseQuantity("0")
	if cumulative.Compare(zero) > 0 {
		return decodedDemoPrivateEvent{
			submission:    submission,
			needsBackfill: true,
		}, nil
	}
	event, err := normalizeDemoOrder(
		order,
		nil,
		submission,
		decoder.now().UTC(),
		body,
	)
	if err != nil {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	return decodedDemoPrivateEvent{
		event:      event,
		submission: submission,
	}, nil
}

func (decoder *demoPrivateDecoder) decodeExecutions(
	ctx context.Context,
	envelope demoPrivateEnvelope,
) (decodedDemoPrivateEvent, error) {
	var executions []demoExecutionPayload
	if strictDecode(envelope.Data, &executions) != nil ||
		len(executions) == 0 {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	clientOrderID := executions[0].OrderLinkID
	submission, found, err := decoder.lookup.SubmissionByClientOrderID(
		ctx,
		decoder.account,
		decoder.epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
	}
	for _, execution := range executions {
		if execution.Category != "spot" ||
			execution.OrderLinkID != clientOrderID ||
			execution.Symbol != submission.Instrument.Symbol() ||
			execution.IsLeverage != "0" {
			return decodedDemoPrivateEvent{}, ErrDemoPrivateEvent
		}
	}
	return decodedDemoPrivateEvent{
		submission:    submission,
		needsBackfill: true,
	}, nil
}

func (decoder *demoPrivateDecoder) decodeWallet(
	envelope demoPrivateEnvelope,
	body []byte,
) (sandbox.PrivateEvent, error) {
	var accounts []walletAccountPayload
	if strictDecode(envelope.Data, &accounts) != nil ||
		len(accounts) != 1 ||
		accounts[0].AccountType != "UNIFIED" ||
		len(accounts[0].Coin) == 0 {
		return sandbox.PrivateEvent{}, ErrDemoPrivateEvent
	}
	canonical, err := canonicalDemoCoins(accounts[0].Coin)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].Coin < canonical[right].Coin
	})
	sourceHash := payloadHash(body)
	event := sandbox.PrivateEvent{
		Identity: fmt.Sprintf(
			"bybit-wallet-%s-%s", envelope.ID, sourceHash[:12],
		),
		AccountID: decoder.account, AccountEpoch: decoder.epoch,
		Kind: sandbox.PrivateBalanceEvent, NativeOrderHash: sourceHash,
		BalanceHash: canonicalDemoHash(canonical),
		OccurredAt:  time.UnixMilli(envelope.CreationTime).UTC(),
		ReceivedAt:  decoder.now().UTC(),
	}
	if event.Validate() != nil {
		return sandbox.PrivateEvent{}, ErrDemoPrivateEvent
	}
	return event, nil
}

func canonicalDemoCoins(
	coins []walletCoinPayload,
) ([]canonicalDemoCoin, error) {
	canonical := make([]canonicalDemoCoin, 0, len(coins))
	seen := make(map[domain.AssetSymbol]struct{}, len(coins))
	for _, coin := range coins {
		asset, assetErr := domain.ParseAssetSymbol(coin.Coin)
		_, walletErr := domain.ParseBalance(coin.WalletBalance)
		_, lockedErr := domain.ParseBalance(coin.Locked)
		if assetErr != nil || walletErr != nil || lockedErr != nil ||
			!zeroOrEmpty(coin.SpotBorrow) ||
			!zeroOrEmpty(coin.BorrowAmount) ||
			!zeroOrEmpty(coin.AccruedInterest) ||
			!zeroOrEmpty(coin.TotalPositionIM) ||
			!zeroOrEmpty(coin.TotalPositionMM) ||
			!zeroOrEmpty(coin.SpotHedgingQuantity) {
			return nil, ErrDemoPrivateEvent
		}
		if _, duplicate := seen[asset]; duplicate {
			return nil, ErrDemoPrivateEvent
		}
		seen[asset] = struct{}{}
		canonical = append(canonical, canonicalDemoCoin{
			Coin:          coin.Coin,
			WalletBalance: coin.WalletBalance,
			Locked:        coin.Locked,
		})
	}
	return canonical, nil
}
