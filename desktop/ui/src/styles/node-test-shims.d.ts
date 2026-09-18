// Minimal ambient typings for the one Node API a test file needs to read a
// source file back off disk. `src/` otherwise never imports Node builtins
// (this app has no `@types/node` dependency), so this stays test-only.
declare module "node:fs" {
  export function readFileSync(path: string, encoding: string): string;
}
