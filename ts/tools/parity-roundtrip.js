// parity-roundtrip — read a crtx v0.1 envelope JSON file at argv[2],
// parse via @hop-top/stem parseEnvelope, re-serialize via
// serializeEnvelope, write to stdout. Used by tools/parity/runner.sh.
//
// Implementation is plain JS so it runs without a TS compiler step.
// It depends on the compiled output in ts/dist/ — the runner builds
// the SDK first via `pnpm build` (or `pnpm install --ignore-scripts`
// + `pnpm build`).

const fs = require("node:fs");
const path = require("node:path");

const stem = require(path.resolve(__dirname, "..", "dist", "index.js"));

const argv = process.argv.slice(2);
if (argv.length !== 1) {
  process.stderr.write("usage: parity-roundtrip <envelope.json>\n");
  process.exit(2);
}

const fixturePath = argv[0];
let text;
try {
  text = fs.readFileSync(fixturePath, "utf8");
} catch (err) {
  process.stderr.write(`parity-roundtrip: read ${fixturePath}: ${err.message}\n`);
  process.exit(1);
}

let env;
try {
  env = stem.parseEnvelope(text);
} catch (err) {
  process.stderr.write(`parity-roundtrip: parse: ${err.message}\n`);
  process.exit(1);
}

let out;
try {
  out = stem.serializeEnvelope(env);
} catch (err) {
  process.stderr.write(`parity-roundtrip: serialize: ${err.message}\n`);
  process.exit(1);
}

process.stdout.write(out);
