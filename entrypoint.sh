#!/bin/sh
set -e

# GitHub Actions passes inputs as INPUT_<NAME> environment variables.

if [ -z "$INPUT_COMMAND" ]; then
  echo "::error::input 'command' is required"
  exit 1
fi

if [ -n "$INPUT_REPLAY" ]; then
  # Replay mode.
  exec ntnbox replay \
    --file "$INPUT_REPLAY" \
    --speed "${INPUT_SPEED:-1}" \
    -- sh -c "$INPUT_COMMAND"
fi

if [ -z "$INPUT_PROFILE" ]; then
  echo "::error::either 'profile' or 'replay' input is required"
  exit 1
fi

# Resolve profile path: if it contains a slash, treat as a path;
# otherwise look up built-in profiles.
profile="$INPUT_PROFILE"
case "$profile" in
  */*) ;;  # path — use as-is
  *)   profile="/profiles/${profile}.yaml" ;;
esac

if [ ! -f "$profile" ]; then
  echo "::error::profile not found: $profile"
  exit 1
fi

# Build ntnbox run command.
args="--profile $profile"
if [ -n "$INPUT_RECORD" ]; then
  args="$args --record $INPUT_RECORD"
fi

exec ntnbox run $args -- sh -c "$INPUT_COMMAND"
