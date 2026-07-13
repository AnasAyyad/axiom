import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const files = [];

function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(absolute);
    } else if (entry.isFile() && entry.name.endsWith(".md")) {
      files.push(absolute);
    }
  }
}

walk(path.join(root, "docs"));
for (const relative of ["AGENTS.md", "README.md", "deploy/README.md"]) {
  const absolute = path.join(root, relative);
  if (fs.existsSync(absolute)) files.push(absolute);
}

const anchorCache = new Map();

function anchorsFor(file) {
  if (anchorCache.has(file)) return anchorCache.get(file);
  const anchors = new Set();
  const duplicates = new Map();
  for (const line of fs.readFileSync(file, "utf8").split(/\r?\n/)) {
    const match = /^(#{1,6})\s+(.+?)\s*#*$/.exec(line);
    if (!match) continue;
    let slug = match[2]
      .toLowerCase()
      .replace(/<[^>]*>/g, "")
      .replace(/[^\p{L}\p{N}\s_-]/gu, "")
      .replace(/\s/g, "-");
    const count = duplicates.get(slug) ?? 0;
    duplicates.set(slug, count + 1);
    if (count > 0) slug += "-" + count;
    anchors.add(slug);
  }
  anchorCache.set(file, anchors);
  return anchors;
}

const failures = [];
for (const source of files) {
  const markdown = fs.readFileSync(source, "utf8");
  const links = /!?\[[^\]]*\]\(([^)]+)\)/g;
  let match;
  while ((match = links.exec(markdown)) !== null) {
    let target = match[1].trim();
    if (target.startsWith("<") && target.endsWith(">")) {
      target = target.slice(1, -1);
    }
    if (/^(https?:|mailto:)/i.test(target)) continue;

    const hashIndex = target.indexOf("#");
    const pathPart = hashIndex >= 0 ? target.slice(0, hashIndex) : target;
    const anchor = hashIndex >= 0 ? target.slice(hashIndex + 1) : "";
    let resolved = source;
    if (pathPart !== "") {
      try {
        resolved = path.resolve(
          path.dirname(source),
          decodeURIComponent(pathPart),
        );
      } catch {
        failures.push(source + ": invalid encoded link " + target);
        continue;
      }
    }
    if (!fs.existsSync(resolved)) {
      failures.push(source + ": missing target " + target);
      continue;
    }
    if (anchor !== "" && resolved.endsWith(".md")) {
      let decodedAnchor;
      try {
        decodedAnchor = decodeURIComponent(anchor).toLowerCase();
      } catch {
        failures.push(source + ": invalid encoded anchor " + target);
        continue;
      }
      if (!anchorsFor(resolved).has(decodedAnchor)) {
        failures.push(source + ": missing anchor " + target);
      }
    }
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}

console.log(
  "validated " +
    files.length +
    " Markdown files; all local links and anchors resolve",
);
