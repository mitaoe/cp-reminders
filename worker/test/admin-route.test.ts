import { env, exports } from "cloudflare:workers";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MAX_TIP_LENGTH } from "../src/core/tip.ts";
import { baseOverrides, rules, tipRules } from "../src/config.ts";
import { formatReminder, formatTime, isoUtc, istClock } from "../src/core/format.ts";
import { PREVIEW_LIMIT } from "../src/preview.ts";
import { MIN, NOW, TARGET, mockFetch, resetDb } from "./helpers.ts";

beforeEach(async () => {
  await resetDb();
  vi.useFakeTimers({ toFake: ["Date"], now: NOW });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const request = (method: string, path: string, body?: unknown, token: string | null = env.ADMIN_TOKEN) =>
  exports.default.fetch(`https://w.test${path}`, {
    method,
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : typeof body === "string" ? body : JSON.stringify(body),
  });

const tipRow = (href: string) =>
  env.DB.prepare("SELECT tip, updated_at FROM tip_overrides WHERE href = ?1").bind(href).first<{ tip: string; updated_at: string }>();

const HREF = TARGET.href;

describe.each([
  ["PUT", "/tips", { href: HREF, tip: "t" }],
  ["DELETE", "/tips", { href: HREF }],
  ["GET", "/preview", undefined],
])("%s %s auth", (method, path, body) => {
  it("rejects missing, wrong and other-role tokens", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mockFetch().fetcher(String(input), init));
    expect((await request(method, path, body, null)).status).toBe(401);
    expect((await request(method, path, body, "wrong")).status).toBe(401);
    expect((await request(method, path, body, env.TICK_TOKEN)).status).toBe(401);
    expect((await request(method, path, body, env.SENDER_TOKEN)).status).toBe(401);
    expect(await tipRow(HREF)).toBeNull();
  });
});

describe("PUT /tips", () => {
  it("upserts a one-off tip", async () => {
    const res = await request("PUT", "/tips", { href: HREF, tip: "bring snacks" });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ ok: true });
    expect(await tipRow(HREF)).toEqual({ tip: "bring snacks", updated_at: new Date(NOW).toISOString() });
    await request("PUT", "/tips", { href: HREF, tip: "hydrate" });
    expect((await tipRow(HREF))?.tip).toBe("hydrate");
  });

  it("accepts punctuation that is not a link", async () => {
    expect((await request("PUT", "/tips", { href: HREF, tip: "e.g. read all problems first... then A." })).status).toBe(200);
  });

  it("stores an empty tip to suppress the tip for that contest", async () => {
    expect((await request("PUT", "/tips", { href: HREF, tip: "" })).status).toBe(200);
    expect((await tipRow(HREF))?.tip).toBe("");
  });

  it.each([
    ["invalid json", "{x"],
    ["missing href", { tip: "t" }],
    ["non-url href", { href: "not a url", tip: "t" }],
    ["missing tip", { href: HREF }],
    ["non-string tip", { href: HREF, tip: 4 }],
    ["oversized tip", { href: HREF, tip: "x".repeat(MAX_TIP_LENGTH + 1) }],
    ["tip with a line break", { href: HREF, tip: "first line\nsecond line" }],
    ["tip with a carriage return", { href: HREF, tip: "first line\rsecond line" }],
    ["tip with a unicode line separator", { href: HREF, tip: `first line${String.fromCodePoint(0x2028)}second line` }],
    ["tip with a link", { href: HREF, tip: "see https://example.com/x" }],
    ["tip with a link to an allowed host", { href: HREF, tip: "see https://codeforces.com/blog" }],
    ["tip with a bare domain", { href: HREF, tip: "join evil.com today" }],
  ])("rejects %s with 400", async (_name, body) => {
    expect((await request("PUT", "/tips", body)).status).toBe(400);
    expect(await tipRow(HREF)).toBeNull();
  });
});

describe("DELETE /tips", () => {
  it("removes a tip override and reports how many rows were deleted", async () => {
    await request("PUT", "/tips", { href: HREF, tip: "x" });
    expect(await (await request("DELETE", "/tips", { href: HREF })).json()).toEqual({ deleted: 1 });
    expect(await tipRow(HREF)).toBeNull();
    expect(await (await request("DELETE", "/tips", { href: HREF })).json()).toEqual({ deleted: 0 });
  });

  it("rejects a body without href", async () => {
    expect((await request("DELETE", "/tips", {})).status).toBe(400);
  });
});

describe("GET /preview", () => {
  const stub = () => {
    const mock = mockFetch();
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mock.fetcher(String(input), init));
  };

  it("lists upcoming reminders with IST send times and rendered text", async () => {
    stub();
    const res = await request("GET", "/preview");
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toMatch(/text\/plain/);
    const body = await res.text();
    const { h, m } = istClock(NOW);
    expect(body.split("\n")[0]).toMatch(new RegExp(`^#1  sends .+, ${formatTime(h, m)} IST$`));
    expect(body).toContain(formatReminder(TARGET, tipRules, baseOverrides));
    expect(body.match(/^#\d+ /gm)!.length).toBeLessThanOrEqual(PREVIEW_LIMIT);
  });

  it("reflects stored tip overrides", async () => {
    stub();
    await request("PUT", "/tips", { href: HREF, tip: "bring snacks" });
    expect(await (await request("GET", "/preview")).text()).toContain("📌 Tip: bring snacks");
  });

  it("says so when nothing is scheduled", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("x", { status: 500 }));
    expect(await (await request("GET", "/preview")).text()).toBe("No upcoming reminders.\n");
  });

  it("covers at most the next PREVIEW_LIMIT reminders", async () => {
    const many = Array.from({ length: PREVIEW_LIMIT + 4 }, (_, i) => ({
      ...TARGET,
      href: `${TARGET.href}-${i}`,
      start: isoUtc(NOW + (rules.leadMinutes + 60 * (i + 1)) * MIN).slice(0, 19),
    }));
    const mock = mockFetch({ clist: () => new Response(JSON.stringify({ objects: many }), { status: 200 }) });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => mock.fetcher(String(input), init));
    const body = await (await request("GET", "/preview")).text();
    expect(body.match(/^#\d+ /gm)).toHaveLength(PREVIEW_LIMIT);
  });
});
