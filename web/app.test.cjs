const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

function loadApp(mermaid) {
  const window = {
    __MDSHELF_TEST__: true,
    matchMedia: () => ({ matches: false }),
    mermaid,
  };
  const documentPath = { hidden: true, textContent: "" };
  const document = {
    querySelector: (selector) => selector === "#document-path" ? documentPath : null,
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
