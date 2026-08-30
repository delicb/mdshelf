// Prepares a disposable copy of the fixtures and runs the mdshelf binary on it.
// Invoked by the webServer entry in playwright.config.js, which guarantees the
// copy exists before any test runs, whatever order Playwright starts things in.
"use strict";

const { spawn } = require("node:child_process");
const fs = require("node:fs");

const { port, binaryPath, fixturesDir, runtimeDir } = require("./server-env.js");

if (!fs.existsSync(binaryPath)) {
  console.error(
    `mdshelf binary not found at ${binaryPath}.\n` +
    "Build it first (from the repository root): go build -o mdshelf-e2e .\n" +
    "Or point MDSHELF_BIN at an existing binary.",
  );
  process.exit(1);
}

fs.rmSync(runtimeDir, { recursive: true, force: true });
fs.cpSync(fixturesDir, runtimeDir, { recursive: true });

const child = spawn(binaryPath, ["-port", String(port), runtimeDir], { stdio: "inherit" });
child.on("error", (error) => {
  console.error(`could not start ${binaryPath}: ${error.message}`);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  process.exit(signal ? 1 : code ?? 1);
});
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => child.kill(signal));
}
