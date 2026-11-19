import type { GGUFFile } from "../tiny-voca.js";

export interface Phonemizer {
  phonemize(text: string): { ids: number[]; stress: number[]; symbols: string[] };
}

export function fromGGUF(file: GGUFFile): Promise<Phonemizer>;
