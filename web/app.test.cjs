const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");
const textSelection = require("./text-selection.js");
require("./text-selection.test.cjs");

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

function loadApp(mermaid, dark = false, stored = {}, katex = null) {
  const storage = new Map(Object.entries(stored));
  const window = {
    __MDSHELF_TEST__: true,
    localStorage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
    katex,
    matchMedia: (query) => ({ matches: dark && query === "(prefers-color-scheme: dark)" }),
    mermaid,
  };
  const appearance = { value: "" };
  const design = { value: "" };
  const documentPath = { hidden: true, textContent: "" };
  const documentView = { hidden: true };
  const root = { dataset: {} };
  const syntaxTheme = { value: "" };
  const elements = new Map([
    ["#appearance", appearance],
    ["#design", design],
    ["#document", documentView],
    ["#document-path", documentPath],
    ["#syntax-theme", syntaxTheme],
  ]);
  const document = {
    createElement: () => ({
      attributes: new Map(),
      dataset: {},
      setAttribute(name, value) { this.attributes.set(name, value); },
    }),
    documentElement: root,
    querySelector: (selector) => elements.get(selector) ?? null,
  };
  const context = vm.createContext({
    AbortController,
    Intl,
    Promise,
    Set,
    Map,
    Uint32Array,
    TextEncoder,
    URL,
    console,
    document,
    window,
  });
  vm.runInContext(fs.readFileSync(new URL("text-selection.js", `file://${__dirname}/`), "utf8"), context);
  vm.runInContext(fs.readFileSync(new URL("app.js", `file://${__dirname}/`), "utf8"), context);
  window.__MDSHELF_TEST_API__.storage = storage;
  return window.__MDSHELF_TEST_API__;
}

function diagram(source) {
  const classes = new Set();
  return {
    textContent: source,
    innerHTML: "",
    classList: { add: (name) => classes.add(name) },
    prepend() {},
    classes,
  };
}

test("The document path shows and hides", () => {
  const api = loadApp(null);
  api.setDocumentPath("/Users/example/notes/guide.md");
  assert.equal(api.documentPathElement.textContent, "/Users/example/notes/guide.md");
  assert.equal(api.documentPathElement.hidden, false);

  api.setDocumentPath("");
  assert.equal(api.documentPathElement.textContent, "");
  assert.equal(api.documentPathElement.hidden, true);
});

test("The embedded demo is always available", () => {
  const api = loadApp(null);
  assert.equal(api.isDocumentAvailable("__mdshelf_demo__"), true);
  assert.equal(api.isDocumentAvailable("missing.md"), false);
  assert.equal(api.buildRoute("__mdshelf_demo__", "alerts and callouts"), "#/__mdshelf_demo__?alerts%20and%20callouts");
  assert.equal(api.shouldReloadDocument("__mdshelf_demo__", true, null), false);
  assert.equal(api.shouldReloadDocument("guide.md", true, null), true);
});

test("Daemon document removal uses the opaque document ID", () => {
  const api = loadApp(null);
  const request = api.daemonRemoveRequest("6391cb20c5940d2f477c6589");
  assert.equal(request.method, "POST");
  assert.equal(request.headers["Content-Type"], "application/json");
  assert.deepEqual(JSON.parse(request.body), { id: "6391cb20c5940d2f477c6589" });
});

test("Heading permalinks use document routes and accessible labels", () => {
  const api = loadApp(null);
  const appended = [];
  const heading = {
    append: (...values) => appended.push(...values),
    id: "setup",
    querySelector: () => null,
    textContent: "Setup",
  };
  api.addHeadingPermalinks({ querySelectorAll: () => [heading] }, "guides/start.md");

  const link = appended[1];
  assert.equal(link.href, "#/guides/start.md?setup");
  assert.equal(link.dataset.documentRoute, "true");
  assert.equal(link.attributes.get("aria-label"), "Link to Setup");
  assert.equal(link.textContent, "#");
});

test("Code copy text excludes rendered line numbers", () => {
  const api = loadApp(null);
  const clone = {
    textContent: "1 code line\n",
    querySelectorAll: () => [{ remove() { clone.textContent = "code line\n"; } }],
  };
  const source = { cloneNode: () => clone };
  const figure = {
    querySelector: (selector) => selector === ".lntd:last-child pre" ? source : null,
  };
  assert.equal(api.codeBlockText(figure), "code line\n");
});

test("Design and appearance load, apply, and persist", () => {
  const api = loadApp(null, true, {
    "mdshelf.design": "signal",
    "mdshelf.appearance": "light",
    "mdshelf.syntaxTheme": "solarized-auto",
  });
  assert.equal(api.rootElement.dataset.design, "signal");
  assert.equal(api.rootElement.dataset.scheme, "light");
  assert.equal(api.rootElement.dataset.syntaxTheme, "solarized-light");
  assert.equal(api.designElement.value, "signal");
  assert.equal(api.appearanceElement.value, "light");

  api.setAppearance("system");
  assert.equal(api.rootElement.dataset.scheme, "dark");
  assert.equal(api.rootElement.dataset.syntaxTheme, "solarized-dark");
  assert.equal(api.storage.get("mdshelf.appearance"), "system");

  api.setDesign("column");
  assert.equal(api.rootElement.dataset.design, "column");
  assert.equal(api.storage.get("mdshelf.design"), "column");

  api.setDesign("nord");
  assert.equal(api.rootElement.dataset.design, "column");

  api.setSyntaxTheme("dracula");
  assert.equal(api.rootElement.dataset.syntaxTheme, "dracula");
  assert.equal(api.storage.get("mdshelf.syntaxTheme"), "dracula");
});

test("Ink is the design a new reader gets", () => {
  const api = loadApp(null);
  assert.equal(api.rootElement.dataset.design, "ink");
  assert.equal(api.rootElement.dataset.appearance, "system");
  assert.equal(api.rootElement.dataset.scheme, "light");
});

test("Math expressions render with safe KaTeX options", () => {
  let call;
  const katex = {
    render(source, element, options) { call = { source, element, options }; },
  };
  const expression = {
    classList: { add() {} },
    dataset: { display: "true" },
    textContent: "E = mc^2",
    title: "",
  };
  const api = loadApp(null, false, {}, katex);
  api.renderMath({ querySelectorAll: () => [expression] });

  assert.equal(call.source, "E = mc^2");
  assert.equal(call.element, expression);
  assert.equal(call.options.displayMode, true);
  assert.equal(call.options.throwOnError, false);
  assert.equal(call.options.trust, false);
});

test("Mermaid uses the active color scheme", () => {
  for (const [dark, want] of [[false, "default"], [true, "dark"]]) {
    let options;
    const api = loadApp({ initialize(value) { options = value; } }, dark);
    api.initializeMermaid();
    assert.equal(options.theme, want);
    assert.equal(options.startOnLoad, false);
    assert.equal(options.securityLevel, "strict");
  }
});

test("Mermaid calls stay serialized across document renders", async () => {
  const firstParse = deferred();
  const calls = [];
  const mermaid = {
    async parse(source) {
      calls.push(`parse:${source}`);
      if (source === "first") await firstParse.promise;
    },
    async render(_id, source) {
      calls.push(`render:${source}`);
      return { svg: `<svg>${source}</svg>` };
    },
  };
  const api = loadApp(mermaid);
  const first = diagram("first");
  const second = diagram("second");
  const firstRender = api.renderMermaid({ querySelectorAll: () => [first] }, () => true);
  const secondRender = api.renderMermaid({ querySelectorAll: () => [second] }, () => true);
  await Promise.resolve();
  assert.deepEqual(calls, ["parse:first"]);

  firstParse.resolve();
  await Promise.all([firstRender, secondRender]);
  assert.deepEqual(calls, ["parse:first", "render:first", "parse:second", "render:second"]);
  assert.equal(first.innerHTML, "<svg>first</svg>");
  assert.equal(second.innerHTML, "<svg>second</svg>");
});

test("A stale Mermaid load cannot mutate its document", async () => {
  const parseGate = deferred();
  let renderCalls = 0;
  let current = true;
  const api = loadApp({
    async parse() { await parseGate.promise; },
    async render() {
      renderCalls += 1;
      return { svg: "<svg>stale</svg>" };
    },
  });
  const stale = diagram("stale");
  const rendering = api.renderMermaid({ querySelectorAll: () => [stale] }, () => current);
  await Promise.resolve();
  current = false;
  parseGate.resolve();
  await rendering;

  assert.equal(renderCalls, 0);
  assert.equal(stale.innerHTML, "");
  assert.equal(stale.classes.size, 0);
});

test("Navigation tokens reject aborted and replaced loads", () => {
  const api = loadApp(null);
  const first = new AbortController();
  const second = new AbortController();
  api.setAbortController(first);
  assert.equal(api.isCurrentLoad(first), true);
  api.setAbortController(second);
  assert.equal(api.isCurrentLoad(first), false);
  api.cancelDocumentLoad();
  assert.equal(second.signal.aborted, true);
  assert.equal(api.isCurrentLoad(second), false);
});

test("Reading shortcuts map to block and panel actions", () => {
  const api = loadApp(null);
  const expected = new Map([
    ["ArrowUp", "previous-block"],
    ["ArrowLeft", "previous-block"],
    ["k", "previous-block"],
    ["h", "previous-block"],
    ["ArrowDown", "next-block"],
    ["ArrowRight", "next-block"],
    ["j", "next-block"],
    ["l", "next-block"],
    ["Home", "first-block"],
    ["End", "last-block"],
    ["c", "comment"],
    ["/", "documents"],
    ["r", "comments"],
    ["?", "shortcuts"],
  ]);
  for (const [key, action] of expected) assert.equal(api.readingShortcutAction({ key }), action);
  assert.equal(api.readingShortcutAction({ key: "j", ctrlKey: true }), "");
  assert.equal(api.readingShortcutAction({ key: "j", altKey: true }), "");
  assert.equal(api.readingShortcutAction({ key: "j", isComposing: true }), "");
  assert.equal(api.readingShortcutAction({ key: "Escape" }), "");
});

test("The viewport selects the block that crosses its reading line", () => {
  const api = loadApp(null);
  const rects = [
    { top: 20, bottom: 100 },
    { top: 120, bottom: 220 },
    { top: 240, bottom: 340 },
  ];
  assert.equal(api.blockIndexAtViewport(rects, 10), 0);
  assert.equal(api.blockIndexAtViewport(rects, 110), 1);
  assert.equal(api.blockIndexAtViewport(rects, 200), 1);
  assert.equal(api.blockIndexAtViewport(rects, 400), 2);
  assert.equal(api.blockIndexAtViewport([], 100), -1);
});

test("Block navigation scrolls only after the selection crosses the midpoint", () => {
  const api = loadApp(null);
  assert.equal(api.navigationScrollDelta({ top: 300, bottom: 340 }, 52, 600, 1), 0);
  assert.equal(api.navigationScrollDelta({ top: 330, bottom: 370 }, 52, 600, 1), 20);
  assert.equal(api.navigationScrollDelta({ top: 290, bottom: 330 }, 52, 600, -1), -20);
  assert.equal(api.navigationScrollDelta({ top: 330, bottom: 370 }, 52, 600, -1), 0);
  assert.equal(api.navigationScrollDelta({ top: 330, bottom: 370 }, 52, 600, 0), 0);
});

test("Document list navigation stays within its visible items", () => {
  const api = loadApp(null);
  assert.equal(api.listNavigationIndex(-1, 3, "next"), 0);
  assert.equal(api.listNavigationIndex(-1, 3, "previous"), 2);
  assert.equal(api.listNavigationIndex(1, 3, "next"), 2);
  assert.equal(api.listNavigationIndex(2, 3, "next"), 2);
  assert.equal(api.listNavigationIndex(1, 3, "previous"), 0);
  assert.equal(api.listNavigationIndex(0, 3, "previous"), 0);
  assert.equal(api.listNavigationIndex(1, 3, "first"), 0);
  assert.equal(api.listNavigationIndex(1, 3, "last"), 2);
  assert.equal(api.listNavigationIndex(0, 0, "next"), -1);
});

test("Block comment controls use hover or a two-step touch action", () => {
  const api = loadApp(null);
  assert.equal(api.blockCommentTapAction({ block: true }), "none");
  assert.equal(api.blockCommentTapAction({ touch: true, block: true }), "arm");
  assert.equal(api.blockCommentTapAction({ touch: true, block: true, interactive: true }), "none");
  assert.equal(api.blockCommentTapAction({ touch: true, block: true, commentButton: true, interactive: true }), "open");
  assert.equal(api.blockCommentTapAction({ commentButton: true }), "open");
});

test("Comment state actions resolve and reopen", () => {
  const api = loadApp(null);
  assert.equal(api.commentStateAction("open"), "resolve");
  assert.equal(api.commentStateAction("addressed"), "resolve");
  assert.equal(api.commentStateAction("resolved"), "reopen");
});

test("Comment replies accept one level and known authors", () => {
  const api = loadApp(null);
  const path = "00112233445566778899aabb";
  const payload = {
    schemaVersion: 1,
    revision: 2,
    document: {
      id: path,
      path,
      title: "Plan",
      sourceHash: "a".repeat(64),
      reviewStatus: "comments",
    },
    comments: [{
      id: "comment_00112233445566778899aabb",
      body: "Root",
      status: "open",
      baseHash: "a".repeat(64),
      outdated: false,
      anchor: null,
      currentLocation: null,
      currentBlockKey: null,
      replies: [{
        id: "reply_00112233445566778899aabb",
        body: "Reply",
        author: "reviewer",
        createdAt: "2026-08-27T12:00:00Z",
      }],
    }],
  };
  assert.equal(api.validateReviewView(payload, path).comments[0].replies[0].body, "Reply");
  payload.comments[0].replies[0].author = "nested";
  assert.throws(() => api.validateReviewView(payload, path), /invalid comment reply/);
});

test("Text range review data stays optional and validates compatibility fields", () => {
  const api = loadApp(null);
  const path = "00112233445566778899aabb";
  const anchor = {
    blockKey: "112233445566778899aabbcc",
    kind: "paragraph",
    startLine: 2,
    endLine: 2,
    headingPath: ["Heading"],
    quote: "Source text",
  };
  const payload = {
    schemaVersion: 1,
    revision: 1,
    document: {
      id: path,
      path,
      title: "Range",
      sourceHash: "a".repeat(64),
      reviewStatus: "comments",
    },
    comments: [{
      id: "comment_00112233445566778899aabb",
      body: "Range comment",
      status: "open",
      baseHash: "a".repeat(64),
      outdated: false,
      anchor,
      textRange: {
        version: 1,
        anchors: [{ ...anchor }],
        startOffset: 0,
        endOffset: 4,
        quote: "text",
        currentBlockKeys: [anchor.blockKey],
      },
      currentLocation: { startLine: 2, endLine: 2 },
      currentBlockKey: anchor.blockKey,
      replies: [],
    }],
  };
  const comment = api.validateReviewView(payload, path).comments[0];
  assert.equal(comment.textRange.quote, "text");
  assert.equal(comment.ranges.length, 0);

  const overlapping = structuredClone(payload);
  overlapping.comments[0].anchor.endLine = 4;
  overlapping.comments[0].textRange.anchors = [
    { ...overlapping.comments[0].anchor },
    {
      ...overlapping.comments[0].anchor,
      blockKey: "2233445566778899aabbccdd",
      startLine: 4,
      endLine: 5,
    },
  ];
  overlapping.comments[0].textRange.currentBlockKeys.push("33445566778899aabbccddee");
  assert.throws(() => api.validateReviewView(overlapping, path), /unordered text range anchors/);

  delete payload.comments[0].textRange.currentBlockKeys;
  assert.throws(() => api.validateReviewView(payload, path), /no current text range blocks/);
  delete payload.comments[0].textRange;
  assert.equal(api.validateReviewView(payload, path).comments[0].textRange, null);
});

test("A delayed comment save cannot update a new document", () => {
  const api = loadApp(null);
  const mutation = { token: 4, path: "document-a" };
  assert.equal(api.reviewMutationIsCurrent(mutation, { token: 4, path: "document-a", enabled: true }), true);
  assert.equal(api.reviewMutationIsCurrent(mutation, { token: 5, path: "document-a", enabled: true }), false);
  assert.equal(api.reviewMutationIsCurrent(mutation, { token: 4, path: "document-b", enabled: true }), false);
  assert.equal(api.reviewMutationIsCurrent(mutation, { token: 4, path: "document-a", enabled: false }), false);
});

test("A pending Save keeps one focusable comment field", () => {
  const api = loadApp(null);
  assert.equal(api.commentComposerEscapeAction(), "none");
  assert.equal(api.commentComposerEscapeAction({ open: true }), "close");
  assert.equal(api.commentComposerEscapeAction({ open: true, pending: true }), "wait");
  assert.deepEqual(
    JSON.parse(JSON.stringify(api.commentComposerControlState({ pending: true }))),
    { bodyDisabled: false, bodyReadOnly: true, saveDisabled: true, actionsDisabled: true },
  );
  assert.deepEqual(
    JSON.parse(JSON.stringify(api.commentComposerControlState({ blocked: true, invalid: true }))),
    { bodyDisabled: false, bodyReadOnly: true, saveDisabled: true, actionsDisabled: false },
  );
});

test("Enter and Space floating Comment activation use the live selection without pointerdown", () => {
  const api = loadApp(null);
  const live = { quote: "selected text" };
  for (const key of ["Enter", "Space"]) {
    assert.equal(api.selectionCommentLiveDescriptor(null, live, true), live, key);
    assert.equal(api.selectionCommentActionDescriptor(null, live), live, key);
  }
  assert.equal(api.selectionCommentLiveDescriptor(null, live, false), null);
  const latched = { quote: "pointer selection" };
  assert.equal(api.selectionCommentActionDescriptor(latched, live), latched);
  assert.equal(api.selectionCommentActionDescriptor(null, null), null);
});

test("A comment submits with Command-Enter or Control-Enter", () => {
  const api = loadApp(null);
  assert.equal(api.commentSubmitShortcut({ key: "Enter", metaKey: true }), true);
  assert.equal(api.commentSubmitShortcut({ key: "Enter", ctrlKey: true }), true);
  assert.equal(api.commentSubmitShortcut({ key: "Enter", metaKey: true, isComposing: true }), false);
  assert.equal(api.commentSubmitShortcut({ key: "Enter" }), false);
  assert.equal(api.commentSubmitShortcut({ key: "Escape", metaKey: true }), false);
});

test("Review status labels are stable", () => {
  const api = loadApp(null);
  const labels = {
    needs_review: "No comments",
    comments: "Comments",
    updated: "Document updated",
    removed: "Removed",
  };
  for (const [status, label] of Object.entries(labels)) assert.equal(api.reviewStatusLabel(status), label);
  assert.equal(api.reviewStatusLabel("new_status"), "Review status unavailable");
});

test("Comment counts use current block keys and unresolved states", () => {
  const api = loadApp(null);
  const counts = api.commentCountsByBlockKey([
    { currentBlockKey: "moved", status: "open", outdated: false },
    { currentBlockKey: "moved", status: "open", outdated: false },
    { currentBlockKey: "moved", status: "addressed", outdated: false },
    { currentBlockKey: "moved", status: "resolved", outdated: false },
    { currentBlockKey: "old", status: "open", outdated: true },
    { currentBlockKey: "", status: "open", outdated: false },
    { currentBlockKey: null, status: "open", outdated: false },
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify(counts)), {
    moved: { unresolved: 3, total: 4 },
  });
  assert.equal(api.commentBlockKey({ currentBlockKey: "moved", outdated: false }), "moved");
  assert.equal(api.commentBlockKey({ anchor: { blockKey: "original" }, outdated: false }), "original");
  assert.equal(api.commentBlockKey({ currentBlockKey: "old", outdated: true }), "");
});

test("Live review events do not render the document", () => {
  const api = loadApp(null);
  const path = "0123456789abcdef01234567";
  const plan = (changes, reset = false, daemon = true) => JSON.parse(JSON.stringify(
    api.planLiveChanges({ changes, reset }, path, daemon),
  ));

  assert.deepEqual(plan([{ path, kind: "review" }]), {
    refreshFiles: true,
    renderCurrent: false,
    refreshCurrentReview: true,
    reviewAfterRender: false,
    currentRemoved: false,
  });
  assert.equal(plan([{ path: "other", kind: "review" }]).refreshCurrentReview, false);
  assert.equal(plan([{ path, kind: "updated" }]).renderCurrent, true);
  assert.deepEqual(plan([{ path, kind: "updated" }, { path, kind: "review" }]), {
    refreshFiles: true,
    renderCurrent: true,
    refreshCurrentReview: false,
    reviewAfterRender: true,
    currentRemoved: false,
  });
  assert.equal(plan([{ path, kind: "removed" }]).currentRemoved, true);
  assert.equal(plan([], true).reviewAfterRender, true);
  assert.equal(plan([{ path, kind: "review" }], false, false).refreshCurrentReview, false);
  assert.equal(plan([{ path, kind: "invalid" }]).refreshFiles, false);
});

test("Adding a comment follows load and mutation state", () => {
  const api = loadApp(null);
  assert.equal(api.commentAddAvailable({ loaded: true }), true);
  assert.equal(api.commentAddAvailable({ loading: true }), false);
  assert.equal(api.commentAddAvailable({ pending: true }), false);
  assert.equal(api.commentAddAvailable({ error: "failed" }), false);
  assert.equal(api.commentAddAvailable({ loaded: false }), false);
});

test("The review panel is available only for daemon documents", () => {
  const api = loadApp(null);
  const daemon = { daemonMode: true, currentPath: "document", removed: false };
  assert.equal(api.reviewPanelAvailable(daemon), true);
  assert.equal(api.reviewPanelAvailable({ ...daemon, loadError: true }), true);
  assert.equal(api.reviewPanelAvailable({ ...daemon, daemonMode: false }), false);
  assert.equal(api.reviewPanelAvailable({ ...daemon, currentPath: "__mdshelf_demo__" }), false);
  assert.equal(api.reviewPanelAvailable({ ...daemon, removed: true }), false);
  assert.equal(api.reviewPanelAvailable({ ...daemon, currentPath: "" }), false);
});
