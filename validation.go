package flagstack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

func ValidateConfiguration(configuration Configuration) error {
	if configuration.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", configuration.SchemaVersion)
	}
	if strings.TrimSpace(configuration.Environment.ID) == "" || strings.TrimSpace(configuration.Environment.Key) == "" {
		return fmt.Errorf("environment ID and key are required")
	}
	seenSegments := make(map[string]struct{}, len(configuration.Segments))
	for _, segment := range configuration.Segments {
		if err := ValidateSegment(segment); err != nil {
			return err
		}
		if _, exists := seenSegments[segment.Key]; exists {
			return fmt.Errorf("duplicate segment key %q", segment.Key)
		}
		seenSegments[segment.Key] = struct{}{}
	}
	seenFlags := make(map[string]struct{}, len(configuration.Flags))
	for _, flag := range configuration.Flags {
		if err := ValidateFlag(flag, configuration.Environment.ID); err != nil {
			return fmt.Errorf("flag %q: %w", flag.Key, err)
		}
		if strings.TrimSpace(flag.Key) == "" {
			return fmt.Errorf("flag key is required")
		}
		if _, exists := seenFlags[flag.Key]; exists {
			return fmt.Errorf("duplicate flag key %q", flag.Key)
		}
		seenFlags[flag.Key] = struct{}{}
	}
	return nil
}

func ValidateFlag(flag Flag, environmentID string) error {
	if strings.TrimSpace(flag.ID) == "" || strings.TrimSpace(environmentID) == "" {
		return fmt.Errorf("flag and environment IDs are required")
	}
	if !validKind(flag.Kind) {
		return fmt.Errorf("unsupported flag kind %q", flag.Kind)
	}
	if err := validateValueKind(flag.Kind, flag.DefaultValue); err != nil {
		return fmt.Errorf("default value: %w", err)
	}
	allowed := map[string]struct{}{"default": {}}
	if flag.Kind == "boolean" {
		allowed["on"] = struct{}{}
		allowed["off"] = struct{}{}
	}
	for _, variant := range flag.Variants {
		key := strings.TrimSpace(variant.Key)
		if key == "" {
			return fmt.Errorf("variant key is required")
		}
		if _, exists := allowed[key]; exists {
			return fmt.Errorf("variant key %q is reserved or duplicated", key)
		}
		if err := validateValueKind(flag.Kind, variant.Value); err != nil {
			return fmt.Errorf("variant %q: %w", key, err)
		}
		allowed[key] = struct{}{}
	}
	seenRules := make(map[string]struct{}, len(flag.Policy.Rules))
	for _, rule := range flag.Policy.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("rule ID is required")
		}
		if _, exists := seenRules[rule.ID]; exists {
			return fmt.Errorf("duplicate rule ID %q", rule.ID)
		}
		seenRules[rule.ID] = struct{}{}
		if err := validateMatchMode(rule.Match); err != nil {
			return fmt.Errorf("rule %q: %w", rule.ID, err)
		}
		if len(rule.Conditions) == 0 {
			return fmt.Errorf("rule %q must contain at least one condition", rule.ID)
		}
		for _, condition := range rule.Conditions {
			if err := validateCondition(condition); err != nil {
				return fmt.Errorf("rule %q: %w", rule.ID, err)
			}
		}
		if err := validateOutcome(rule.Outcome, allowed, true); err != nil {
			return fmt.Errorf("rule %q: %w", rule.ID, err)
		}
	}
	if err := validateOutcome(flag.Policy.Fallthrough, allowed, false); err != nil {
		return fmt.Errorf("fallthrough: %w", err)
	}
	return nil
}

func ValidateSegment(segment Segment) error {
	if strings.TrimSpace(segment.Key) == "" {
		return fmt.Errorf("segment key is required")
	}
	if err := validateMatchMode(segment.Match); err != nil {
		return fmt.Errorf("segment %q: %w", segment.Key, err)
	}
	if len(segment.Conditions) == 0 {
		return fmt.Errorf("segment %q must contain at least one condition", segment.Key)
	}
	for _, condition := range segment.Conditions {
		if err := validateCondition(condition); err != nil {
			return fmt.Errorf("segment %q: %w", segment.Key, err)
		}
	}
	return nil
}

func validateOutcome(outcome Outcome, allowed map[string]struct{}, required bool) error {
	hasVariant := strings.TrimSpace(outcome.Variant) != ""
	hasRollout := len(outcome.Rollout) > 0
	if hasVariant && hasRollout {
		return fmt.Errorf("outcome cannot contain both a variant and a rollout")
	}
	if !hasVariant && !hasRollout {
		if required {
			return fmt.Errorf("outcome must contain a variant or rollout")
		}
		return nil
	}
	if hasVariant {
		if _, exists := allowed[outcome.Variant]; !exists {
			return fmt.Errorf("unknown variant %q", outcome.Variant)
		}
		return nil
	}
	total := 0
	for _, allocation := range outcome.Rollout {
		if _, exists := allowed[allocation.Variant]; !exists {
			return fmt.Errorf("unknown rollout variant %q", allocation.Variant)
		}
		if allocation.Weight <= 0 {
			return fmt.Errorf("rollout weights must be positive")
		}
		total += allocation.Weight
	}
	if total != BucketScale {
		return fmt.Errorf("rollout weights must total %d", BucketScale)
	}
	return nil
}

func validateCondition(condition Condition) error {
	switch condition.Operator {
	case OperatorInSegment, OperatorNotInSegment:
		value, err := decodeRaw(condition.Value)
		if err != nil {
			return fmt.Errorf("segment reference: %w", err)
		}
		if segmentKey, ok := value.(string); !ok || strings.TrimSpace(segmentKey) == "" {
			return fmt.Errorf("segment reference must be a non-empty string")
		}
		return nil
	case OperatorExists, OperatorNotExists:
		if strings.TrimSpace(condition.Attribute) == "" {
			return fmt.Errorf("condition attribute is required")
		}
		return nil
	}
	if strings.TrimSpace(condition.Attribute) == "" {
		return fmt.Errorf("condition attribute is required")
	}
	value, err := decodeRaw(condition.Value)
	if err != nil {
		return fmt.Errorf("condition value: %w", err)
	}
	switch condition.Operator {
	case OperatorEquals, OperatorNotEquals, OperatorContains, OperatorNotContains, OperatorStartsWith, OperatorEndsWith, OperatorGreaterThan, OperatorGreaterThanOrEqual, OperatorLessThan, OperatorLessThanOrEqual:
		return nil
	case OperatorIn, OperatorNotIn:
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%s condition value must be an array", condition.Operator)
		}
		return nil
	case OperatorMatchesRegex:
		pattern, ok := value.(string)
		if !ok {
			return fmt.Errorf("regex condition value must be a string")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regular expression: %w", err)
		}
		return nil
	case OperatorSemverGreaterThan, OperatorSemverGreaterThanOrEqual, OperatorSemverLessThan, OperatorSemverLessThanOrEqual:
		version, ok := value.(string)
		if !ok {
			return fmt.Errorf("semantic-version condition value must be a valid semantic version")
		}
		if _, valid := compareSemver(version, version); !valid {
			return fmt.Errorf("semantic-version condition value must be a valid semantic version")
		}
		return nil
	default:
		return fmt.Errorf("unsupported operator %q", condition.Operator)
	}
}

func validateMatchMode(mode MatchMode) error {
	if mode != MatchAll && mode != MatchAny {
		return fmt.Errorf("match mode must be %q or %q", MatchAll, MatchAny)
	}
	return nil
}

func validateValueKind(kind string, raw json.RawMessage) error {
	value, err := decodeRaw(raw)
	if err != nil {
		return err
	}
	switch kind {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("must be a string")
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("must be a number")
		}
	case "json":
		return nil
	default:
		return fmt.Errorf("unsupported flag kind %q", kind)
	}
	return nil
}

func decodeRaw(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("JSON value is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("multiple JSON values are not allowed")
	}
	return value, nil
}

func validKind(kind string) bool {
	return kind == "boolean" || kind == "string" || kind == "number" || kind == "json"
}
