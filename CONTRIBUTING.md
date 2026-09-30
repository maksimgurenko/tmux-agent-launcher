# Development and releases

Source layout and session behavior are documented in [docs/architecture.md](docs/architecture.md). Keep application source and adjacent tests under `internal/launcher/`, the entry point under `cmd/tmux-agent-launcher/`, and maintained tooling under `scripts/`. Project documentation and decision records live in this repository.

Use Go 1.26+, Git, tmux 3.7c+ and GNU installer utilities on Linux. Ordinary tests check configuration, discovery, adapters and disposable installation. Integration tests require compatible tmux; `REQUIRE_TMUX=1` turns a missing/incompatible binary into a failure.

```sh
gofmt -w cmd internal scripts
go vet ./...
REQUIRE_TMUX=1 go test -race ./...
./scripts/build.sh
./scripts/release.sh v0.1.1
```

Tests never start actual coding agents. Exercise real picker/terminal/compositor behavior with a synthetic `sleep` profile in a disposable session, including cancellation, fresh attachments and fzf without an initial TTY. Adapter argv tests cannot establish behavior of an unavailable host application. Native ARM64 checks are separate from cross-compilation.

Before publication, review the full allowed tree and licenses, then scan exact outgoing history/metadata and both archives/binaries for secrets/private identifiers. Generated paths/VCS metadata must stay disabled. Run install/repeat/update/rollback/uninstall checks in disposable destinations. Review data preservation and any untested hosts explicitly.

Set `VERSION`, `PUBLIC_BASE` to an actual intended public source commit, and `EXPECTED_SOURCE_DIGEST` to its digest when building releases. An unpublished candidate can use `unpublished`. `scripts/build.sh --digest` hashes runtime files/module metadata/embedded config including their relative names. Do not update the recorded public base just to hide local edits. Build both target architectures using `scripts/release.sh`, verify hashes, and publish only reviewed branch/tag/assets through the chosen repository. There is no automatic upload script.

Forks change module/repository documentation as desired and choose their release download origin via `--repo`. Tests/builds need no maintainer account, token or private fixture. Keep MIT attribution/dependency notices. Submit focused patches with problem, changed behavior, meaningful checks and host limitations; document new config/adapter contracts alongside code.
