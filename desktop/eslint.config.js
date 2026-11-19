import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

/**
 * Lint rules, kept short. Most of what a config like this usually enforces is
 * already enforced by `tsc` with `strict`, `noUnusedLocals` and
 * `noUnusedParameters`, and a second copy of those rules only produces two
 * places to silence the same complaint.
 *
 * What is here that tsc cannot do: the architectural boundaries. `src/shared/`
 * holds code the sandboxed renderer runs, so an import of Electron, Node or the
 * IPC bridge from inside it is not a style issue — it is a bundle that fails at
 * runtime.
 *
 * `ui/` is the SPA and lints with its own config; this one must not reach into
 * it.
 */
export default tseslint.config(
  // Upstream tiny-voca scripts, copied unchanged (see its README.md).
  { ignores: ["dist/**", "release/**", "bin/**", "node_modules/**", "ui/**", "embedder/src/tts/tinyvoca/**"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { ecmaVersion: 2023, sourceType: "module" },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // A floating promise in the main process is a supervisor action nobody
      // is waiting on and nobody will hear fail.
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      "no-console": ["warn", { allow: ["warn", "error"] }],
    },
  },
  {
    // The boundary that makes the future `shared/` package possible.
    files: ["src/shared/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: [
            { name: "electron", message: "src/shared must stay runnable in a browser — no Electron." },
            { name: "node:fs", message: "src/shared must stay runnable in a browser — no Node built-ins." },
            { name: "node:path", message: "src/shared must stay runnable in a browser — no Node built-ins." },
            { name: "node:child_process", message: "src/shared must stay runnable in a browser — no Node built-ins." },
            { name: "node:os", message: "src/shared must stay runnable in a browser — no Node built-ins." },
            { name: "node:crypto", message: "src/shared must stay runnable in a browser — no Node built-ins." },
            { name: "node:net", message: "src/shared must stay runnable in a browser — no Node built-ins." },
          ],
          patterns: [
            {
              group: ["**/ipc/*", "@ipc/*", "**/main/*", "**/local/*"],
              message: "src/shared must not know the IPC bridge or the Electron main process exists.",
            },
          ],
        },
      ],
    },
  },
  {
    // The renderer is sandboxed with no node integration; a Node import there
    // would not even bundle, so failing at lint is the faster feedback.
    files: ["src/renderer/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: [{ name: "electron", message: "The renderer talks to the main process over IPC, never directly." }],
          patterns: [
            { group: ["node:*"], message: "The renderer is sandboxed — no Node built-ins." },
            { group: ["**/main/*"], message: "The renderer must not import main-process code." },
          ],
        },
      ],
    },
  },
);
