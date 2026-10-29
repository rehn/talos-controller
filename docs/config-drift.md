# Config drift

**Config drift** compares the live machine configs of nodes **that share a role** and shows the settings that differ. It answers questions like "why does worker-2 behave differently?" or "did that patch reach every node?".

## Reading the report

There is one card per role:

- **consistent** – every compared setting is identical on all nodes of the role.
- **N differences** – a table with one row per differing setting and one column per node.
- **nothing to compare** – the role has a single node.

In each row, values that differ from the **majority** are highlighted. *not set* means the node doesn't have the setting at all. If no value is shared by more nodes than any other (e.g. two nodes, two values), the row says **no majority** and every value is highlighted.

Settings are shown as paths, e.g. `machine › kubelet › extraArgs › max-pods`. Settings from additional config documents (Talos 1.12+ multi-document configs) are prefixed with the document kind, e.g. `LinkConfig eth0 › mtu`.

Secrets are redacted before comparison, so differing keys or tokens are not reported.

## Per-node settings

Some settings are expected to differ between nodes and are hidden by default: hostname, network interfaces and addresses, extra host entries, install disk, certSANs, kubelet node IP, and per-node network and volume documents. Tick **Include per-node settings** to see them as well.

## Fixing drift

When a row has a majority value, **Align &lt;node&gt;…** opens [Config patches](config-patches.md) with a patch that sets the outliers to the majority value and only those nodes selected. Dry run it, then roll it out.

There is no Align button when:

- the majority **doesn't** have the setting (the fix is removing it from the outlier; write a JSON6902 `remove` patch yourself),
- there is no majority,
- the setting lives inside a list or in an additional config document.

## When to look

- After an upgrade or a rollout, to confirm every node got the change.
- After replacing or adding a node.
- Before a role-wide patch, to see what is different today.
