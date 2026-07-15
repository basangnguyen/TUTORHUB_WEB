import { readFile, mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import openapiTS, { astToString, COMMENT_HEADER } from "openapi-typescript";
import { format } from "prettier";

const packageDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const contractPath = resolve(packageDirectory, "../../openapi/tutorhub.yaml");
const outputPath = resolve(packageDirectory, "src/generated/schema.ts");
const checkOnly = process.argv.includes("--check");

const ast = await openapiTS(pathToFileURL(contractPath), {
  alphabetize: true,
  exportType: true,
  immutable: true,
});
const generated = await format(`${COMMENT_HEADER}${astToString(ast)}`, {
  endOfLine: "lf",
  parser: "typescript",
});

if (checkOnly) {
  let committed;
  try {
    committed = await readFile(outputPath, "utf8");
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") {
      console.error(
        "Generated API contract is missing. Run pnpm api:generate.",
      );
      process.exitCode = 1;
      process.exit();
    }
    throw error;
  }

  if (committed !== generated) {
    console.error(
      "Generated API contract is stale. Run pnpm api:generate and commit the result.",
    );
    process.exitCode = 1;
  }
} else {
  await mkdir(dirname(outputPath), { recursive: true });
  await writeFile(outputPath, generated, "utf8");
  console.log(`Generated ${outputPath}`);
}
