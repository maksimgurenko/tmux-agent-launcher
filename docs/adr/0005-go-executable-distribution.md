---
status: accepted
---

# Implement in Go and distribute executables

The launcher is implemented in Go and distributed as prebuilt Linux executables, with source-build instructions. Go's standard library covers process execution, filesystem traversal and synchronization; TOML parsing is a declared build dependency.

Compared with interpreted implementations, this removes a language-runtime dependency for release users. Compared with retaining a shell implementation, structured configuration and process arguments are easier to validate. The cost is a Go toolchain and rebuild for source changes. Native ARM64 runtime checks remain separate from cross-compilation.
