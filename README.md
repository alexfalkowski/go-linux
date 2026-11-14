# go-linux

Linux APIs with idiomatic Go interfaces.

`github.com/alexfalkowski/go-linux` is a Go library scaffold. Linux API
implementations will be added as packages as the library grows.

## 🚀 Setup

Use the Go version declared in [go.mod](go.mod).

Initialize the shared build tooling before using Make targets. Fetching the
submodule requires SSH access to GitHub:

```sh
git submodule sync && git submodule update --init
make dep
make help
```

The shared tooling uses `betteralign` and `golangci-lint` for linting,
`gotestsum` for tests, and `govulncheck` and Trivy for security checks. The CI
image supplies these tools; install them locally to run the corresponding
targets.

See [AGENTS.md](AGENTS.md) for contributor commands, the [implementation plan](IMPLEMENTATION.md) for work order, and [verification guidance](docs/verification.md) for profiles, reports, and runner requirements.
