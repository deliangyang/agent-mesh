import assert from "node:assert/strict";
import { test } from "node:test";
import { explainAttachError } from "./attachUpload.ts";

function fail(reason: "file-too-large" | "invalid-type" | "upload-failed", message = ""): Parameters<typeof explainAttachError>[0] {
  return {
    reason,
    file: { name: "shot.png" } as File,
    message,
  };
}

test("附件过大和类型错误用中文说明", () => {
  assert.equal(explainAttachError(fail("file-too-large")), "「shot.png」超过 20MB");
  assert.equal(explainAttachError(fail("invalid-type")), "「shot.png」类型不支持");
});

test("读取失败和其它失败都标出文件名", () => {
  assert.equal(explainAttachError(fail("upload-failed", "Failed to read file as base64")), "「shot.png」读取失败");
  assert.equal(explainAttachError(fail("upload-failed", "boom")), "「shot.png」上传失败");
});
