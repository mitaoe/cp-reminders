import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { renderContract } from "../test/contract.ts";

const target = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "testdata", "reminders.json");
writeFileSync(target, `${JSON.stringify(renderContract(), null, 2)}\n`);
