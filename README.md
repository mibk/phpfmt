# phpfmt

*The opinionated PHP code formatter powered by Go—think `gofmt`, but for PHP.*

> **TL;DR** Run `phpfmt -w .` and stop arguing about code style. \
> **Status** Beta – already production-ready for many teams,
> but we still expect to receive reports of edge cases.

## Why phpfmt?

 1. **Zero-config, one true style** – phpfmt enforces a single canonical layout
    so you never waste time on bikeshedding.
 2. **Drop-in for CI** – fail the build when files aren’t formatted,
    or auto-fix in pre-commit hooks.

## Installation

    go install mibk.dev/phpfmt@latest
