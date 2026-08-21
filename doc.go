// Package switchonyourcode provides the official server-side Go SDK for Switch On Your Code.
//
// The SDK downloads environment-scoped schema-v1 configuration from Switch On Your Code
// and evaluates feature flags locally inside the application process. It
// supports typed flag values, targeting rules, reusable segments, deterministic
// percentage and multivariate rollouts, ETag-aware refreshes, last-known-good
// configuration retention, optional background polling and configuration-change
// subscriptions.
//
// Construct a Client with NewClient when the application controls its initial
// refresh explicitly, or NewClientAndWait when startup should fail unless an
// initial configuration can be loaded. Client is safe for concurrent use.
package switchonyourcode
