<img src="assets/logo.svg" width="64" alt="">

# Talos Controller documentation

How to install and use Talos Controller to supervise and maintain a Talos Linux cluster.

| Guide | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install with Docker, first login, connect to your cluster |
| [Configuration](configuration.md) | Environment variables, data directory, credentials, reverse proxy |
| [Monitoring](monitoring.md) | Dashboard and health checks, nodes, logs, workloads, etcd |
| [Upgrades](upgrades.md) | Rolling Talos upgrades, Kubernetes upgrades, choosing versions |
| [Config patches](config-patches.md) | Patching machine config on roles or nodes, rolling apply |
| [Config drift](config-drift.md) | Finding settings that differ between nodes of the same role |
| [Jobs](jobs.md) | Following, cancelling and reviewing long-running operations |
| [Troubleshooting](troubleshooting.md) | Common errors and how to fix them |

## In one minute

1. Run the container (see [Getting started](getting-started.md)).
2. Open `http://<host>:8080` and log in.
3. On **Settings**, paste your talosconfig and set the control plane endpoint(s).
4. The **Dashboard** shows what needs attention. **Upgrades**, **Config patches** and **Config drift** help you fix it, one node at a time.
