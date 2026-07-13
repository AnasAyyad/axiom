import fs from "node:fs";
import path from "node:path";

const source = path.resolve("web/dist");
const destination = path.resolve("internal/api/static/dist");

if (!fs.existsSync(path.join(source, "index.html"))) {
  throw new Error("web_build_missing");
}

fs.mkdirSync(destination, { recursive: true });
for (const entry of fs.readdirSync(destination)) {
  if (entry !== ".gitkeep") {
    fs.rmSync(path.join(destination, entry), { recursive: true, force: true });
  }
}
fs.cpSync(source, destination, { recursive: true });
console.log("embedded React build output into the Go asset package");
