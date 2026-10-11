#!/usr/bin/env bash
# Installs a git pre-commit hook that runs gitleaks on staged changes (P3).
set -euo pipefail

if ! command -v gitleaks >/dev/null 2>&1; then
  echo "gitleaks is not installed (brew install gitleaks)" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel)"
hooks_dir="$(git rev-parse --git-path hooks)"
hook="${hooks_dir}/pre-commit"
mkdir -p "${hooks_dir}"

cat > "${hook}" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail
exec gitleaks git --pre-commit --staged --redact --no-banner --verbose \
  --config "$(git rev-parse --show-toplevel)/.gitleaks.toml"
HOOK
chmod 755 "${hook}"
echo "installed pre-commit hook: ${hook#"${repo_root}/"}"
