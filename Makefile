include bin/build/make/help.mak
# Keep local rules before the shared fragments: BIN_ROOT uses their include path.
include build/verify.mak
include bin/build/make/go.mak
include bin/build/make/git.mak
include bin/build/make/claude.mak
include bin/build/make/codex.mak
