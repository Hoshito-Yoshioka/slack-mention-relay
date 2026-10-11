#!/usr/bin/env bash
# Downloads a pinned bark-server release, verifies its SHA256 and installs it
# to ~/.slack-relay/bin/bark-server. Nothing downloaded is executed here.
#
# Before bumping VERSION, review the upstream changes (especially outbound
# connections and logging) and update both checksums (P17).
set -euo pipefail

VERSION="v2.3.7" # Finb/bark-server @ 5ea5486, reviewed 2026-10
SHA256_ARM64="fc8020cbdbfb5df45094664329840630abe949d20fa11b95a0dcebbd32f274bb"
SHA256_AMD64="57cf83b1246a48516165412b91988330b0958aebbc026897b5d24152d78611a5"

case "$(uname -m)" in
  arm64) asset="bark-server_darwin_arm64"; want="${SHA256_ARM64}" ;;
  x86_64) asset="bark-server_darwin_amd64"; want="${SHA256_AMD64}" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

dest_dir="${HOME}/.slack-relay/bin"
dest="${dest_dir}/bark-server"
url="https://github.com/Finb/bark-server/releases/download/${VERSION}/${asset}"

umask 077
mkdir -p "${dest_dir}"
chmod 700 "${HOME}/.slack-relay" "${dest_dir}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

echo "downloading ${asset} ${VERSION}"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
  --output "${tmp}/${asset}" "${url}"

got="$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')"
if [[ "${got}" != "${want}" ]]; then
  echo "SHA256 mismatch: aborting (got ${got}, want ${want})" >&2
  exit 1
fi
echo "SHA256 OK"

chmod 700 "${tmp}/${asset}"
mv -f "${tmp}/${asset}" "${dest}"
echo "installed: ~/.slack-relay/bin/bark-server (${VERSION})"
