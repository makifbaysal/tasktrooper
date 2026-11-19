export interface GGUFFile {
  meta: Record<string, unknown>;
  tensors: Record<string, { shape: number[]; type: number; data: Float32Array }>;
  files: Record<string, Uint8Array>;
}

export function readGGUF(buffer: ArrayBuffer): GGUFFile;

export class TinyVoca {
  constructor(file: GGUFFile);
  readonly config: { sample_rate: number; max_tokens: number; [key: string]: unknown };
  readonly voices: string[];
  synthesize(
    ids: ArrayLike<number>,
    stress: ArrayLike<number>,
    options?: { voice?: string | number; speed?: number; pitchShift?: number; pitchRange?: number; seed?: number },
  ): { audio: Float32Array; durations: Int32Array; f0: Float32Array };
}
