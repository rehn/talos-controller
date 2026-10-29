# Config patches

**Config patches** changes the machine configuration of a whole role or of selected nodes, safely and one node at a time.

## 1. Write the patch

Two formats are accepted:

**Strategic merge patch** (YAML, may contain several documents) – the parts you write are merged into the existing config:

```yaml
machine:
  sysctls:
    vm.max_map_count: "262144"
```

**JSON6902 patch** – a list of operations, useful for removing or replacing values:

```yaml
- op: add
  path: /machine/network/nameservers
  value:
    - 1.1.1.1
    - 9.9.9.9
```

The buttons above the editor insert examples (NTP servers, sysctl, kubelet extra args, node label, JSON6902). File references such as `@patch.yaml` are not supported; paste the content.

## 2. Choose the apply mode

| Mode | Behaviour |
|---|---|
| **Auto** (default) | Applied live; the node reboots only if the change requires it |
| **No reboot** | Applied live; fails instead of rebooting if a reboot would be needed |
| **Staged** | Saved now, takes effect on the next reboot |
| **Reboot** | Applied and the node always reboots |

## 3. Choose the targets

- **All control planes / All workers / All nodes** target a whole role. The role is resolved when the rollout starts, so nodes added in the meantime are included. Unreachable nodes are skipped and listed.
- Ticking individual nodes switches to exactly those nodes.

## 4. Dry run

Click **Dry run**. Every selected node reports whether the patch is valid, shows the resulting **config diff**, and whether it would need a reboot (**needs reboot**). **Roll out** stays disabled until a dry run succeeds, and any change to the patch, mode or targets requires a new dry run.

## 5. Roll out

Click **Roll out** and confirm. The rollout runs as a [job](jobs.md):

1. The patch is dry-run again on **every** target. If any of them fails, **nothing is applied**.
2. Nodes are patched **one at a time, control planes first**.
3. If a node reboots to apply the change, the next node waits until it is back (up to 20 minutes).
4. Each node must be **Ready** in Kubernetes and, on control planes, have a healthy **etcd member** before the next one starts.
5. If a node fails, the rollout stops – unless **Continue with the next node if one fails** is ticked.

Only one rollout or Talos upgrade can run at a time.

## Tips

- To patch a single node, open it on **Nodes** → **Config** tab → **Patch…**.
- To bring an odd node in line with its peers, use **Align…** on [Config drift](config-drift.md); it opens this page with the patch and node pre-filled.
- Settings that must differ per node (hostname, addresses, install disk) don't belong in a role-wide patch.
- The **Config** tab on a node shows the result after the rollout.
