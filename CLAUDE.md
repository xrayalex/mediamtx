# CLAUDE.md — mediamtx fork (vms/playback-fixes)

## Context
This is a fork of bluenviron/mediamtx with patches for arbitrary-timestamp
playback seeking. Parent project: ../ip-cam (sentinel VMS).

## Rules
- This is UPSTREAM's code style, not ours. Copy patterns from neighboring files.
- Zero new dependencies. Everything needed is in go.mod already.
- Minimal diffs. Each patch isolated to one commit with clear issue reference.
- Keep PRs upstream-friendly — they might accept some.
- No references to ip-cam or our internal conventions in commit messages or code.

## Branch strategy
- Base branch from latest release tag, not main (stability).
- One branch per patch cluster: vms/playback-fixes (A + C together).
- Tag our builds: vms-v0.1, vms-v0.2 etc.

## Do not
- Reformat whole files.
- Add our chi/uuid/pgx etc.
- Mention "ip-cam" or "sentinel" in code.
- Bump go.mod dependencies.