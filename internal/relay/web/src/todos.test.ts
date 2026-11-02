import assert from "node:assert/strict";
import { test } from "node:test";
import { applyLocalePref } from "./locale.ts";
import {
  formatTodoTitle,
  isTodoTool,
  mergeTodos,
  parseTodoCall,
  todoLabel,
  todosFromMessages,
} from "./todos.ts";

applyLocalePref("en");

test("detect TodoWrite and calls nested in CallDynamicTool", () => {
  assert.equal(isTodoTool("TodoWrite"), true);
  assert.equal(isTodoTool("todo_write"), true);
  assert.equal(
    isTodoTool("CallDynamicTool", { namespace: "cursor", toolName: "TodoWrite" }),
    true,
  );
  assert.equal(
    isTodoTool("GetDynamicTools", { namespace: "cursor", toolName: "TodoWrite" }),
    false,
  );
  assert.equal(isTodoTool("CallDynamicTool", { namespace: "cursor", toolName: "Read" }), false);
});

test("parse Cursor TodoWrite arguments", () => {
  const patch = parseTodoCall("TodoWrite", {
    merge: false,
    todos: [
      { id: "1", content: "read the code", status: "completed" },
      { id: "2", content: "change the UI", status: "in_progress" },
      { id: "3", content: "add tests", status: "pending" },
    ],
  });
  assert.equal(patch?.merge, false);
  assert.equal(patch?.items.length, 3);
  assert.equal(patch?.items[1].status, "in_progress");
  assert.equal(formatTodoTitle(patch?.items || []), "Todos 1/3");
});

test("parse todos nested in CallDynamicTool", () => {
  const patch = parseTodoCall("CallDynamicTool", {
    namespace: "cursor",
    toolName: "TodoWrite",
    arguments: {
      merge: true,
      todos: [{ id: "2", content: "change the UI", status: "completed" }],
    },
  });
  assert.equal(patch?.merge, true);
  assert.equal(patch?.items[0].id, "2");
  assert.equal(patch?.items[0].status, "completed");
});

test("parse arguments that are still a JSON string", () => {
  const patch = parseTodoCall(
    "TodoWrite",
    '{"todos":[{"id":"a","content":"write styles","status":"pending"}],"merge":false}',
  );
  assert.equal(patch?.items[0].content, "write styles");
});

test("merge updates by id and keeps old text when content is missing", () => {
  const prev = parseTodoCall("TodoWrite", {
    merge: false,
    todos: [
      { id: "1", content: "read the code", status: "pending" },
      { id: "2", content: "change the UI", status: "pending" },
    ],
  });
  const next = mergeTodos(prev?.items || [], {
    merge: true,
    items: [{ id: "2", content: "", status: "in_progress" }],
  });
  assert.equal(next.length, 2);
  assert.equal(next[1].content, "change the UI");
  assert.equal(next[1].status, "in_progress");
});

test("merge=false replaces the whole list", () => {
  const next = mergeTodos(
    [{ id: "old", content: "old task", status: "pending" }],
    {
      merge: false,
      items: [{ id: "new", content: "new task", status: "pending" }],
    },
  );
  assert.equal(next.length, 1);
  assert.equal(next[0].id, "new");
});

test("items without text are not visible todos", () => {
  assert.equal(todoLabel({ id: "1", content: "", status: "in_progress" }), "");
  assert.equal(todoLabel({ id: "2", content: "   ", status: "pending" }), "");
});

test("in-progress items prefer activeForm", () => {
  assert.equal(
    todoLabel({
      id: "1",
      content: "change the UI",
      activeForm: "changing the UI",
      status: "in_progress",
    }),
    "changing the UI",
  );
  assert.equal(
    todoLabel({
      id: "1",
      content: "change the UI",
      activeForm: "changing the UI",
      status: "completed",
    }),
    "change the UI",
  );
});

test("rebuild todos from history up to this message", () => {
  const items = todosFromMessages(
    [
      {
        id: "m1",
        role: "assistant",
        toolCalls: [
          {
            function: {
              name: "TodoWrite",
              arguments: {
                merge: false,
                todos: [
                  { id: "1", content: "read the code", status: "in_progress" },
                  { id: "2", content: "change the UI", status: "pending" },
                ],
              },
            },
          },
        ],
      },
      {
        id: "m2",
        role: "assistant",
        toolCalls: [
          {
            function: {
              name: "CallDynamicTool",
              arguments: JSON.stringify({
                toolName: "TodoWrite",
                arguments: {
                  merge: true,
                  todos: [{ id: "1", content: "read the code", status: "completed" }],
                },
              }),
            },
          },
        ],
      },
      {
        id: "m3",
        role: "assistant",
        toolCalls: [
          {
            function: {
              name: "TodoWrite",
              arguments: {
                merge: true,
                todos: [{ id: "2", content: "change the UI", status: "in_progress" }],
              },
            },
          },
        ],
      },
    ],
    "m2",
  );
  assert.equal(items.length, 2);
  assert.equal(items[0].status, "completed");
  assert.equal(items[1].status, "pending");
});

test("partial JSON does not create todos", () => {
  assert.equal(parseTodoCall("TodoWrite", '{"todos":[{"id":"1","content":"'), null);
});
