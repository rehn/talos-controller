# Monitoring

## Dashboard

The dashboard refreshes every 20 seconds (click **Refresh** to force it). The badge next to the cluster name is the worst result of all health checks: **Healthy**, **Degraded** or **Failing**.

The tiles show nodes ready, Talos and Kubernetes versions, etcd members and pods. Click **upgrades** or **need attention** to jump to the relevant page.

### Health checks

| Check | Warns when | Fails when |
|---|---|---|
| Talos API reachable | – | A node does not answer on the Talos API |
| Kubernetes nodes Ready | Kubernetes API unavailable | A node is NotReady or cordoned |
| Consistent versions | Nodes run different Talos or kubelet versions (normal during an upgrade) | – |
| etcd quorum | A member is unhealthy but quorum holds, alarms are active, no leader, or member count ≠ control plane count | Fewer healthy members than quorum needs |
| Talos services healthy | – | A Talos service is not Running/Finished, or reports unhealthy |
| Memory pressure | A node has less than 10% memory available | – |
| Disk usage | `/var` or `/system/state` is ≥ 85% full | ≥ 95% full |
| Workloads | Problem pods or controllers missing replicas | A problem pod in `kube-system` |

Each failing check lists the affected nodes, services or pods.

## Nodes

**Nodes** lists every node with role, status, Talos and kubelet versions, CPU load (1-minute load / cores), memory and uptime. Status is:

- **Ready** – Kubernetes reports the node Ready and the Talos API answers
- **Ready, cordoned** – Ready but unschedulable
- **NotReady** – Kubernetes reports it NotReady
- **Unreachable** – the Talos API did not answer (the error is shown below)
- **Talos only** – answers on the Talos API but isn't registered in Kubernetes

Click a node to open its detail page.

### Node detail

| Tab | What you can do |
|---|---|
| **Overview** | Versions, load, memory, uptime, installer image, and usage of every filesystem |
| **Services** | All Talos services with state and health. **Logs** opens the service log; **Restart** restarts the service (restarting `etcd` or `apid` briefly affects cluster access) |
| **Logs** | The last 100–3000 lines of any Talos service (`kubelet`, `etcd`, `containerd`, `apid`, …). Tick **auto-refresh** to reload every 5 seconds |
| **Kernel log** | The last 500 lines of `dmesg` |
| **Config** | The node's active machine config with secrets redacted. **Patch…** opens [Config patches](config-patches.md) for this node |

Buttons at the top:

- **Upgrade** – opens [Upgrades](upgrades.md) with only this node selected.
- **Reboot** – type the node name to confirm. The reboot runs as a [job](jobs.md) that waits up to 20 minutes for the node to come back. Pods on the node are interrupted; on a control plane node make sure etcd keeps quorum without it.

## Workloads

Covers all namespaces:

- **Problem pods** – pods that are `CrashLoopBackOff`, `ImagePullBackOff`/`ErrImagePull`, have container config errors, have been `Pending` for more than 5 minutes (with the scheduler's reason if unschedulable), are `Failed`, or have been running but NotReady for more than 5 minutes.
- **Controllers** – Deployments, StatefulSets and DaemonSets with fewer ready replicas than desired. Tick **show healthy** to see all of them.
- **Recent warning events** – Kubernetes warning events from the last 2 hours (at most 100).

## etcd

Shows each member with leader/learner flags, health, database size (in use vs. allocated) and raft index, plus:

- **Fault tolerance** – how many members can fail right now without losing quorum.
- **Alarms** – active etcd alarms such as `NOSPACE`.

With a single control plane node you get a warning: take regular snapshots with `talosctl etcd snapshot`.
