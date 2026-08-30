// @ts-check
"use strict";

const path = require("node:path");
const { defineConfig } = require("@playwright/test");

const { baseURL } = require("./server-env.js");

/*
 * The suite runs against a prebuilt mdshelf binary:
 *   MDSHELF_BIN       path to the binary (default ../mdshelf-e2e, built with `go build -o mdshelf-e2e .`)
 *   MDSHELF_E2E_PORT  port the test server listens on (default 7399)
 *   MDSHELF_CHROMIUM  optional Chromium executablePath override for sandboxes
 *                     where `npx playwright install` cannot download browsers
 *
 * start-server.js copies e2e/fixtures into e2e/.runtime and serves that copy,
 * so tests may freely edit files on disk (live-update coverage).
 */
module.exports = defineConfig({
  testDir: path.join(__dirname, "tests"),
  outputDir: path.join(__dirname, "test-results"),
  // One worker: every test talks to the same server instance, and the
  // live-update test writes to the served directory mid-run.
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI
    ? [["list"], ["html", { outputFolder: path.join(__dirname, "playwright-report"), open: "never" }]]
    : [["list"]],
  use: {
    baseURL,
    viewport: { width: 1280, height: 900 },
    trace: "retain-on-failure",
    ...(process.env.MDSHELF_CHROMIUM
      ? { launchOptions: { executablePath: process.env.MDSHELF_CHROMIUM } }
      : {}),
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: {
    command: `node ${JSON.stringify(path.join(__dirname, "start-server.js"))}`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
