import { test } from "node:test";
import assert from "node:assert/strict";
import { MISSING, formatValue, prettyPath, roleSelection, nodesOfRole } from "./drift.ts";

test("formatValue", () => {
  assert.equal(formatValue(MISSING), "not set");
  assert.equal(formatValue('"262144"'), "262144");
  assert.equal(formatValue("true"), "true");
  assert.equal(formatValue("9000"), "9000");
  assert.equal(formatValue('["1.1.1.1","9.9.9.9"]'), "1.1.1.1, 9.9.9.9");
  assert.equal(formatValue("[]"), "[]");
  assert.equal(formatValue("{}"), "{}");
  assert.equal(formatValue('{"a":1}'), '{"a":1}');
  assert.equal(formatValue("not json"), "not json");
});

test("prettyPath", () => {
  assert.equal(prettyPath("/machine/sysctls/vm.max_map_count"), "machine › sysctls › vm.max_map_count");
  assert.equal(prettyPath("/machine/nodeLabels/topology.kubernetes.io~1zone"), "machine › nodeLabels › topology.kubernetes.io/zone");
  assert.equal(prettyPath("/#LinkConfig:eth0/mtu"), "LinkConfig eth0 › mtu");
  assert.equal(prettyPath("/#HostnameConfig/hostname"), "HostnameConfig › hostname");
  assert.equal(prettyPath("/a~0b"), "a~b");
});

const nodes = [
  { name: "cp1", role: "controlplane", reachable: true },
  { name: "w1", role: "worker", reachable: true },
  { name: "w2", role: "worker", reachable: false },
];

test("roleSelection selects reachable nodes of a role", () => {
  assert.deepEqual(roleSelection(nodes, "worker"), { w1: true });
  assert.deepEqual(roleSelection(nodes, "controlplane"), { cp1: true });
  assert.deepEqual(roleSelection(nodes, "all"), { cp1: true, w1: true });
});

test("nodesOfRole counts all nodes of the role", () => {
  assert.equal(nodesOfRole(nodes, "worker").length, 2);
  assert.equal(nodesOfRole(nodes, "all").length, 3);
});
