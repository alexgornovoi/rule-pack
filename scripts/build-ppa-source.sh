#!/usr/bin/env bash
set -euo pipefail

if [[ ! -f "go.mod" || ! -d "debian" ]]; then
  echo "run this script from the repository root" >&2
  exit 1
fi

tag="${1:-}"
dist="${2:-jammy}"
ppa_rev="${3:-1}"

if [[ -z "${tag}" ]]; then
  tag="$(git describe --tags --abbrev=0 2>/dev/null || true)"
fi
if [[ -z "${tag}" ]]; then
  echo "missing tag (example: v0.2.0)" >&2
  exit 1
fi

base_version="${tag#v}"
version="${base_version}-0ppa${ppa_rev}~${dist}"
orig_tarball="../rulepack_${base_version}.orig.tar.gz"

maintainer_name="${DEBFULLNAME:-Alex Gornovoi}"
maintainer_email="${DEBEMAIL:-alex.gornovoi@gmail.com}"
changed_at="$(date -R)"

cat > debian/changelog <<CHANGELOG
rulepack (${version}) ${dist}; urgency=medium

  * Release ${tag} for Ubuntu ${dist}.

 -- ${maintainer_name} <${maintainer_email}>  ${changed_at}
CHANGELOG

if [[ ! -f "${orig_tarball}" ]]; then
  git archive --format=tar.gz --prefix="rulepack-${base_version}/" --output="${orig_tarball}" "${tag}"
fi

dpkg-buildpackage -S -sa -us -uc

echo "Built source package artifacts in parent directory:"
ls -1 ../rulepack_* || true
