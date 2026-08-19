# FlagStack Go SDK

Official Go SDK for [FlagStack](https://github.com/flagstack/flagstack).

> **Status:** Planned / early development. Not yet ready for production use.

## Goals

The Go SDK will provide an idiomatic, lightweight FlagStack client for Go applications with:

- local feature-flag evaluation;
- real-time configuration updates;
- resilient cached configuration;
- safe concurrent use;
- minimal runtime overhead;
- context-aware APIs where appropriate;
- OpenFeature integration.

## Module

The intended Go module path is:

```text
github.com/flagstack/sdk-go
```

A final public API will be designed before the first release.

## Design principles

The SDK should keep flag evaluation inside the application process. Applications should not need to make a network request to FlagStack for every feature evaluation.

The FlagStack service will primarily provide initial configuration and subsequent updates, allowing applications to continue evaluating known flags if connectivity is temporarily lost.

## Related repositories

- [FlagStack](https://github.com/flagstack/flagstack)
- [Python SDK](https://github.com/flagstack/sdk-python)
- [JavaScript / TypeScript SDK](https://github.com/flagstack/sdk-js)
- [.NET SDK](https://github.com/flagstack/sdk-dotnet)

## License

A license will be selected before the first public release.
