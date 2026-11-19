import type { Phonemizer } from "./dizione/engine.js";

export interface Utterance {
  ids: number[];
  stress: number[];
  phonemes: string;
}

export function utterancesOf(text: string, phonemizer: Phonemizer, limit: number): Utterance[];
export function limited(audio: Float32Array): Float32Array;
