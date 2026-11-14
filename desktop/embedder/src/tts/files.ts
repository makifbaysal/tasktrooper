/**
 * The Turkish voice's files, pinned by SHA-256. The one list both sides read:
 * `scripts/build-embedder.mjs` downloads, verifies and stages exactly these
 * into `dist/embedder/voice/`, and `index.ts` checks they are there before it
 * offers `/v1/audio/speech`.
 *
 * Bundled with the app rather than downloaded at run time, so the voice works
 * offline from the first launch. They come from a release of this repository
 * rather than the upstream model hub because upstream publishes PyTorch
 * checkpoints only; `desktop/voice-models/README.md` is how they were made.
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

export const VOICE_FILES: readonly VoiceFile[] = [
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

/** Written beside the files as NOTICE.txt: both upstreams are Apache-2.0, which asks for attribution where they ship. */
export const VOICE_NOTICE = `Voice model files for TaskTrooper's Turkish voice.

ema_text.onnx, ema_sound.onnx, ema_decoder.onnx, ema.json
  Converted from EMA Lightning by Canberk Aslan, Apache License 2.0.
  https://huggingface.co/canberkkkkkk/ema-lightning (revision e7c96f2f)
  https://github.com/canberk7/ema-lightning
  The weights are unchanged; only the format was converted to ONNX.

normalizer.wasm
  normalizer-tr by Erdem Tuna, Apache License 2.0, compiled to WebAssembly.
  https://github.com/erdemtuna/normalizer-tr (commit 62264b76)

How these were produced: desktop/voice-models/README.md in this repository.
`;
