/**
 * English text to the phonemes Kokoro reads: a dictionary G2P ported from
 * misaki's `en.py` (hexgrad/misaki, Apache-2.0), the frontend Kokoro was
 * trained with, minus the two parts this cannot ship: spaCy (no part-of-speech
 * tags here, so heteronyms take their default reading) and the out-of-vocabulary
 * fallback (espeak-ng, which is GPL, or a seq2seq network).
 *
 * In their place, a word in neither dictionary is, in order: spelled letter
 * by letter when it reads as an acronym, split into known words when it is a
 * compound ("webhook", "useState"), and otherwise read by letter-to-sound
 * rules in the style of the NRL report (Elovitz et al., 1976). Numbers, money,
 * times, ordinals and years are read the way misaki reads them through
 * num2words.
 *
 * The output keeps the text's punctuation and spacing, because Kokoro reads
 * its phrasing from them: "Hello, world!" is "həlˈO, wˈɜɹld!".
 */

export type Entry = string | Readonly<Record<string, string | null>>;

/** One accent's dictionaries, as `misaki_<accent>.json.gz` holds them. */
export interface LexiconData {
  gold: Record<string, Entry>;
  silver: Record<string, Entry>;
}

const PRIMARY = "ˈ";
const SECONDARY = "ˌ";
const STRESSES = new Set([PRIMARY, SECONDARY]);
const VOWELS = new Set("AIOQWYaiuæɑɒɔəɛɜɪʊʌᵻ");
const CONSONANTS = new Set("bdfhjklmnpstvwzðŋɡɹɾʃʒʤʧθ");
const PUNCTS = new Set(';:,.!?—…"“”');
const NON_QUOTE_PUNCTS = new Set(";:,.!?—…");
const US_TAUS = new Set("AIOWYiuæɑəɛɪɹʊʌ");
const LEXICON_CHARS = /^[A-Za-z'-]+$/;
const ALPHA = /^\p{L}+$/u;
const DIGITS = /^[0-9]+$/;
const ORDINALS = new Set(["st", "nd", "rd", "th"]);
const SYMBOLS: Record<string, string> = { "%": "percent", "&": "and", "+": "plus", "@": "at" };
const CURRENCIES: Record<string, [string, string]> = { $: ["dollar", "cent"], "£": ["pound", "pence"], "€": ["euro", "cent"] };

/**
 * Words an agent says that neither dictionary has, or has only in a reading
 * that is wrong in a software conversation. Our own transcriptions, in
 * misaki's notation.
 */
const TECH: Record<string, string> = {
  api: "ˌApˌiˈI",
  cli: "sˌiˌɛlˈI",
  ci: "sˌiˈI",
  ui: "jˌuˈI",
  url: "jˌuˌɑɹˈɛl",
  json: "ʤˈAsᵊn",
  yaml: "jˈæmᵊl",
  config: "kˈɑnfɪɡ",
  configs: "kˈɑnfɪɡz",
  dev: "dˈɛv",
  devs: "dˈɛvz",
  kubernetes: "kˌubəɹnˈɛtiz",
  postgres: "pˈOstɡɹɛs",
  wasm: "wˈɑzᵊm",
  onnx: "ˈɑnɪks",
  macos: "mˌækˌOˈɛs",
  oauth: "ˈOˌɔθ",
  readme: "ɹˈɛdmˌi",
  todo: "tˈudˌu",
  iphone: "ˈIfˌOn",
  stdout: "stˌændəɹdˈWt",
  stderr: "stˌændəɹdˈɛɹəɹ",
  prs: "pˌiˈɑɹz",
  repos: "ɹˈipˌOz",
};

/** Read as their expansion rather than letter by letter. */
const ABBREVIATIONS: Record<string, string> = {
  "e.g.": "for example",
  "i.e.": "that is",
  "etc.": "et cetera",
  "vs.": "versus",
  "vs": "versus",
};

interface Context {
  /** Whether the next word starts with a vowel; null at the end of a phrase. */
  futureVowel: boolean | null;
  futureTo: boolean;
}

const END: Context = { futureVowel: null, futureTo: false };

/** misaki's `apply_stress`: stress < -1 strips, -1 demotes, 0..1 adds secondary, >1 adds primary. */
export function applyStress(ps: string, stress: number | null): string {
  if (stress === null) return ps;
  const hasPrimary = ps.includes(PRIMARY);
  const hasAny = [...ps].some((c) => STRESSES.has(c));
  const hasVowel = [...ps].some((c) => VOWELS.has(c));
  if (stress < -1) return ps.replaceAll(PRIMARY, "").replaceAll(SECONDARY, "");
  if (stress === -1 || ((stress === 0 || stress === -0.5) && hasPrimary)) {
    return ps.replaceAll(SECONDARY, "").replaceAll(PRIMARY, SECONDARY);
  }
  if ((stress === 0 || stress === 0.5 || stress === 1) && !hasAny) return hasVowel ? restress(SECONDARY + ps) : ps;
  if (stress >= 1 && !hasPrimary && ps.includes(SECONDARY)) return ps.replaceAll(SECONDARY, PRIMARY);
  if (stress > 1 && !hasAny) return hasVowel ? restress(PRIMARY + ps) : ps;
  return ps;
}

/** Move every stress mark to just before the vowel it stresses. */
function restress(ps: string): string {
  const chars = [...ps];
  const keyed = chars.map((c, i) => ({ c, key: i }));
  chars.forEach((c, i) => {
    if (!STRESSES.has(c)) return;
    const j = chars.findIndex((v, k) => k >= i && VOWELS.has(v));
    if (j !== -1) keyed[i].key = j - 0.5;
  });
  return keyed
    .sort((a, b) => a.key - b.key)
    .map((k) => k.c)
    .join("");
}

const isUpper = (w: string): boolean => w === w.toUpperCase();
const isLower = (w: string): boolean => w === w.toLowerCase();
const capitalize = (w: string): string => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase();

/** misaki's `grow_dictionary`: every lowercase word also answers capitalized, and the other way round. */
function grow(dict: Record<string, Entry>): Map<string, Entry> {
  const out = new Map<string, Entry>();
  for (const [k, v] of Object.entries(dict)) {
    if (k.length < 2) continue;
    if (isLower(k)) {
      if (k !== capitalize(k)) out.set(capitalize(k), v);
    } else if (k === capitalize(k)) {
      out.set(k.toLowerCase(), v);
    }
  }
  for (const [k, v] of Object.entries(dict)) out.set(k, v);
  return out;
}

export class EnglishG2P {
  readonly #golds: Map<string, Entry>;
  readonly #silvers: Map<string, Entry>;
  readonly #british: boolean;

  constructor(lexicon: LexiconData, british: boolean) {
    this.#golds = grow(lexicon.gold);
    this.#silvers = grow(lexicon.silver);
    this.#british = british;
  }

  /** Text in, Kokoro phonemes out: words separated by single spaces, punctuation kept. */
  phonemize(text: string): string {
    const tokens = tokenize(normalize(text));
    let ctx = END;
    for (let i = tokens.length - 1; i >= 0; i--) {
      const tk = tokens[i];
      if (tk.ps === null) tk.ps = this.#token(tk.text, ctx);
      ctx = nextContext(ctx, tk.ps, tk.text);
    }
    let out = "";
    let wordBefore = false;
    for (const tk of tokens) {
      if (tk.ps === "") {
        if (tk.space && out !== "" && !out.endsWith(" ")) out += " ";
        continue;
      }
      const word = !PUNCT_RUN.test(tk.text);
      if (word && wordBefore && !out.endsWith(" ")) out += " ";
      out += tk.ps + (tk.space ? " " : "");
      wordBefore = word;
    }
    out = out.replace(/ +/g, " ").replace(/ ([;:,.!?…”)])/g, "$1").trim();
    // Kokoro v1.0 was trained on misaki 1.x output, which writes the flap and
    // the glottal stop as T and t.
    return this.#british ? out : out.replaceAll("ɾ", "T").replaceAll("ʔ", "t");
  }

  /** One word-ish token: a number, a word, or a run of them glued by symbols. */
  #token(text: string, ctx: Context): string {
    const money = /^([$£€])(.+)$/.exec(text);
    if (money) return this.#number(money[2], money[1]) ?? "";
    const abbreviation = ABBREVIATIONS[text.toLowerCase()];
    if (abbreviation) return this.#phrase(abbreviation, ctx);
    const whole = this.#word(text, ctx);
    if (whole !== null) return whole;
    const parts = split(text);
    if (parts.length === 1 && parts[0].text === text) return this.#fallback(text, ctx);
    const words: string[] = [];
    let c = ctx;
    let glue = false;
    for (let i = parts.length - 1; i >= 0; i--) {
      const { text: part, glued } = parts[i];
      const spoken = SPOKEN_SEPARATORS[part];
      const ps = spoken !== undefined ? this.#phrase(spoken, c) : (this.#word(part, c) ?? this.#fallback(part, c));
      c = nextContext(c, ps, part);
      if (ps === "") continue;
      // camelCase humps are one word said as a compound: one primary stress.
      if (glue && words.length > 0) words[0] = ps + applyStress(words[0], -1);
      else words.unshift(ps);
      glue = glued;
    }
    return words.join(" ");
  }

  #phrase(words: string, ctx: Context): string {
    const out: string[] = [];
    let c = ctx;
    for (const w of words.split(" ").reverse()) {
      const ps = this.#word(w, c) ?? "";
      out.unshift(ps);
      c = nextContext(c, ps, w);
    }
    return out.join(" ");
  }

  /** misaki's `Lexicon.__call__`: the dictionaries, their stems, or a number; null when none of them knows it. */
  #word(raw: string, ctx: Context): string | null {
    const word = raw.replaceAll("‘", "'").replaceAll("’", "'").normalize("NFKC");
    const tech = TECH[word.toLowerCase()];
    if (tech && !this.#golds.has(word)) return tech;
    const stress = isLower(word) ? null : isUpper(word) ? 2 : 0.5;
    const ps = this.#getWord(word, stress, ctx);
    if (ps !== null) return ps;
    if (isNumber(word)) return this.#number(word, null);
    return null;
  }

  #fallback(word: string, ctx: Context): string {
    const letters = word.replace(/[^\p{L}]/gu, "");
    if (letters === "") return "";
    if (!LEXICON_CHARS.test(letters)) {
      const ascii = letters.normalize("NFKD").replace(/\p{M}/gu, "");
      if (ascii !== letters && LEXICON_CHARS.test(ascii)) return this.#word(ascii, ctx) ?? this.#fallback(ascii, ctx);
      return "";
    }
    if (looksLikeAcronym(letters)) return this.#spell(letters) ?? "";
    return this.#compound(letters.toLowerCase(), ctx) ?? letterToSound(letters.toLowerCase());
  }

  /** The fewest known words, each at least three letters, that make up `word`; at most three. */
  #compound(word: string, ctx: Context): string | null {
    const n = word.length;
    const best: ({ pieces: string[]; shortest: number } | null)[] = Array.from({ length: n + 1 }, () => null);
    best[0] = { pieces: [], shortest: Infinity };
    for (let end = 3; end <= n; end++) {
      for (let start = 0; start + 3 <= end; start++) {
        const before = best[start];
        if (!before || before.pieces.length >= 3) continue;
        const piece = word.slice(start, end);
        if (!this.#known(piece)) continue;
        const candidate = { pieces: [...before.pieces, piece], shortest: Math.min(before.shortest, piece.length) };
        const current = best[end];
        if (
          !current ||
          candidate.pieces.length < current.pieces.length ||
          (candidate.pieces.length === current.pieces.length && candidate.shortest > current.shortest)
        ) {
          best[end] = candidate;
        }
      }
    }
    const found = best[n];
    if (!found || found.pieces.length < 2) return null;
    const out: string[] = [];
    let c = ctx;
    for (const [i, piece] of [...found.pieces.entries()].reverse()) {
      const ps = this.#getWord(piece, null, c) ?? "";
      out.unshift(i === 0 ? ps : applyStress(ps, -1));
      c = nextContext(c, ps, piece);
    }
    return out.join("");
  }

  #known(piece: string): boolean {
    return this.#golds.has(piece) || this.#silvers.has(piece) || this.#stemS(piece, null, END) !== null;
  }

  #spell(word: string): string | null {
    const ps: string[] = [];
    for (const ch of word) {
      if (!/\p{L}/u.test(ch)) continue;
      const p = this.#golds.get(ch.toUpperCase());
      if (typeof p !== "string") return null;
      ps.push(p);
    }
    const joined = applyStress(ps.join(""), 0);
    const at = joined.lastIndexOf(SECONDARY);
    return at === -1 ? joined : joined.slice(0, at) + PRIMARY + joined.slice(at + 1);
  }

  #specialCase(word: string, ctx: Context): string | null {
    if (SYMBOLS[word]) return this.#lookup(SYMBOLS[word], null, ctx);
    const inner = word.replace(/^\.+|\.+$/g, "");
    if (inner.includes(".") && ALPHA.test(word.replaceAll(".", "")) && Math.max(...word.split(".").map((s) => s.length)) < 3) {
      return this.#spell(word);
    }
    switch (word) {
      case "a":
      case "A":
        return "ɐ";
      case "am":
      case "Am":
      case "AM":
        if (ctx.futureVowel === null || word !== "am") return entryString(this.#golds.get("am"));
        return "ɐm";
      case "an":
      case "An":
      case "AN":
        return "ɐn";
      case "I":
        return `${SECONDARY}I`;
      case "to":
      case "To":
      case "TO":
        return ctx.futureVowel === null ? entryString(this.#golds.get("to")) : ctx.futureVowel ? "tʊ" : "tə";
      case "in":
      case "In":
      case "IN":
        return (ctx.futureVowel === null ? PRIMARY : "") + "ɪn";
      case "the":
      case "The":
      case "THE":
        return ctx.futureVowel === true ? "ði" : "ðə";
      case "used":
      case "Used":
      case "USED": {
        const used = this.#golds.get("used");
        if (typeof used === "string" || !used) return entryString(used);
        return (ctx.futureTo ? used.VBD : used.DEFAULT) ?? null;
      }
      default:
        return null;
    }
  }

  #isKnown(word: string): boolean {
    if (this.#golds.has(word) || SYMBOLS[word] || this.#silvers.has(word)) return true;
    if (!ALPHA.test(word) || !LEXICON_CHARS.test(word)) return false;
    if (word.length === 1) return true;
    if (isUpper(word) && this.#golds.has(word.toLowerCase())) return true;
    return word.slice(1) === word.slice(1).toUpperCase();
  }

  #lookup(input: string, stress: number | null, ctx: Context | null): string | null {
    let word = input;
    if (isUpper(word) && !this.#golds.has(word)) word = word.toLowerCase();
    let entry = this.#golds.get(word);
    if (entry === undefined) entry = this.#silvers.get(word);
    let ps: string | null | undefined;
    if (entry !== undefined && typeof entry !== "string") {
      ps = ctx && ctx.futureVowel === null && "None" in entry ? entry.None : entry.DEFAULT;
    } else {
      ps = entry;
    }
    if (ps === null || ps === undefined) return this.#spell(word);
    return applyStress(ps, stress);
  }

  #getWord(input: string, stress: number | null, ctx: Context): string | null {
    const special = this.#specialCase(input, ctx);
    if (special !== null) return special;
    let word = input;
    const wl = word.toLowerCase();
    if (
      word.length > 1 &&
      ALPHA.test(word.replaceAll("'", "")) &&
      word !== wl &&
      !this.#golds.has(word) &&
      !this.#silvers.has(word) &&
      (isUpper(word) || word.slice(1) === word.slice(1).toLowerCase()) &&
      (this.#golds.has(wl) || this.#silvers.has(wl) || this.#stemS(wl, stress, ctx) !== null || this.#stemEd(wl, stress, ctx) !== null || this.#stemIng(wl, stress, ctx) !== null)
    ) {
      word = wl;
    }
    if (this.#isKnown(word)) return this.#lookup(word, stress, ctx);
    if (word.endsWith("s'") && this.#isKnown(`${word.slice(0, -2)}'s`)) return this.#lookup(`${word.slice(0, -2)}'s`, stress, ctx);
    if (word.endsWith("'") && this.#isKnown(word.slice(0, -1))) return this.#lookup(word.slice(0, -1), stress, ctx);
    return this.#stemS(word, stress, ctx) ?? this.#stemEd(word, stress, ctx) ?? this.#stemIng(word, stress ?? 0.5, ctx);
  }

  #s(stem: string | null): string | null {
    if (!stem) return null;
    const last = stem.at(-1) ?? "";
    if ("ptkfθ".includes(last)) return `${stem}s`;
    if ("szʃʒʧʤ".includes(last)) return `${stem}${this.#british ? "ɪ" : "ᵻ"}z`;
    return `${stem}z`;
  }

  #stemS(word: string, stress: number | null, ctx: Context): string | null {
    if (word.length < 3 || !word.endsWith("s")) return null;
    let stem: string;
    if (!word.endsWith("ss") && this.#isKnown(word.slice(0, -1))) stem = word.slice(0, -1);
    else if ((word.endsWith("'s") || (word.length > 4 && word.endsWith("es") && !word.endsWith("ies"))) && this.#isKnown(word.slice(0, -2))) {
      stem = word.slice(0, -2);
    } else if (word.length > 4 && word.endsWith("ies") && this.#isKnown(`${word.slice(0, -3)}y`)) stem = `${word.slice(0, -3)}y`;
    else return null;
    return this.#s(this.#lookup(stem, stress, ctx));
  }

  #ed(stem: string | null): string | null {
    if (!stem) return null;
    const last = stem.at(-1) ?? "";
    if ("pkfθʃsʧ".includes(last)) return `${stem}t`;
    if (last === "d") return `${stem}${this.#british ? "ɪ" : "ᵻ"}d`;
    if (last !== "t") return `${stem}d`;
    if (this.#british || stem.length < 2) return `${stem}ɪd`;
    if (US_TAUS.has(stem.at(-2) ?? "")) return `${stem.slice(0, -1)}ɾᵻd`;
    return `${stem}ᵻd`;
  }

  #stemEd(word: string, stress: number | null, ctx: Context): string | null {
    if (word.length < 4 || !word.endsWith("d")) return null;
    let stem: string;
    if (!word.endsWith("dd") && this.#isKnown(word.slice(0, -1))) stem = word.slice(0, -1);
    else if (word.length > 4 && word.endsWith("ed") && !word.endsWith("eed") && this.#isKnown(word.slice(0, -2))) stem = word.slice(0, -2);
    else return null;
    return this.#ed(this.#lookup(stem, stress, ctx));
  }

  #ing(stem: string | null): string | null {
    if (!stem) return null;
    if (this.#british) {
      if ("əː".includes(stem.at(-1) ?? "")) return null;
    } else if (stem.length > 1 && stem.at(-1) === "t" && US_TAUS.has(stem.at(-2) ?? "")) {
      return `${stem.slice(0, -1)}ɾɪŋ`;
    }
    return `${stem}ɪŋ`;
  }

  #stemIng(word: string, stress: number | null, ctx: Context): string | null {
    if (word.length < 5 || !word.endsWith("ing")) return null;
    let stem: string;
    if (word.length > 5 && this.#isKnown(word.slice(0, -3))) stem = word.slice(0, -3);
    else if (this.#isKnown(`${word.slice(0, -3)}e`)) stem = `${word.slice(0, -3)}e`;
    else if (word.length > 5 && /([bcdgklmnprstvxz])\1ing$|cking$/.test(word) && this.#isKnown(word.slice(0, -4))) stem = word.slice(0, -4);
    else return null;
    return this.#ing(this.#lookup(stem, stress, ctx));
  }

  /** misaki's `get_number`, for a whole number token (`is_head`), with `currency` the symbol before it if any. */
  #number(input: string, currency: string | null): string | null {
    const suffixMatch = /[a-z']+$/.exec(input);
    const suffix = suffixMatch ? suffixMatch[0] : null;
    let word = suffix ? input.slice(0, -suffix.length) : input;
    const result: string[] = [];
    if (word.startsWith("-")) {
      result.push(this.#lookup("minus", null, null) ?? "");
      word = word.slice(1);
    }
    const say = (words: string): void => {
      for (const w of words.split(/[^a-z]+/)) {
        if (w === "" || w === "and") continue;
        result.push(this.#lookup(w, w === "point" ? -2 : null, null) ?? "");
      }
    };
    const plain = word.replaceAll(",", "");
    if (DIGITS.test(word) && suffix && ORDINALS.has(suffix)) {
      say(ordinalWords(BigInt(word)));
    } else if (result.length === 0 && word.length === 4 && !(currency && CURRENCIES[currency]) && DIGITS.test(word)) {
      say(yearWords(Number(word)));
    } else if ((word.match(/\./g) ?? []).length > 1) {
      say(
        plain
          .split(".")
          .filter((n) => n !== "")
          .map((n) => cardinalWords(BigInt(n)))
          .join(" point "),
      );
    } else if (currency && CURRENCIES[currency] && isCurrency(word)) {
      const [whole, cents = ""] = plain.split(".");
      const units = CURRENCIES[currency];
      let pairs: [bigint, string][] = [[BigInt(whole || "0"), units[0]]];
      if (cents !== "") pairs.push([BigInt(cents.padEnd(2, "0")), units[1]]);
      if (pairs.length > 1) {
        if (pairs[1][0] === 0n) pairs = pairs.slice(0, 1);
        else if (pairs[0][0] === 0n) pairs = pairs.slice(1);
      }
      pairs.forEach(([n, unit], i) => {
        if (i > 0) result.push(this.#lookup("and", null, null) ?? "");
        say(cardinalWords(n));
        result.push((n !== 1n && unit !== "pence" ? this.#stemS(`${unit}s`, null, END) : this.#lookup(unit, null, null)) ?? "");
      });
    } else if (DIGITS.test(plain)) {
      say(suffix && ORDINALS.has(suffix) ? ordinalWords(BigInt(plain)) : cardinalWords(BigInt(plain)));
    } else if (/^\d*\.\d+$/.test(plain)) {
      const [whole, frac] = plain.split(".");
      const digits = frac.replace(/0+$/, "");
      say(`${whole === "" ? "" : cardinalWords(BigInt(whole))} ${digits === "" ? "" : `point ${[...digits].map((d) => cardinalWords(BigInt(d))).join(" ")}`}`);
    } else {
      return null;
    }
    const spoken = result.filter((p) => p !== "").join(" ");
    if (spoken === "") return null;
    if (suffix === "s" || suffix === "'s") return this.#s(spoken);
    if (suffix === "ed" || suffix === "'d") return this.#ed(spoken);
    if (suffix === "ing") return this.#ing(spoken);
    return spoken;
  }

}

function entryString(entry: Entry | undefined): string | null {
  if (entry === undefined) return null;
  return typeof entry === "string" ? entry : (entry.DEFAULT ?? null);
}

function isCurrency(word: string): boolean {
  const dots = (word.match(/\./g) ?? []).length;
  if (dots === 0) return true;
  if (dots > 1) return false;
  return word.split(".")[1].length < 3;
}

function isNumber(input: string): boolean {
  if (!/[0-9]/.test(input)) return false;
  let word = input;
  for (const s of ["ing", "'d", "ed", "'s", ...ORDINALS, "s"]) {
    if (word.endsWith(s)) {
      word = word.slice(0, -s.length);
      break;
    }
  }
  return [...word].every((c, i) => /[0-9,.]/.test(c) || (i === 0 && c === "-"));
}

/** An all-capitals word, or a short one with no vowel letter in it ("npm", "src"), is read letter by letter. */
function looksLikeAcronym(word: string): boolean {
  if (word.length > 1 && isUpper(word)) return true;
  return word.length <= 5 && !/[aeiouy]/i.test(word);
}

function nextContext(ctx: Context, ps: string | null, text: string): Context {
  let futureVowel = ctx.futureVowel;
  if (ps) {
    for (const c of ps) {
      if (NON_QUOTE_PUNCTS.has(c)) {
        futureVowel = null;
        break;
      }
      if (VOWELS.has(c)) {
        futureVowel = true;
        break;
      }
      if (CONSONANTS.has(c)) {
        futureVowel = false;
        break;
      }
    }
  }
  return { futureVowel, futureTo: text === "to" || text === "To" || text === "TO" };
}

// --- text to tokens ----------------------------------------------------------

interface Token {
  text: string;
  /** Phonemes; null until the word is read, "" for something silent. */
  ps: string | null;
  /** Followed by whitespace. */
  space: boolean;
}

const TYPOGRAPHY: Record<string, string> = {
  "‘": "'",
  "’": "'",
  "ʼ": "'",
  "´": "'",
  "`": "'",
  "„": "“",
  "«": "“",
  "»": "”",
  "–": "—",
  "−": "-",
  " ": " ",
};
const TYPOGRAPHY_RE = new RegExp(`[${Object.keys(TYPOGRAPHY).join("")}]`, "g");

/** Spelled-out forms of what a regex is better at than a tokenizer: times, ranges, am/pm, URLs' schemes. */
function normalize(text: string): string {
  return (
    text
      .normalize("NFKC")
      .replace(TYPOGRAPHY_RE, (c) => TYPOGRAPHY[c] ?? c)
      .replace(/\.{3,}/g, "…")
      .replace(/\bhttps?:\/\//gi, "")
      .replace(/\b(\d{1,2}):(\d{2})\b/g, (_m, h: string, m: string) => {
        const minutes = Number(m);
        if (minutes === 0) return `${Number(h)} o'clock`;
        return minutes < 10 ? `${Number(h)} oh ${minutes}` : `${Number(h)} ${minutes}`;
      })
      .replace(/(\d)\s*([ap])\.?m\.?(?=[\s,;:!?)]|$)/gi, (_m, d: string, ap: string) => `${d} ${ap.toUpperCase()}M`)
      .replace(/(?<![\d-])(\d+)\s?[-—]\s?(\d+)(?![\d-])/g, "$1 to $2")
      // A dash between words is a pause; a lone hyphen between spaces is one too.
      .replace(/\s+-\s+/g, " — ")
  );
}

const PUNCT_RUN = /^[;:,.!?—…"“”()]+$/;
const PIECE = /[$£€]?-?[0-9](?:[0-9]|,(?=[0-9]{3}))*(?:\.[0-9]+)*(?:st|nd|rd|th|s|'s|ing|ed|'d)?(?![0-9])|[\p{L}\p{M}\p{N}][\p{L}\p{M}\p{N}'.\-_/@+&]*|[;:,.!?—…"“”()]|[%&+@]|\S/gu;

export function tokenize(text: string): Token[] {
  const tokens: Token[] = [];
  for (const m of text.matchAll(PIECE)) {
    let piece = m[0];
    const end = (m.index ?? 0) + piece.length;
    const space = end < text.length && /\s/.test(text[end]);
    // A sentence's last word keeps its period apart, unless it is part of an
    // abbreviation the reader knows ("e.g.", "U.S.").
    let trailing = "";
    while (/[.\-_/@+&]$/.test(piece) && !ABBREVIATIONS[piece.toLowerCase()] && !/^(?:\p{L}\.){2,}$/u.test(piece) && piece.length > 1) {
      trailing = piece.slice(-1) + trailing;
      piece = piece.slice(0, -1);
    }
    const money = /^([$£€])(.+)$/.exec(piece);
    if (money) {
      tokens.push({ text: piece, ps: null, space: trailing === "" && space });
    } else if (PUNCT_RUN.test(piece)) {
      tokens.push({ text: piece, ps: piece, space });
      continue;
    } else if (SYMBOLS[piece]) {
      tokens.push({ text: piece, ps: null, space });
      continue;
    } else if (/^-?[\p{L}\p{M}\p{N}]/u.test(piece)) {
      tokens.push({ text: piece, ps: null, space: trailing === "" && space });
    } else {
      tokens.push({ text: piece, ps: "", space });
      continue;
    }
    for (const [i, c] of [...trailing].entries()) {
      tokens.push({ text: c, ps: PUNCTS.has(c) ? c : "", space: i === trailing.length - 1 && space });
    }
  }
  return tokens;
}

const SPOKEN_SEPARATORS: Record<string, string> = { ".": "dot", "@": "at", "+": "plus", "&": "and" };

/**
 * A token that is not one word: at the symbols inside it (spoken where they
 * have a name, silent otherwise), between letters and digits, and at
 * camelCase humps, the way misaki's subtokenizer cuts.
 */
export function split(text: string): { text: string; glued: boolean }[] {
  const out: { text: string; glued: boolean }[] = [];
  const pieces = /[0-9]+(?:[.,][0-9]+)*(?:st|nd|rd|th)?|[.@+&]|[-_/']+|[\p{L}\p{M}]+|[^\p{L}\p{M}0-9.@+&\-_/']+/gu;
  for (const [part] of text.matchAll(pieces)) {
    if (SPOKEN_SEPARATORS[part]) {
      out.push({ text: part, glued: false });
    } else if (/^[\p{L}\p{M}]/u.test(part)) {
      const humps = [...part.matchAll(/\p{Lu}+(?=\p{Lu}\p{Ll})|\p{L}*?\p{Ll}(?=\p{Lu})|[\p{L}\p{M}]+/gu)].map((m) => m[0]);
      humps.forEach((hump, i) => out.push({ text: hump, glued: i > 0 && hump.length > 1 && humps[i - 1].length > 1 }));
    } else if (/^[0-9]/.test(part)) {
      out.push({ text: part, glued: false });
    }
  }
  return out;
}

// --- numbers -----------------------------------------------------------------

const ONES = ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"];
const TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];
const SCALES = ["", "thousand", "million", "billion", "trillion", "quadrillion"];

function belowThousand(n: number): string {
  const words: string[] = [];
  if (n >= 100) {
    words.push(`${ONES[Math.floor(n / 100)]} hundred`);
    n %= 100;
  }
  if (n >= 20) {
    words.push(n % 10 === 0 ? TENS[Math.floor(n / 10)] : `${TENS[Math.floor(n / 10)]}-${ONES[n % 10]}`);
  } else if (n > 0) {
    words.push(ONES[n]);
  }
  return words.join(" ");
}

/** num2words' English cardinal, without its "and"s (which misaki drops anyway). Past the quadrillions, digit by digit. */
export function cardinalWords(n: bigint): string {
  if (n < 0n) return `minus ${cardinalWords(-n)}`;
  if (n === 0n) return "zero";
  if (n >= 1000n ** BigInt(SCALES.length)) return [...n.toString()].map((d) => ONES[Number(d)]).join(" ");
  const groups: string[] = [];
  let rest = n;
  let scale = 0;
  while (rest > 0n) {
    const group = Number(rest % 1000n);
    if (group > 0) groups.unshift(`${belowThousand(group)}${SCALES[scale] ? ` ${SCALES[scale]}` : ""}`);
    rest /= 1000n;
    scale++;
  }
  return groups.join(" ");
}

const ORDINAL_IRREGULAR: Record<string, string> = { one: "first", two: "second", three: "third", five: "fifth", eight: "eighth", nine: "ninth", twelve: "twelfth" };

export function ordinalWords(n: bigint): string {
  const words = cardinalWords(n);
  const m = /([a-z]+)$/.exec(words);
  if (!m) return words;
  const last = m[1];
  const ordinal = ORDINAL_IRREGULAR[last] ?? (last.endsWith("y") ? `${last.slice(0, -1)}ieth` : `${last}th`);
  return words.slice(0, -last.length) + ordinal;
}

/** num2words' `to="year"`: 1999 is "nineteen ninety-nine", 1905 "nineteen oh-five", 2005 "two thousand five". */
export function yearWords(n: number): string {
  const high = Math.floor(n / 100);
  const low = n % 100;
  if (high === 0 || (high % 10 === 0 && low < 10) || high >= 100) return cardinalWords(BigInt(n));
  const lowText = low === 0 ? "hundred" : low < 10 ? `oh-${cardinalWords(BigInt(low))}` : cardinalWords(BigInt(low));
  return `${cardinalWords(BigInt(high))} ${lowText}`;
}

// --- letter-to-sound ---------------------------------------------------------

/**
 * [left context, letters, right context, phonemes], tried in order for the
 * letter at hand; the first whose letters and contexts match wins. Contexts
 * use the NRL report's symbols: `#` one or more vowels, `.` a voiced
 * consonant, `^` one consonant, `:` zero or more consonants, `+` e, i or y,
 * `%` a suffix (e, er, es, ed, ing, ely) at the end, and a space for the edge
 * of the word. The rules and their phonemes are this project's, not the
 * report's.
 */
type Rule = readonly [string, string, string, string];
const RULES: Record<string, readonly Rule[]> = {
  a: [
    [" ", "a", " ", "ɐ"],
    ["", "ation", "", "Aʃən"],
    ["", "arr", "", "æɹ"],
    ["", "are", " ", "ɛɹ"],
    ["", "ar", "#", "ɛɹ"],
    ["", "ar", "", "ɑɹ"],
    ["", "air", "", "ɛɹ"],
    ["", "ai", "", "A"],
    ["", "ay", "", "A"],
    ["", "au", "", "ɔ"],
    ["", "aw", "", "ɔ"],
    ["", "all", "", "ɔl"],
    ["", "alk", "", "ɔk"],
    ["", "a", "^%", "A"],
    [":", "a", " ", "ə"],
    ["", "a", "", "æ"],
  ],
  b: [
    ["", "bb", "", "b"],
    ["m", "b", " ", ""],
    ["", "b", "", "b"],
  ],
  c: [
    ["", "ch", "", "ʧ"],
    ["", "ck", "", "k"],
    ["", "cc", "+", "ks"],
    ["", "c", "+", "s"],
    ["", "c", "", "k"],
  ],
  d: [
    ["", "dg", "", "ʤ"],
    ["", "dd", "", "d"],
    ["", "d", "", "d"],
  ],
  e: [
    ["t", "ed", " ", "ᵻd"],
    ["d", "ed", " ", "ᵻd"],
    ["#:", "ed", " ", "d"],
    ["&", "es", " ", "ᵻz"],
    ["#:", "e", "s ", ""],
    ["#:", "e", " ", ""],
    ["", "e", " ", "i"],
    ["", "eer", "", "ɪɹ"],
    ["", "ear", "", "ɪɹ"],
    ["", "ee", "", "i"],
    ["", "ea", "", "i"],
    ["", "ew", "", "u"],
    ["", "ey", " ", "i"],
    ["", "ei", "", "A"],
    ["", "eu", "", "ju"],
    ["", "er", "#", "ɛɹ"],
    ["", "er", "", "əɹ"],
    ["", "e", "^%", "i"],
    ["", "e", "", "ɛ"],
  ],
  f: [
    ["", "ff", "", "f"],
    ["", "f", "", "f"],
  ],
  g: [
    [" ", "gh", "", "ɡ"],
    ["", "gh", "", ""],
    ["", "gg", "", "ɡ"],
    ["", "gn", " ", "n"],
    ["", "g", "e ", "ʤ"],
    ["", "g", "+", "ʤ"],
    ["", "g", "", "ɡ"],
  ],
  h: [
    ["", "h", "#", "h"],
    ["", "h", "", ""],
  ],
  i: [
    ["", "igh", "", "I"],
    ["", "ies", " ", "iz"],
    ["", "ie", " ", "I"],
    ["", "ie", "", "i"],
    ["", "ir", "#", "Iɹ"],
    ["", "ir", "", "ɜɹ"],
    ["", "i", "nd", "I"],
    ["", "i", "ld", "I"],
    ["", "i", "^%", "I"],
    [" ", "i", " ", "I"],
    ["", "i", "", "ɪ"],
  ],
  j: [["", "j", "", "ʤ"]],
  k: [
    [" ", "kn", "", "n"],
    ["", "k", "", "k"],
  ],
  l: [
    ["^", "le", " ", "ᵊl"],
    ["", "ll", "", "l"],
    ["", "l", "", "l"],
  ],
  m: [
    ["", "mm", "", "m"],
    ["", "m", "", "m"],
  ],
  n: [
    ["", "ng", "", "ŋ"],
    ["", "nk", "", "ŋk"],
    ["", "nn", "", "n"],
    ["", "n", "", "n"],
  ],
  o: [
    ["", "ook", "", "ʊk"],
    ["", "oo", "", "u"],
    ["", "oa", "", "O"],
    ["", "oi", "", "Y"],
    ["", "oy", "", "Y"],
    ["", "ou", "", "W"],
    ["", "ow", "", "O"],
    ["", "or", "", "ɔɹ"],
    ["", "old", "", "Old"],
    ["", "o", "^%", "O"],
    ["", "o", " ", "O"],
    ["", "o", "", "ɑ"],
  ],
  p: [
    ["", "ph", "", "f"],
    ["", "pp", "", "p"],
    ["", "p", "", "p"],
  ],
  q: [
    ["", "qu", "", "kw"],
    ["", "q", "", "k"],
  ],
  r: [
    ["", "rr", "", "ɹ"],
    ["", "r", "", "ɹ"],
  ],
  s: [
    ["", "sh", "", "ʃ"],
    ["", "sion", "", "ʒən"],
    ["", "ss", "", "s"],
    [".", "s", " ", "z"],
    ["#:.e", "s", " ", "z"],
    ["#", "s", "#", "z"],
    ["", "s", "", "s"],
  ],
  t: [
    ["", "tion", "", "ʃən"],
    ["", "tch", "", "ʧ"],
    ["", "th", "", "θ"],
    ["", "tt", "", "t"],
    ["", "t", "", "t"],
  ],
  u: [
    ["", "ur", "", "ɜɹ"],
    ["", "ue", " ", "u"],
    ["", "ui", "", "u"],
    ["", "u", "^%", "u"],
    ["", "u", "", "ʌ"],
  ],
  v: [["", "v", "", "v"]],
  w: [
    [" ", "wr", "", "ɹ"],
    ["", "wh", "", "w"],
    ["", "w", "", "w"],
  ],
  x: [
    [" ", "x", "", "z"],
    ["", "x", "", "ks"],
  ],
  y: [
    [" ", "y", "#", "j"],
    ["#:^", "y", " ", "i"],
    [" :", "y", " ", "I"],
    ["", "y", "^%", "I"],
    ["", "y", "", "ɪ"],
  ],
  z: [
    ["", "zz", "", "z"],
    ["", "z", "", "z"],
  ],
};

const LTS_VOWELS = new Set("aeiouy");
const VOICED = new Set("bdvgjlmnrwz");
const SIBILANTS = ["ch", "sh", "s", "c", "g", "z", "x", "j"];
const SUFFIXES = ["ely", "ing", "er", "es", "ed", "e"];

/** Does `pattern` match `word` going right from `at`? Returns where it stopped, or -1. */
function matchRight(pattern: string, word: string, at: number): number {
  let i = at;
  for (const p of pattern) {
    if (p === " ") {
      if (i < word.length) return -1;
    } else if (p === "#") {
      if (i >= word.length || !LTS_VOWELS.has(word[i])) return -1;
      while (i < word.length && LTS_VOWELS.has(word[i])) i++;
    } else if (p === ".") {
      if (i >= word.length || !VOICED.has(word[i])) return -1;
      i++;
    } else if (p === "^") {
      if (i >= word.length || LTS_VOWELS.has(word[i])) return -1;
      i++;
    } else if (p === ":") {
      while (i < word.length && !LTS_VOWELS.has(word[i])) i++;
    } else if (p === "+") {
      if (i >= word.length || !"eiy".includes(word[i])) return -1;
      i++;
    } else if (p === "%") {
      const s = SUFFIXES.find((x) => word.startsWith(x, i) && i + x.length === word.length);
      if (!s) return -1;
      i += s.length;
    } else {
      if (word[i] !== p) return -1;
      i++;
    }
  }
  return i;
}

function matchLeft(pattern: string, word: string, at: number): boolean {
  let i = at - 1;
  for (const p of [...pattern].reverse()) {
    if (p === " ") {
      if (i >= 0) return false;
    } else if (p === "#") {
      if (i < 0 || !LTS_VOWELS.has(word[i])) return false;
      while (i >= 0 && LTS_VOWELS.has(word[i])) i--;
    } else if (p === ".") {
      if (i < 0 || !VOICED.has(word[i])) return false;
      i--;
    } else if (p === "^") {
      if (i < 0 || LTS_VOWELS.has(word[i])) return false;
      i--;
    } else if (p === ":") {
      while (i >= 0 && !LTS_VOWELS.has(word[i])) i--;
    } else if (p === "&") {
      const s = SIBILANTS.find((x) => i - x.length + 1 >= 0 && word.slice(i - x.length + 1, i + 1) === x);
      if (!s) return false;
      i -= s.length;
    } else {
      if (word[i] !== p) return false;
      i--;
    }
  }
  return true;
}

/** The last resort for a word nothing else could read: rules, then primary stress on its first vowel. */
export function letterToSound(word: string): string {
  let out = "";
  let i = 0;
  while (i < word.length) {
    const rules = RULES[word[i]];
    let matched = false;
    for (const [left, letters, right, ps] of rules ?? []) {
      if (!word.startsWith(letters, i)) continue;
      if (!matchLeft(left, word, i)) continue;
      if (matchRight(right, word, i + letters.length) === -1) continue;
      out += ps;
      i += letters.length;
      matched = true;
      break;
    }
    if (!matched) i++;
  }
  const first = [...out].findIndex((c) => VOWELS.has(c));
  if (first === -1) return out;
  const chars = [...out];
  chars.splice(first, 0, PRIMARY);
  return chars.join("");
}
