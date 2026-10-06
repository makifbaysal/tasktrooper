// Key-for-key parity between en and every other UI language.
//
// TypeScript already refuses a dictionary whose SHAPE differs from en's
// (`Dict`), so this is the check that shape cannot make: for the dynamically
// built keys the compiler never sees, that every dictionary still agrees with
// en after a prune, and that each translation interpolates exactly the
// `{placeholder}` names its English string does (t() leaves an unknown one
// as literal text and silently drops a missing one).
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

function placeholders(text) {
  return [...new Set([...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort().join(", ");
}

const { LANGS } = await jiti.import(path.join(root, "src/lib/languages.ts"));
const { en } = await jiti.import(path.join(root, "src/locales/en.ts"));
const enKeys = flatten(en, "", new Map());

let problems = 0;
for (const lang of LANGS) {
  if (lang === "en") continue;
  const mod = await jiti.import(path.join(root, `src/locales/${lang}.ts`));
  const dict = mod[lang];
  if (!dict) {
    console.error(`src/locales/${lang}.ts does not export \`${lang}\``);
    problems++;
    continue;
  }
  const keys = flatten(dict, "", new Map());
  for (const k of enKeys.keys()) {
    if (!keys.has(k)) {
      console.error(`missing in ${lang}: ${k}`);
      problems++;
    }
  }
  for (const k of keys.keys()) {
    if (!enKeys.has(k)) {
      console.error(`not in en (${lang}): ${k}`);
      problems++;
    }
  }
  for (const [k, text] of keys) {
    const want = enKeys.has(k) ? placeholders(enKeys.get(k)) : undefined;
    if (want !== undefined && placeholders(text) !== want) {
      console.error(`placeholders differ in ${lang}: ${k} — en {${want}}, ${lang} {${placeholders(text)}}`);
      problems++;
    }
  }
}

if (problems) {
  console.error(`\n${problems} locale problem(s).`);
  process.exit(1);
}
console.log(`locales in parity: ${enKeys.size} keys × ${LANGS.length} languages (${LANGS.join(", ")})`);
