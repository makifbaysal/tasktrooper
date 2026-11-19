# tiny-voca, vendored

`tiny-voca.js`, `text.js` and `dizione/` are the plain JavaScript model and
phonemizer from <https://github.com/n4ze3m/tiny-voca> (`js/`, commit
20f0f21c), Apache License 2.0 (`LICENSE`), copied unchanged. The `.d.ts`
files beside them are ours: the types `../tinyvoca.ts` uses. To update, copy
the same files from a newer commit and run the embedder tests; the model file
they read is pinned in `../files.ts`.
