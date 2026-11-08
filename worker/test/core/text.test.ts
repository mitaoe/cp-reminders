import { describe, expect, test } from "vitest";
import { MAX_TIP_LENGTH, defuseLinks, isSafeText, isValidTip } from "../../src/core/text.ts";
import freeText from "../../../testdata/free-text.json" with { type: "json" };

const ch = (cp: number) => String.fromCodePoint(cp);
const expand = (s: string) => s.replace(/<U\+([0-9A-F]{4,6})>/g, (_m, hex: string) => ch(Number.parseInt(hex, 16)));

describe("isSafeText agrees with the shared cases in testdata/free-text.json", () => {
  test("the shared cases exist", () => {
    expect(freeText.safe.length).toBeGreaterThan(0);
    expect(freeText.unsafe.length).toBeGreaterThan(0);
  });

  test.each(freeText.safe)("accepts %j", (text) => {
    expect(isSafeText(expand(text))).toBe(true);
  });

  test.each(freeText.unsafe)("refuses %j", (text) => {
    expect(isSafeText(expand(text))).toBe(false);
  });
});

describe("isValidTip", () => {
  test("accepts safe text up to the length limit in code points, and the empty tip", () => {
    expect(isValidTip("wake up, code, then chai")).toBe(true);
    expect(isValidTip("x".repeat(MAX_TIP_LENGTH))).toBe(true);
    expect(isValidTip(ch(0x1f525).repeat(MAX_TIP_LENGTH))).toBe(true);
    expect(isValidTip("")).toBe(true);
  });

  test.each([
    ["too long", "x".repeat(MAX_TIP_LENGTH + 1)],
    ["too long in code points", ch(0x1f525).repeat(MAX_TIP_LENGTH + 1)],
    ["only spaces", "   "],
    ["unsafe", "join evil.com"],
  ])("refuses a tip that is %s", (_name, tip) => {
    expect(isValidTip(tip)).toBe(false);
  });
});

describe("defuseLinks", () => {
  test("replaces the dots of domains and IP addresses with a one dot leader", () => {
    expect(defuseLinks("Refact.ai Match 1 (Codeforces Round 985)")).toBe(`Refact${ch(0x2024)}ai Match 1 (Codeforces Round 985)`);
    expect(defuseLinks("based on St.Petersburg Olympiad")).toBe(`based on St${ch(0x2024)}Petersburg Olympiad`);
    expect(defuseLinks("mirror at 203.0.113.5")).toBe(`mirror at 203${ch(0x2024)}0${ch(0x2024)}113${ch(0x2024)}5`);
    expect(defuseLinks(`evil${ch(0x3002)}com and evil${ch(0xfe52)}net`)).toBe(`evil${ch(0x2024)}com and evil${ch(0x2024)}net`);
  });

  test("makes every domain or address in the shared unsafe cases safe", () => {
    for (const text of ["Refact.ai and evil.co.uk", "join evil.कॉम", "a@evil.ලංකා", `evil${ch(0xff0e)}com`, "1.2.3.4.5.6.7"]) {
      expect(isSafeText(defuseLinks(text))).toBe(true);
    }
  });

  test("leaves other punctuation alone", () => {
    for (const name of ["Codeforces Round 1134 (Div. 4)", "Educational Codeforces Round 190 (Rated for Div. 2)", "e.g. Starters 264", "U.S. Open"]) {
      expect(defuseLinks(name)).toBe(name);
    }
  });
});
