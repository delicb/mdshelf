// Shared knowledge between playwright.config.js, start-server.js, and tests.
"use strict";

const path = require("node:path");

const port = Number(process.env.MDSHELF_E2E_PORT || 7399);
const baseURL = `http://localhost:${port}`;
const binaryPath = process.env.MDSHELF_BIN
  ? path.resolve(process.env.MDSHELF_BIN)
  : path.resolve(__dirname, "..", "mdshelf-e2e");
const fixturesDir = path.join(__dirname, "fixtures");
// The server serves a disposable copy of the fixtures so tests can edit files.
const runtimeDir = path.join(__dirname, ".runtime");

module.exports = { port, baseURL, binaryPath, fixturesDir, runtimeDir };
