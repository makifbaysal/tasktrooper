/**
 * The voices' files, pinned by SHA-256. The one list both sides read:
 * `scripts/build-embedder.mjs` downloads, verifies and stages exactly these
 * into `dist/embedder/voice/`, and `index.ts` checks they are there before it
 * offers a voice on `/v1/audio/speech`.
 *
 * Bundled with the app rather than downloaded at run time, so the voice works
 * offline from the first launch. They come from a release of this repository
 * rather than the upstream model hub because EMA's upstream publishes PyTorch
 * checkpoints only, and so that every file the build fetches comes from one
 * place under one tag; `desktop/voice-models/README.md` is how they were made.
 * The tag is never moved: a new export is a new tag and new hashes here.
 *
 * No imports, on purpose: the build script loads this file on its own.
 */

export const VOICE_DIR_NAME = "voice";

const RELEASE = "https://github.com/makifbaysal/tasktrooper/releases/download/models-voice-1";

export interface VoiceFile {
  name: string;
  url: string;
  sha256: string;
  sizeBytes: number;
}

export const TURKISH_FILES: readonly VoiceFile[] = [
  {
    name: "ema.json",
    url: `${RELEASE}/ema.json`,
    sha256: "5b4ea5d874e3cac190f8cd048144fa5b29835848c4e5a823f7965e43be72307b",
    sizeBytes: 350,
  },
  {
    name: "ema_text.onnx",
    url: `${RELEASE}/ema_text.onnx`,
    sha256: "2db0589acd183afa43aa6ed5b5a68d750625a860158fd36c9aab91a953b1466c",
    sizeBytes: 4_808_210,
  },
  {
    name: "ema_sound.onnx",
    url: `${RELEASE}/ema_sound.onnx`,
    sha256: "5d4a004d1c3296c03eb4e7fe13dd47b7976870d6000a736d6679b807c73496f1",
    sizeBytes: 18_135_494,
  },
  {
    name: "ema_decoder.onnx",
    url: `${RELEASE}/ema_decoder.onnx`,
    sha256: "869aa1ec1f58971362f24cb0a9e44fe1cc2e827d6057d96fa588ecebfbdd0dd9",
    sizeBytes: 12_032_047,
  },
  {
    name: "normalizer.wasm",
    url: `${RELEASE}/normalizer.wasm`,
    sha256: "77c1f15adfbe2b4f7fb8f468aedc5ec5f897f40ee3c144a6b4d370c53dbd8cee",
    sizeBytes: 1_320_572,
  },
];

export const KOKORO_MODEL = "kokoro_v1_q8.onnx";
export const KOKORO_CONFIG = "kokoro.json";

/** The English voice styles bundled, by Kokoro's own names: a/b American/British, f/m female/male. */
export const ENGLISH_STYLES: readonly string[] = ["af_heart", "af_bella", "af_nicole", "am_michael", "am_fenrir", "bf_emma", "bm_george"];
export const ENGLISH_DEFAULT_STYLE = "af_heart";

export function kokoroStyleFile(style: string): string {
  return `kokoro_${style}.bin`;
}

export function misakiLexiconFile(accent: "us" | "gb"): string {
  return `misaki_${accent}.json.gz`;
}

const STYLE_SHA256: Record<string, string> = {
  af_heart: "d583ccff3cdca2f7fae535cb998ac07e9fcb90f09737b9a41fa2734ec44a8f0b",
  af_bella: "f69d836209b78eb8c66e75e3cda491e26ea838a3674257e9d4e5703cbaf55c8b",
  af_nicole: "cd2191ab31b914ed7b318416b0e4440fdf392ddad9106a060819aa600a64f59a",
  am_michael: "1d1f21dd8da39c30705cd4c75d039d265e9bc4a2a93ed09bc9e1b1225eb95ba1",
  am_fenrir: "c27989f741f7ee34d273a39d8a595cc0837d35f5ced9a29b7cc162614616df43",
  bf_emma: "669fe0647f9dd04fcab92f1439a40eeb4c8b4ab1f82e4996fe3d918ce4a63b73",
  bm_george: "c4b235a4c1f2cd3b939fed08b899ce9385638b763f7b73a59616c4fc9bd6c9bc",
};

export const ENGLISH_FILES: readonly VoiceFile[] = [
  {
    name: KOKORO_CONFIG,
    url: `${RELEASE}/${KOKORO_CONFIG}`,
    sha256: "9a58ddb2adc7b7764d15c96c34f1e5bbc283e2f07443cf8cb0f79d46456db947",
    sizeBytes: 946,
  },
  {
    name: KOKORO_MODEL,
    url: `${RELEASE}/${KOKORO_MODEL}`,
    sha256: "fbae9257e1e05ffc727e951ef9b9c98418e6d79f1c9b6b13bd59f5c9028a1478",
    sizeBytes: 92_361_116,
  },
  {
    name: misakiLexiconFile("us"),
    url: `${RELEASE}/${misakiLexiconFile("us")}`,
    sha256: "09cdaad4d8e023aead7e74fdd6069239700d0ede6addbb2330d47abd5322fa4c",
    sizeBytes: 1_401_724,
  },
  {
    name: misakiLexiconFile("gb"),
    url: `${RELEASE}/${misakiLexiconFile("gb")}`,
    sha256: "b3bb63084ffd8534c244d11c24e35cb1c01ba523373272b06a92fa1fe4ee858a",
    sizeBytes: 1_495_541,
  },
  ...ENGLISH_STYLES.map((style) => ({
    name: kokoroStyleFile(style),
    url: `${RELEASE}/${kokoroStyleFile(style)}`,
    sha256: STYLE_SHA256[style],
    sizeBytes: 522_240,
  })),
];

/** Everything the build stages into `voice/`. */
export const VOICE_FILES: readonly VoiceFile[] = [...TURKISH_FILES, ...ENGLISH_FILES];

/** Written beside the files as NOTICE.txt: every upstream is Apache-2.0, which asks for attribution where they ship. */
export const VOICE_NOTICE = `Voice model files for TaskTrooper's Turkish and English voices.

ema_text.onnx, ema_sound.onnx, ema_decoder.onnx, ema.json
  Converted from EMA Lightning by Canberk Aslan, Apache License 2.0.
  https://huggingface.co/canberkkkkkk/ema-lightning (revision e7c96f2f)
  https://github.com/canberk7/ema-lightning
  The weights are unchanged; only the format was converted to ONNX.

normalizer.wasm
  normalizer-tr by Erdem Tuna, Apache License 2.0, compiled to WebAssembly.
  https://github.com/erdemtuna/normalizer-tr (commit 62264b76)

kokoro_v1_q8.onnx, kokoro.json, kokoro_*.bin
  Kokoro-82M v1.0 by hexgrad, Apache License 2.0, in the ONNX export (8-bit
  quantized) and voice styles published by onnx-community.
  https://huggingface.co/hexgrad/Kokoro-82M
  https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX (revision 1939ad2a)
  kokoro.json is that export's tokenizer vocabulary. The files are unchanged.

misaki_us.json.gz, misaki_gb.json.gz
  The American and British English pronunciation dictionaries of misaki by
  hexgrad, Apache License 2.0 (misaki/data/*_gold.json and *_silver.json).
  https://github.com/hexgrad/misaki (commit fba12365)
  Repackaged: each accent's two dictionaries in one gzipped JSON object,
  silver entries already in gold left out.

How these were produced: desktop/voice-models/README.md in this repository.
`;
