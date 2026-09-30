# Development

This is a Linux Go executable. `main.go` owns CLI/selection; `config.go` merges TOML; `discovery.go` walks roots; `picker.go` and `terminal.go` adapt argv; `tmux.go` owns sessions/locking; `runner.go` preserves native argv/environment. Argument arrays never implicitly invoke a shell. Preserve literal names, canonical identity, live attachment after configuration changes, and cancellation.

Run `gofmt`, `go vet ./...` and `REQUIRE_TMUX=1 go test -race ./...` with tmux 3.7c+. Tests build a standalone child and use disposable sockets/directories, never the personal tmux server or paid agents. Validate release archives/installer updates/removal, and report real-host gaps. Use synthetic fixtures; never add credentials, private paths or machine configuration.

`build/` and `dist/` are generated. Runtime source identity covers the explicit file list in `build.sh`; update it when adding runtime files. Public-base/version linker values must represent actual provenance; private VCS stamping stays disabled. Preserve NOTICE and dependency licenses. Review binary/archive strings and public Git metadata before release. Release checks are in CONTRIBUTING.md.

Installation instructions are INSTALL.agent.md. A checkout does not authorize installation, desktop edits or publication. No private helper or maintainer credentials may be required to build/test/fork.
