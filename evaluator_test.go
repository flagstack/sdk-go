package switchonyourcode

import (
	"encoding/json"
	"testing"
)

func raw(value string) json.RawMessage { return json.RawMessage(value) }

func booleanFlag(overrides func(*Flag)) Flag {
	flag := Flag{
		ID:           "flag-1",
		Key:          "new-checkout",
		Kind:         "boolean",
		DefaultValue: raw("false"),
		Enabled:      true,
		Variants:     []Variant{},
		Policy:       Policy{},
		Revision:     1,
	}
	if overrides != nil {
		overrides(&flag)
	}
	return flag
}

func TestBucketCompatibilityVector(t *testing.T) {
	if got := Bucket("env-1", "flag-1", "user-123"); got != 3837 {
		t.Fatalf("bucket = %d, want 3837", got)
	}
}

func TestDisabledFlagReturnsDefault(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) { flag.Enabled = false })
	result := Evaluate(flag, "env-1", EvaluationContext{}, nil)
	if string(result.Value) != "false" || result.Variant != "default" || result.Reason != ReasonDisabled {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestEnabledBooleanWithoutPolicyReturnsOn(t *testing.T) {
	result := Evaluate(booleanFlag(nil), "env-1", EvaluationContext{}, nil)
	if string(result.Value) != "true" || result.Variant != "on" || result.Reason != ReasonStatic {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOrderedRulesAndTransitiveSegments(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{
			Rules: []Rule{{
				ID:         "staff-rule",
				Match:      MatchAll,
				Conditions: []Condition{{Operator: OperatorInSegment, Value: raw(`"staff"`)}},
				Outcome:    Outcome{Variant: "on"},
			}},
			Fallthrough: Outcome{Variant: "off"},
		}
	})
	segments := []Segment{
		{Key: "staff", Name: "Staff", Match: MatchAll, Conditions: []Condition{{Operator: OperatorInSegment, Value: raw(`"internal"`)}}},
		{Key: "internal", Name: "Internal", Match: MatchAll, Conditions: []Condition{{Attribute: "profile.email", Operator: OperatorEndsWith, Value: raw(`"@example.com"`)}}},
	}
	result := Evaluate(flag, "env-1", EvaluationContext{TargetingKey: "user-1", Attributes: map[string]any{"profile": map[string]any{"email": "adam@example.com"}}}, segments)
	if string(result.Value) != "true" || result.Reason != ReasonTargetingMatch || result.RuleID != "staff-rule" {
		t.Fatalf("unexpected matching result: %+v", result)
	}
	result = Evaluate(flag, "env-1", EvaluationContext{TargetingKey: "user-2", Attributes: map[string]any{"profile": map[string]any{"email": "user@elsewhere.test"}}}, segments)
	if string(result.Value) != "false" || result.Reason != ReasonStatic {
		t.Fatalf("unexpected fallthrough result: %+v", result)
	}
}

func TestPercentageRolloutIsStable(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{Fallthrough: Outcome{Rollout: []Allocation{{Variant: "on", Weight: 25_000}, {Variant: "off", Weight: 75_000}}}}
	})
	result := Evaluate(flag, "env-1", EvaluationContext{TargetingKey: "user-123"}, nil)
	if string(result.Value) != "true" || result.Reason != ReasonSplit {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRolloutWithoutTargetingKeyFailsSafely(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{Fallthrough: Outcome{Rollout: []Allocation{{Variant: "on", Weight: 50_000}, {Variant: "off", Weight: 50_000}}}}
	})
	result := Evaluate(flag, "env-1", EvaluationContext{}, nil)
	if result.Reason != ReasonError || result.ErrorCode != ErrorTargetingKeyMissing || string(result.Value) != "false" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRegexUsesGoRE2Syntax(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{
			Rules:       []Rule{{ID: "staff-email", Match: MatchAll, Conditions: []Condition{{Attribute: "email", Operator: OperatorMatchesRegex, Value: raw(`"(?i)@example\\.com$"`)}}, Outcome: Outcome{Variant: "on"}}},
			Fallthrough: Outcome{Variant: "off"},
		}
	})
	result := Evaluate(flag, "env-1", EvaluationContext{Attributes: map[string]any{"email": "Adam@EXAMPLE.COM"}}, nil)
	if string(result.Value) != "true" {
		t.Fatalf("regex did not match: %+v", result)
	}
}

func TestSemverAllowsShorthand(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{
			Rules:       []Rule{{ID: "modern", Match: MatchAll, Conditions: []Condition{{Attribute: "app_version", Operator: OperatorSemverGreaterThanOrEqual, Value: raw(`"2.4"`)}}, Outcome: Outcome{Variant: "on"}}},
			Fallthrough: Outcome{Variant: "off"},
		}
	})
	if result := Evaluate(flag, "env-1", EvaluationContext{Attributes: map[string]any{"app_version": "v2.4.1"}}, nil); string(result.Value) != "true" {
		t.Fatalf("expected v2.4.1 to match: %+v", result)
	}
	if result := Evaluate(flag, "env-1", EvaluationContext{Attributes: map[string]any{"app_version": "2.3.9"}}, nil); string(result.Value) != "false" {
		t.Fatalf("expected 2.3.9 to fall through: %+v", result)
	}
}

func TestSegmentCycleFailsSafely(t *testing.T) {
	flag := booleanFlag(func(flag *Flag) {
		flag.Policy = Policy{Rules: []Rule{{ID: "cycle", Match: MatchAll, Conditions: []Condition{{Operator: OperatorInSegment, Value: raw(`"a"`)}}, Outcome: Outcome{Variant: "on"}}}}
	})
	segments := []Segment{
		{Key: "a", Name: "A", Match: MatchAll, Conditions: []Condition{{Operator: OperatorInSegment, Value: raw(`"b"`)}}},
		{Key: "b", Name: "B", Match: MatchAll, Conditions: []Condition{{Operator: OperatorInSegment, Value: raw(`"a"`)}}},
	}
	result := Evaluate(flag, "env-1", EvaluationContext{}, segments)
	if result.Reason != ReasonError || result.ErrorCode != ErrorParse {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCustomBucketScalarUsesGoJSONEncoding(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"float integer", 1.0, "1"},
		{"small exponent", 1e-7, "1e-7"},
		{"large decimal", 1e20, "100000000000000000000"},
		{"large exponent", 1e21, "1e+21"},
		{"negative zero", -0.0, "0"},
		{"escaped string", "a\nb", `"a\nb"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scalarBucketValue(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("encoded = %q, want %q", got, tc.want)
			}
		})
	}
}
