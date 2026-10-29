# Troubleshooting

## "No talosconfig configured yet" / redirected to Settings

No talosconfig has been uploaded and none is mounted. Add one on **Settings**.

## "context … has no endpoints"

The selected talosconfig context has no `endpoints`. Enter your control plane IP(s) under **Settings → Context & endpoints**.

## An endpoint test fails on Settings

- `connection refused` / `i/o timeout` – the host can't reach the node on port 50000. Check the IP, firewalls and that the container's network can reach your LAN/VPN.
- `certificate signed by unknown authority` / `x509` errors – the talosconfig belongs to a different cluster, or the endpoint is another cluster's node.
- `permission denied` – the talosconfig's role doesn't allow the call. See [Credentials](configuration.md#credentials).

## "Kubernetes unavailable"

Kubernetes data (Ready status, workloads, node list) is missing while Talos data works.

- Without an uploaded or mounted kubeconfig, it is fetched through the Talos API, which needs an `os:admin` talosconfig. Upload a kubeconfig on Settings otherwise.
- The kubeconfig's server address must be reachable from the container (port 6443).

## A node shows "Unreachable"

The node didn't answer the Talos API within a few seconds: it may be down, rebooting, or its IP (the Kubernetes InternalIP) isn't reachable from the container.

## Settings are lost after restart / "saving … is DATA_DIR writable?"

The data directory is not writable by the container user. Create it before starting and run the container with `user: "<your uid>:<your gid>"`, or `chown 65532:65532` it. See [Data directory](configuration.md#data-directory).

## Logged out after every restart

Set `SESSION_SECRET` to a fixed value (`openssl rand -hex 32`).

## "too many failed attempts"

Five failed logins from the same IP within 5 minutes lock that IP out for a few minutes. Wait and try again.

## "Could not check GitHub for releases"

The host can't reach `api.github.com`, or the anonymous rate limit was hit. Everything else keeps working; set `GITHUB_TOKEN` to raise the limit.

## "another job of this kind is already running"

An upgrade or config rollout is still running. Wait for it or cancel it on **Jobs**.

## An upgrade stopped with "came back on vX, expected vY"

The node booted the old version again, so Talos rolled the upgrade back. Check the node's **Kernel log** and `machined` logs, and verify that the installer image exists and fits the node's architecture.

## Container logs

```bash
docker logs -f talos-controller
```

Set `DEBUG=1` for more detail.
