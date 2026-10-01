#!/bin/sh
# Publish the package in the working directory, unless it is already there.
#
# Two checks that publish byte-identical packages share one registry, because the
# registry is keyed on its content — so "already published" is the normal case,
# not an error. And the volume outlives a run, so it is normal across runs too.
#
# Decided by asking the registry rather than by matching an error string: npm, the
# registry and the proxy all word a 409 differently, and a check that greps for
# one of them passes for the wrong reason when the wording moves.
set -e

name=$(node -p "require('./package.json').name")
version=$(node -p "require('./package.json').version")

published() {
  npm view "$name@$version" version >/dev/null 2>&1
}

if published; then
  echo "already published: $name@$version"
  exit 0
fi

if npm publish >/tmp/publish.log 2>&1; then
  echo "published: $name@$version"
  exit 0
fi

# Either another check published the same bytes between the check above and here,
# or this genuinely failed. The registry knows which.
if published; then
  echo "published concurrently: $name@$version"
  exit 0
fi

cat /tmp/publish.log
exit 1
