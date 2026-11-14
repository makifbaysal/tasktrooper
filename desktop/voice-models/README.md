# Voice models

Three voice engines ship inside the app: EMA Lightning (Turkish), Kokoro
(English) and Supertonic 3 (both), picked per language in Settings. Nothing here is built by `npm run build`: these are the steps that
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
| `supertonic_*.onnx`, `supertonic_tts.json`, `supertonic_unicode_indexer.json`, `supertonic_<style>.json`, `LICENSE-supertonic.txt` | `prepare_supertonic.py` |

The code here and in `embedder/src/tts/` is Apache-2.0, like the rest of the
repository. The model files are Apache-2.0 too, except Supertonic 3's, which
are under the BigScience OpenRAIL-M license (below). Nothing GPL is used, at
build time or at run time: in particular not espeak-ng, the phonemizer
Kokoro's own pipeline falls back on.

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

## Supertonic 3 (Turkish and English)

Supertonic 3 is by Supertone Inc.: <https://huggingface.co/Supertone/supertonic-3>
(revision `3cadd1ee`), runtime code (MIT) at
<https://github.com/supertone-inc/supertonic>. One model, 31 languages, 44.1
kHz, about 99M parameters in four ONNX graphs: a duration predictor, a text
encoder, a vector estimator run once per flow-matching step, and a vocoder.
It reads characters, not phonemes: NFKD text inside a language tag
(`<tr>…</tr>`), each code point through `unicode_indexer.json`. A voice style
is a JSON file of two tensors; the ten presets `F1`–`F5` and `M1`–`M5` all
ship, `F1` the default. `embedder/src/tts/supertonic.ts` reimplements the
official runtime's glue (`supertonic-plan.ts` has the text and the
arithmetic), with 5 steps where the official default is 8.

`prepare_supertonic.py` downloads the official fp32 graphs (398 MB), checks
them against the hub's SHA-256, and quantizes them weight-only to int8 per
output channel (98 MB): activations stay float, and a DequantizeLinear node
in front of each layer turns its weights back. The output is byte-for-byte
the same on every run with the pinned onnx and numpy. Measured on
onnxruntime-web's wasm backend (4 threads, Apple M3 Pro under other load):

| Variant | Size | Speed (short sentence, 5 steps) | Peak RSS |
|---|---|---|---|
| official fp32 | 398 MB | 740 ms | ~1.6 GB |
| weight-only int8 (shipped) | 98 MB | 765 ms | ~650 MB |
| dynamic int8 (`soniqo/Supertonic-3-ONNX-INT8`) | 102 MB | 1130 ms | ~650 MB |

The dynamic quantization runs ConvInteger and MatMulInteger, which that
backend runs half again as slow; the weight-only one runs the same float
kernels as fp32. Apple's recognizer transcribes the shipped variant back as
well as fp32 at 5 steps and at 8 (one ASR slip in 131 words, the same one);
at 4 and 3 steps it mishears more (5% and 8% word error).

Turkish text goes through normalizer-tr first: on its own Supertonic reads
"3. kat" right but "%25" and "1.250 TL" wrong, and the normalizer reads those
right but "3. kat" as "üç. kat". So a number with a period and then a
lowercase word is first written as an ordinal (`3’üncü kat`), which the
normalizer reads right. English text has amounts of money ("$1,250") and
percentages spelled out; times, dates, ordinals and other numbers Supertonic
reads right on its own.

```sh
uv venv -p 3.12 venv && VIRTUAL_ENV=venv uv pip install onnx==1.23.2 numpy==2.5.4
venv/bin/python prepare_supertonic.py out    # prints each file's SHA-256 and size
```

### License: OpenRAIL-M

The weights are under the BigScience OpenRAIL-M license, whose full text is
below and travels with the files as `LICENSE-supertonic.txt` and at the end
of `NOTICE.txt`. What it asks of a redistributor, and how this repository
does it:

- **A copy of the license** to everyone who gets the files: staged beside
  them in the app and published in the release.
- **Changed files say so**: each quantized graph's `doc_string` and
  `modified_by` metadata, and `NOTICE.txt`.
- **Attribution kept**: `NOTICE.txt`.
- **The use restrictions (paragraph 5, Attachment A) passed on**, as an
  enforceable provision of any agreement under which the model is
  distributed, with notice to those who get it. `NOTICE.txt` gives that
  notice; the app's own terms of use, where it has them, have to carry the
  restrictions too.

<details>
<summary>BigScience OpenRAIL-M License (full text)</summary>

```
BigScience Open RAIL-M License
dated August 18, 2022

Section I: PREAMBLE

This Open RAIL-M License was created by BigScience, a collaborative open innovation project aimed at
the responsible development and use of large multilingual datasets and Large Language Models
(“LLMs”). While a similar license was originally designed for the BLOOM model, we decided to adapt it
and create this license in order to propose a general open and responsible license applicable to other
machine learning based AI models (e.g. multimodal generative models).
In short, this license strives for both the open and responsible downstream use of the accompanying
model. When it comes to the open character, we took inspiration from open source permissive licenses
regarding the grant of IP rights. Referring to the downstream responsible use, we added use-based
restrictions not permitting the use of the Model in very specific scenarios, in order for the licensor to be
able to enforce the license in case potential misuses of the Model may occur. Even though downstream
derivative versions of the model could be released under different licensing terms, the latter will always
have to include - at minimum - the same use-based restrictions as the ones in the original license (this
license).
The development and use of artificial intelligence (“AI”), does not come without concerns. The world has
witnessed how AI techniques may, in some instances, become risky for the public in general. These risks
come in many forms, from racial discrimination to the misuse of sensitive information.
BigScience believes in the intersection between open and responsible AI development; thus, this License
aims to strike a balance between both in order to enable responsible open-science in the field of AI.
This License governs the use of the model (and its derivatives) and is informed by the model card
associated with the model.

NOW THEREFORE, You and Licensor agree as follows:

1. Definitions
(a) "License" means the terms and conditions for use, reproduction, and Distribution as defined in
this document.
(b) “Data” means a collection of information and/or content extracted from the dataset used with the
Model, including to train, pretrain, or otherwise evaluate the Model. The Data is not licensed under
this License.
(c)“Output” means the results of operating a Model as embodied in informational content resulting
therefrom.
(d)“Model” means any accompanying machine-learning based assemblies (including checkpoints),
consisting of learnt weights, parameters (including optimizer states), corresponding to the model
architecture as embodied in the Complementary Material, that have been trained or tuned, in whole or
in part on the Data, using the Complementary Material.
(e) “Derivatives of the Model” means all modifications to the Model, works based on the Model, or any
other model which is created or initialized by transfer of patterns of the weights, parameters,
activations or output of the Model, to the other model, in order to cause the other model to perform
similarly to the Model, including - but not limited to - distillation methods entailing the use of
intermediate data representations or methods based on the generation of synthetic data by the Model
for training the other model.
(f)“Complementary Material” means the accompanying source code and scripts used to define,
run, load, benchmark or evaluate the Model, and used to prepare data for training or evaluation, if
any. This includes any accompanying documentation, tutorials, examples, etc, if any.
(g) “Distribution” means any transmission, reproduction, publication or other sharing of the Model or
Derivatives of the Model to a third party, including providing the Model as a hosted service made
available by electronic or other remote means - e.g. API-based or web access.
(h) “Licensor” means the copyright owner or entity authorized by the copyright owner that is
granting the License, including the persons or entities that may have rights in the Model and/or
distributing the Model.
(i) "You" (or "Your") means an individual or Legal Entity exercising permissions granted by this
License and/or making use of the Model for whichever purpose and in any field of use, including
usage of the Model in an end-use application - e.g. chatbot, translator, image generator.
(j) “Third Parties” means individuals or legal entities that are not under common control with
Licensor or You.
(k) "Contribution" means any work of authorship, including the original version of the Model and
any modifications or additions to that Model or Derivatives of the Model thereof, that is
intentionally submitted to Licensor for inclusion in the Model by the copyright owner or by an
individual or Legal Entity authorized to submit on behalf of the copyright owner. For the
purposes of this definition,
“submitted” means any form of electronic, verbal, or written
communication sent to the Licensor or its representatives, including but not limited to
communication on electronic mailing lists, source code control systems, and issue tracking
systems that are managed by, or on behalf of, the Licensor for the purpose of discussing and
improving the Model, but excluding communication that is conspicuously marked or otherwise
designated in writing by the copyright owner as "Not a Contribution."
(l) "Contributor" means Licensor and any individual or Legal Entity on behalf of whom a
Contribution has been received by Licensor and subsequently incorporated within the Model.


Section II: INTELLECTUAL PROPERTY RIGHTS

Both copyright and patent grants apply to the Model, Derivatives of the Model and Complementary
Material. The Model and Derivatives of the Model are subject to additional terms as described in Section III.

2. Grant of Copyright License. Subject to the terms and conditions of this License, each Contributor
hereby grants to You a perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable copyright license to reproduce, prepare, publicly display, publicly perform, sublicense, and distribute the
Complementary Material, the Model, and Derivatives of the Model.

3. Grant of Patent License. Subject to the terms and conditions of this License and where and as
applicable, each Contributor hereby grants to You a perpetual, worldwide, non-exclusive, no-charge,
royalty-free, irrevocable (except as stated in this paragraph) patent license to make, have made, use, offer
to sell, sell, import, and otherwise transfer the Model and the Complementary Material, where such
license applies only to those patent claims licensable by such Contributor that are necessarily infringed by
their Contribution(s) alone or by combination of their Contribution(s) with the Model to which such
Contribution(s) was submitted. If You institute patent litigation against any entity (including a cross-claim
or counterclaim in a lawsuit) alleging that the Model and/or Complementary Material or a Contribution
incorporated within the Model and/or Complementary Material constitutes direct or contributory patent
infringement, then any patent licenses granted to You under this License for the Model and/or Work shall
terminate as of the date such litigation is asserted or filed.
Section III: CONDITIONS OF USAGE, DISTRIBUTION AND REDISTRIBUTION

4. Distribution and Redistribution. You may host for Third Party remote access purposes (e.g.
software-as-a-service), reproduce and distribute copies of the Model or Derivatives of the Model thereof
in any medium, with or without modifications, provided that You meet the following conditions:

a. Use-based restrictions as referenced in paragraph 5 MUST be included as an enforceable provision
by You in any type of legal agreement (e.g. a license) governing the use and/or distribution of the
Model or Derivatives of the Model, and You shall give notice to subsequent users You Distribute to,
that the Model or Derivatives of the Model are subject to paragraph 5. This provision does not apply
to the use of Complementary Material.

b. You must give any Third Party recipients of the Model or Derivatives of the Model a copy of this
License;

c. You must cause any modified files to carry prominent notices stating that You changed the files;

d. You must retain all copyright, patent, trademark, and attribution notices excluding those notices
that do not pertain to any part of the Model, Derivatives of the Model.
You may add Your own copyright statement to Your modifications and may provide additional or
different license terms and conditions - respecting paragraph 4.a.
- for use, reproduction, or Distribution
of Your modifications, or for any such Derivatives of the Model as a whole, provided Your use,
reproduction, and Distribution of the Model otherwise complies with the conditions stated in this License.

5. Use-based restrictions. The restrictions set forth in Attachment A are considered Use-based restrictions.
Therefore You cannot use the Model and the Derivatives of the Model for the specified restricted uses. You
may use the Model subject to this License, including only for lawful purposes and in accordance with the
License. Use may include creating any content with, finetuning, updating, running, training, evaluating and/or
reparametrizing the Model. You shall require all of Your users who use the Model or a Derivative of the Model
to comply with the terms of this paragraph (paragraph 5).

6. The Output You Generate. Except as set forth herein, Licensor claims no rights in the Output You
generate using the Model. You are accountable for the Output you generate and its subsequent uses. No
use of the output can contravene any provision as stated in the License.

Section IV: OTHER PROVISIONS

7. Updates and Runtime Restrictions. To the maximum extent permitted by law, Licensor reserves the
right to restrict (remotely or otherwise) usage of the Model in violation of this License, update the Model
through electronic means, or modify the Output of the Model based on updates. You shall undertake
reasonable efforts to use the latest version of the Model.

8. Trademarks and related. Nothing in this License permits You to make use of Licensors’ trademarks,
trade names, logos or to otherwise suggest endorsement or misrepresent the relationship between the
parties; and any rights not expressly granted herein are reserved by the Licensors.

9. Disclaimer of Warranty. Unless required by applicable law or agreed to in writing, Licensor provides
the Model and the Complementary Material (and each Contributor provides its Contributions) on an "AS
IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied,
including, without limitation, any warranties or conditions of TITLE, NON-INFRINGEMENT,
MERCHANTABILITY , or FITNESS FOR A PARTICULAR PURPOSE. You are solely responsible for
determining the appropriateness of using or redistributing the Model, Derivatives of the Model, and the
Complementary Material and assume any risks associated with Your exercise of permissions under this
License.

10. Limitation of Liability. In no event and under no legal theory, whether in tort (including negligence),
contract, or otherwise, unless required by applicable law (such as deliberate and grossly negligent acts) or
agreed to in writing, shall any Contributor be liable to You for damages, including any direct, indirect,
special, incidental, or consequential damages of any character arising as a result of this License or out of
the use or inability to use the Model and the Complementary Material (including but not limited to
damages for loss of goodwill, work stoppage, computer failure or malfunction, or any and all other
commercial damages or losses), even if such Contributor has been advised of the possibility of such
damages.

11. Accepting Warranty or Additional Liability. While redistributing the Model, Derivatives of the
Model and the Complementary Material thereof, You may choose to offer, and charge a fee for, acceptance
of support, warranty, indemnity, or other liability obligations and/or rights consistent with this License.
However, in accepting such obligations, You may act only on Your own behalf and on Your sole
responsibility, not on behalf of any other Contributor, and only if You agree to indemnify, defend, and
hold each Contributor harmless for any liability incurred by, or claims asserted against, such Contributor
by reason of your accepting any such warranty or additional liability.

12. If any provision of this License is held to be invalid, illegal or unenforceable, the remaining
provisions shall be unaffected thereby and remain valid as if such provision had not been set forth herein.

END OF TERMS AND CONDITIONS

Attachment A

Use Restrictions

You agree not to use the Model or Derivatives of the Model:
(a) In any way that violates any applicable national, federal, state, local or international law
or regulation;
(b) For the purpose of exploiting, harming or attempting to exploit or harm minors in any
way;
(c) To generate or disseminate verifiably false information and/or content with the purpose of
harming others;
(d) To generate or disseminate personal identifiable information that can be used to harm an
individual;
(e) To generate or disseminate information and/or content (e.g. images, code, posts, articles),
and place the information and/or content in any context (e.g. bot generating tweets)
without expressly and intelligibly disclaiming that the information and/or content is
machine generated;
(f) To defame, disparage or otherwise harass others;
(g) To impersonate or attempt to impersonate (e.g. deepfakes) others without their consent;
(h) For fully automated decision making that adversely impacts an individual’s legal rights or
otherwise creates or modifies a binding, enforceable obligation;
(i) For any use intended to or which has the effect of discriminating against or harming
individuals or groups based on online or offline social behavior or known or predicted
personal or personality characteristics;
(j) To exploit any of the vulnerabilities of a specific group of persons based on their age,
social, physical or mental characteristics, in order to materially distort the behavior of a
person pertaining to that group in a manner that causes or is likely to cause that person or
another person physical or psychological harm;
(k) For any use intended to or which has the effect of discriminating against individuals or
groups based on legally protected characteristics or categories;
(l) To provide medical advice and medical results interpretation;
(m) To generate or disseminate information for the purpose to be used for administration of
justice, law enforcement, immigration or asylum processes, such as predicting an
individual will commit fraud/crime commitment (e.g. by text profiling, drawing causal
relationships between assertions made in documents, indiscriminate and
arbitrarily-targeted use).
```

</details>

## Publishing to the release

The tag starts with `models-`, not `v`, so the release workflow never takes
it for an app release. Files are added to it, never replaced in place: a
changed file is a new tag and new hashes in `files.ts`. `NOTICE.txt` is the
exception, as it carries no hash.

```sh
gh release upload models-voice-1 -R makifbaysal/tasktrooper out/kokoro_v1_q8.onnx out/kokoro.json out/kokoro_*.bin out/misaki_*.json.gz
gh release upload models-voice-1 -R makifbaysal/tasktrooper out/supertonic_* out/LICENSE-supertonic.txt
gh release upload models-voice-1 -R makifbaysal/tasktrooper NOTICE.txt --clobber
```

`NOTICE.txt` is what `npm run build:embedder` writes into
`dist/embedder/voice/`: `VOICE_NOTICE`, then the OpenRAIL-M text.
