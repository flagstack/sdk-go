// Package openfeature adapts the Switch On Your Code Go SDK to the OpenFeature provider
// API.
//
// Provider delegates all local evaluation to github.com/switchonyourcode/sdk-go, so
// native and OpenFeature consumers share identical targeting, rollout, variant
// and error semantics. Provider supports OpenFeature lifecycle handling,
// evaluation context mapping, Switch On Your Code metadata and post-initialisation
// configuration-change events.
package openfeature
