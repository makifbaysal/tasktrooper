// A small QR Code encoder for the pairing screen: byte mode, error correction
// L or M, versions 1-10 (up to 213 bytes at M, 271 at L), every mask tried.
// The pairing link is ~150 characters, so nothing larger is ever needed; a
// text that does not fit throws and the screen shows the code instead.
//
// Written against ISO/IEC 18004; the structure follows the reference
// algorithm that Project Nayuki's MIT-licensed QR Code generator documents.

export type QrMatrix = boolean[][];
export type QrLevel = "L" | "M";

const MAX_VERSION = 10;

const ECC_PER_BLOCK: Record<QrLevel, readonly number[]> = {
  L: [-1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18],
  M: [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26],
};
const BLOCKS: Record<QrLevel, readonly number[]> = {
  L: [-1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4],
  M: [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5],
};
const FORMAT_BITS: Record<QrLevel, number> = { L: 1, M: 0 };

function rawModules(version: number): number {
  let result = (16 * version + 128) * version + 64;
  if (version >= 2) {
    const align = Math.floor(version / 7) + 2;
    result -= (25 * align - 10) * align - 55;
    if (version >= 7) result -= 36;
  }
  return result;
}

/** Data codewords (not counting error correction) a symbol of this version and level holds. */
export function dataCodewords(version: number, level: QrLevel): number {
  return Math.floor(rawModules(version) / 8) - (ECC_PER_BLOCK[level][version] as number) * (BLOCKS[level][version] as number);
}

function gfMultiply(x: number, y: number): number {
  let z = 0;
  for (let i = 7; i >= 0; i -= 1) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((y >>> i) & 1) * x;
  }
  return z;
}

function rsDivisor(degree: number): number[] {
  const result = new Array<number>(degree).fill(0);
  result[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i += 1) {
    for (let j = 0; j < degree; j += 1) {
      result[j] = gfMultiply(result[j] as number, root);
      if (j + 1 < degree) result[j] = (result[j] as number) ^ (result[j + 1] as number);
    }
    root = gfMultiply(root, 0x02);
  }
  return result;
}

function rsRemainder(data: readonly number[], divisor: readonly number[]): number[] {
  const result = new Array<number>(divisor.length).fill(0);
  for (const byte of data) {
    const factor = byte ^ (result.shift() as number);
    result.push(0);
    divisor.forEach((coef, i) => {
      result[i] = (result[i] as number) ^ gfMultiply(coef, factor);
    });
  }
  return result;
}

function interleave(data: readonly number[], version: number, level: QrLevel): number[] {
  const blocks = BLOCKS[level][version] as number;
  const eccLen = ECC_PER_BLOCK[level][version] as number;
  const raw = Math.floor(rawModules(version) / 8);
  const shortBlocks = blocks - (raw % blocks);
  const shortLen = Math.floor(raw / blocks);
  const divisor = rsDivisor(eccLen);
  const all: number[][] = [];
  for (let i = 0, k = 0; i < blocks; i += 1) {
    const chunk = data.slice(k, k + shortLen - eccLen + (i < shortBlocks ? 0 : 1));
    k += chunk.length;
    const ecc = rsRemainder(chunk, divisor);
    if (i < shortBlocks) chunk.push(0);
    all.push([...chunk, ...ecc]);
  }
  const out: number[] = [];
  for (let i = 0; i < (all[0] as number[]).length; i += 1) {
    all.forEach((block, j) => {
      if (i !== shortLen - eccLen || j >= shortBlocks) out.push(block[i] as number);
    });
  }
  return out;
}

function alignmentPositions(version: number): number[] {
  if (version === 1) return [];
  const count = Math.floor(version / 7) + 2;
  const size = version * 4 + 17;
  const step = Math.ceil((version * 4 + 4) / (count * 2 - 2)) * 2;
  const result = [6];
  for (let pos = size - 7; result.length < count; pos -= step) result.splice(1, 0, pos);
  return result;
}

const bit = (value: number, index: number): boolean => ((value >>> index) & 1) !== 0;

function buildMatrix(codewords: readonly number[], version: number, level: QrLevel): QrMatrix {
  const size = version * 4 + 17;
  const modules: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const fn: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const setFn = (x: number, y: number, dark: boolean): void => {
    (modules[y] as boolean[])[x] = dark;
    (fn[y] as boolean[])[x] = true;
  };

  for (let i = 0; i < size; i += 1) {
    setFn(6, i, i % 2 === 0);
    setFn(i, 6, i % 2 === 0);
  }
  const finder = (cx: number, cy: number): void => {
    for (let dy = -4; dy <= 4; dy += 1) {
      for (let dx = -4; dx <= 4; dx += 1) {
        const dist = Math.max(Math.abs(dx), Math.abs(dy));
        const x = cx + dx;
        const y = cy + dy;
        if (x >= 0 && x < size && y >= 0 && y < size) setFn(x, y, dist !== 2 && dist !== 4);
      }
    }
  };
  finder(3, 3);
  finder(size - 4, 3);
  finder(3, size - 4);
  const align = alignmentPositions(version);
  align.forEach((ax, i) => {
    align.forEach((ay, j) => {
      if ((i === 0 && j === 0) || (i === 0 && j === align.length - 1) || (i === align.length - 1 && j === 0)) return;
      for (let dy = -2; dy <= 2; dy += 1) for (let dx = -2; dx <= 2; dx += 1) setFn(ax + dx, ay + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    });
  });

  const formatBits = (mask: number): void => {
    const data = (FORMAT_BITS[level] << 3) | mask;
    let rem = data;
    for (let i = 0; i < 10; i += 1) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
    const bits = ((data << 10) | rem) ^ 0x5412;
    for (let i = 0; i <= 5; i += 1) setFn(8, i, bit(bits, i));
    setFn(8, 7, bit(bits, 6));
    setFn(8, 8, bit(bits, 7));
    setFn(7, 8, bit(bits, 8));
    for (let i = 9; i < 15; i += 1) setFn(14 - i, 8, bit(bits, i));
    for (let i = 0; i < 8; i += 1) setFn(size - 1 - i, 8, bit(bits, i));
    for (let i = 8; i < 15; i += 1) setFn(8, size - 15 + i, bit(bits, i));
    setFn(8, size - 8, true);
  };
  formatBits(0);
  if (version >= 7) {
    let rem = version;
    for (let i = 0; i < 12; i += 1) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
    const bits = (version << 12) | rem;
    for (let i = 0; i < 18; i += 1) {
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      setFn(a, b, bit(bits, i));
      setFn(b, a, bit(bits, i));
    }
  }

  let i = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert += 1) {
      for (let j = 0; j < 2; j += 1) {
        const x = right - j;
        const upward = ((right + 1) & 2) === 0;
        const y = upward ? size - 1 - vert : vert;
        if (!(fn[y] as boolean[])[x] && i < codewords.length * 8) {
          (modules[y] as boolean[])[x] = bit(codewords[i >>> 3] as number, 7 - (i & 7));
          i += 1;
        }
      }
    }
  }

  const applyMask = (mask: number): void => {
    for (let y = 0; y < size; y += 1) {
      for (let x = 0; x < size; x += 1) {
        let invert: boolean;
        switch (mask) {
          case 0: invert = (x + y) % 2 === 0; break;
          case 1: invert = y % 2 === 0; break;
          case 2: invert = x % 3 === 0; break;
          case 3: invert = (x + y) % 3 === 0; break;
          case 4: invert = (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0; break;
          case 5: invert = ((x * y) % 2) + ((x * y) % 3) === 0; break;
          case 6: invert = (((x * y) % 2) + ((x * y) % 3)) % 2 === 0; break;
          default: invert = (((x + y) % 2) + ((x * y) % 3)) % 2 === 0; break;
        }
        if (!(fn[y] as boolean[])[x] && invert) (modules[y] as boolean[])[x] = !(modules[y] as boolean[])[x];
      }
    }
  };

  let best = 0;
  let bestPenalty = Infinity;
  for (let mask = 0; mask < 8; mask += 1) {
    applyMask(mask);
    formatBits(mask);
    const penalty = penaltyScore(modules);
    if (penalty < bestPenalty) {
      best = mask;
      bestPenalty = penalty;
    }
    applyMask(mask);
  }
  applyMask(best);
  formatBits(best);
  return modules;
}

const FINDER_A = [true, false, true, true, true, false, true, false, false, false, false];
const FINDER_B = [false, false, false, false, true, false, true, true, true, false, true];

function penaltyScore(m: QrMatrix): number {
  const size = m.length;
  const at = (x: number, y: number): boolean => (m[y] as boolean[])[x] as boolean;
  let score = 0;
  for (const horizontal of [true, false]) {
    for (let a = 0; a < size; a += 1) {
      const line = Array.from({ length: size }, (_, b) => (horizontal ? at(b, a) : at(a, b)));
      let run = 1;
      for (let b = 1; b <= size; b += 1) {
        if (b < size && line[b] === line[b - 1]) run += 1;
        else {
          if (run >= 5) score += 3 + (run - 5);
          run = 1;
        }
      }
      for (let b = 0; b + 11 <= size; b += 1) {
        const window = line.slice(b, b + 11);
        if (window.every((v, k) => v === FINDER_A[k]) || window.every((v, k) => v === FINDER_B[k])) score += 40;
      }
    }
  }
  let dark = 0;
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      if (at(x, y)) dark += 1;
      if (x + 1 < size && y + 1 < size && at(x, y) === at(x + 1, y) && at(x, y) === at(x, y + 1) && at(x, y) === at(x + 1, y + 1)) score += 3;
    }
  }
  const total = size * size;
  score += (Math.ceil(Math.abs(dark * 20 - total * 10) / total) - 1) * 10;
  return score;
}

/** The module matrix for `text` (true = dark), without the quiet zone. Throws when it does not fit. */
export function encodeQr(text: string, preferred: QrLevel = "M"): QrMatrix {
  const bytes = [...new TextEncoder().encode(text)];
  const levels: QrLevel[] = preferred === "M" ? ["M", "L"] : ["L"];
  for (const level of levels) {
    for (let version = 1; version <= MAX_VERSION; version += 1) {
      const capacity = dataCodewords(version, level) * 8;
      const countBits = version <= 9 ? 8 : 16;
      if (4 + countBits + bytes.length * 8 > capacity) continue;

      const bits: number[] = [];
      const push = (value: number, count: number): void => {
        for (let i = count - 1; i >= 0; i -= 1) bits.push((value >>> i) & 1);
      };
      push(0b0100, 4);
      push(bytes.length, countBits);
      for (const b of bytes) push(b, 8);
      push(0, Math.min(4, capacity - bits.length));
      while (bits.length % 8 !== 0) bits.push(0);
      const data: number[] = [];
      for (let i = 0; i < bits.length; i += 8) data.push(bits.slice(i, i + 8).reduce((acc, b) => (acc << 1) | b, 0));
      for (let pad = 0xec; data.length < capacity / 8; pad ^= 0xec ^ 0x11) data.push(pad);

      return buildMatrix(interleave(data, version, level), version, level);
    }
  }
  throw new Error("The text is too long for a QR code.");
}
