import { isoUtc } from "./core/format.ts";
import { MAX_TIP_LENGTH, isValidTip } from "./core/text.ts";
import { badRequest, isRecord, json, readJson, text } from "./http.ts";
import { renderPreview } from "./preview.ts";
import type { Route } from "./router.ts";

export const MAX_HREF_LENGTH = 500;

function parseHref(value: unknown): string | undefined {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_HREF_LENGTH) return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? value : undefined;
  } catch {
    return undefined;
  }
}

export const adminRoutes: Route[] = [
  {
    method: "GET",
    path: "/preview",
    secret: "ADMIN_TOKEN",
    handle: async ({ env, now, fetcher }) => text(await renderPreview(env, now, fetcher)),
  },
  {
    method: "PUT",
    path: "/tips",
    secret: "ADMIN_TOKEN",
    async handle({ request, env, now }) {
      const body = await readJson(request);
      if (!isRecord(body)) return badRequest("body must be a JSON object");
      const href = parseHref(body.href);
      if (href === undefined) return badRequest("href must be an http(s) URL");
      const { tip } = body;
      if (typeof tip !== "string" || !isValidTip(tip)) {
        return badRequest(`tip must be one line of up to ${MAX_TIP_LENGTH} characters without links, domains, IP addresses or phone numbers`);
      }
      await env.DB
        .prepare(
          "INSERT INTO tip_overrides (href, tip, updated_at) VALUES (?1, ?2, ?3) " +
            "ON CONFLICT(href) DO UPDATE SET tip = excluded.tip, updated_at = excluded.updated_at",
        )
        .bind(href, tip, isoUtc(now))
        .run();
      return json({ ok: true });
    },
  },
  {
    method: "DELETE",
    path: "/tips",
    secret: "ADMIN_TOKEN",
    async handle({ request, env }) {
      const body = await readJson(request);
      if (!isRecord(body)) return badRequest("body must be a JSON object");
      const href = parseHref(body.href);
      if (href === undefined) return badRequest("href must be an http(s) URL");
      const result = await env.DB.prepare("DELETE FROM tip_overrides WHERE href = ?1").bind(href).run();
      return json({ deleted: result.meta.changes });
    },
  },
];
