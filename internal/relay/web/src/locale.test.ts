import assert from "node:assert/strict";
import { afterEach, describe, it } from "node:test";
import {
  applyLocalePref,
  cycleLocale,
  detectSystemLocale,
  locale,
  localeButtonLabel,
  localePref,
  resolveLocale,
  t,
} from "./locale.ts";

afterEach(() => {
  applyLocalePref("en");
});

describe("detectSystemLocale", () => {
  it("maps zh* to zh-CN", () => {
    assert.equal(detectSystemLocale("zh-CN"), "zh-CN");
    assert.equal(detectSystemLocale("zh-TW"), "zh-CN");
    assert.equal(detectSystemLocale("zh"), "zh-CN");
  });

  it("defaults other languages to en", () => {
    assert.equal(detectSystemLocale("en-US"), "en");
    assert.equal(detectSystemLocale("ja"), "en");
    assert.equal(detectSystemLocale(""), "en");
  });
});

describe("resolveLocale", () => {
  it("honors explicit prefs", () => {
    assert.equal(resolveLocale("en", "zh-CN"), "en");
    assert.equal(resolveLocale("zh-CN", "en-US"), "zh-CN");
  });

  it("detects from system when pref is system", () => {
    assert.equal(resolveLocale("system", "zh-CN"), "zh-CN");
    assert.equal(resolveLocale("system", "en-GB"), "en");
  });
});

describe("t", () => {
  it("returns English by default", () => {
    applyLocalePref("en");
    assert.equal(t("login.enter"), "Enter");
    assert.equal(t("toast.requestFailedStatus", { status: 502 }), "Request failed (502)");
  });

  it("returns Chinese when locale is zh-CN", () => {
    applyLocalePref("zh-CN");
    assert.equal(t("login.enter"), "进入");
    assert.equal(locale.value, "zh-CN");
  });
});

describe("cycleLocale", () => {
  it("cycles system → en → zh-CN → system", () => {
    applyLocalePref("system");
    assert.equal(cycleLocale(), "en");
    assert.equal(localePref.value, "en");
    assert.equal(cycleLocale(), "zh-CN");
    assert.equal(cycleLocale(), "system");
  });

  it("labels match prefs", () => {
    assert.equal(localeButtonLabel("system"), "Auto");
    assert.equal(localeButtonLabel("en"), "EN");
    assert.equal(localeButtonLabel("zh-CN"), "中文");
  });
});
