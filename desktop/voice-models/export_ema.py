import sys, json, torch
torch.expm1 = lambda x: torch.exp(x) - 1  # no ONNX op for expm1
sys.path.insert(0, sys.argv[1])
from huggingface_hub import hf_hub_download
from ema_lightning.model import load_acoustic
from ema_lightning.decoder import load_decoder
REV = "e7c96f2fc0ba843cd0f86e02786655bb56f50df8"  # canberkkkkkk/ema-lightning on the Hub
R = "canberkkkkkk/ema-lightning"
m = load_acoustic(hf_hub_download(R, "ema.pt", revision=REV), "cpu")
d = load_decoder(hf_hub_download(R, "decoder.pt", revision=REV), "cpu")
out = sys.argv[2]
json.dump({"vocab": m.vocab, "times": m.times, "latent_dim": m.latent_dim, "hop": d.hop, "rate": 48000}, open(f"{out}/ema.json", "w"), ensure_ascii=False)

class Text(torch.nn.Module):
    def __init__(s, m): super().__init__(); s.m = m
    def forward(s, ids):
        mask = ids != 0
        return s.m.text_stage(ids, mask)
class Sound(torch.nn.Module):
    def __init__(s, m): super().__init__(); s.m = m
    def forward(s, h, dur, cw, wstart, fw, fp, noise):
        mask = torch.ones(h.shape[:2], dtype=torch.bool)
        fmask = torch.ones(fw.shape, dtype=torch.bool)
        return s.m.sound_stage(h, dur, mask, cw, wstart, fw, fp, fmask, noise)
class Dec(torch.nn.Module):
    def __init__(s, d): super().__init__(); s.d = d
    def forward(s, z): return s.d(z.transpose(1, 2), None)

L, T = 20, 60
ids = torch.randint(2, len(m.vocab), (1, L))
cw = torch.arange(L)[None] // 5; wstart = (cw * 5)
fw = (torch.arange(T)[None] * 4 // T); fp = torch.rand(1, T)
h, dur = Text(m)(ids)
noise = torch.randn(1, len(m.times), T, m.latent_dim)
kw = dict(opset_version=17, dynamo=False)
torch.onnx.export(Text(m), (ids,), f"{out}/ema_text.onnx", input_names=["ids"], output_names=["h", "dur"],
    dynamic_axes={"ids": {1: "L"}, "h": {1: "L"}, "dur": {1: "L"}}, **kw)
torch.onnx.export(Sound(m), (h, dur, cw, wstart, fw, fp, noise), f"{out}/ema_sound.onnx",
    input_names=["h", "dur", "cw", "wstart", "fw", "fp", "noise"], output_names=["latents"],
    dynamic_axes={"h": {1: "L"}, "dur": {1: "L"}, "cw": {1: "L"}, "wstart": {1: "L"}, "fw": {1: "T"}, "fp": {1: "T"}, "noise": {2: "T"}, "latents": {1: "T"}}, **kw)
z = torch.randn(1, T, m.latent_dim)
torch.onnx.export(Dec(d), (z,), f"{out}/ema_decoder.onnx", input_names=["z"], output_names=["audio"],
    dynamic_axes={"z": {1: "T"}, "audio": {1: "S"}}, **kw)
print("ok")
