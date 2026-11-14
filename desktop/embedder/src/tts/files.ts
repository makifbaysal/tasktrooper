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

const NORMALIZER: VoiceFile = {
  name: "normalizer.wasm",
  url: `${RELEASE}/normalizer.wasm`,
  sha256: "77c1f15adfbe2b4f7fb8f468aedc5ec5f897f40ee3c144a6b4d370c53dbd8cee",
  sizeBytes: 1_320_572,
};

/** EMA Lightning, Turkish only. */
export const EMA_FILES: readonly VoiceFile[] = [
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
  NORMALIZER,
];

export const KOKORO_MODEL = "kokoro_v1_q8.onnx";
export const KOKORO_CONFIG = "kokoro.json";

/** Kokoro's styles bundled, by its own names: a/b American/British, f/m female/male. */
export const KOKORO_STYLES: readonly string[] = ["af_heart", "af_bella", "af_nicole", "am_michael", "am_fenrir", "bf_emma", "bm_george"];
export const KOKORO_DEFAULT_STYLE = "af_heart";

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

/** Kokoro-82M, English only. */
export const KOKORO_FILES: readonly VoiceFile[] = [
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
  ...KOKORO_STYLES.map((style) => ({
    name: kokoroStyleFile(style),
    url: `${RELEASE}/${kokoroStyleFile(style)}`,
    sha256: STYLE_SHA256[style],
    sizeBytes: 522_240,
  })),
];

export const SUPERTONIC_CONFIG = "supertonic_tts.json";
export const SUPERTONIC_INDEXER = "supertonic_unicode_indexer.json";
export const SUPERTONIC_GRAPHS = ["duration_predictor", "text_encoder", "vector_estimator", "vocoder"] as const;
export type SupertonicGraph = (typeof SUPERTONIC_GRAPHS)[number];
/** The OpenRAIL-M license the weights come under, which has to travel with them. */
export const SUPERTONIC_LICENSE = "LICENSE-supertonic.txt";

/** Supertonic's preset voices, by its own names: F female, M male. */
export const SUPERTONIC_STYLES: readonly string[] = ["F1", "F2", "F3", "F4", "F5", "M1", "M2", "M3", "M4", "M5"];
export const SUPERTONIC_DEFAULT_STYLE = "F1";

export function supertonicGraphFile(graph: SupertonicGraph): string {
  return `supertonic_${graph}.onnx`;
}

export function supertonicStyleFile(style: string): string {
  return `supertonic_${style}.json`;
}

const SUPERTONIC_GRAPH_PINS: Record<SupertonicGraph, [string, number]> = {
  duration_predictor: ["5079961d5194fb4c38329021955e515e5faf4743b1d6c45f2a1e9da181dce37f", 1_188_118],
  text_encoder: ["3265749cbf22ccfdf673af99312772890905bd4a6065bd0229ad0f3545ec19f2", 9_701_677],
  vector_estimator: ["172b4b2abfc179575ce78fefc95d97c39e00fc8f2f12f81dfdb7fbe31d6dea87", 65_669_107],
  vocoder: ["a74defcdffd73ef66f8a38dfa4765deacd3f9bfdb10d52762d8611a7390cb6e0", 25_789_755],
};

const SUPERTONIC_STYLE_PINS: Record<string, [string, number]> = {
  F1: ["bbdec6ee00231c2c742ad05483df5334cab3b52fda3ba38e6a07059c4563dbc2", 292_046],
  F2: ["7c722c6a72707b1a77f035d67f0d1351ba187738e06f7683e8c72b1df3477fc6", 292_423],
  F3: ["12f6ef2573baa2defa1128069cb59f203e3ab67c92af77b42df8a0e3a2f7c6ab", 290_794],
  F4: ["c2fa764c1225a76dfc3e2c73e8aa4f70d9ee48793860eb34c295fff01c2e032b", 291_808],
  F5: ["45966e73316415626cf41a7d1c6f3b4c70dbc1ba2bee5c1978ef0ce33244fc8d", 291_479],
  M1: ["e35604687f5d23694b8e91593a93eec0e4eca6c0b02bb8ed69139ab2ea6b0a5b", 291_748],
  M2: ["b76cbf62bac707c710cf0ae5aba5e31eea1a6339a9734bfae33ab98499534a50", 292_055],
  M3: ["ea1ac35ccb91b0d7ecad533a2fbd0eec10c91513d8951e3b25fbba99954e159b", 290_198],
  M4: ["ca8eefad4fcd989c9379032ff3e50738adc547eeb5e221b82593a6d7b3bac303", 291_522],
  M5: ["dd22b92740314321f8ae11c5e87f8dd60d060f15dd3a632b5adf77f471f77af2", 291_469],
};

function pinned(name: string, [sha256, sizeBytes]: [string, number]): VoiceFile {
  return { name, url: `${RELEASE}/${name}`, sha256, sizeBytes };
}

/** Supertonic 3, one model for Turkish and English; Turkish text goes through the normalizer first. */
export const SUPERTONIC_FILES: readonly VoiceFile[] = [
  pinned(SUPERTONIC_CONFIG, ["42078d3aef1cd43ab43021f3c54f47d2d75ceb4e75f627f118890128b06a0d09", 8_253]),
  pinned(SUPERTONIC_INDEXER, ["9bf7346e43883a81f8645c81224f786d43c5b57f3641f6e7671a7d6c493cb24f", 277_676]),
  ...SUPERTONIC_GRAPHS.map((graph) => pinned(supertonicGraphFile(graph), SUPERTONIC_GRAPH_PINS[graph])),
  ...SUPERTONIC_STYLES.map((style) => pinned(supertonicStyleFile(style), SUPERTONIC_STYLE_PINS[style])),
  pinned(SUPERTONIC_LICENSE, ["0d944a9110fed9a9602d60e0423a272903e7bd21ab060490774efc77c2275e9f", 15_007]),
  NORMALIZER,
];

/** Everything the build stages into `voice/`, each file once. */
export const VOICE_FILES: readonly VoiceFile[] = [...EMA_FILES, ...KOKORO_FILES, ...SUPERTONIC_FILES].filter(
  (file, i, all) => all.findIndex((f) => f.name === file.name) === i,
);

/**
 * Written beside the files as NOTICE.txt, followed by the full text of
 * SUPERTONIC_LICENSE (the build appends it). Apache-2.0 asks for attribution
 * where the files ship; OpenRAIL-M also for its use restrictions to be passed
 * on and for changed files to say they were changed.
 */
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

supertonic_*.onnx, supertonic_tts.json, supertonic_unicode_indexer.json,
supertonic_F1..F5.json, supertonic_M1..M5.json
  Supertonic 3 by Supertone Inc., Copyright (c) 2026 Supertone Inc.,
  under the BigScience OpenRAIL-M License (LICENSE-supertonic.txt, and in
  full at the end of this file).
  https://huggingface.co/Supertone/supertonic-3 (revision 3cadd1ee)
  MODIFIED FILES: the four supertonic_*.onnx graphs were changed by
  TaskTrooper: their weights of 4096 values or more were quantized to int8
  per output channel, with DequantizeLinear nodes in front of the layers that
  read them (each graph's metadata says so too). The configuration, the
  character index and the ten voice styles are unchanged.

  USE RESTRICTIONS. The Supertonic model and these derivatives of it are
  subject to paragraph 5 and Attachment A of that license: you may not use
  them for the restricted uses it lists, among them breaking the law, harming
  minors, impersonating others without their consent (for example deepfakes),
  or disseminating machine-generated content without disclosing that it is
  machine-generated. Anyone you give them to is bound by the same
  restrictions, and you must pass them on.

How these were produced: desktop/voice-models/README.md in this repository.
`;
