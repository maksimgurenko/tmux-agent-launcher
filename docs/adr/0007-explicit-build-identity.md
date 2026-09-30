---
status: accepted
---

# Record source and binary identity explicitly

A version or source revision alone cannot establish which code produced a locally rebuilt executable. Builds embed the selected public base, a digest of runtime source/module metadata/embedded configuration, and whether that digest differs from the expected base. Source changes leave the selected public base intact and set the local-change marker.

Paths are part of the digest, so relocating source changes its identity even when behavior is unchanged. `-trimpath` and `-buildvcs=false` prevent incidental checkout paths or enclosing repository metadata from being stamped automatically. Installation owners can separately record the executable's hash to detect replacement. Restoration must use an exact matching artifact or rebuild the selected source.
