const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

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
  const colorTheme = { value: "" };
  const documentPath = { hidden: true, textContent: "" };
  const documentView = { hidden: true };
  const root = { dataset: {} };
  const syntaxTheme = { value: "" };
  const elements = new Map([
    ["#color-theme", colorTheme],
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
    URL,
    console,
    document,
    window,
  });
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

test("Theme choices load, apply, and persist", () => {
  const api = loadApp(null, true, {
    "mdshelf.colorTheme": "light",
    "mdshelf.syntaxTheme": "solarized-auto",
  });
  assert.equal(api.rootElement.dataset.colorTheme, "light");
  assert.equal(api.rootElement.dataset.syntaxTheme, "solarized-light");
  assert.equal(api.colorThemeElement.value, "light");
  assert.equal(api.syntaxThemeElement.value, "solarized-auto");

  api.setColorTheme("nord");
  assert.equal(api.rootElement.dataset.colorTheme, "nord");
  assert.equal(api.rootElement.dataset.syntaxTheme, "solarized-dark");
  assert.equal(api.storage.get("mdshelf.colorTheme"), "nord");

  api.setSyntaxTheme("dracula");
  assert.equal(api.rootElement.dataset.syntaxTheme, "dracula");
  assert.equal(api.storage.get("mdshelf.syntaxTheme"), "dracula");
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
