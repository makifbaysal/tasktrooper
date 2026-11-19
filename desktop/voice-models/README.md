# Voice models

Two voice engines ship inside the app: EMA Lightning (Turkish, one voice) and
tiny-voca (English, a female and a male voice), the voice picked per language
in Settings → Voice. Nothing here is built by `npm run build`: the build
downloads the files pinned by SHA-256 in `embedder/src/tts/files.ts`. EMA's
and the normalizer's come from the `models-voice-1` release of this
repository, made by the steps below; tiny-voca's from its own model hub at a
fixed revision. The release also holds `NOTICE.txt`, the attribution the
build writes beside the files (`VOICE_NOTICE` in the same file, followed by
the license texts of `VOICE_LICENSES`).

| File | From |
|---|---|
| `ema_text.onnx`, `ema_sound.onnx`, `ema_decoder.onnx`, `ema.json` | `export_ema.py` |
| `normalizer.wasm` | `normalizer-wasm/` |
| `tiny-voca-js.gguf`, `LICENSE-cmudict.txt` | <https://huggingface.co/n4ze3m/tiny-voca>, unchanged |

The code here and in `embedder/src/tts/` is Apache-2.0, like the rest of the
repository; `embedder/src/tts/tinyvoca/` is upstream tiny-voca's own
Apache-2.0 JavaScript, copied unchanged. The model files are Apache-2.0 too;
tiny-voca's pronunciation dictionary is derived from the CMU Pronouncing
Dictionary, whose BSD license travels with it. Nothing GPL is used, at build
time or at run time (no espeak-ng).

## EMA Lightning → ONNX

EMA Lightning (Apache-2.0) is by Canberk Aslan:
<https://huggingface.co/canberkkkkkk/ema-lightning>, code at
<https://github.com/canberk7/ema-lightning>. Its weights are published as
PyTorch checkpoints only. `export_ema.py` loads them at a pinned revision and
exports its three stages unchanged, batch size one:

- `ema_text`: letter ids → letter features `h` and per-letter durations `dur`.
- `ema_sound`: `h`, `dur`, the frame timeline (`cw`, `wstart`, `fw`, `fp`) and
  four steps of Gaussian noise → 64-dim latents at 25 Hz.
- `ema_decoder`: latents → 48 kHz audio.

The glue between them (word timeline, decode windows) is reimplemented in
`embedder/src/tts/ema.ts`. Against the PyTorch package with the same noise,
the ONNX path matched to 4e-4 on latents and 1.4e-4 on audio.

```sh
git clone https://github.com/canberk7/ema-lightning   # at 68f54a18
uv venv -p 3.12 venv && VIRTUAL_ENV=venv uv pip install torch onnx onnxscript huggingface_hub numpy
mkdir out && venv/bin/python export_ema.py ema-lightning/src out
```

## normalizer-tr → WASM

EMA reads numbers, dates, money and abbreviations through normalizer-tr
(Apache-2.0, Erdem Tuna, <https://github.com/erdemtuna/normalizer-tr>), a Rust
crate. It is compiled as-is:

```sh
rustup target add wasm32-unknown-unknown
cd normalizer-wasm && cargo build --release --target wasm32-unknown-unknown
cp target/wasm32-unknown-unknown/release/normalizer_wasm.wasm ../out/normalizer.wasm
```

## tiny-voca (English)

tiny-voca (Apache-2.0) is by n4ze3m: <https://github.com/n4ze3m/tiny-voca>,
weights at <https://huggingface.co/n4ze3m/tiny-voca>. 4.5M parameters, two
American English voices (`F2` female, the default, and `M2` male), 24 kHz.
The app runs upstream's plain JavaScript port (`js/` in that repository,
vendored in `embedder/src/tts/tinyvoca/`): no ONNX Runtime, one GGUF file of
8 MB holding the 8-bit model and the phonemizer's (dizione's) dictionary,
unknown-word model and tables. It reads about eight times faster than real
time on one core and is ready about 200 ms after the file is read.

`gguf/tiny-voca-js.gguf` and `phonemizer/LICENSE.cmudict` are used as
published, pinned to revision `1e73262226dbf4ef02e9df277e81aaa1db5ce16c`. A
newer one is a new revision and new hashes in `files.ts`, with the vendored
scripts copied from the matching commit.

## Publishing to the release

The tag starts with `models-`, not `v`, so the release workflow never takes
it for an app release. Files are added to it, never replaced in place: a
changed file is a new tag and new hashes in `files.ts`. `NOTICE.txt` is the
exception, as it carries no hash.

```sh
gh release upload models-voice-1 -R makifbaysal/tasktrooper NOTICE.txt --clobber
```

`NOTICE.txt` is what `npm run build:embedder` writes into
`dist/embedder/voice/`: `VOICE_NOTICE`, then the CMU dictionary license.
