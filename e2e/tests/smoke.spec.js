// Browser smoke tests for behavior the node:test unit suite cannot exercise:
// real event wiring, focus management, keyboard navigation, and live updates.
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { test, expect } = require("@playwright/test");

const { runtimeDir } = require("../server-env.js");

async function openDocument(page, hash = "") {
  await page.goto(`/${hash}`);
  await expect(page.locator("#document")).toBeVisible();
}

test.describe("MDShelf smoke", () => {
  test("document list renders and a document opens", async ({ page }) => {
    await openDocument(page);
    // Without a readme the first document alphabetically becomes the home page.
    await expect(page.locator("#document h1").first()).toContainText("Field Guide");

    await page.getByRole("button", { name: "Open document list" }).click();
    await expect(page.locator("#file-count")).toHaveText("3 documents");
    await expect(page.locator(".file-link")).toHaveCount(3);

    await page.getByRole("link", { name: "notes.md" }).click();
    await expect(page.locator("#current-file")).toHaveText("notes.md");
    await expect(page.locator("#document h1").first()).toContainText("Meeting Notes");
  });

  test("drawer filter narrows the list and Enter opens the selection", async ({ page }) => {
    await openDocument(page);

    await page.keyboard.press("/");
    const filter = page.locator("#file-filter");
    await expect(filter).toBeFocused();

    await filter.fill("note");
    await expect(page.locator("#file-count")).toHaveText("1 of 3 documents");
    await expect(page.locator(".file-link")).toHaveCount(1);

    await page.keyboard.press("ArrowDown");
    await expect(page.locator(".file-link", { hasText: "notes.md" })).toBeFocused();
    await page.keyboard.press("Enter");

    await expect(page.locator("#current-file")).toHaveText("notes.md");
    await expect(page.locator("#document h1").first()).toContainText("Meeting Notes");
  });

  test("j and k move the active block marker", async ({ page }) => {
    await openDocument(page, "#/guide.md");
    await expect(page.locator("#document h1").first()).toContainText("Field Guide");

    const active = page.locator(".md-block.is-keyboard-active");
    const status = page.locator("#route-status");

    await page.keyboard.press("j");
    await expect(status).toHaveText(/^Block 2 of \d+$/);
    await expect(active).toHaveCount(1);
    const secondKey = await active.getAttribute("data-md-block");

    await page.keyboard.press("j");
    await expect(status).toHaveText(/^Block 3 of \d+$/);
    const thirdKey = await active.getAttribute("data-md-block");
    expect(thirdKey).not.toBe(secondKey);

    await page.keyboard.press("k");
    await expect(status).toHaveText(/^Block 2 of \d+$/);
    await expect(active).toHaveAttribute("data-md-block", secondKey);
  });

  test("? opens the shortcuts dialog, traps focus, and Escape restores focus", async ({ page }) => {
    await openDocument(page);

    const dialog = page.locator("#shortcut-dialog");
    const openButton = page.getByRole("button", { name: "Show keyboard shortcuts" });
    await openButton.click();
    await expect(dialog).toBeVisible();

    // The backdrop shares the accessible name, so target the dialog's button.
    const closeButton = page.locator("#shortcut-close");
    await expect(closeButton).toBeFocused();

    // The close button is the dialog's only focusable control, so a working
    // focus trap keeps cycling back to it in both directions.
    await page.keyboard.press("Tab");
    await expect(closeButton).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(closeButton).toBeFocused();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(openButton).toBeFocused();
  });

  test("demo review flow: comment, reply, resolve", async ({ page }) => {
    await openDocument(page, "#/__mdshelf_demo__");
    await expect(page.locator("#current-file")).toHaveText("MDShelf demo");
    await expect(page.locator("#review-button")).toBeVisible();
    await expect(page.locator(".md-block-comment").first()).toBeEnabled();

    // "c" comments on the active block (demo comments are in-memory in ad hoc mode).
    await page.keyboard.press("c");
    const composer = page.locator("#comment-composer");
    await expect(composer).toBeVisible();
    await expect(page.locator("#comment-body")).toBeFocused();

    await page.locator("#comment-body").fill("Needs a clearer intro.");
    await page.getByRole("button", { name: "Save comment" }).click();
    await expect(page.locator("#review-live-status")).toHaveText("Demo comment added");

    const bubble = page.locator(".md-comment-bubble");
    await expect(bubble).toHaveCount(1);
    await expect(bubble).toContainText("Needs a clearer intro.");

    const reply = bubble.locator(".comment-reply-input");
    await reply.fill("Thanks, fixed.");
    await reply.press("Enter");
    await expect(page.locator("#review-live-status")).toHaveText("Demo reply added");
    await expect(bubble.locator(".comment-replies li")).toContainText("Thanks, fixed.");

    await bubble.getByRole("button", { name: "Resolve comment" }).click();
    await expect(page.locator("#review-live-status")).toHaveText("Demo comment resolved");
    await expect(page.locator(".md-comment-bubble")).toHaveCount(0);

    // The panel still lists the resolved thread.
    await page.keyboard.press("r");
    await expect(page.locator("#review-panel")).toBeVisible();
    await expect(page.locator("#review-count-summary")).toHaveText("1 comment");
    const thread = page.locator(".review-thread");
    await expect(thread).toHaveClass(/is-resolved/);
    await expect(thread.locator(".review-thread-status")).toHaveText("Resolved");
    await expect(thread).toContainText("Needs a clearer intro.");
  });

  test("design and appearance settings persist across a reload", async ({ page }) => {
    await openDocument(page);

    await page.getByRole("button", { name: "Open settings" }).click();
    await expect(page.locator("#settings-popup")).toBeVisible();

    await page.locator("#design").selectOption("signal");
    await page.locator("#appearance").selectOption("dark");
    const root = page.locator("html");
    await expect(root).toHaveAttribute("data-design", "signal");
    await expect(root).toHaveAttribute("data-appearance", "dark");
    await expect(root).toHaveAttribute("data-scheme", "dark");

    await page.reload();
    await expect(page.locator("#document")).toBeVisible();
    await expect(root).toHaveAttribute("data-design", "signal");
    await expect(root).toHaveAttribute("data-appearance", "dark");
    await expect(root).toHaveAttribute("data-scheme", "dark");
    await expect(page.locator("#design")).toHaveValue("signal");
    await expect(page.locator("#appearance")).toHaveValue("dark");
  });

  test("editing a file on disk updates and highlights the changed block", async ({ page }) => {
    // Reset the served copy first so reruns and CI retries start clean, and
    // use a unique marker so every attempt produces a real content change.
    const livePath = path.join(runtimeDir, "live.md");
    const seedText = "The seed paragraph sits here until the live-update test rewrites it.";
    const marker = `rewrite-${Date.now()}`;
    const updatedText = `The rewritten paragraph (${marker}) proves live updates reach the browser.`;
    const header = "# Live Notes\n\nThis opening paragraph never changes during the test run.\n\n";
    fs.writeFileSync(livePath, `${header}${seedText}\n`);

    await openDocument(page, "#/live.md");
    await expect(page.locator("#document")).toContainText(seedText);

    // Arm the highlight wait before touching the file: the content-change
    // class is only present for about a second after the update lands.
    const highlighted = page.waitForSelector(".markdown .content-change", {
      state: "attached",
      timeout: 15_000,
    });

    fs.writeFileSync(livePath, `${header}${updatedText}\n`);

    await expect(page.locator("#document")).toContainText(updatedText, { timeout: 15_000 });
    const changed = await highlighted;
    expect(await changed.textContent()).toContain(marker);
    await expect(page.locator("#update-notice")).toHaveText("live.md updated");
  });

  test("heading permalinks appear on hover and on focus", async ({ page }) => {
    await openDocument(page, "#/guide.md");

    const heading = page.locator("#document h2", { hasText: "Getting started" });
    const permalink = heading.locator(".heading-permalink");
    await expect(permalink).toHaveAttribute("aria-label", "Link to Getting started");
    await expect(permalink).toHaveAttribute("href", /#\/guide\.md\?getting-started$/);
    await expect(permalink).toHaveCSS("opacity", "0");

    await heading.hover();
    await expect(permalink).toHaveCSS("opacity", "1");

    // Move the pointer away, then reveal the same permalink with the keyboard.
    await page.locator("#document h1").first().hover();
    await expect(permalink).toHaveCSS("opacity", "0");
    await permalink.focus();
    await expect(permalink).toHaveCSS("opacity", "1");
  });
});
