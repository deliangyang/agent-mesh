import assert from "node:assert/strict";
import { test } from "node:test";
import { clearAsk, dismissAsk, meshAsk, noteAskEvents } from "./ask.ts";

const askText = JSON.stringify({
  id: "q1",
  kind: "question",
  title: "写法",
  questions: [
    {
      id: "写法",
      prompt: "用哪种写法？",
      header: "写法",
      options: [
        { id: "重写", label: "重写" },
        { id: "修补", label: "修补" },
      ],
    },
  ],
});

function askEvent(id = "q1"): { type: string; tool: string; text: string } {
  const text = JSON.stringify({
    ...JSON.parse(askText),
    id,
  });
  return { type: "ask", tool: "AskUserQuestion", text };
}

test("未回答的 ask 会留下选项", () => {
  clearAsk();
  noteAskEvents([{ type: "user", role: "user", text: "hi" }, askEvent()], true);
  assert.equal(meshAsk.prompt?.id, "q1");
});

test("别处已选：后续用户消息会关掉选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "user", role: "user", text: "重写" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("别处已选：工具结果会关掉选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_result", tool: "AskUserQuestion", text: "重写" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("别处已选：无名 tool_result 也会关掉选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_result", text: "重写" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("别处已选：后续助手回复会关掉选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "assistant", text: "那就重写。" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("别处已选：其它工具开始执行会关掉选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_call", tool: "Bash", text: "{}" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("无名 tool_call 不会误关选项", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_call", tool: "tool", text: "{}" }], true);
  assert.equal(meshAsk.prompt?.id, "q1");
});

test("手动关闭后，同一题重放不再弹出", () => {
  clearAsk();
  noteAskEvents([askEvent()], true);
  assert.ok(meshAsk.prompt);
  dismissAsk();
  assert.equal(meshAsk.prompt, null);
  noteAskEvents([askEvent()], true);
  assert.equal(meshAsk.prompt, null);
});

test("手动关闭后，新的一题仍会弹出", () => {
  clearAsk();
  noteAskEvents([askEvent("q1")], true);
  dismissAsk();
  noteAskEvents([askEvent("q2")], true);
  assert.equal(meshAsk.prompt?.id, "q2");
});
