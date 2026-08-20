package switchonyourcode

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

func Evaluate(flag Flag, environmentID string, ctx EvaluationContext, segments []Segment) RawEvaluationDetails {
	if err := ValidateFlag(flag, environmentID); err != nil {
		return errorResult(flag, ErrorParse, err)
	}
	segmentIndex := make(map[string]Segment, len(segments))
	for _, segment := range segments {
		if err := ValidateSegment(segment); err != nil {
			return errorResult(flag, ErrorParse, err)
		}
		segmentIndex[segment.Key] = segment
	}
	if !flag.Enabled {
		return RawEvaluationDetails{Value: cloneRaw(flag.DefaultValue), Variant: "default", Reason: ReasonDisabled}
	}
	for _, rule := range flag.Policy.Rules {
		matched, err := matchConditions(rule.Match, rule.Conditions, ctx, segmentIndex, map[string]bool{})
		if err != nil {
			return errorResultFromEvaluation(flag, err)
		}
		if !matched {
			continue
		}
		result, err := resolveOutcome(flag, environmentID, rule.Outcome, ctx)
		if err != nil {
			return errorResultFromEvaluation(flag, err)
		}
		result.RuleID = rule.ID
		if len(rule.Outcome.Rollout) > 0 {
			result.Reason = ReasonSplit
		} else {
			result.Reason = ReasonTargetingMatch
		}
		return result
	}
	if outcomeEmpty(flag.Policy.Fallthrough) {
		if flag.Kind == "boolean" {
			return RawEvaluationDetails{Value: json.RawMessage("true"), Variant: "on", Reason: ReasonStatic}
		}
		return RawEvaluationDetails{Value: cloneRaw(flag.DefaultValue), Variant: "default", Reason: ReasonDefault}
	}
	result, err := resolveOutcome(flag, environmentID, flag.Policy.Fallthrough, ctx)
	if err != nil {
		return errorResultFromEvaluation(flag, err)
	}
	if len(flag.Policy.Fallthrough.Rollout) > 0 {
		result.Reason = ReasonSplit
	} else {
		result.Reason = ReasonStatic
	}
	return result
}

func resolveOutcome(flag Flag, environmentID string, outcome Outcome, ctx EvaluationContext) (RawEvaluationDetails, error) {
	if outcome.Variant != "" {
		value, err := variantValue(flag, outcome.Variant)
		if err != nil {
			return RawEvaluationDetails{}, err
		}
		return RawEvaluationDetails{Value: value, Variant: outcome.Variant}, nil
	}
	bucketValue, err := bucketValue(ctx, outcome.BucketBy)
	if err != nil {
		return RawEvaluationDetails{}, err
	}
	selected := Bucket(environmentID, flag.ID, bucketValue)
	cumulative := 0
	for _, allocation := range outcome.Rollout {
		cumulative += allocation.Weight
		if selected < cumulative {
			value, err := variantValue(flag, allocation.Variant)
			if err != nil {
				return RawEvaluationDetails{}, err
			}
			return RawEvaluationDetails{Value: value, Variant: allocation.Variant}, nil
		}
	}
	return RawEvaluationDetails{}, evaluationError(ErrorParse, "rollout did not resolve a variant")
}

func variantValue(flag Flag, key string) (json.RawMessage, error) {
	switch key {
	case "default":
		return cloneRaw(flag.DefaultValue), nil
	case "on":
		if flag.Kind == "boolean" {
			return json.RawMessage("true"), nil
		}
	case "off":
		if flag.Kind == "boolean" {
			return json.RawMessage("false"), nil
		}
	}
	for _, variant := range flag.Variants {
		if variant.Key == key {
			return cloneRaw(variant.Value), nil
		}
	}
	return nil, evaluationError(ErrorParse, "unknown variant %q", key)
}

func bucketValue(ctx EvaluationContext, bucketBy string) (string, error) {
	if bucketBy == "" || bucketBy == "targetingKey" {
		if ctx.TargetingKey == "" {
			return "", evaluationError(ErrorTargetingKeyMissing, "targeting key is required for percentage rollout")
		}
		return ctx.TargetingKey, nil
	}
	value, exists := contextValue(ctx, bucketBy)
	if !exists {
		return "", evaluationError(ErrorInvalidContext, "bucket attribute %q is missing", bucketBy)
	}
	encoded, err := scalarBucketValue(value)
	if err != nil {
		return "", evaluationError(ErrorInvalidContext, "bucket attribute %q: %v", bucketBy, err)
	}
	return encoded, nil
}

func scalarBucketValue(value any) (string, error) {
	switch value.(type) {
	case string, bool, json.Number, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	default:
		return "", fmt.Errorf("must be a scalar string, boolean or number")
	}
}

func matchConditions(mode MatchMode, conditions []Condition, ctx EvaluationContext, segments map[string]Segment, visiting map[string]bool) (bool, error) {
	if mode == MatchAny {
		for _, condition := range conditions {
			matched, err := conditionMatches(condition, ctx, segments, visiting)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	}
	for _, condition := range conditions {
		matched, err := conditionMatches(condition, ctx, segments, visiting)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func conditionMatches(condition Condition, ctx EvaluationContext, segments map[string]Segment, visiting map[string]bool) (bool, error) {
	if condition.Operator == OperatorInSegment || condition.Operator == OperatorNotInSegment {
		value, err := decodeRaw(condition.Value)
		if err != nil {
			return false, evaluationError(ErrorParse, "%v", err)
		}
		segmentKey, ok := value.(string)
		if !ok {
			return false, evaluationError(ErrorParse, "segment condition must reference a string key")
		}
		matched, err := matchSegment(segmentKey, ctx, segments, visiting)
		if err != nil {
			return false, err
		}
		if condition.Operator == OperatorNotInSegment {
			return !matched, nil
		}
		return matched, nil
	}
	actual, exists := contextValue(ctx, condition.Attribute)
	if condition.Operator == OperatorExists {
		return exists, nil
	}
	if condition.Operator == OperatorNotExists {
		return !exists, nil
	}
	if !exists {
		return false, nil
	}
	expected, err := decodeRaw(condition.Value)
	if err != nil {
		return false, evaluationError(ErrorParse, "%v", err)
	}
	switch condition.Operator {
	case OperatorEquals:
		return equalValues(actual, expected), nil
	case OperatorNotEquals:
		return !equalValues(actual, expected), nil
	case OperatorIn, OperatorNotIn:
		values, ok := expected.([]any)
		if !ok {
			return false, evaluationError(ErrorParse, "%s expects an array", condition.Operator)
		}
		matched := false
		for _, candidate := range values {
			if equalValues(actual, candidate) {
				matched = true
				break
			}
		}
		if condition.Operator == OperatorNotIn {
			return !matched, nil
		}
		return matched, nil
	case OperatorContains, OperatorNotContains:
		matched := containsValue(actual, expected)
		if condition.Operator == OperatorNotContains {
			return !matched, nil
		}
		return matched, nil
	case OperatorStartsWith, OperatorEndsWith:
		actualString, actualOK := actual.(string)
		expectedString, expectedOK := expected.(string)
		if !actualOK || !expectedOK {
			return false, nil
		}
		if condition.Operator == OperatorStartsWith {
			return strings.HasPrefix(actualString, expectedString), nil
		}
		return strings.HasSuffix(actualString, expectedString), nil
	case OperatorGreaterThan, OperatorGreaterThanOrEqual, OperatorLessThan, OperatorLessThanOrEqual:
		actualNumber, actualOK := numberValue(actual)
		expectedNumber, expectedOK := numberValue(expected)
		if !actualOK || !expectedOK {
			return false, nil
		}
		switch condition.Operator {
		case OperatorGreaterThan:
			return actualNumber > expectedNumber, nil
		case OperatorGreaterThanOrEqual:
			return actualNumber >= expectedNumber, nil
		case OperatorLessThan:
			return actualNumber < expectedNumber, nil
		default:
			return actualNumber <= expectedNumber, nil
		}
	case OperatorMatchesRegex:
		actualString, actualOK := actual.(string)
		pattern, expectedOK := expected.(string)
		if !actualOK || !expectedOK {
			return false, nil
		}
		matched, err := regexp.MatchString(pattern, actualString)
		if err != nil {
			return false, evaluationError(ErrorParse, "%v", err)
		}
		return matched, nil
	case OperatorSemverGreaterThan, OperatorSemverGreaterThanOrEqual, OperatorSemverLessThan, OperatorSemverLessThanOrEqual:
		actualString, actualOK := actual.(string)
		expectedString, expectedOK := expected.(string)
		if !actualOK || !expectedOK {
			return false, nil
		}
		comparison, ok := compareSemver(actualString, expectedString)
		if !ok {
			return false, nil
		}
		switch condition.Operator {
		case OperatorSemverGreaterThan:
			return comparison > 0, nil
		case OperatorSemverGreaterThanOrEqual:
			return comparison >= 0, nil
		case OperatorSemverLessThan:
			return comparison < 0, nil
		default:
			return comparison <= 0, nil
		}
	default:
		return false, evaluationError(ErrorParse, "unsupported operator %q", condition.Operator)
	}
}

func matchSegment(key string, ctx EvaluationContext, segments map[string]Segment, visiting map[string]bool) (bool, error) {
	segment, exists := segments[key]
	if !exists {
		return false, nil
	}
	if visiting[key] {
		return false, evaluationError(ErrorParse, "segment cycle detected at %q", key)
	}
	visiting[key] = true
	defer delete(visiting, key)
	return matchConditions(segment.Match, segment.Conditions, ctx, segments, visiting)
}

func contextValue(ctx EvaluationContext, path string) (any, bool) {
	if path == "targetingKey" {
		if ctx.TargetingKey == "" {
			return nil, false
		}
		return ctx.TargetingKey, true
	}
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = ctx.Attributes
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func containsValue(actual, expected any) bool {
	switch value := actual.(type) {
	case string:
		expectedString, ok := expected.(string)
		return ok && strings.Contains(value, expectedString)
	case []any:
		for _, candidate := range value {
			if equalValues(candidate, expected) {
				return true
			}
		}
	case []string:
		expectedString, ok := expected.(string)
		if !ok {
			return false
		}
		for _, candidate := range value {
			if candidate == expectedString {
				return true
			}
		}
	case map[string]any:
		expectedString, ok := expected.(string)
		if !ok {
			return false
		}
		_, exists := value[expectedString]
		return exists
	}
	return false
}

func equalValues(left, right any) bool {
	leftNumber, leftOK := numberValue(left)
	rightNumber, rightOK := numberValue(right)
	if leftOK && rightOK {
		return leftNumber == rightNumber
	}
	return reflect.DeepEqual(left, right)
}

func numberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func outcomeEmpty(outcome Outcome) bool {
	return strings.TrimSpace(outcome.Variant) == "" && len(outcome.Rollout) == 0
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func errorResult(flag Flag, code EvaluationErrorCode, err error) RawEvaluationDetails {
	return RawEvaluationDetails{Value: cloneRaw(flag.DefaultValue), Variant: "default", Reason: ReasonError, ErrorCode: code, ErrorMessage: err.Error()}
}

func errorResultFromEvaluation(flag Flag, err error) RawEvaluationDetails {
	if evaluationErr, ok := err.(*evaluationFailure); ok {
		return errorResult(flag, evaluationErr.code, evaluationErr.err)
	}
	return errorResult(flag, ErrorParse, err)
}
