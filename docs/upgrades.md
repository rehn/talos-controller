# Upgrades

The **Upgrades** page shows what you run, what is available, and performs rolling Talos upgrades.

## Versions at a glance

- **Talos running / Latest Talos** – the latest release comes from GitHub (checked at most once per hour; **Refresh** forces a check).
- **Kubernetes running / Latest Kubernetes** – the API server version and the latest upstream release.
- **Recent Talos releases** – with the Kubernetes version each Talos release ships with.

### Recommended version

Talos should be upgraded **one minor release at a time** (e.g. 1.12 → 1.13 → 1.14). The target list marks the **recommended** version: the newest patch of the next minor after the oldest version in your cluster. If you are several minors behind, a note tells you the path.

Upgrade Talos first, then Kubernetes.

## Rolling Talos upgrade

1. Pick the **Target** version.
2. Select the nodes. Unreachable nodes can't be selected. Nodes already on the target show **up to date**.
3. Check the **installer image** per node. Each node keeps its own image (for example an Image Factory URL with your schematic and extensions); only the tag is replaced. Edit the field to use a different image for a node.
4. Choose options, click **Upgrade N nodes**, type the version to confirm.

You are taken to the [job](jobs.md) that runs the upgrade.

### What happens

Nodes are upgraded **one at a time, control planes first**. For each node:

1. Skip it if it already runs the target version (unless **Force** is set).
2. **Preflight**: all Kubernetes nodes must be Ready; for a control plane node every etcd member must be healthy and no alarms active.
3. Start the upgrade and wait for the node to reboot (up to 30 minutes).
4. Verify that it now runs the target version. A different version means the upgrade rolled back, and the rollout stops.
5. Wait for the node to be **Ready** in Kubernetes (up to 10 minutes) and, on control planes, for its **etcd member** to be healthy (up to 5 minutes).
6. Wait 30 seconds to let it settle, then continue with the next node.

If a node fails, the rollout stops so you can investigate, unless **Continue on error** is set.

### Options

| Option | Use it when |
|---|---|
| **Stage** | The upgrade fails because files are in use; the new version is installed on the next boot |
| **Force** | You want to reinstall the same version, or skip Talos's own etcd health check on the node (risky) |
| **Skip preflight** | The cluster is already degraded and you know why (e.g. a dead worker you are replacing) |
| **Continue on error** | You prefer to upgrade as many nodes as possible rather than stop at the first failure |

Only one upgrade or config rollout can run at a time.

## Kubernetes upgrades

Kubernetes upgrades touch every control plane component, the kubelets and bootstrap manifests, so they are done with `talosctl`. The Kubernetes card shows the target version (the one shipped with the selected Talos release, or the latest upstream release) and the commands to run from a machine with `talosctl`:

```bash
talosctl --nodes <control-plane-ip> upgrade-k8s --to 1.35.1 --dry-run
talosctl --nodes <control-plane-ip> upgrade-k8s --to 1.35.1
```

Click **Copy** to copy them. Check the Talos support matrix before moving to a new Kubernetes minor version.
