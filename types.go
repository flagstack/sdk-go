package flagstack

import "encoding/json"

const (
	SchemaVersion = 1
	BucketScale   = 100_000
)

type MatchMode string
type Operator string
type EvaluationReason string
type EvaluationErrorCode string

const (
	MatchAll MatchMode = "all"
	MatchAny MatchMode = "any"

	OperatorEquals                   Operator = "equals"
	OperatorNotEquals                Operator = "not_equals"
	OperatorIn                       Operator = "in"
	OperatorNotIn                    Operator = "not_in"
	OperatorContains                 Operator = "contains"
	OperatorNotContains              Operator = "not_contains"
	OperatorStartsWith               Operator = "starts_with"
	OperatorEndsWith                 Operator = "ends_with"
	OperatorGreaterThan              Operator = "greater_than"
	OperatorGreaterThanOrEqual       Operator = "greater_than_or_equal"
	OperatorLessThan                 Operator = "less_than"
	OperatorLessThanOrEqual          Operator = "less_than_or_equal"
	OperatorExists                   Operator = "exists"
	OperatorNotExists                Operator = "not_exists"
	OperatorMatchesRegex             Operator = "matches_regex"
	OperatorSemverGreaterThan        Operator = "semver_greater_than"
	OperatorSemverGreaterThanOrEqual Operator = "semver_greater_than_or_equal"
	OperatorSemverLessThan           Operator = "semver_less_than"
	OperatorSemverLessThanOrEqual    Operator = "semver_less_than_or_equal"
	OperatorInSegment                Operator = "in_segment"
	OperatorNotInSegment             Operator = "not_in_segment"

	ReasonStatic         EvaluationReason = "STATIC"
	ReasonDefault        EvaluationReason = "DEFAULT"
	ReasonTargetingMatch EvaluationReason = "TARGETING_MATCH"
	ReasonSplit          EvaluationReason = "SPLIT"
	ReasonDisabled       EvaluationReason = "DISABLED"
	ReasonError          EvaluationReason = "ERROR"

	ErrorParse               EvaluationErrorCode = "PARSE_ERROR"
	ErrorTargetingKeyMissing EvaluationErrorCode = "TARGETING_KEY_MISSING"
	ErrorInvalidContext      EvaluationErrorCode = "INVALID_CONTEXT"
	ErrorProviderNotReady    EvaluationErrorCode = "PROVIDER_NOT_READY"
	ErrorFlagNotFound        EvaluationErrorCode = "FLAG_NOT_FOUND"
	ErrorTypeMismatch        EvaluationErrorCode = "TYPE_MISMATCH"
)

type EvaluationContext struct {
	TargetingKey string         `json:"targetingKey,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

type Variant struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type Condition struct {
	Attribute string          `json:"attribute,omitempty"`
	Operator  Operator        `json:"operator"`
	Value     json.RawMessage `json:"value,omitempty"`
}

type Allocation struct {
	Variant string `json:"variant"`
	Weight  int    `json:"weight"`
}

type Outcome struct {
	Variant  string       `json:"variant,omitempty"`
	Rollout  []Allocation `json:"rollout,omitempty"`
	BucketBy string       `json:"bucket_by,omitempty"`
}

type Rule struct {
	ID         string      `json:"id"`
	Name       string      `json:"name,omitempty"`
	Match      MatchMode   `json:"match"`
	Conditions []Condition `json:"conditions"`
	Outcome    Outcome     `json:"outcome"`
}

type Policy struct {
	Rules       []Rule  `json:"rules,omitempty"`
	Fallthrough Outcome `json:"fallthrough,omitempty"`
}

type Segment struct {
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	Match      MatchMode   `json:"match"`
	Conditions []Condition `json:"conditions"`
}

type Environment struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

type Flag struct {
	ID           string          `json:"id"`
	Key          string          `json:"key"`
	Kind         string          `json:"kind"`
	DefaultValue json.RawMessage `json:"default_value"`
	Enabled      bool            `json:"enabled"`
	Variants     []Variant       `json:"variants"`
	Policy       Policy          `json:"policy"`
	Revision     int64           `json:"revision"`
}

type Configuration struct {
	SchemaVersion int         `json:"schema_version"`
	Environment   Environment `json:"environment"`
	Flags         []Flag      `json:"flags"`
	Segments      []Segment   `json:"segments"`
}

type RawEvaluationDetails struct {
	Value        json.RawMessage
	Variant      string
	Reason       EvaluationReason
	RuleID       string
	ErrorCode    EvaluationErrorCode
	ErrorMessage string
}

type EvaluationDetails[T any] struct {
	Value        T
	Variant      string
	Reason       EvaluationReason
	RuleID       string
	ErrorCode    EvaluationErrorCode
	ErrorMessage string
}

type FlagInfo struct {
	ID          string
	Key         string
	Kind        string
	Enabled     bool
	Revision    int64
	Environment Environment
}

type RefreshResult struct {
	Modified      bool
	Configuration Configuration
}
