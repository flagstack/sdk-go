package openfeature

import (
	"context"
	"encoding/json"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	of "github.com/open-feature/go-sdk/openfeature"
	switchonyourcode "github.com/switchonyourcode/sdk-go"
)

const providerName = "Switch On Your Code"

type ProviderOptions struct {
	Client   switchonyourcode.ClientOptions
	AutoPoll bool
}

type Provider struct {
	client      *switchonyourcode.Client
	autoPoll    bool
	events      chan of.Event
	initialized atomic.Bool
	initMu      sync.Mutex
	unsubscribe func()
}

var (
	_ of.FeatureProvider          = (*Provider)(nil)
	_ of.StateHandler             = (*Provider)(nil)
	_ of.ContextAwareStateHandler = (*Provider)(nil)
	_ of.EventHandler             = (*Provider)(nil)
)

func NewProvider(options ProviderOptions) (*Provider, error) {
	client, err := switchonyourcode.NewClient(options.Client)
	if err != nil {
		return nil, err
	}
	provider := &Provider{
		client:   client,
		autoPoll: options.AutoPoll,
		events:   make(chan of.Event, 16),
	}
	provider.unsubscribe = client.Subscribe(provider.configurationChanged)
	return provider, nil
}

func (p *Provider) Client() *switchonyourcode.Client { return p.client }

func (p *Provider) Metadata() of.Metadata { return of.Metadata{Name: providerName} }

func (p *Provider) Hooks() []of.Hook { return nil }

func (p *Provider) EventChannel() <-chan of.Event { return p.events }

func (p *Provider) Init(evaluationContext of.EvaluationContext) error {
	return p.InitWithContext(context.Background(), evaluationContext)
}

func (p *Provider) InitWithContext(ctx context.Context, _ of.EvaluationContext) error {
	p.initMu.Lock()
	defer p.initMu.Unlock()
	if p.initialized.Load() {
		return nil
	}
	if _, err := p.client.Refresh(ctx); err != nil {
		return err
	}
	p.initialized.Store(true)
	if p.autoPoll {
		if err := p.client.StartPolling(context.Background()); err != nil {
			p.initialized.Store(false)
			return err
		}
	}
	return nil
}

func (p *Provider) Shutdown() {
	_ = p.ShutdownWithContext(context.Background())
}

func (p *Provider) ShutdownWithContext(_ context.Context) error {
	p.client.Close()
	p.initialized.Store(false)
	if p.unsubscribe != nil {
		p.unsubscribe()
		p.unsubscribe = nil
	}
	return nil
}

func (p *Provider) BooleanEvaluation(_ context.Context, flag string, defaultValue bool, flatCtx of.FlattenedContext) of.BoolResolutionDetail {
	details := p.client.BooleanDetails(flag, defaultValue, evaluationContext(flatCtx))
	return of.BoolResolutionDetail{Value: details.Value, ProviderResolutionDetail: p.resolutionDetail(flag, details.Variant, details.Reason, details.RuleID, details.ErrorCode, details.ErrorMessage)}
}

func (p *Provider) StringEvaluation(_ context.Context, flag string, defaultValue string, flatCtx of.FlattenedContext) of.StringResolutionDetail {
	details := p.client.StringDetails(flag, defaultValue, evaluationContext(flatCtx))
	return of.StringResolutionDetail{Value: details.Value, ProviderResolutionDetail: p.resolutionDetail(flag, details.Variant, details.Reason, details.RuleID, details.ErrorCode, details.ErrorMessage)}
}

func (p *Provider) FloatEvaluation(_ context.Context, flag string, defaultValue float64, flatCtx of.FlattenedContext) of.FloatResolutionDetail {
	details := p.client.NumberDetails(flag, defaultValue, evaluationContext(flatCtx))
	return of.FloatResolutionDetail{Value: details.Value, ProviderResolutionDetail: p.resolutionDetail(flag, details.Variant, details.Reason, details.RuleID, details.ErrorCode, details.ErrorMessage)}
}

func (p *Provider) IntEvaluation(_ context.Context, flag string, defaultValue int64, flatCtx of.FlattenedContext) of.IntResolutionDetail {
	info, exists := p.client.FlagInfo(flag)
	if !exists {
		raw := p.client.RawDetails(flag, evaluationContext(flatCtx))
		return of.IntResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, raw.Variant, raw.Reason, raw.RuleID, raw.ErrorCode, raw.ErrorMessage)}
	}
	if info.Kind != "number" {
		return of.IntResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, "default", switchonyourcode.ReasonError, "", switchonyourcode.ErrorTypeMismatch, "Switch On Your Code flag is not a number")}
	}
	raw := p.client.RawDetails(flag, evaluationContext(flatCtx))
	if raw.ErrorCode != "" {
		return of.IntResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, raw.Variant, raw.Reason, raw.RuleID, raw.ErrorCode, raw.ErrorMessage)}
	}
	rational, ok := new(big.Rat).SetString(string(raw.Value))
	if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
		return of.IntResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, "default", switchonyourcode.ReasonError, raw.RuleID, switchonyourcode.ErrorTypeMismatch, "Switch On Your Code number is not an exact OpenFeature int64 value")}
	}
	return of.IntResolutionDetail{Value: rational.Num().Int64(), ProviderResolutionDetail: p.resolutionDetail(flag, raw.Variant, raw.Reason, raw.RuleID, "", "")}
}

func (p *Provider) ObjectEvaluation(_ context.Context, flag string, defaultValue any, flatCtx of.FlattenedContext) of.InterfaceResolutionDetail {
	details := p.client.JSONDetails(flag, defaultValue, evaluationContext(flatCtx))
	if details.ErrorCode != "" {
		return of.InterfaceResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, details.Variant, details.Reason, details.RuleID, details.ErrorCode, details.ErrorMessage)}
	}
	value, ok := normalizeObject(details.Value)
	if !ok {
		return of.InterfaceResolutionDetail{Value: defaultValue, ProviderResolutionDetail: p.resolutionDetail(flag, "default", switchonyourcode.ReasonError, details.RuleID, switchonyourcode.ErrorTypeMismatch, "Switch On Your Code JSON value is not an OpenFeature object or array")}
	}
	return of.InterfaceResolutionDetail{Value: value, ProviderResolutionDetail: p.resolutionDetail(flag, details.Variant, details.Reason, details.RuleID, "", "")}
}

func (p *Provider) resolutionDetail(flag, variant string, reason switchonyourcode.EvaluationReason, ruleID string, errorCode switchonyourcode.EvaluationErrorCode, errorMessage string) of.ProviderResolutionDetail {
	metadata := of.FlagMetadata{}
	if info, ok := p.client.FlagInfo(flag); ok {
		metadata["switchonyourcode.environment"] = info.Environment.Key
		metadata["switchonyourcode.environment_id"] = info.Environment.ID
		metadata["switchonyourcode.flag_id"] = info.ID
		metadata["switchonyourcode.revision"] = info.Revision
		metadata["switchonyourcode.enabled"] = info.Enabled
	}
	if ruleID != "" {
		metadata["switchonyourcode.rule_id"] = ruleID
	}
	return of.ProviderResolutionDetail{
		Reason:          of.Reason(reason),
		Variant:         variant,
		FlagMetadata:    metadata,
		ResolutionError: resolutionError(errorCode, errorMessage),
	}
}

func resolutionError(code switchonyourcode.EvaluationErrorCode, message string) of.ResolutionError {
	switch code {
	case "":
		return of.ResolutionError{}
	case switchonyourcode.ErrorProviderNotReady:
		return of.NewProviderNotReadyResolutionError(message)
	case switchonyourcode.ErrorFlagNotFound:
		return of.NewFlagNotFoundResolutionError(message)
	case switchonyourcode.ErrorParse:
		return of.NewParseErrorResolutionError(message)
	case switchonyourcode.ErrorTypeMismatch:
		return of.NewTypeMismatchResolutionError(message)
	case switchonyourcode.ErrorTargetingKeyMissing:
		return of.NewTargetingKeyMissingResolutionError(message)
	case switchonyourcode.ErrorInvalidContext:
		return of.NewInvalidContextResolutionError(message)
	default:
		return of.NewGeneralResolutionError(message)
	}
}

func evaluationContext(flatCtx of.FlattenedContext) switchonyourcode.EvaluationContext {
	attributes := make(map[string]any, len(flatCtx))
	targetingKey := ""
	for key, value := range flatCtx {
		if key == of.TargetingKey {
			if stringValue, ok := value.(string); ok {
				targetingKey = stringValue
			}
			continue
		}
		attributes[key] = normalizeContextValue(value)
	}
	return switchonyourcode.EvaluationContext{TargetingKey: targetingKey, Attributes: attributes}
}

func normalizeContextValue(value any) any {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if typed == nil {
			return nil
		}
		return typed.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = normalizeContextValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = normalizeContextValue(nested)
		}
		return result
	default:
		return value
	}
}

func normalizeObject(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = normalizeJSONValue(nested)
		}
		return result, true
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = normalizeJSONValue(nested)
		}
		return result, true
	default:
		return nil, false
	}
}

func normalizeJSONValue(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if number, err := typed.Float64(); err == nil {
			return number
		}
		return typed.String()
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = normalizeJSONValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = normalizeJSONValue(nested)
		}
		return result
	default:
		return value
	}
}

func (p *Provider) configurationChanged(configuration switchonyourcode.Configuration) {
	if !p.initialized.Load() {
		return
	}
	flags := make([]string, 0, len(configuration.Flags))
	for _, flag := range configuration.Flags {
		flags = append(flags, flag.Key)
	}
	event := of.Event{
		ProviderName: providerName,
		EventType:    of.ProviderConfigChange,
		ProviderEventDetails: of.ProviderEventDetails{
			FlagChanges: flags,
			EventMetadata: map[string]any{
				"environment":    configuration.Environment.Key,
				"environment_id": configuration.Environment.ID,
			},
		},
	}
	select {
	case p.events <- event:
	default:
	}
}
