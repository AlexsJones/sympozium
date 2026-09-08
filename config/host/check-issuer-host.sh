#!/usr/bin/env bash
# Read-only layout preflight. Never opens credential contents, starts a service,
# changes ownership or claims that file/device presence proves KVM operation.
set -euo pipefail

failed=0
check() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'OK: %s\n' "$description"
  else
    printf 'MISSING/UNVERIFIED: %s\n' "$description"
    failed=1
  fi
}

check 'dedicated celln user exists' getent passwd celln
check 'dedicated celln group exists' getent group celln
check 'host kvm group exists' getent group kvm
check '/dev/kvm is a character device (not a hardware proof)' test -c /dev/kvm

# Called indirectly by check's command argument.
# shellcheck disable=SC2329
root_owned_unwritable() {
  local path="$1" owner mode
  [[ -e "$path" && ! -L "$path" ]] || return 1
  owner=$(stat -c '%u' -- "$path")
  mode=$(stat -c '%a' -- "$path")
  [[ "$owner" == 0 ]] && (( (8#$mode & 8#022) == 0 ))
}

for path in /opt /opt/sympozium-celln /opt/sympozium-celln/bin /etc /etc/sympozium-issuer; do
  check "administrator-owned, non-symlink, non-group/world-writable directory: $path" root_owned_unwritable "$path"
  check "directory exists: $path" test -d "$path"
done
for path in /opt/sympozium-celln/bin/sympozium /opt/sympozium-celln/bin/celln; do
  check "administrator-owned, non-symlink, non-group/world-writable executable: $path" root_owned_unwritable "$path"
  check "regular executable: $path" test -f "$path"
  check "executable access for invoking identity: $path" test -x "$path"
done
for path in /etc/sympozium-issuer/issuer.json /etc/sympozium-issuer/kubeconfig; do
  check "administrator-owned, non-symlink, non-group/world-writable configuration: $path" root_owned_unwritable "$path"
  check "regular configuration file: $path" test -f "$path"
done
check 'durable policy root directory exists: /var/lib/celln' test -d /var/lib/celln
check 'installed issuer unit is administrator-owned and not group/world writable' root_owned_unwritable /etc/systemd/system/sympozium-celln-issuer.service

printf '%s\n' 'NOT TESTED: service-account file access/ACLs, credential confidentiality/expiry, kubeconfig authentication/RBAC, JSON or TLS configuration, binary provenance, KVM ioctl/sealing, filesystem durability, service sandbox/startup, firewall and restart/reboot recovery.'
printf '%s\n' 'This command performs no installation or service changes. A clean layout report is not installation acceptance.'
exit "$failed"
