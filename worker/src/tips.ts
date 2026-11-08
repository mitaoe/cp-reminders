import type { TipOverrides } from "./core/format.ts";
import { isValidTip } from "./core/text.ts";
import { baseOverrides } from "./config.ts";

export async function loadOverrides(env: Env): Promise<TipOverrides> {
  const { results } = await env.DB.prepare("SELECT href, tip FROM tip_overrides").all<{ href: string; tip: string }>();
  const stored: TipOverrides = {};
  for (const row of results) {
    if (isValidTip(row.tip)) stored[row.href] = row.tip;
    else console.error(`ignoring the stored tip for ${row.href}: not a valid tip`);
  }
  return { ...baseOverrides, ...stored };
}
