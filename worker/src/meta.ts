export const setMeta = (db: D1Database, key: string, value: string): D1PreparedStatement =>
  db
    .prepare("INSERT INTO meta (k, v) VALUES (?1, ?2) ON CONFLICT(k) DO UPDATE SET v = excluded.v")
    .bind(key, value);

export const deleteMeta = (db: D1Database, key: string): D1PreparedStatement =>
  db.prepare("DELETE FROM meta WHERE k = ?1").bind(key);

export const allMetaStatement = (db: D1Database): D1PreparedStatement => db.prepare("SELECT k, v FROM meta");

export async function getMeta(db: D1Database, key: string): Promise<string | null> {
  const row = await db.prepare("SELECT v FROM meta WHERE k = ?1").bind(key).first<{ v: string }>();
  return row?.v ?? null;
}

export async function getAllMeta(db: D1Database): Promise<Map<string, string>> {
  const { results } = await allMetaStatement(db).all<{ k: string; v: string }>();
  return new Map(results.map((r) => [r.k, r.v]));
}
