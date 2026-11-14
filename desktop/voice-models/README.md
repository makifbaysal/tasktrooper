# Voice models

The Turkish voice (EMA Lightning) and the English voice (Kokoro) ship inside
the app. Nothing here is built by `npm run build`: these are the steps that
produced the files the build downloads, pinned by SHA-256 in
`embedder/src/tts/files.ts`, from the `models-voice-1` release of this
repository. The release also holds `NOTICE.txt`, the attribution the build
writes beside the files (`VOICE_NOTICE` in the same file).

| File | From |
|---|---|
| `ema_text.onnx`, `ema_sound.onnx`, `ema_decoder.onnx`, `ema.json` | `export_ema.py` |
| `normalizer.wasm` | `normalizer-wasm/` |
| `kokoro_v1_q8.onnx`, `kokoro.json`, `kokoro_<style>.bin` | `prepare_kokoro.py` |
| `misaki_us.json.gz`, `misaki_gb.json.gz` | `prepare_kokoro.py` |

Everything here is Apache-2.0. Nothing GPL is used, at build time or at run
time: in particular not espeak-ng, the phonemizer Kokoro's own pipeline falls
back on.

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

## Kokoro-82M (English)

Kokoro-82M v1.0 (Apache-2.0) is by hexgrad:
<https://huggingface.co/hexgrad/Kokoro-82M>. onnx-community publishes it as
ONNX with its voice styles: <https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX>.
The app ships that export's 8-bit model (`model_quantized.onnx`, 92 MB),
unchanged but renamed, and seven styles: `af_heart` (the default),
`af_bella`, `af_nicole`, `am_michael`, `am_fenrir`, `bf_emma`, `bm_george`.
A style file is 510 rows of 256 floats; a pass of n phonemes reads row n.

Why the 8-bit model: on onnxruntime-web's wasm CPU backend it is a third the
size of fp32 and transcribes back the same (Apple's recognizer, every style);
fp32 runs about 1.7 times as fast but is 326 MB, fp16 is 163 MB, and `q8f16`
saves 6 MB for a slower load.

Kokoro reads phonemes, not text. The phonemes are misaki's (hexgrad,
Apache-2.0, <https://github.com/hexgrad/misaki>), the frontend Kokoro was
trained with: its American and British dictionaries ship as
`misaki_us.json.gz` and `misaki_gb.json.gz`, and `embedder/src/tts/english.ts`
ports its lookup (stress, `-s`/`-ed`/`-ing` stems, numbers, the
context-dependent "the", "to", "a"). What misaki gets from spaCy and from
espeak-ng is replaced: heteronyms take their default reading, and a word in
neither dictionary is spelled when it reads as an acronym, split into known
words when it is a compound, or else read by letter-to-sound rules. On the
sentences in `embedder/src/tts/testdata/english.json`, the port and misaki
(with spaCy) write the same phonemes.

```sh
git clone https://github.com/hexgrad/misaki && git -C misaki checkout fba12365
python3 prepare_kokoro.py misaki out    # prints each file's SHA-256 and size
```

## Publishing to the release

The tag starts with `models-`, not `v`, so the release workflow never takes
it for an app release. Files are added to it, never replaced in place: a
changed file is a new tag and new hashes in `files.ts`. `NOTICE.txt` is the
exception, as it carries no hash.

```sh
gh release upload models-voice-1 -R makifbaysal/tasktrooper out/kokoro_v1_q8.onnx out/kokoro.json out/kokoro_*.bin out/misaki_*.json.gz
gh release upload models-voice-1 -R makifbaysal/tasktrooper NOTICE.txt --clobber
```
