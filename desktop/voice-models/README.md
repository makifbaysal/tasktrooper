# Voice models

The Turkish voice (EMA Lightning) ships inside the app. Nothing here is built
by `npm run build`: these are the steps that produced the files the build
downloads, pinned by SHA-256 in `embedder/src/tts/files.ts`, from the
`models-voice-1` release of this repository.

| File | From |
|---|---|
| `ema_text.onnx`, `ema_sound.onnx`, `ema_decoder.onnx`, `ema.json` | `export_ema.py` |
| `normalizer.wasm` | `normalizer-wasm/` |

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
