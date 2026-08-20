# Switch On Your Code Go SDK

Official Go SDK for [Switch On Your Code](https://github.com/switchonyourcode/switchonyourcode).

> **Status:** Early development. The API is being validated before the first production release.

The SDK downloads schema-v1 configuration from Switch On Your Code and evaluates flags locally inside your application. Flag evaluation never requires a network request.

## Requirements

- Go 1.25 or later.
- A Switch On Your Code server SDK key (`syoc_server_...`) for the target environment.

The current minimum Go version matches the OpenFeature Go SDK used by the optional provider package.

## Install

```bash
go get github.com/switchonyourcode/sdk-go
```

## Native SDK

Create and initialise a client during application startup:

```go
package main

import (
    "context"
    "log"

    switchonyourcode "github.com/switchonyourcode/sdk-go"
)

func main() {
    ctx := context.Background()
    flags, err := switchonyourcode.NewClientAndWait(ctx, switchonyourcode.ClientOptions{
        BaseURL:   "https://flags.example.com",
        ServerKey: "syoc_server_...",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer flags.Close()

    enabled := flags.Boolean("new-checkout", false, switchonyourcode.EvaluationContext{
        TargetingKey: "user-123",
        Attributes: map[string]any{
            "plan":    "enterprise",
            "country": "GB",
        },
    })

    _ = enabled
}
```

`NewClientAndWait` performs the first configuration refresh before returning. `NewClient` is available when application code wants to control initialisation explicitly with `Refresh`.

### Typed evaluation

The native API provides value and resolution-detail methods for every Switch On Your Code flag type:

```go
enabled := flags.Boolean("new-checkout", false, ctx)
layout := flags.String("checkout-layout", "control", ctx)
limit := flags.Number("max-items", 10, ctx)
config := flags.JSON("checkout-config", map[string]any{}, ctx)

result := flags.BooleanDetails("new-checkout", false, ctx)
log.Printf("value=%v variant=%s reason=%s rule=%s", result.Value, result.Variant, result.Reason, result.RuleID)
```

Evaluation details retain Switch On Your Code's OpenFeature-aligned reasons and error codes, including `TARGETING_MATCH`, `SPLIT`, `DISABLED`, `FLAG_NOT_FOUND` and `TARGETING_KEY_MISSING`.

### Configuration refreshes

Configuration refresh is ETag-aware. An unchanged configuration is answered with `304 Not Modified`, while a failed refresh leaves the last known-good configuration available for local evaluation.

Long-running services can opt into polling:

```go
if err := flags.StartPolling(context.Background()); err != nil {
    log.Fatal(err)
}
defer flags.StopPolling()
```

The default interval is 30 seconds. Override it with `ClientOptions.PollInterval`.

Polling is deliberately opt-in so command-line and short-lived processes are not kept alive unexpectedly.

Applications can also subscribe to successful configuration changes:

```go
unsubscribe := flags.Subscribe(func(configuration switchonyourcode.Configuration) {
    log.Printf("SwitchOnYourCode configuration changed for %s", configuration.Environment.Key)
})
defer unsubscribe()
```

### Custom HTTP clients

Pass `ClientOptions.HTTPClient` to control proxying, transports, TLS, tracing or timeouts. When omitted, Switch On Your Code uses an `http.Client` with a 10-second timeout.

## Targeting and rollout

The Go evaluator implements the same schema-v1 semantics as the Switch On Your Code control plane and the JavaScript/Python SDKs:

- boolean, string, number and JSON flags;
- named variants;
- ordered targeting rules;
- arbitrary nested evaluation context;
- reusable/transitive segments;
- deterministic percentage and multivariate rollout;
- `targetingKey` or custom scalar rollout bucketing;
- RE2 regular expressions through Go's standard `regexp` package;
- Switch On Your Code semantic-version comparisons;
- deterministic SHA-256 bucket assignment across SDK languages.

The compatibility vector:

```text
environment = env-1
flag        = flag-1
context key = user-123
bucket      = 3837
```

is tested by the SDK and matches the Switch On Your Code reference evaluator.

## OpenFeature

The `openfeature` subpackage implements the OpenFeature Go provider API:

```go
package main

import (
    "context"
    "log"

    switchonyourcode "github.com/switchonyourcode/sdk-go"
    switchonyourcodeof "github.com/switchonyourcode/sdk-go/openfeature"
    of "github.com/open-feature/go-sdk/openfeature"
)

func main() {
    provider, err := switchonyourcodeof.NewProvider(switchonyourcodeof.ProviderOptions{
        Client: switchonyourcode.ClientOptions{
            BaseURL:   "https://flags.example.com",
            ServerKey: "syoc_server_...",
        },
        AutoPoll: true,
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := of.SetProviderAndWait(provider); err != nil {
        log.Fatal(err)
    }
    defer of.Shutdown()

    client := of.NewDefaultClient()
    enabled, err := client.BooleanValue(
        context.Background(),
        "new-checkout",
        false,
        of.NewEvaluationContext("user-123", map[string]any{"plan": "enterprise"}),
    )
    if err != nil {
        log.Fatal(err)
    }

    _ = enabled
}
```

The provider maps Switch On Your Code variants, reasons and error codes into OpenFeature resolution details, exposes environment/revision/rule metadata, supports context-aware initialisation/shutdown, and emits `PROVIDER_CONFIGURATION_CHANGED` after post-initialisation configuration updates.

Switch On Your Code `number` values map to both OpenFeature float and integer evaluation. Integer evaluation only succeeds when the resolved JSON number is exactly representable as an `int64`; it never rounds through `float64`.

## Failure behaviour

Switch On Your Code follows caller-fallback semantics for typed evaluation errors. If the client is not ready, a flag is missing, or its type does not match the requested getter, the supplied fallback value is returned with error metadata in the corresponding `...Details` method.

A configuration response is validated completely before it replaces the current snapshot. Unsupported schema versions, invalid variants/rules, malformed regexes or invalid rollout weights therefore cannot overwrite a previously valid configuration.

## Development

```bash
go mod tidy
gofmt -w .
go vet ./...
go test -race ./...
```

CI validates Go 1.25 and 1.26.

## Contributing

Organisation-wide contribution guidelines are maintained in [`switchonyourcode/.github`](https://github.com/switchonyourcode/.github). Switch On Your Code uses a linear Git history and integrates pull requests by rebase only.

## Related repositories

- [Switch On Your Code](https://github.com/switchonyourcode/switchonyourcode)
- [Python SDK](https://github.com/switchonyourcode/sdk-python)
- [JavaScript / TypeScript SDK](https://github.com/switchonyourcode/sdk-js)
- [.NET SDK](https://github.com/switchonyourcode/sdk-dotnet)

## Licence

This SDK is licensed under the **Apache License 2.0**. See [`LICENSE`](LICENSE).
