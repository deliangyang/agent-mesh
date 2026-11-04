import assert from "node:assert/strict";
import { test } from "node:test";
import { clearAsk, dismissAsk, formatAskAnswer, meshAsk, noteAskEvents, noteAskFromTool } from "./ask.ts";

const askText = JSON.stringify({
  id: "q1",
  kind: "question",
  title: "style",
  questions: [
    {
      id: "style",
      prompt: "Which style?",
      header: "style",
      options: [
        { id: "rewrite", label: "rewrite" },
        { id: "patch", label: "patch" },
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

test("an unanswered ask keeps its options", () => {
  clearAsk();
  noteAskEvents([{ type: "user", role: "user", text: "hi" }, askEvent()], true);
  assert.equal(meshAsk.prompt?.id, "q1");
});

test("chosen elsewhere: a later user message dismisses options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "user", role: "user", text: "rewrite" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("chosen elsewhere: a tool result dismisses options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_result", tool: "AskUserQuestion", text: "rewrite" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("chosen elsewhere: a nameless tool_result also dismisses options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_result", text: "rewrite" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("chosen elsewhere: a later assistant reply dismisses options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "assistant", text: "Then rewrite it." }], true);
  assert.equal(meshAsk.prompt, null);
});

test("chosen elsewhere: another tool starting dismisses options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_call", tool: "Bash", text: "{}" }], true);
  assert.equal(meshAsk.prompt, null);
});

test("a nameless tool_call must not dismiss options", () => {
  clearAsk();
  noteAskEvents([askEvent(), { type: "tool_call", tool: "tool", text: "{}" }], true);
  assert.equal(meshAsk.prompt?.id, "q1");
});

test("after a manual dismiss, the same question does not pop again", () => {
  clearAsk();
  noteAskEvents([askEvent()], true);
  assert.ok(meshAsk.prompt);
  dismissAsk();
  assert.equal(meshAsk.prompt, null);
  noteAskEvents([askEvent()], true);
  assert.equal(meshAsk.prompt, null);
});

test("after a manual dismiss, a new question still pops", () => {
  clearAsk();
  noteAskEvents([askEvent("q1")], true);
  dismissAsk();
  noteAskEvents([askEvent("q2")], true);
  assert.equal(meshAsk.prompt?.id, "q2");
});

test("a plan tool call asks for approval with a recommended choice", () => {
  clearAsk();
  noteAskFromTool("ExitPlanMode", { plan: "1. read the code\n2. fix the bug" }, "p1");
  const prompt = meshAsk.prompt;
  assert.ok(prompt);
  assert.equal(prompt.kind, "plan");
  assert.equal(prompt.questions[0].detail.includes("fix the bug"), true);
  assert.equal(prompt.questions[0].options[0].recommended, true);
});

test("free text alone is a valid answer", () => {
  assert.equal(formatAskAnswer({ kind: "plan", questions: [] }, {}, "looks good"), "looks good");
  assert.equal(formatAskAnswer({ kind: "plan", questions: [] }, {}, "") , '{"answers":{}}');
});
