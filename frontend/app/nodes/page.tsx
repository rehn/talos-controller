"use client";

import { api, type Node } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ErrorBox, Loading, PageHead, RefreshButton } from "@/components/ui";
import NodeTable from "@/components/NodeTable";

export default function NodesPage() {
  const nodes = usePoll(() => api.get<Node[]>("/nodes"), 15000);

  return (
    <>
      <PageHead title="Nodes" subtitle="Kubernetes and Talos state for every machine">
        <RefreshButton onClick={nodes.refresh} lastUpdated={nodes.lastUpdated} />
      </PageHead>
      <ErrorBox error={nodes.error} />
      {nodes.loading && !nodes.data ? <Loading /> : null}
      {nodes.data ? (
        <div className="card">
          <NodeTable nodes={nodes.data} />
        </div>
      ) : null}
    </>
  );
}
