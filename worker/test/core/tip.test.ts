import { describe, expect, test } from "vitest";
import { MAX_TIP_LENGTH, hasLink, isValidTip } from "../../src/core/tip.ts";

describe("hasLink", () => {
  test.each([
    "https://example.com",
    "see http://codeforces.com/blog",
    "ftp://files.example.org/x",
    "join evil.com",
    "JOIN EVIL.COM",
    "chat.whatsapp.com/abc",
    "www.codechef.com",
    "пример.рф",
  ])("finds a link in %j", (text) => {
    expect(hasLink(text)).toBe(true);
  });

  test.each([
    "never started cp? scared of cf?",
    "e.g. read all problems first... then A.",
    "Div. 3 at 8:00 PM, 1:30 hours",
    "aim for D today, upsolve E after",
    "",
  ])("finds no link in %j", (text) => {
    expect(hasLink(text)).toBe(false);
  });
});

describe("isValidTip", () => {
  test("accepts one line without a link, up to the length limit", () => {
    expect(isValidTip("wake up, code, then chai")).toBe(true);
    expect(isValidTip("x".repeat(MAX_TIP_LENGTH))).toBe(true);
    expect(isValidTip("")).toBe(true);
  });

  test.each([
    ["too long", "x".repeat(MAX_TIP_LENGTH + 1)],
    ["line feed", "a\nb"],
    ["carriage return", "a\rb"],
    ["line separator", `a${String.fromCodePoint(0x2028)}b`],
    ["paragraph separator", `a${String.fromCodePoint(0x2029)}b`],
    ["link", "see https://codeforces.com/blog"],
    ["bare domain", "join evil.com"],
  ])("rejects a tip with a %s", (_name, tip) => {
    expect(isValidTip(tip)).toBe(false);
  });
});
