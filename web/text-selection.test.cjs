const assert = require("node:assert/strict");
const test = require("node:test");
const textSelection = require("./text-selection.js");

function fixtureText(value) {
  return { nodeType: 3, nodeValue: value, data: value, parentElement: null, parentNode: null };
}

function fixtureElement(tagName, className = "", children = []) {
  const element = {
    nodeType: 1,
    tagName: tagName.toUpperCase(),
    className,
    childNodes: children,
    parentElement: null,
    parentNode: null,
    dataset: {},
  };
  for (const child of children) {
    child.parentElement = element;
    child.parentNode = element;
  }
  return element;
}

function setFixtureStyles(root) {
  const ownerDocument = {
    defaultView: {
      getComputedStyle(element) {
        return {
          display: "block",
          visibility: "visible",
          userSelect: element.fixtureUserSelect || "auto",
          webkitUserSelect: element.fixtureWebkitUserSelect || element.fixtureUserSelect || "auto",
        };
      },
    },
  };
  function visit(node) {
    node.ownerDocument = ownerDocument;
    for (const child of node.childNodes || []) visit(child);
  }
  visit(root);
}

function indexedFixture(records, generation = 1) {
  return textSelection.buildIndex(records.map(([key, element]) => ({ key, element })), generation);
}

test("The canonical index includes text, whitespace, and line breaks", () => {
  const root = fixtureElement("div", "md-block", [
    fixtureText("alpha"),
    fixtureText("   "),
    fixtureElement("br"),
    fixtureText("omega"),
  ]);
  const index = indexedFixture([["a", root]]);
  assert.equal(index.blocks[0].text, "alpha   \nomega");
  assert.deepEqual(index.blocks[0].runs.map((run) => run.type), ["text", "text", "break", "text"]);
});

test("The canonical index excludes each named subtree", () => {
  const classes = [
    "md-block-review-controls",
    "heading-permalink",
    "code-toolbar",
    "ln",
    "lnt",
    "katex-mathml",
    "sr-only",
  ];
  for (const className of classes) {
    const root = fixtureElement("div", "md-block", [
      fixtureText("keep"),
      fixtureElement("span", className, [fixtureText(`drop-${className}`)]),
    ]);
    assert.equal(textSelection.canonicalBlock(className, root).text, "keep", className);
  }
  for (const tagName of ["script", "style"]) {
    const root = fixtureElement("div", "md-block", [fixtureText("keep"), fixtureElement(tagName, "", [fixtureText("drop")])]);
    assert.equal(textSelection.canonicalBlock(tagName, root).text, "keep", tagName);
  }
  const visibleMath = fixtureElement("span", "katex-html", [fixtureText("x + y")]);
  visibleMath.ariaHidden = "true";
  assert.equal(textSelection.canonicalBlock("math", fixtureElement("div", "md-block", [visibleMath])).text, "x + y");

  const mermaidSVG = fixtureElement("svg", "", [fixtureText("diagram label")]);
  const mermaid = fixtureElement("pre", "md-block mermaid-rendered", [mermaidSVG]);
  assert.equal(textSelection.canonicalBlock("mermaid", mermaid).text, "");
});

test("Selection descriptors keep one-block and cross-block offsets", () => {
  const first = { key: "first", order: 0, text: "Heading text" };
  const second = { key: "second", order: 1, text: "Body text" };
  assert.deepEqual(textSelection.requestSelection(textSelection.descriptorFromSlices([
    { block: first, start: 0, end: 7 },
  ], 4)), {
    version: 1,
    blockKeys: ["first"],
    startOffset: 0,
    endOffset: 7,
    quote: "Heading",
  });
  assert.deepEqual(textSelection.requestSelection(textSelection.descriptorFromSlices([
    { block: first, start: 8, end: 12 },
    { block: second, start: 0, end: 4 },
  ], 4)), {
    version: 1,
    blockKeys: ["first", "second"],
    startOffset: 8,
    endOffset: 4,
    quote: "text\n\nBody",
  });
});

test("Selection descriptors enforce the mapped-block limit", () => {
  const blocks = Array.from({ length: textSelection.maxBlocks + 1 }, (_, order) => ({
    key: `block-${order}`,
    order,
    text: "x",
  }));
  assert.ok(textSelection.descriptorFromSlices(blocks.slice(0, textSelection.maxBlocks).map((block) => ({
    block,
    start: 0,
    end: 1,
  })), 1));
  assert.equal(textSelection.descriptorFromSlices(blocks.map((block) => ({ block, start: 0, end: 1 })), 1), null);
});

test("Selection descriptors reject surrogate splits and enforce the UTF-8 limit", () => {
  const unicode = { key: "unicode", order: 0, text: "A😀B" };
  assert.equal(textSelection.descriptorFromSlices([{ block: unicode, start: 2, end: 4 }], 1), null);
  assert.equal(textSelection.descriptorFromSlices([{ block: unicode, start: 1, end: 2 }], 1), null);
  assert.equal(textSelection.descriptorFromSlices([{ block: unicode, start: 1, end: 3 }], 1).quote, "😀");

  const exact = "x".repeat(16 * 1024);
  const block = { key: "limit", order: 0, text: `${exact}x` };
  assert.ok(textSelection.descriptorFromSlices([{ block, start: 0, end: exact.length }], 1));
  assert.equal(textSelection.descriptorFromSlices([{ block, start: 0, end: exact.length + 1 }], 1), null);

  const multibyte = "é".repeat(8 * 1024);
  const unicodeLimit = { key: "unicode-limit", order: 0, text: `${multibyte}é` };
  assert.ok(textSelection.descriptorFromSlices([{ block: unicodeLimit, start: 0, end: multibyte.length }], 1));
  assert.equal(textSelection.descriptorFromSlices([{ block: unicodeLimit, start: 0, end: multibyte.length + 1 }], 1), null);
});

test("Native forward, backward, text, and element endpoints use document order", () => {
  const text = fixtureText("select me");
  const root = fixtureElement("div", "md-block", [text]);
  root.dataset.mdBlock = "block";
  const article = fixtureElement("article", "", [root]);
  const index = indexedFixture([["block", root]], 8);
  const textRange = {
    collapsed: false,
    startContainer: text,
    startOffset: 0,
    endContainer: text,
    endOffset: 6,
    comparePoint(node, offset) {
      if (node !== text) throw new Error("wrong node");
      if (offset < this.startOffset) return -1;
      if (offset > this.endOffset) return 1;
      return 0;
    },
    intersectsNode: () => false,
    cloneRange() { return this; },
    getClientRects: () => [],
  };
  const forward = { rangeCount: 1, isCollapsed: false, anchorNode: text, focusNode: text, getRangeAt: () => textRange };
  const backward = { ...forward, anchorOffset: 6, focusOffset: 0 };
  assert.equal(textSelection.captureSelection(forward, index, article).quote, "select");
  assert.equal(textSelection.captureSelection(backward, index, article).quote, "select");

  const elementRange = {
    ...textRange,
    startContainer: root,
    startOffset: 0,
    endContainer: root,
    endOffset: 1,
    comparePoint: () => 0,
  };
  assert.equal(textSelection.captureSelection({ ...forward, getRangeAt: () => elementRange }, index, article).quote, "select me");
});

test("Selection capture removes an empty boundary block", () => {
  const empty = fixtureElement("div", "md-block");
  empty.dataset.mdBlock = "empty";
  const text = fixtureText("keep");
  const filled = fixtureElement("div", "md-block", [text]);
  filled.dataset.mdBlock = "filled";
  const article = fixtureElement("article", "", [empty, filled]);
  const index = indexedFixture([["empty", empty], ["filled", filled]]);
  const range = {
    collapsed: false,
    startContainer: empty,
    startOffset: 0,
    endContainer: text,
    endOffset: text.nodeValue.length,
    comparePoint: () => 0,
    cloneRange() { return this; },
    getClientRects: () => [],
  };
  const selection = { rangeCount: 1, isCollapsed: false, getRangeAt: () => range };
  assert.deepEqual(textSelection.requestSelection(textSelection.captureSelection(selection, index, article)), {
    version: 1,
    blockKeys: ["filled"],
    startOffset: 0,
    endOffset: 4,
    quote: "keep",
  });
});

test("Selection capture rejects excluded boundary content", () => {
  const included = fixtureText("keep");
  const excluded = fixtureText("comment control");
  const control = fixtureElement("div", "md-block-review-controls", [excluded]);
  const root = fixtureElement("div", "md-block", [included, control]);
  root.dataset.mdBlock = "block";
  const article = fixtureElement("article", "", [root]);
  const index = indexedFixture([["block", root]]);
  const range = {
    collapsed: false,
    startContainer: excluded,
    startOffset: 0,
    endContainer: excluded,
    endOffset: excluded.nodeValue.length,
  };
  const selection = { rangeCount: 1, isCollapsed: false, getRangeAt: () => range };
  assert.equal(textSelection.captureSelection(selection, index, article), null);
});

test("Selection capture rejects selectable excluded text inside the native range", () => {
  for (const [tagName, className] of [["a", "heading-permalink"], ["div", "code-toolbar"]]) {
    const first = fixtureText("before");
    const excluded = fixtureElement(tagName, className, [fixtureText("excluded")]);
    const last = fixtureText("after");
    const root = fixtureElement("div", "md-block", [first, excluded, last]);
    root.dataset.mdBlock = "block";
    const article = fixtureElement("article", "", [root]);
    setFixtureStyles(article);
    const index = indexedFixture([["block", root]]);
    const range = {
      collapsed: false,
      startContainer: first,
      startOffset: 0,
      endContainer: last,
      endOffset: last.nodeValue.length,
      comparePoint: () => 0,
      intersectsNode: (node) => node === excluded,
      cloneRange() { return this; },
      getClientRects: () => [],
    };
    const selection = { rangeCount: 1, isCollapsed: false, getRangeAt: () => range };
    assert.equal(textSelection.captureSelection(selection, index, article), null, className);
  }
});

test("Cross-block selection ignores nonselectable and hidden exclusions between endpoints", () => {
  const firstText = fixtureText("first");
  const secondText = fixtureText("second");
  const controls = fixtureElement("div", "md-block-review-controls", [fixtureText("+")]);
  controls.fixtureWebkitUserSelect = "none";
  const lineNumber = fixtureElement("span", "ln", [fixtureText("1")]);
  lineNumber.fixtureUserSelect = "none";
  const tableLineNumber = fixtureElement("span", "lnt", [fixtureText("2")]);
  tableLineNumber.fixtureUserSelect = "none";
  const first = fixtureElement("div", "md-block", [
    firstText,
    controls,
    lineNumber,
    tableLineNumber,
    fixtureElement("span", "katex-mathml", [fixtureText("math duplicate")]),
    fixtureElement("span", "sr-only", [fixtureText("screen reader text")]),
    fixtureElement("script", "", [fixtureText("script text")]),
    fixtureElement("style", "", [fixtureText("style text")]),
  ]);
  first.dataset.mdBlock = "first";
  const second = fixtureElement("div", "md-block", [secondText]);
  second.dataset.mdBlock = "second";
  const article = fixtureElement("article", "", [first, second]);
  setFixtureStyles(article);
  const index = indexedFixture([["first", first], ["second", second]], 3);
  const range = {
    collapsed: false,
    startContainer: firstText,
    startOffset: 0,
    endContainer: secondText,
    endOffset: secondText.nodeValue.length,
    comparePoint: () => 0,
    intersectsNode: () => true,
    cloneRange() { return this; },
    getClientRects: () => [],
  };
  const selection = { rangeCount: 1, isCollapsed: false, getRangeAt: () => range };
  assert.deepEqual(textSelection.requestSelection(textSelection.captureSelection(selection, index, article)), {
    version: 1,
    blockKeys: ["first", "second"],
    startOffset: 0,
    endOffset: 6,
    quote: "first\n\nsecond",
  });
});

test("Text range reconstruction checks the quote and merges safe fragments", () => {
  const ownerDocument = {
    createRange() {
      return {
        setStart(node, offset) { this.startContainer = node; this.startOffset = offset; },
        setEnd(node, offset) { this.endContainer = node; this.endOffset = offset; },
      };
    },
  };
  const text = fixtureText("abcdef");
  text.ownerDocument = ownerDocument;
  const root = fixtureElement("div", "md-block", [text]);
  const index = indexedFixture([["block", root]]);
  const value = {
    version: 1,
    currentBlockKeys: ["block"],
    startOffset: 1,
    endOffset: 5,
    quote: "bcde",
  };
  const rebuilt = textSelection.reconstructTextRange(value, index);
  assert.equal(rebuilt.available, true);
  assert.equal(rebuilt.ranges.length, 1);
  assert.equal(rebuilt.ranges[0].startOffset, 1);
  assert.equal(rebuilt.ranges[0].endOffset, 5);
  assert.equal(textSelection.reconstructTextRange({ ...value, quote: "wrong" }, index).available, false);

  const fragments = textSelection.mergeFragmentSpecs([
    { node: text, start: 0, end: 2, text: "ab" },
    { node: text, start: 2, end: 4, text: "cd" },
  ]);
  assert.deepEqual(fragments.map(({ start, end, text: valueText }) => ({ start, end, text: valueText })), [
    { start: 0, end: 4, text: "abcd" },
  ]);
});

test("Text range reconstruction limits highlight fragments", () => {
  const children = [];
  for (let index = 0; index <= textSelection.maxFragments; index += 1) {
    const node = fixtureText("x");
    node.ownerDocument = { createRange: () => ({ setStart() {}, setEnd() {} }) };
    children.push(node);
  }
  const root = fixtureElement("div", "md-block", children);
  const index = indexedFixture([["block", root]]);
  const value = {
    version: 1,
    currentBlockKeys: ["block"],
    startOffset: 0,
    endOffset: children.length,
    quote: "x".repeat(children.length),
  };
  assert.equal(textSelection.reconstructTextRange(value, index).available, false);
});

test("Text range reconstruction keeps line-break offsets without marking break nodes", () => {
  const ownerDocument = {
    createRange() {
      return {
        setStart(node, offset) { this.startContainer = node; this.startOffset = offset; },
        setEnd(node, offset) { this.endContainer = node; this.endOffset = offset; },
      };
    },
  };
  const first = fixtureText("a");
  const second = fixtureText("b");
  first.ownerDocument = ownerDocument;
  second.ownerDocument = ownerDocument;
  const root = fixtureElement("div", "md-block", [first, fixtureElement("br"), second]);
  const index = indexedFixture([["block", root]]);
  const rebuilt = textSelection.reconstructTextRange({
    version: 1,
    currentBlockKeys: ["block"],
    startOffset: 0,
    endOffset: 3,
    quote: "a\nb",
  }, index);
  assert.equal(rebuilt.available, true);
  assert.equal(rebuilt.ranges.length, 2);
  assert.deepEqual(rebuilt.ranges.map((range) => range.startContainer), [first, second]);
});

test("Selection state keeps captured request data immutable", () => {
  const descriptor = {
    version: 1,
    blockKeys: ["first"],
    startOffset: 2,
    endOffset: 5,
    quote: "abc",
    generation: 9,
  };
  const captured = textSelection.requestSelection(descriptor);
  const latched = textSelection.latchDescriptor({
    ...descriptor,
    nativeRange: { cloneRange: () => ({ saved: true }) },
    rect: { top: 1 },
  });
  descriptor.blockKeys[0] = "changed";
  assert.deepEqual(captured.blockKeys, ["first"]);
  assert.deepEqual(latched.blockKeys, ["first"]);
  assert.deepEqual(latched.nativeRange, { saved: true });
  assert.equal(textSelection.selectionForGeneration(descriptor, 9), descriptor);
  assert.equal(textSelection.selectionForGeneration(descriptor, 10), null);
});

test("Selection action geometry refreshes and text ranges scroll to their marks", () => {
  const saved = { top: 10, right: 30, bottom: 20, left: 5, width: 25, height: 10 };
  const current = { top: 110, right: 130, bottom: 120, left: 105, width: 25, height: 10 };
  const descriptor = {
    rect: saved,
    nativeRange: { getClientRects: () => [current] },
  };
  assert.deepEqual(textSelection.descriptorRect(descriptor), current);

  const above = { getBoundingClientRect: () => ({ top: 0, bottom: 10 }) };
  const below = { getBoundingClientRect: () => ({ top: 180, bottom: 200 }) };
  assert.equal(textSelection.rangeOutsideViewport([above], 10, 180), true);
  assert.equal(textSelection.rangeOutsideViewport([below], 10, 180), true);
  assert.equal(textSelection.rangeScrollDelta([above], 10, 180), -90);
  assert.equal(textSelection.rangeScrollDelta([below], 10, 180), 95);
  assert.equal(textSelection.rangeScrollDelta([
    { getBoundingClientRect: () => ({ top: 40, bottom: 50 }) },
  ], 10, 180), 0);
});

test("Text comment hit testing follows visible marks and cycles overlapping comments", () => {
  const marked = {
    getClientRects: () => [
      { top: 10, right: 40, bottom: 20, left: 10 },
      { top: 30, right: 60, bottom: 40, left: 20 },
    ],
  };
  const comments = [
    { id: "one", status: "open", textRange: {}, ranges: [marked], outdated: false, rangeUnavailable: false },
    { id: "two", status: "addressed", textRange: {}, ranges: [marked], outdated: false, rangeUnavailable: false },
    { id: "resolved", status: "resolved", textRange: {}, ranges: [marked], outdated: false, rangeUnavailable: false },
    { id: "block", status: "open", textRange: null, ranges: [marked], outdated: false, rangeUnavailable: false },
    { id: "old", status: "open", textRange: {}, ranges: [marked], outdated: true, rangeUnavailable: false },
    { id: "missing", status: "open", textRange: {}, ranges: [marked], outdated: false, rangeUnavailable: true },
  ];

  assert.deepEqual(textSelection.textCommentIDsAtPoint(comments, 15, 15), ["one", "two"]);
  assert.equal(textSelection.textCommentIDAtPoint(comments, 15, 15), "one");
  assert.equal(textSelection.textCommentIDAtPoint(comments, 15, 15, "one"), "two");
  assert.equal(textSelection.textCommentIDAtPoint(comments, 15, 15, "two"), "one");
  assert.equal(textSelection.textCommentIDAtPoint(comments, 15, 15, "resolved"), "one");
  assert.equal(textSelection.textCommentIDAtPoint(comments, 18, 25), "");
  assert.equal(textSelection.pointInRange(marked, Number.NaN, 15), false);
});

test("Highlight groups separate current and active ranges", () => {
  const openRange = { id: "open" };
  const resolvedRange = { id: "resolved" };
  const comments = [
    { id: "one", status: "open", textRange: {}, ranges: [openRange], outdated: false, rangeUnavailable: false },
    { id: "two", status: "resolved", textRange: {}, ranges: [resolvedRange], outdated: false, rangeUnavailable: false },
  ];
  assert.deepEqual(textSelection.planHighlightGroups(comments, "two", true), {
    current: [openRange],
    active: [],
  });
  assert.deepEqual(textSelection.planHighlightGroups(comments, "one", true), {
    current: [openRange],
    active: [openRange],
  });
  assert.deepEqual(textSelection.planHighlightGroups(comments, "one", false), {
    current: [],
    active: [],
  });
  assert.equal(textSelection.supportsHighlights({
    CSS: { highlights: new Map(), supports: () => true },
    Highlight: class {},
  }), true);
  assert.equal(textSelection.supportsHighlights({ CSS: { highlights: new Map(), supports: () => false }, Highlight: class {} }), false);
});
