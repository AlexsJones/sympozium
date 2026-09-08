# Installing the host issuer under systemd

The checked-in [system service](../../config/host/sympozium-celln-issuer.service)
supervises the actual `serve-issuer` command. It does not install Celln artifacts,
grant authority, configure the dispatcher/router, or make their network paths
secure. Service syntax and packaging checks pass; **startup, KVM operation and
restart under these exact sandbox settings still require host qualification**.

## Required host layout

| Path | Owner and purpose |
| --- | --- |
| `/opt/sympozium-celln/bin/sympozium` | Administrator-owned reviewed executable; not writable by the service. |
| `/opt/sympozium-celln/bin/celln` | Administrator-owned pinned compatible Celln executable, named by issuer JSON. |
| `/etc/sympozium-issuer/` | Administrator-owned configuration and private credentials, readable by the service account. |
| `/var/lib/celln/` | Existing durable Celln policy/artifact state owned by the chosen trusted host-service identity. Never temporary storage. |
| `/dev/kvm` | Actual host KVM device; accessible to the service via the `kvm` group. |

The unit expects an existing dedicated `celln` user/group and host `kvm` group.
Check these identities against the already provisioned Celln store. If the store
uses another identity, review a consistent deployment-specific unit/configuration
instead of recursively changing ownership on live state. Do not use DynamicUser:
issuer journals and dispatcher policy must retain stable ownership across restart.

Use the strict JSON schema in [issuer service](celln-issuer-service.md), setting
`cellnBinary` to `/opt/sympozium-celln/bin/celln` and `policyRoot` to
`/var/lib/celln`. All referenced certificate/key/token paths must lie under the
configured readable host locations. Do not put dependencies in a home directory:
`ProtectHome=yes` deliberately hides it. Keep publisher signing seeds offline;
this service accepts independently admitted artifacts, not tenant signing keys.

The kubeconfig must explicitly select the intended API and a dedicated identity
with only `get` permission for the configured Agents, AgentRuns, AgentRuntimes,
CellnTools and named approval ConfigMaps. Restrict namespaces and resource names
where stable; run names are dynamic. No Secret read, approval write, run write or
cluster-admin credential is required by the issuer. Review Kubernetes certificate
or token expiry/rotation separately: the service does not mint its own identity.
The administrator-owned kubeconfig must not contain unreviewed exec plugins.

Configure verified TLS and a private reachable address restricted to controller
clients. The unit allows normal IP/Unix sockets; it is not an ingress firewall or
an egress allowlist. In particular, do not copy the Kind fixture's gateway address
to a different host and assume it works. Model credentials stay in the host-only
mapping; never place their contents in the unit, environment, Helm values or logs.

## Install and inspect, then start explicitly

After provisioning and independently reviewing the paths above, an administrator
can install the reviewed unit from the repository checkout:

```sh
sudo install -o root -g root -m 0644 \
  config/host/sympozium-celln-issuer.service \
  /etc/systemd/system/sympozium-celln-issuer.service
sudo systemd-analyze verify /etc/systemd/system/sympozium-celln-issuer.service
sudo systemctl daemon-reload
sudo systemctl cat sympozium-celln-issuer.service
sudo systemctl start sympozium-celln-issuer.service
sudo systemctl status sympozium-celln-issuer.service
```

This does not enable boot startup automatically. Missing configured paths or KVM
can leave the unit skipped by its conditions; a successful `systemctl start`
alone is not readiness. Inspect `ActiveState`, `SubState`, `Result` and conditions,
then use the authenticated, CA-verified issuer status request. An open issuer gate
still does not establish executable readiness or authorize execution.

`ProtectSystem=strict` leaves only `/var/lib/celln` plus private temporary space
writable. The host configuration and binaries remain read-only. The device policy
permits real `/dev/kvm`; `PrivateDevices` is deliberately off because hiding KVM
would prevent sealed-member checks. No new capabilities or privilege escalation
are granted. These are service process restrictions, not guest hardware evidence.

Before enabling boot startup, run the actual admitted Harness/model journey under
this service, including controller authentication refusals, cancellation, approval
withdrawal, process restart and journal recovery. Only then explicitly enable the
unit with `sudo systemctl enable sympozium-celln-issuer.service`.

## State, restart and upgrade

Preserve the **entire** provisioned policy root, including
`sympozium-issuer-journal`, profiles/grants, policy identities and admitted
artifacts. Keep the router ownership ledger and dispatcher execution journal too;
they may use separately configured paths. Copying only the issuer journal is not
a consistent system backup. Qualify locking, atomic rename and file/directory
fsync on the actual storage; a PVC/access-mode declaration does not prove them.

The unit restarts an unexpectedly failed issuer process after five seconds, with
a bounded restart burst. The command runs journal recovery before opening its
provisioning gate. Restart is not permission to renew an old grant or replay an
execution. The whole process group is stopped, including Celln verification
children, with a 100-second stop budget. Host boot-bound profile expiry remains
separate from service restart; serving-host reboot recovery is not proven here.

For upgrades, quiesce new submissions, reconcile outstanding runs against their
original owners, retain a consistent backup, stop the service, and install the
reviewed compatible binaries/configuration before starting again. Record source
revisions and binary hashes before and after. Never clear journals to make startup
succeed. Do not assume an older binary understands newly written journal formats
or the Celln-only CRD boundary. Rollback requires a compatibility decision, not
blind restoration of older executables while work is active.

Stopping/disabling or removing this unit does not revoke existing authority,
delete host state, or cancel cells. Perform those operations through their explicit
protocols and verify receipts/teardown. The unit deliberately has no destructive
stop hook or automatic state-directory ownership migration.
