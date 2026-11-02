import assert from "node:assert/strict";
import { test } from "node:test";
import {
  formatTodoTitle,
  isTodoTool,
  mergeTodos,
  parseTodoCall,
  todoLabel,
  todosFromMessages,
} from "./todos.ts";

test("识别 TodoWrite 和套在 CallDynamicTool 里的调用", () => {
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

test("解析 Cursor TodoWrite 参数", () => {
  const patch = parseTodoCall("TodoWrite", {
    merge: false,
    todos: [
      { id: "1", content: "读代码", status: "completed" },
      { id: "2", content: "改界面", status: "in_progress" },
      { id: "3", content: "补测试", status: "pending" },
    ],
  });
  assert.equal(patch?.merge, false);
  assert.equal(patch?.items.length, 3);
  assert.equal(patch?.items[1].status, "in_progress");
  assert.equal(formatTodoTitle(patch?.items || []), "待办 1/3");
});

test("解析套在 CallDynamicTool 里的待办", () => {
  const patch = parseTodoCall("CallDynamicTool", {
    namespace: "cursor",
    toolName: "TodoWrite",
    arguments: {
      merge: true,
      todos: [{ id: "2", content: "改界面", status: "completed" }],
    },
  });
  assert.equal(patch?.merge, true);
  assert.equal(patch?.items[0].id, "2");
  assert.equal(patch?.items[0].status, "completed");
});

test("参数还是 JSON 字符串也能解析", () => {
  const patch = parseTodoCall(
    "TodoWrite",
    '{"todos":[{"id":"a","content":"写样式","status":"pending"}],"merge":false}',
  );
  assert.equal(patch?.items[0].content, "写样式");
});

test("merge 按 id 更新，缺 content 时沿用旧文案", () => {
  const prev = parseTodoCall("TodoWrite", {
    merge: false,
    todos: [
      { id: "1", content: "读代码", status: "pending" },
      { id: "2", content: "改界面", status: "pending" },
    ],
  });
  const next = mergeTodos(prev?.items || [], {
    merge: true,
    items: [{ id: "2", content: "", status: "in_progress" }],
  });
  assert.equal(next.length, 2);
  assert.equal(next[1].content, "改界面");
  assert.equal(next[1].status, "in_progress");
});

test("merge=false 整表替换", () => {
  const next = mergeTodos(
    [{ id: "old", content: "旧任务", status: "pending" }],
    {
      merge: false,
      items: [{ id: "new", content: "新任务", status: "pending" }],
    },
  );
  assert.equal(next.length, 1);
  assert.equal(next[0].id, "new");
});

test("没有文案的条目不算可见待办", () => {
  assert.equal(todoLabel({ id: "1", content: "", status: "in_progress" }), "");
  assert.equal(todoLabel({ id: "2", content: "   ", status: "pending" }), "");
});

test("进行中优先显示 activeForm", () => {
  assert.equal(
    todoLabel({
      id: "1",
      content: "改界面",
      activeForm: "正在改界面",
      status: "in_progress",
    }),
    "正在改界面",
  );
  assert.equal(
    todoLabel({
      id: "1",
      content: "改界面",
      activeForm: "正在改界面",
      status: "completed",
    }),
    "改界面",
  );
});

test("从消息历史还原到当前这条为止的待办", () => {
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
                  { id: "1", content: "读代码", status: "in_progress" },
                  { id: "2", content: "改界面", status: "pending" },
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
                  todos: [{ id: "1", content: "读代码", status: "completed" }],
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
                todos: [{ id: "2", content: "改界面", status: "in_progress" }],
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

test("残缺 JSON 不生成待办", () => {
  assert.equal(parseTodoCall("TodoWrite", '{"todos":[{"id":"1","content":"'), null);
});
