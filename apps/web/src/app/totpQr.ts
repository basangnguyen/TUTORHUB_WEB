/**
 * AUTH-09B: a small, dependency-free QR Code encoder (ISO/IEC 18004, byte
 * mode) for the authenticator-app enrollment screen. The `otpauth://` URI
 * carries the TOTP seed, so it is rendered entirely in the browser: it never
 * reaches an image service, the network, Web Storage or a log.
 *
 * Scope: versions 1..20, error correction M (preferred) or L (when the text
 * only fits at L), automatic mask selection with the standard penalty rules,
 * and a 4-module quiet zone. Text that does not fit returns `null`; the UI
 * then shows only the grouped setup key.
 */

export type QrErrorCorrection = "L" | "M";

export interface QrMatrix {
  readonly errorCorrection: QrErrorCorrection;
  readonly mask: number;
  /** `modules[y][x]`; `true` is a dark module. Excludes the quiet zone. */
  readonly modules: readonly (readonly boolean[])[];
  /** Modules per side: `17 + 4 * version`. */
  readonly size: number;
  readonly version: number;
}

/** Largest version produced (97 × 97 modules, ≤ 858 bytes at level L). */
export const QR_MAX_VERSION = 20;
/** Light border required around the symbol (ISO/IEC 18004 §6.3.8). */
export const QR_QUIET_ZONE = 4;

// Per-version tables (index 0 unused), from the ISO/IEC 18004 capacity table.
const ECC_CODEWORDS_PER_BLOCK: Readonly<
  Record<QrErrorCorrection, readonly number[]>
> = {
  L: [
    0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30,
    28, 28,
  ],
  M: [
    0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26,
    26, 26,
  ],
};
const ERROR_CORRECTION_BLOCKS: Readonly<
  Record<QrErrorCorrection, readonly number[]>
> = {
  L: [0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8],
  M: [0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16],
};
/** Two-bit error-correction indicator used in the format information. */
const FORMAT_LEVEL_BITS: Readonly<Record<QrErrorCorrection, number>> = {
  L: 0b01,
  M: 0b00,
};

/* ---------- GF(256) arithmetic, primitive polynomial x^8+x^4+x^3+x^2+1 ---------- */

const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);
{
  let value = 1;
  for (let power = 0; power < 255; power += 1) {
    GF_EXP[power] = value;
    GF_LOG[value] = power;
    value <<= 1;
    if (value & 0x100) value ^= 0x11d;
  }
  for (let power = 255; power < 512; power += 1) {
    GF_EXP[power] = GF_EXP[power - 255] ?? 0;
  }
}

function gfMultiply(left: number, right: number) {
  if (left === 0 || right === 0) return 0;
  return GF_EXP[(GF_LOG[left] ?? 0) + (GF_LOG[right] ?? 0)] ?? 0;
}

/** Monic generator polynomial ∏(x − αⁱ), i < degree; highest degree first. */
function generatorPolynomial(degree: number) {
  let polynomial = [1];
  for (let index = 0; index < degree; index += 1) {
    const root = GF_EXP[index] ?? 0;
    const next = new Array<number>(polynomial.length + 1).fill(0);
    for (let term = 0; term < polynomial.length; term += 1) {
      const coefficient = polynomial[term] ?? 0;
      next[term] = (next[term] ?? 0) ^ coefficient;
      next[term + 1] = (next[term + 1] ?? 0) ^ gfMultiply(coefficient, root);
    }
    polynomial = next;
  }
  return polynomial;
}

/** Reed–Solomon remainder of `data · x^degree` divided by the generator. */
export function reedSolomonRemainder(
  data: readonly number[],
  degree: number,
): number[] {
  const generator = generatorPolynomial(degree);
  const remainder = new Array<number>(degree).fill(0);
  for (const byte of data) {
    const factor = byte ^ (remainder[0] ?? 0);
    remainder.shift();
    remainder.push(0);
    for (let index = 0; index < degree; index += 1) {
      remainder[index] =
        (remainder[index] ?? 0) ^ gfMultiply(generator[index + 1] ?? 0, factor);
    }
  }
  return remainder;
}

/* ---------- Capacity ---------- */

/** Modules available for data and error correction (incl. remainder bits). */
function rawDataModules(version: number) {
  let modules = (16 * version + 128) * version + 64;
  if (version >= 2) {
    const alignmentCount = Math.floor(version / 7) + 2;
    modules -= (25 * alignmentCount - 10) * alignmentCount - 55;
    if (version >= 7) modules -= 36;
  }
  return modules;
}

function totalCodewords(version: number) {
  return Math.floor(rawDataModules(version) / 8);
}

function dataCodewords(version: number, level: QrErrorCorrection) {
  return (
    totalCodewords(version) -
    (ECC_CODEWORDS_PER_BLOCK[level][version] ?? 0) *
      (ERROR_CORRECTION_BLOCKS[level][version] ?? 0)
  );
}

function countBits(version: number) {
  return version <= 9 ? 8 : 16;
}

/** Largest UTF-8 payload (bytes) a version holds in byte mode. */
export function byteCapacity(version: number, level: QrErrorCorrection) {
  return Math.floor(
    (dataCodewords(version, level) * 8 - 4 - countBits(version)) / 8,
  );
}

/* ---------- Data codewords ---------- */

class BitBuffer {
  readonly bits: number[] = [];

  append(value: number, length: number) {
    for (let shift = length - 1; shift >= 0; shift -= 1) {
      this.bits.push((value >>> shift) & 1);
    }
  }
}

function encodeDataCodewords(
  payload: Uint8Array,
  version: number,
  level: QrErrorCorrection,
) {
  const capacityBits = dataCodewords(version, level) * 8;
  const buffer = new BitBuffer();
  buffer.append(0b0100, 4); // Byte mode indicator.
  buffer.append(payload.length, countBits(version));
  for (const byte of payload) buffer.append(byte, 8);
  // Terminator (up to four zero bits), then pad to a byte boundary.
  buffer.append(0, Math.min(4, capacityBits - buffer.bits.length));
  buffer.append(0, (8 - (buffer.bits.length % 8)) % 8);
  const codewords: number[] = [];
  for (let index = 0; index < buffer.bits.length; index += 8) {
    let byte = 0;
    for (let bit = 0; bit < 8; bit += 1) {
      byte = (byte << 1) | (buffer.bits[index + bit] ?? 0);
    }
    codewords.push(byte);
  }
  const target = capacityBits / 8;
  for (let pad = 0; codewords.length < target; pad += 1) {
    codewords.push(pad % 2 === 0 ? 0xec : 0x11);
  }
  return codewords;
}

/** Splits into blocks, appends each block's ECC and interleaves (§7.6). */
function interleaveWithErrorCorrection(
  data: readonly number[],
  version: number,
  level: QrErrorCorrection,
) {
  const blockCount = ERROR_CORRECTION_BLOCKS[level][version] ?? 1;
  const eccLength = ECC_CODEWORDS_PER_BLOCK[level][version] ?? 0;
  const total = totalCodewords(version);
  const shortBlocks = blockCount - (total % blockCount);
  const shortBlockData = Math.floor(total / blockCount) - eccLength;
  const dataBlocks: number[][] = [];
  const eccBlocks: number[][] = [];
  let offset = 0;
  for (let block = 0; block < blockCount; block += 1) {
    const length = shortBlockData + (block < shortBlocks ? 0 : 1);
    const slice = data.slice(offset, offset + length);
    offset += length;
    dataBlocks.push(slice);
    eccBlocks.push(reedSolomonRemainder(slice, eccLength));
  }
  const result: number[] = [];
  for (let index = 0; index <= shortBlockData; index += 1) {
    for (const block of dataBlocks) {
      if (index < block.length) result.push(block[index] ?? 0);
    }
  }
  for (let index = 0; index < eccLength; index += 1) {
    for (const block of eccBlocks) result.push(block[index] ?? 0);
  }
  return result;
}

/* ---------- Function patterns ---------- */

/** Row/column centres of the alignment patterns (Annex E). */
export function alignmentPatternPositions(version: number): number[] {
  if (version < 2) return [];
  const count = Math.floor(version / 7) + 2;
  const size = 17 + 4 * version;
  const step = Math.ceil((version * 4 + 4) / (count * 2 - 2)) * 2;
  const positions = [6];
  for (let position = size - 7; positions.length < count; position -= step) {
    positions.splice(1, 0, position);
  }
  return positions;
}

/** BCH(15,5) format information, already XOR-masked with 0x5412. */
export function formatBits(level: QrErrorCorrection, mask: number) {
  const data = (FORMAT_LEVEL_BITS[level] << 3) | mask;
  let remainder = data;
  for (let index = 0; index < 10; index += 1) {
    remainder = (remainder << 1) ^ ((remainder >>> 9) * 0x537);
  }
  return ((data << 10) | (remainder & 0x3ff)) ^ 0x5412;
}

/** BCH(18,6) version information for versions 7 and above. */
export function versionBits(version: number) {
  let remainder = version;
  for (let index = 0; index < 12; index += 1) {
    remainder = (remainder << 1) ^ ((remainder >>> 11) * 0x1f25);
  }
  return (version << 12) | (remainder & 0xfff);
}

class Grid {
  readonly dark: boolean[][];
  readonly reserved: boolean[][];
  readonly size: number;

  constructor(size: number) {
    this.size = size;
    this.dark = Array.from({ length: size }, () =>
      new Array<boolean>(size).fill(false),
    );
    this.reserved = Array.from({ length: size }, () =>
      new Array<boolean>(size).fill(false),
    );
  }

  setFunction(x: number, y: number, dark: boolean) {
    const row = this.dark[y];
    const reservedRow = this.reserved[y];
    if (!row || !reservedRow || x < 0 || x >= this.size) return;
    row[x] = dark;
    reservedRow[x] = true;
  }
}

function drawFinder(grid: Grid, centreX: number, centreY: number) {
  // A 7×7 finder plus its one-module light separator.
  for (let dy = -4; dy <= 4; dy += 1) {
    for (let dx = -4; dx <= 4; dx += 1) {
      const distance = Math.max(Math.abs(dx), Math.abs(dy));
      grid.setFunction(
        centreX + dx,
        centreY + dy,
        distance !== 2 && distance !== 4,
      );
    }
  }
}

function drawAlignment(grid: Grid, centreX: number, centreY: number) {
  for (let dy = -2; dy <= 2; dy += 1) {
    for (let dx = -2; dx <= 2; dx += 1) {
      grid.setFunction(
        centreX + dx,
        centreY + dy,
        Math.max(Math.abs(dx), Math.abs(dy)) !== 1,
      );
    }
  }
}

function drawFormat(grid: Grid, level: QrErrorCorrection, mask: number) {
  const bits = formatBits(level, mask);
  const bit = (index: number) => ((bits >>> index) & 1) === 1;
  const { size } = grid;
  // Around the top-left finder.
  for (let index = 0; index <= 5; index += 1)
    grid.setFunction(8, index, bit(index));
  grid.setFunction(8, 7, bit(6));
  grid.setFunction(8, 8, bit(7));
  grid.setFunction(7, 8, bit(8));
  for (let index = 9; index < 15; index += 1) {
    grid.setFunction(14 - index, 8, bit(index));
  }
  // Second copy: under the top-right finder and beside the bottom-left one.
  for (let index = 0; index < 8; index += 1) {
    grid.setFunction(size - 1 - index, 8, bit(index));
  }
  for (let index = 8; index < 15; index += 1) {
    grid.setFunction(8, size - 15 + index, bit(index));
  }
  grid.setFunction(8, size - 8, true); // The always-dark module.
}

function drawFunctionPatterns(grid: Grid, version: number) {
  const { size } = grid;
  for (let index = 0; index < size; index += 1) {
    grid.setFunction(6, index, index % 2 === 0);
    grid.setFunction(index, 6, index % 2 === 0);
  }
  drawFinder(grid, 3, 3);
  drawFinder(grid, size - 4, 3);
  drawFinder(grid, 3, size - 4);
  const positions = alignmentPatternPositions(version);
  const last = positions.length - 1;
  positions.forEach((y, row) => {
    positions.forEach((x, column) => {
      const overlapsFinder =
        (row === 0 && column === 0) ||
        (row === 0 && column === last) ||
        (row === last && column === 0);
      if (!overlapsFinder) drawAlignment(grid, x, y);
    });
  });
  // Reserve the format areas now; real bits are drawn per mask candidate.
  drawFormat(grid, "M", 0);
  if (version >= 7) {
    const bits = versionBits(version);
    for (let index = 0; index < 18; index += 1) {
      const dark = ((bits >>> index) & 1) === 1;
      const near = Math.floor(index / 3);
      const far = size - 11 + (index % 3);
      grid.setFunction(far, near, dark);
      grid.setFunction(near, far, dark);
    }
  }
}

/** Zig-zag placement of the codeword bits from the bottom-right corner. */
function placeCodewords(grid: Grid, codewords: readonly number[]) {
  const { size } = grid;
  const totalBits = codewords.length * 8;
  let bitIndex = 0;
  let upward = true;
  for (let column = size - 1; column > 0; column -= 2) {
    if (column === 6) column -= 1; // Skip the vertical timing pattern.
    for (let step = 0; step < size; step += 1) {
      const y = upward ? size - 1 - step : step;
      for (let offset = 0; offset < 2; offset += 1) {
        const x = column - offset;
        if (grid.reserved[y]?.[x]) continue;
        const dark =
          bitIndex < totalBits &&
          (((codewords[bitIndex >>> 3] ?? 0) >>> (7 - (bitIndex & 7))) & 1) ===
            1;
        const row = grid.dark[y];
        if (row) row[x] = dark;
        bitIndex += 1;
      }
    }
    upward = !upward;
  }
}

/* ---------- Masking ---------- */

function maskApplies(mask: number, x: number, y: number) {
  switch (mask) {
    case 0:
      return (x + y) % 2 === 0;
    case 1:
      return y % 2 === 0;
    case 2:
      return x % 3 === 0;
    case 3:
      return (x + y) % 3 === 0;
    case 4:
      return (Math.floor(y / 2) + Math.floor(x / 3)) % 2 === 0;
    case 5:
      return ((x * y) % 2) + ((x * y) % 3) === 0;
    case 6:
      return (((x * y) % 2) + ((x * y) % 3)) % 2 === 0;
    default:
      return (((x + y) % 2) + ((x * y) % 3)) % 2 === 0;
  }
}

function applyMask(grid: Grid, mask: number) {
  for (let y = 0; y < grid.size; y += 1) {
    const row = grid.dark[y];
    const reservedRow = grid.reserved[y];
    if (!row || !reservedRow) continue;
    for (let x = 0; x < grid.size; x += 1) {
      if (!reservedRow[x] && maskApplies(mask, x, y)) row[x] = !row[x];
    }
  }
}

const FINDER_LIKE = [true, false, true, true, true, false, true];

/** Penalty score of the four ISO/IEC 18004 rules (§7.8.3); lower is better. */
export function maskPenalty(modules: readonly (readonly boolean[])[]) {
  const size = modules.length;
  const at = (x: number, y: number) =>
    x >= 0 && y >= 0 && x < size && y < size
      ? (modules[y]?.[x] ?? false)
      : false;
  let penalty = 0;
  let darkCount = 0;
  for (let line = 0; line < size; line += 1) {
    for (const horizontal of [true, false]) {
      const read = (index: number) =>
        horizontal ? at(index, line) : at(line, index);
      // N1: runs of five or more same-coloured modules.
      let runColour = read(0);
      let runLength = 1;
      for (let index = 1; index <= size; index += 1) {
        const colour = index < size ? read(index) : !runColour;
        if (index < size && colour === runColour) {
          runLength += 1;
          continue;
        }
        if (runLength >= 5) penalty += 3 + (runLength - 5);
        runColour = colour;
        runLength = 1;
      }
      // N3: 1:1:3:1:1 finder-like patterns with four light modules on a side
      // (the area outside the symbol counts as light).
      for (let start = -4; start + 6 < size + 4; start += 1) {
        if (
          !FINDER_LIKE.every((dark, offset) => read(start + offset) === dark)
        ) {
          continue;
        }
        const lightBefore = [1, 2, 3, 4].every((gap) => !read(start - gap));
        const lightAfter = [7, 8, 9, 10].every((gap) => !read(start + gap));
        if (lightBefore || lightAfter) penalty += 40;
      }
    }
  }
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      const dark = at(x, y);
      if (dark) darkCount += 1;
      // N2: 2×2 blocks of one colour.
      if (
        x + 1 < size &&
        y + 1 < size &&
        dark === at(x + 1, y) &&
        dark === at(x, y + 1) &&
        dark === at(x + 1, y + 1)
      ) {
        penalty += 3;
      }
    }
  }
  // N4: 10 points per 5% deviation of the dark ratio from 50%.
  const total = size * size;
  penalty += Math.floor(Math.abs((darkCount * 100) / total - 50) / 5) * 10;
  return penalty;
}

/* ---------- Public API ---------- */

export interface QrEncodeOptions {
  /** Forces one level instead of "M, else L" (tests and diagnostics). */
  errorCorrection?: QrErrorCorrection;
  /** Forces a mask 0..7 instead of the lowest-penalty one (tests). */
  mask?: number;
  /** Caps the version (1..20); defaults to `QR_MAX_VERSION`. */
  maxVersion?: number;
}

function chooseVersion(
  length: number,
  maxVersion: number,
  levels: readonly QrErrorCorrection[],
) {
  for (const level of levels) {
    for (let version = 1; version <= maxVersion; version += 1) {
      if (length <= byteCapacity(version, level)) return { level, version };
    }
  }
  return null;
}

/**
 * Encodes `text` (UTF-8, byte mode). Returns `null` for an empty text or one
 * longer than `maxVersion` (≤ 20) holds at level L.
 */
export function encodeQrCode(
  text: string,
  options: QrEncodeOptions = {},
): QrMatrix | null {
  if (typeof text !== "string" || text.length === 0) return null;
  const maxVersion = Math.min(
    QR_MAX_VERSION,
    Math.max(1, Math.floor(options.maxVersion ?? QR_MAX_VERSION)),
  );
  const forcedMask =
    options.mask !== undefined &&
    Number.isInteger(options.mask) &&
    options.mask >= 0 &&
    options.mask <= 7
      ? options.mask
      : null;
  const payload = new TextEncoder().encode(text);
  const choice = chooseVersion(
    payload.length,
    maxVersion,
    options.errorCorrection ? [options.errorCorrection] : ["M", "L"],
  );
  if (!choice) return null;
  const { level, version } = choice;
  const codewords = interleaveWithErrorCorrection(
    encodeDataCodewords(payload, version, level),
    version,
    level,
  );
  const size = 17 + 4 * version;
  const base = new Grid(size);
  drawFunctionPatterns(base, version);
  placeCodewords(base, codewords);

  let best: { mask: number; modules: boolean[][]; penalty: number } | null =
    null;
  for (let mask = 0; mask < 8; mask += 1) {
    if (forcedMask !== null && mask !== forcedMask) continue;
    const candidate = new Grid(size);
    for (let y = 0; y < size; y += 1) {
      candidate.dark[y] = [...(base.dark[y] ?? [])];
      candidate.reserved[y] = [...(base.reserved[y] ?? [])];
    }
    applyMask(candidate, mask);
    drawFormat(candidate, level, mask);
    const penalty = maskPenalty(candidate.dark);
    if (!best || penalty < best.penalty) {
      best = { mask, modules: candidate.dark, penalty };
    }
  }
  if (!best) return null;
  return {
    errorCorrection: level,
    mask: best.mask,
    modules: best.modules,
    size,
    version,
  };
}

/**
 * One SVG path (horizontal runs of dark modules) offset by the quiet zone,
 * plus the side length of the square viewBox that includes it.
 */
export function qrSvgPath(matrix: QrMatrix, quietZone = QR_QUIET_ZONE) {
  const parts: string[] = [];
  matrix.modules.forEach((row, y) => {
    let x = 0;
    while (x < row.length) {
      if (!row[x]) {
        x += 1;
        continue;
      }
      const start = x;
      while (x < row.length && row[x]) x += 1;
      parts.push(
        `M${start + quietZone} ${y + quietZone}h${x - start}v1H${start + quietZone}z`,
      );
    }
  });
  return { path: parts.join(""), viewBoxSize: matrix.size + quietZone * 2 };
}

/**
 * Groups a Base32 setup key in fours ("ABCD EFGH …") for manual entry in
 * Google Authenticator, Microsoft Authenticator or Apple Passwords.
 */
export function formatTotpSecret(secret: string) {
  const compact = secret.replace(/[\s-]+/g, "").toUpperCase();
  return compact.match(/.{1,4}/g)?.join(" ") ?? "";
}
