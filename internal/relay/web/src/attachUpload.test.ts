import assert from "node:assert/strict";
import { test } from "node:test";
import { explainAttachError } from "./attachUpload.ts";
import { applyLocalePref } from "./locale.ts";

applyLocalePref("en");

function fail(reason: "file-too-large" | "invalid-type" | "upload-failed", message = ""): Parameters<typeof explainAttachError>[0] {
  return {
    reason,
    file: { name: "shot.png" } as File,
    message,
  };
}

test("explain oversized and invalid attachments", () => {
  assert.equal(explainAttachError(fail("file-too-large")), '"shot.png" is over 20MB');
  assert.equal(explainAttachError(fail("invalid-type")), '"shot.png" type is not supported');
});

test("name the file on read and other failures", () => {
  assert.equal(explainAttachError(fail("upload-failed", "Failed to read file as base64")), 'failed to read "shot.png"');
  assert.equal(explainAttachError(fail("upload-failed", "boom")), 'failed to upload "shot.png"');
});
