// Validates every spec/examples frontmatter against spec/schema/*.json,
// so the JSON Schemas and the Go validator cannot silently drift apart.
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const yaml = require("js-yaml");
const addFormats = require("ajv-formats");
const Ajv2020Module = require("ajv/dist/2020.js");
const Ajv2020 = Ajv2020Module.default ?? Ajv2020Module;

const root = fileURLToPath(new URL("../spec", import.meta.url));
const ajv = new Ajv2020({ strict: false });
addFormats(ajv);

function loadSchema(name) {
  const schema = JSON.parse(readFileSync(join(root, "schema", name), "utf8"));
  return ajv.compile(schema);
}

function frontmatter(path) {
  const lines = readFileSync(path, "utf8").split("\n");
  if (lines[0].trim() !== "---") throw new Error(`${path}: no frontmatter`);
  const end = lines.indexOf("---", 1);
  if (end < 0) throw new Error(`${path}: unterminated frontmatter`);
  return yaml.load(lines.slice(1, end).join("\n"));
}

let checked = 0;
const nodeValidate = loadSchema("node.schema.json");
const entryValidate = loadSchema("entry.schema.json");
const repoValidate = loadSchema("repo.schema.json");

const repoDoc = yaml.load(readFileSync(join(root, "examples", "memory.yaml"), "utf8"));
if (!repoValidate(repoDoc)) throw new Error(`memory.yaml: ${JSON.stringify(repoValidate.errors)}`);
checked += 1;

function walk(dir) {
  for (const name of readdirSync(dir)) {
    if (name === "memories") {
      for (const entry of readdirSync(join(dir, name))) {
        if (!entry.endsWith(".md")) continue;
        const doc = frontmatter(join(dir, name, entry));
        if (!entryValidate(doc)) {
          throw new Error(`${entry}: ${JSON.stringify(entryValidate.errors)}`);
        }
        checked += 1;
      }
      continue;
    }
    if (name === "node.md") {
      const doc = frontmatter(join(dir, name));
      if (!nodeValidate(doc)) throw new Error(`${dir}/node.md: ${JSON.stringify(nodeValidate.errors)}`);
      checked += 1;
      continue;
    }
    let nested;
    try {
      nested = readdirSync(join(dir, name), { withFileTypes: true });
    } catch {
      continue;
    }
    if (Array.isArray(nested) && nested.some((d) => d.isDirectory())) {
      walk(join(dir, name));
    }
  }
}
walk(join(root, "examples"));
console.log(`schema-check: ${checked} documents valid`);
