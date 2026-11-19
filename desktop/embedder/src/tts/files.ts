/**
 * The voices' files, pinned by SHA-256. The one list both sides read:
 * `scripts/build-embedder.mjs` downloads, verifies and stages exactly these
 * into `dist/embedder/voice/`, and `index.ts` checks they are there before it
 * offers a voice on `/v1/audio/speech`.
 *
 * Bundled with the app rather than downloaded at run time, so the voice works
 * offline from the first launch. EMA's come from a release of this repository
 * because its upstream publishes PyTorch checkpoints only;
 * `desktop/voice-models/README.md` is how they were made. The tag is never
 * moved: a new export is a new tag and new hashes here. tiny-voca publishes
 * the exact file it runs, so that one comes from its hub at a fixed revision.
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

const TINYVOCA_REVISION = "1e73262226dbf4ef02e9df277e81aaa1db5ce16c";
const TINYVOCA_HUB = `https://huggingface.co/n4ze3m/tiny-voca/resolve/${TINYVOCA_REVISION}`;

/** The model, its phonemizer's dictionary and tables, and its config, in one GGUF file (`js/pack.mjs`'s). */
export const TINYVOCA_MODEL = "tiny-voca-js.gguf";
/** The CMU Pronouncing Dictionary's BSD license, which the phonemizer's dictionary comes under and has to travel with it. */
export const CMUDICT_LICENSE = "LICENSE-cmudict.txt";

/** tiny-voca's two voices, by its own names: F2 female, M2 male. */
export const TINYVOCA_STYLES: readonly string[] = ["F2", "M2"];
export const TINYVOCA_DEFAULT_STYLE = "F2";

/** tiny-voca, English only; fetched from its own model hub at a pinned revision, unchanged. */
export const TINYVOCA_FILES: readonly VoiceFile[] = [
  {
    name: TINYVOCA_MODEL,
    url: `${TINYVOCA_HUB}/gguf/${TINYVOCA_MODEL}`,
    sha256: "8436ad1c9b57c3533c925588a1b3563f906079a89c081ac0bec9f31ad6e28653",
    sizeBytes: 7_988_384,
  },
  {
    name: CMUDICT_LICENSE,
    url: `${TINYVOCA_HUB}/phonemizer/LICENSE.cmudict`,
    sha256: "bd4ce8e44170a5f9f481310ca85c51de3c4f851a65e679b40e603b143bd3542a",
    sizeBytes: 1_754,
  },
];

/** Everything the build stages into `voice/`, each file once. */
export const VOICE_FILES: readonly VoiceFile[] = [...EMA_FILES, ...TINYVOCA_FILES].filter(
  (file, i, all) => all.findIndex((f) => f.name === file.name) === i,
);

/** License texts the build appends to NOTICE.txt in full, in this order. */
export const VOICE_LICENSES: readonly string[] = [CMUDICT_LICENSE];

/**
 * Written beside the files as NOTICE.txt, followed by the full text of each
 * of VOICE_LICENSES (the build appends them). Apache-2.0 asks for attribution
 * where the files ship; the CMU dictionary's BSD license for its notice.
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

tiny-voca-js.gguf
  tiny-voca by n4ze3m, Apache License 2.0: the model (8-bit, as published)
  and the tables of its phonemizer, dizione.
  https://huggingface.co/n4ze3m/tiny-voca (revision 1e732622)
  https://github.com/n4ze3m/tiny-voca
  The file is unchanged. Its pronunciation dictionary is derived from the
  CMU Pronouncing Dictionary (LICENSE-cmudict.txt, and in full at the end of
  this file) and its homograph data is under the Apache License 2.0.

How these were produced: desktop/voice-models/README.md in this repository.
`;
