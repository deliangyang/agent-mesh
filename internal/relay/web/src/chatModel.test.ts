import assert from "node:assert/strict";
import { test } from "node:test";
import { chatModelOf, modelFitsEngine } from "./chatModel.ts";

test("chatModelOf keeps the selected model", () => {
  assert.equal(chatModelOf("n1/cursor/composer-2.5", "n1", "cursor"), "n1/cursor/composer-2.5");
  assert.equal(chatModelOf("cursor/composer-2.5", "", "cursor"), "cursor/composer-2.5");
  assert.equal(chatModelOf("composer-2.5", "n1", "cursor"), "n1/cursor/composer-2.5");
  assert.equal(modelFitsEngine("n1/cursor/composer-2.5", "cursor", "n1"), true);
  assert.equal(modelFitsEngine("claude/opus", "cursor", "n1"), false);
});
