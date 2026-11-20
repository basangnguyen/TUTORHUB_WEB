import { describe, expect, it } from "vitest";
import {
  QR_MAX_VERSION,
  QR_QUIET_ZONE,
  alignmentPatternPositions,
  byteCapacity,
  encodeQrCode,
  formatBits,
  formatTotpSecret,
  qrSvgPath,
  reedSolomonRemainder,
  versionBits,
  type QrMatrix,
} from "./totpQr";

const SECRET = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";

function otpauthURI(email: string) {
  return (
    `otpauth://totp/TutorHub:${encodeURIComponent(email)}` +
    `?algorithm=SHA1&digits=6&issuer=TutorHub&period=30&secret=${SECRET}`
  );
}

function encodeOrFail(text: string, options = {}): QrMatrix {
  const matrix = encodeQrCode(text, options);
  if (!matrix) throw new Error(`expected a QR code for ${text.length} chars`);
  return matrix;
}

function module(matrix: QrMatrix, x: number, y: number) {
  return matrix.modules[y]?.[x] === true;
}

/** Finder (7×7) with its light separator, anchored at the top-left module. */
function expectFinder(matrix: QrMatrix, left: number, top: number) {
  for (let dy = -1; dy <= 7; dy += 1) {
    for (let dx = -1; dx <= 7; dx += 1) {
      const x = left + dx;
      const y = top + dy;
      if (x < 0 || y < 0 || x >= matrix.size || y >= matrix.size) continue;
      const ring = Math.max(Math.abs(dx - 3), Math.abs(dy - 3));
      expect(module(matrix, x, y), `finder ${left},${top} at ${x},${y}`).toBe(
        ring !== 2 && ring !== 4,
      );
    }
  }
}

function readFormatCopies(matrix: QrMatrix) {
  const { size } = matrix;
  const bit = (x: number, y: number, index: number) =>
    module(matrix, x, y) ? 1 << index : 0;
  let first = 0;
  for (let index = 0; index <= 5; index += 1) first |= bit(8, index, index);
  first |= bit(8, 7, 6) | bit(8, 8, 7) | bit(7, 8, 8);
  for (let index = 9; index < 15; index += 1) {
    first |= bit(14 - index, 8, index);
  }
  let second = 0;
  for (let index = 0; index < 8; index += 1) {
    second |= bit(size - 1 - index, 8, index);
  }
  for (let index = 8; index < 15; index += 1) {
    second |= bit(8, size - 15 + index, index);
  }
  return { first, second };
}

describe("encodeQrCode", () => {
  it("matches an independently decoded golden symbol (version 2-M)", () => {
    // Cross-checked with OpenCV's QRCodeDetector, which decodes it back to
    // the same text.
    const golden = [
      "#######.#....###..#######",
      "#.....#....##..#..#.....#",
      "#.###.#.#....#....#.###.#",
      "#.###.#..####..#..#.###.#",
      "#.###.#...#.#..##.#.###.#",
      "#.....#.####.##...#.....#",
      "#######.#.#.#.#.#.#######",
      ".............#..#........",
      "#.#...##...#.#.#...#..#.#",
      "#.##...##.#.#...#.#....##",
      "..#.#.#.#..#.###.....##.#",
      "#..#.#.#.####.########...",
      "##.##.#....#.##.#.##.#..#",
      "...#.#...##..###.##....##",
      "###.###.##.#...##.##..#.#",
      "...#...####..#......##.##",
      "##.#.##..#.###.######....",
      "........#.#.#..##...##..#",
      "#######.#...###.#.#.#.#.#",
      "#.....#..####.###...##...",
      "#.###.#......########..##",
      "#.###.#..#...##..#.###.#.",
      "#.###.#.#.##....##.######",
      "#.....#...#..#.####..#...",
      "#######.######..#..#.#..#",
    ];
    const matrix = encodeOrFail("otpauth://totp/T");
    expect(matrix).toMatchObject({ errorCorrection: "M", mask: 1, version: 2 });
    expect(
      matrix.modules.map((row) =>
        row.map((dark) => (dark ? "#" : ".")).join(""),
      ),
    ).toEqual(golden);
  });

  it.each([
    ["a short otpauth URI", otpauthURI("a@b.co")],
    ["a typical otpauth URI", otpauthURI("teacher.nguyen@example.test")],
    [
      "an otpauth URI with a very long email",
      otpauthURI(`${"x".repeat(300)}@school-district.example.edu.vn`),
    ],
  ])("encodes %s with the standard structure", (_label, text) => {
    const matrix = encodeOrFail(text);
    const { size, version } = matrix;
    expect(version).toBeGreaterThanOrEqual(1);
    expect(version).toBeLessThanOrEqual(QR_MAX_VERSION);
    expect(size).toBe(17 + 4 * version);
    expect(matrix.modules).toHaveLength(size);
    for (const row of matrix.modules) expect(row).toHaveLength(size);

    expectFinder(matrix, 0, 0);
    expectFinder(matrix, size - 7, 0);
    expectFinder(matrix, 0, size - 7);

    // Timing patterns alternate between the finders, starting dark.
    for (let index = 8; index < size - 8; index += 1) {
      expect(module(matrix, index, 6)).toBe(index % 2 === 0);
      expect(module(matrix, 6, index)).toBe(index % 2 === 0);
    }

    // The dark module beside the bottom-left finder.
    expect(module(matrix, 8, size - 8)).toBe(true);

    // Every alignment pattern that does not overlap a finder.
    const positions = alignmentPatternPositions(version);
    const last = positions.length - 1;
    positions.forEach((cy, row) =>
      positions.forEach((cx, column) => {
        if (
          (row === 0 && column === 0) ||
          (row === 0 && column === last) ||
          (row === last && column === 0)
        ) {
          return;
        }
        for (let dy = -2; dy <= 2; dy += 1) {
          for (let dx = -2; dx <= 2; dx += 1) {
            expect(module(matrix, cx + dx, cy + dy)).toBe(
              Math.max(Math.abs(dx), Math.abs(dy)) !== 1,
            );
          }
        }
      }),
    );

    // Both format copies carry the level and mask that were used.
    const copies = readFormatCopies(matrix);
    const expected = formatBits(matrix.errorCorrection, matrix.mask);
    expect(copies.first).toBe(expected);
    expect(copies.second).toBe(expected);

    if (version >= 7) {
      const bits = versionBits(version);
      for (let index = 0; index < 18; index += 1) {
        const dark = ((bits >>> index) & 1) === 1;
        const near = Math.floor(index / 3);
        const far = size - 11 + (index % 3);
        expect(module(matrix, far, near)).toBe(dark);
        expect(module(matrix, near, far)).toBe(dark);
      }
    }
  });

  it("grows with the input and prefers level M", () => {
    const short = encodeOrFail(otpauthURI("a@b.co"));
    const long = encodeOrFail(otpauthURI(`${"y".repeat(200)}@example.test`));
    expect(long.version).toBeGreaterThan(short.version);
    expect(short.errorCorrection).toBe("M");
    expect(long.errorCorrection).toBe("M");
  });

  it("falls back to level L, then refuses text beyond version 20", () => {
    expect(byteCapacity(20, "M")).toBe(666);
    expect(byteCapacity(20, "L")).toBe(858);
    expect(byteCapacity(1, "M")).toBe(14);
    expect(byteCapacity(1, "L")).toBe(17);

    expect(encodeOrFail("A".repeat(666))).toMatchObject({
      errorCorrection: "M",
      version: 20,
    });
    // Beyond 20-M, level L holds it in a smaller version.
    expect(encodeOrFail("A".repeat(667))).toMatchObject({
      errorCorrection: "L",
      version: 18,
    });
    expect(encodeOrFail("A".repeat(858))).toMatchObject({
      errorCorrection: "L",
      version: 20,
    });
    expect(encodeQrCode("A".repeat(859))).toBeNull();
    // Multi-byte characters count as their UTF-8 length.
    expect(encodeQrCode("ệ".repeat(287))).toBeNull();
  });

  it("returns null for empty input and honours a lower version cap", () => {
    expect(encodeQrCode("")).toBeNull();
    expect(encodeQrCode("A".repeat(40), { maxVersion: 1 })).toBeNull();
    expect(encodeOrFail("A".repeat(17), { maxVersion: 1 })).toMatchObject({
      errorCorrection: "L",
      version: 1,
    });
  });

  it("honours a forced mask and level", () => {
    for (let mask = 0; mask < 8; mask += 1) {
      const matrix = encodeOrFail("TutorHub", { errorCorrection: "L", mask });
      expect(matrix.mask).toBe(mask);
      expect(readFormatCopies(matrix).first).toBe(formatBits("L", mask));
    }
  });
});

describe("QR building blocks", () => {
  it("computes the Reed-Solomon remainder of a known 1-M block", () => {
    expect(
      reedSolomonRemainder(
        [32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17],
        10,
      ),
    ).toEqual([196, 35, 39, 119, 235, 215, 231, 226, 93, 23]);
  });

  it("produces the published format and version information", () => {
    expect(formatBits("M", 0)).toBe(0b101010000010010);
    expect(formatBits("L", 0)).toBe(0b111011111000100);
    expect(formatBits("L", 4)).toBe(0b110011000101111);
    expect(versionBits(7)).toBe(0x07c94);
  });

  it("places alignment patterns at the standard positions", () => {
    expect(alignmentPatternPositions(1)).toEqual([]);
    expect(alignmentPatternPositions(2)).toEqual([6, 18]);
    expect(alignmentPatternPositions(7)).toEqual([6, 22, 38]);
    expect(alignmentPatternPositions(15)).toEqual([6, 26, 48, 70]);
    expect(alignmentPatternPositions(20)).toEqual([6, 34, 62, 90]);
  });
});

describe("qrSvgPath", () => {
  it("adds a four-module quiet zone around the symbol", () => {
    const matrix = encodeOrFail(otpauthURI("a@b.co"));
    const { path, viewBoxSize } = qrSvgPath(matrix);
    expect(QR_QUIET_ZONE).toBe(4);
    expect(viewBoxSize).toBe(matrix.size + 8);
    const starts = [...path.matchAll(/M(\d+) (\d+)/g)].map(([, x, y]) => [
      Number(x),
      Number(y),
    ]);
    expect(starts.length).toBeGreaterThan(0);
    for (const [x, y] of starts) {
      expect(x).toBeGreaterThanOrEqual(QR_QUIET_ZONE);
      expect(y).toBeGreaterThanOrEqual(QR_QUIET_ZONE);
      expect(x).toBeLessThan(matrix.size + QR_QUIET_ZONE);
      expect(y).toBeLessThan(matrix.size + QR_QUIET_ZONE);
    }
    // The top-left finder's first row is one 7-module run.
    expect(path.startsWith("M4 4h7v1H4z")).toBe(true);
  });
});

describe("formatTotpSecret", () => {
  it("groups the Base32 key in fours for manual entry", () => {
    expect(formatTotpSecret("JBSWY3DPEHPK3PXP")).toBe("JBSW Y3DP EHPK 3PXP");
    expect(formatTotpSecret("jbsw y3dp-ehpk3pxpA")).toBe(
      "JBSW Y3DP EHPK 3PXP A",
    );
    expect(formatTotpSecret("")).toBe("");
  });
});
