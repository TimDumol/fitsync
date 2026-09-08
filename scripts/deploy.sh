#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
cd "$repo_root"

inventory="$repo_root/deploy/ansible/inventory/hosts.yml"
playbook="$repo_root/deploy/ansible/site.yml"
ansible_config="$repo_root/deploy/ansible/ansible.cfg"
vault_file="$repo_root/deploy/ansible/group_vars/aries/secrets.yml"

usage() {
  printf '%s\n' \
    'Usage: ./scripts/deploy.sh [--check] [--diff] [--ask-vault-pass]' \
    '' \
    'Runs the guarded Ansible deployment after the repository test and build checks.'
}

ansible_args=()
ask_vault_pass=0
while (($# > 0)); do
  case "$1" in
    --check|--diff)
      ansible_args+=("$1")
      shift
      ;;
    --ask-vault-pass)
      ask_vault_pass=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

if ((ask_vault_pass)) && [[ -f "$vault_file" ]]; then
  ansible_args+=(--ask-vault-pass)
elif ((ask_vault_pass)); then
  printf '%s\n' "No $vault_file found; skipping the vault-password prompt because missing server secrets are generated remotely." >&2
fi

if [[ ! -f "$inventory" ]]; then
  printf 'missing %s; copy hosts.example.yml and set the deployment host first\n' "$inventory" >&2
  exit 1
fi

if ! command -v ansible-playbook >/dev/null 2>&1; then
  printf 'ansible-playbook is required; see deploy/ansible/README.md\n' >&2
  exit 1
fi

printf '%s\n' 'Running the required repository test suite before deployment...'
./scripts/test.sh
printf '%s\n' 'Running the required repository build before deployment...'
./scripts/build.sh

ANSIBLE_CONFIG="$ansible_config" exec ansible-playbook \
  --inventory "$inventory" \
  "${ansible_args[@]}" \
  "$playbook"
