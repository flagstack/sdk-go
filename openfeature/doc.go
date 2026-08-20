// Package openfeature adapts the FlagStack Go SDK to the OpenFeature provider
// API.
//
// Provider delegates all local evaluation to github.com/flagstack/sdk-go, so
// native and OpenFeature consumers share identical targeting, rollout, variant
// and error semantics. Provider supports OpenFeature lifecycle handling,
// evaluation context mapping, FlagStack metadata and post-initialisation
// configuration-change events.
package openfeature
