import assert from "node:assert/strict";
import { test } from "node:test";
import {
  contentWithAttachRefs,
  keepMediaContent,
  parseAttachBlock,
  rememberMedia,
  recallMedia,
} from "./attach.ts";

test("parseAttachBlock keeps inbox paths only", () => {
  const raw =
    "hi\n\n<<MESH_ATTACH>>\n- /tmp/p/.agent-mesh/inbox/a.png (image/png)\n- /etc/passwd (text/plain)\n<</MESH_ATTACH>>";
  const got = parseAttachBlock(raw);
  assert.equal(got.length, 1);
  assert.equal(got[0].path, "/tmp/p/.agent-mesh/inbox/a.png");
  assert.equal(got[0].mime, "image/png");
});

test("contentWithAttachRefs builds image url parts", () => {
  const content = contentWithAttachRefs("look", [{ path: "/x/.agent-mesh/inbox/a.png", mime: "image/png", name: "a.png" }], "n1");
  assert.ok(Array.isArray(content));
  const parts = content as Array<Record<string, unknown>>;
  assert.equal(parts[0].type, "text");
  assert.equal(parts[1].type, "image");
  const src = parts[1].source as { type: string; value: string };
  assert.equal(src.type, "url");
  assert.match(src.value, /\/v1\/openai\/file\?/);
  assert.match(src.value, /node=n1/);
});

test("keepMediaContent restores from bank when history is text-only", () => {
  const media = [
    {
      type: "text",
      text: "see this",
    },
    {
      type: "image",
      source: { type: "data", value: "abc", mimeType: "image/png" },
      metadata: { filename: "a.png" },
    },
  ];
  rememberMedia("see this", media);
  assert.ok(recallMedia("see this"));
  const next = keepMediaContent(
    [{ id: "1", role: "user", content: "see this" }],
    [],
  );
  assert.ok(Array.isArray(next[0].content));
  assert.equal((next[0].content as unknown[]).length, 2);
});
