// Appends headers.extra (security and caching headers) to the dist/_headers file that Blume writes.
//
// Blume generates _headers with the charset and discovery rules it needs, and leaves the file alone when public/_headers exists. Shipping our
// own public/_headers would therefore mean copying Blume's rules and keeping them in step with every upgrade, so the extra rules are
// appended after the build instead. Cloudflare applies every rule whose path matches, so the two sets combine.
import { appendFile, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const target = join(root, "dist", "_headers");

const extra = await readFile(join(root, "headers.extra"), "utf8");
let current = "";
try {
  current = await readFile(target, "utf8");
} catch (error) {
  if (error.code !== "ENOENT") {
    throw error;
  }
}

const separator = current === "" || current.endsWith("\n") ? "" : "\n";
await appendFile(target, `${separator}${extra}`);
console.log(`Appended headers.extra to ${target}`);
