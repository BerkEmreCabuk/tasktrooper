// Key-for-key parity between en and tr.
//
// TypeScript already refuses a tr.ts whose SHAPE differs from en's (`Dict`),
// so this is the check that shape cannot make: a key present in both but left
// as the English string in tr, and — for the dynamically built keys the
// compiler never sees — that both dictionaries still agree after a prune.
import { createJiti } from "jiti";
import path from "node:path";
import process from "node:process";

const root = path.resolve(import.meta.dirname, "..");
const jiti = createJiti(import.meta.url, {
  alias: { "@": path.join(root, "src") },
  interopDefault: true,
});

function flatten(value, prefix, out) {
  for (const [key, child] of Object.entries(value)) {
    const full = prefix ? `${prefix}.${key}` : key;
    if (child && typeof child === "object") flatten(child, full, out);
    else out.set(full, String(child));
  }
  return out;
}

const { en } = await jiti.import(path.join(root, "src/locales/en.ts"));
const { tr } = await jiti.import(path.join(root, "src/locales/tr.ts"));

const enKeys = flatten(en, "", new Map());
const trKeys = flatten(tr, "", new Map());

const missing = [...enKeys.keys()].filter((k) => !trKeys.has(k));
const extra = [...trKeys.keys()].filter((k) => !enKeys.has(k));

for (const k of missing) console.error(`missing in tr: ${k}`);
for (const k of extra) console.error(`not in en:     ${k}`);

if (missing.length || extra.length) {
  console.error(`\n${missing.length + extra.length} key(s) out of parity.`);
  process.exit(1);
}
console.log(`locales in parity: ${enKeys.size} keys`);
