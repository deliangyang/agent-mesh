import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "style.css"), "utf8");

function ruleBody(selector: string): string {
  const start = css.indexOf(selector);
  assert.ok(start >= 0, `missing selector ${selector}`);
  const open = css.indexOf("{", start);
  const close = css.indexOf("}", open);
  assert.ok(open > start && close > open, `broken rule for ${selector}`);
  return css.slice(open + 1, close);
}

describe("assistant markdown code block CSS", () => {
  it("stacks shiki lines with flex so missing newlines still wrap", () => {
    const code = ruleBody('.copilot-chat-assistant-markdown [data-streamdown="code-body"] code');
    assert.match(code, /display:\s*flex/);
    assert.match(code, /flex-direction:\s*column/);

    const line = ruleBody('.copilot-chat-assistant-markdown [data-streamdown="code-body"] .line');
    assert.match(line, /display:\s*block/);
    assert.doesNotMatch(line, /display:\s*inline/);
  });

  it("hides empty streaming open fences that otherwise paint a black strip", () => {
    assert.match(
      css,
      /\[data-streamdown="code-block"\]\[data-open-fence="true"\]:has\(code:empty\)/,
    );
    assert.match(
      css,
      /\[data-streamdown="code-block"\]\[data-streaming="true"\]:has\(code:empty\)/,
    );
  });
});
