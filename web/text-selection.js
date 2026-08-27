(() => {
  "use strict";

  const indexVersion = 1;
  const rangeVersion = 1;
  const maxQuoteBytes = 16 * 1024;
  const maxBlocks = 16;
  const maxFragments = 256;
  const maxOffset = 1 << 30;
  const highlightNames = {
    comments: "mdshelf-text-comments",
    active: "mdshelf-active-text-comment",
  };
  const excludedSelectors = [
    ".md-block-review-controls",
    ".heading-permalink",
    ".code-toolbar",
    ".ln",
    ".lnt",
    ".katex-mathml",
    ".sr-only",
    "script",
    "style",
  ];
  const excludedClasses = new Set(excludedSelectors.filter((value) => value.startsWith(".")).map((value) => value.slice(1)));
  const excludedTags = new Set(["SCRIPT", "STYLE"]);
  const hiddenExcludedClasses = new Set(["katex-mathml", "sr-only"]);

  function classContains(element, name) {
    if (element?.classList?.contains) return element.classList.contains(name);
    return String(element?.className || "").split(/\s+/u).includes(name);
  }

  function isExcludedElement(element) {
    if (!element || element.nodeType !== 1) return false;
    if (excludedTags.has(String(element.tagName || "").toUpperCase())) return true;
    for (const name of excludedClasses) {
      if (classContains(element, name)) return true;
    }
    if (["SVG", "FOREIGNOBJECT"].includes(String(element.tagName || "").toUpperCase())) {
      return Boolean(element.closest?.(".mermaid-rendered")) || classContains(element.parentElement, "mermaid-rendered");
    }
    return false;
  }

  function canonicalBlock(key, element, order = 0) {
    const runs = [];
    let text = "";
    let offset = 0;

    function append(type, node, value) {
      if (!value) return;
      const run = { type, node, start: offset, end: offset + value.length, text: value };
      runs.push(run);
      text += value;
      offset = run.end;
    }

    function visit(node, root = false) {
      if (!node) return;
      if (node.nodeType === 3) {
        append("text", node, String(node.nodeValue ?? node.data ?? ""));
        return;
      }
      if (node.nodeType !== 1 && node.nodeType !== 11) return;
      if (!root && isExcludedElement(node)) return;
      if (String(node.tagName || "").toUpperCase() === "BR") {
        append("break", node, "\n");
        return;
      }
      for (const child of node.childNodes || []) visit(child);
    }

    visit(element, true);
    return { key, element, order, text, runs };
  }

  function buildIndex(records, generation) {
    if (!Array.isArray(records)) throw new Error("Text index blocks must be an array.");
    const blocks = [];
    const byKey = new Map();
    for (let order = 0; order < records.length; order += 1) {
      const record = records[order];
      if (!record || typeof record.key !== "string" || byKey.has(record.key) || !record.element) {
        throw new Error("Text index block data is invalid.");
      }
      const block = canonicalBlock(record.key, record.element, order);
      blocks.push(block);
      byKey.set(block.key, block);
    }
    return { version: indexVersion, generation, blocks, byKey };
  }

  function textByteLength(value) {
    return new TextEncoder().encode(String(value)).length;
  }

  function splitsSurrogate(value, offset) {
    if (offset <= 0 || offset >= value.length) return false;
    const left = value.charCodeAt(offset - 1);
    const right = value.charCodeAt(offset);
    return left >= 0xd800 && left <= 0xdbff && right >= 0xdc00 && right <= 0xdfff;
  }

  function descriptorFromSlices(slices, generation) {
    if (!Array.isArray(slices) || slices.length < 1 || slices.length > maxBlocks) return null;
    const normalized = [];
    for (const slice of slices) {
      const block = slice?.block;
      const start = slice?.start;
      const end = slice?.end;
      if (
        !block
        || typeof block.key !== "string"
        || typeof block.text !== "string"
        || !Number.isSafeInteger(start)
        || !Number.isSafeInteger(end)
        || start < 0
        || end <= start
        || end > block.text.length
        || end > maxOffset
        || splitsSurrogate(block.text, start)
        || splitsSurrogate(block.text, end)
      ) return null;
      normalized.push({ block, start, end, text: block.text.slice(start, end) });
    }
    for (let index = 1; index < normalized.length; index += 1) {
      if (normalized[index].block.order !== normalized[index - 1].block.order + 1) return null;
    }
    const quote = normalized.map((slice) => slice.text).join("\n\n");
    if (!quote.trim() || quote.includes("\0") || textByteLength(quote) > maxQuoteBytes) return null;
    return {
      version: rangeVersion,
      blockKeys: normalized.map((slice) => slice.block.key),
      startOffset: normalized[0].start,
      endOffset: normalized.at(-1).end,
      quote,
      generation,
    };
  }

  function elementForNode(node) {
    if (!node) return null;
    return node.nodeType === 1 ? node : node.parentElement || node.parentNode || null;
  }

  function closestIndexedBlock(node, index, article) {
    let element = elementForNode(node);
    while (element && element !== article) {
      const key = element.dataset?.mdBlock;
      if (key && index.byKey.get(key)?.element === element) return index.byKey.get(key);
      element = element.parentElement || element.parentNode || null;
    }
    return null;
  }

  function boundaryIsExcluded(node, article) {
    let element = elementForNode(node);
    while (element && element !== article) {
      if (isExcludedElement(element)) return true;
      element = element.parentElement || element.parentNode || null;
    }
    return false;
  }

  function textIntersection(run, range) {
    const length = run.text.length;
    let startRelation;
    let endRelation;
    try {
      startRelation = range.comparePoint(run.node, 0);
      endRelation = range.comparePoint(run.node, length);
    } catch {
      return null;
    }
    if (startRelation === 1 || endRelation === -1) return null;
    let start = 0;
    let end = length;
    if (startRelation === -1) {
      if (range.startContainer !== run.node) return null;
      start = range.startOffset;
    }
    if (endRelation === 1) {
      if (range.endContainer !== run.node) return null;
      end = range.endOffset;
    }
    if (end <= start) return null;
    return { start: run.start + start, end: run.start + end };
  }

  function breakIntersects(run, range) {
    try {
      return range.intersectsNode(run.node);
    } catch {
      return false;
    }
  }

  function subtreeHasText(node) {
    if (node?.nodeType === 3) return Boolean(String(node.nodeValue ?? node.data ?? "").trim());
    for (const child of node?.childNodes || []) {
      if (subtreeHasText(child)) return true;
    }
    return false;
  }

  function excludedTextIsSelectable(element, article) {
    if (excludedTags.has(String(element.tagName || "").toUpperCase())) return false;
    for (const name of hiddenExcludedClasses) {
      if (classContains(element, name)) return false;
    }
    for (let current = element; current && current !== article; current = current.parentElement || current.parentNode || null) {
      if (current.hidden) return false;
      let style;
      try {
        style = current.ownerDocument?.defaultView?.getComputedStyle?.(current);
      } catch {
        style = null;
      }
      if (!style) continue;
      if (style.userSelect === "none" || style.webkitUserSelect === "none") return false;
      if (style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse") return false;
    }
    return true;
  }

  function rangeIncludesExcludedText(range, article) {
    function visit(node, root = false) {
      if (!node || (node.nodeType !== 1 && node.nodeType !== 11)) return false;
      if (!root && isExcludedElement(node)) {
        if (!subtreeHasText(node) || !excludedTextIsSelectable(node, article)) return false;
        try {
          return range.intersectsNode(node);
        } catch {
          return true;
        }
      }
      for (const child of node.childNodes || []) {
        if (visit(child)) return true;
      }
      return false;
    }
    return visit(article, true);
  }

  function slicesForRange(range, index, article) {
    if (!range || range.collapsed || index?.version !== indexVersion) return null;
    if (boundaryIsExcluded(range.startContainer, article) || boundaryIsExcluded(range.endContainer, article)) return null;
    if (rangeIncludesExcludedText(range, article)) return null;
    const firstBoundaryBlock = closestIndexedBlock(range.startContainer, index, article);
    const lastBoundaryBlock = closestIndexedBlock(range.endContainer, index, article);
    if (!firstBoundaryBlock || !lastBoundaryBlock || firstBoundaryBlock.order > lastBoundaryBlock.order) return null;

    const slices = [];
    for (let order = firstBoundaryBlock.order; order <= lastBoundaryBlock.order; order += 1) {
      const block = index.blocks[order];
      let start = Number.POSITIVE_INFINITY;
      let end = -1;
      for (const run of block.runs) {
        const intersection = run.type === "text"
          ? textIntersection(run, range)
          : (breakIntersects(run, range) ? { start: run.start, end: run.end } : null);
        if (!intersection) continue;
        start = Math.min(start, intersection.start);
        end = Math.max(end, intersection.end);
      }
      if (end > start) slices.push({ block, start, end });
    }
    return slices;
  }

  function selectionRect(range) {
    try {
      const rects = [...(range.getClientRects?.() || [])].filter((rect) => rect.width || rect.height);
      const rect = rects.at(-1) || range.getBoundingClientRect?.();
      if (!rect) return null;
      return { top: rect.top, right: rect.right, bottom: rect.bottom, left: rect.left, width: rect.width, height: rect.height };
    } catch {
      return null;
    }
  }

  function descriptorRect(descriptor) {
    if (!descriptor) return null;
    return selectionRect(descriptor.nativeRange) || descriptor.rect || null;
  }

  function captureSelection(selection, index, article) {
    if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) return null;
    const range = selection.getRangeAt(0);
    const slices = slicesForRange(range, index, article);
    const descriptor = descriptorFromSlices(slices, index.generation);
    if (!descriptor) return null;
    return { ...descriptor, nativeRange: range.cloneRange(), rect: selectionRect(range) };
  }

  function latchDescriptor(descriptor) {
    if (!descriptor) return null;
    return {
      ...descriptor,
      blockKeys: [...descriptor.blockKeys],
      nativeRange: descriptor.nativeRange?.cloneRange?.() || descriptor.nativeRange || null,
      rect: descriptor.rect ? { ...descriptor.rect } : null,
    };
  }

  function selectionForGeneration(descriptor, generation) {
    return descriptor?.generation === generation ? descriptor : null;
  }

  function requestSelection(descriptor) {
    if (!descriptor) return null;
    return {
      version: descriptor.version,
      blockKeys: [...descriptor.blockKeys],
      startOffset: descriptor.startOffset,
      endOffset: descriptor.endOffset,
      quote: descriptor.quote,
    };
  }

  function slicesFromTextRange(textRange, index) {
    if (!textRange || textRange.version !== rangeVersion || !Array.isArray(textRange.currentBlockKeys)) return null;
    if (textRange.currentBlockKeys.length < 1 || textRange.currentBlockKeys.length > maxBlocks) return null;
    const blocks = textRange.currentBlockKeys.map((key) => index.byKey.get(key));
    if (blocks.some((block) => !block)) return null;
    for (let offset = 1; offset < blocks.length; offset += 1) {
      if (blocks[offset].order !== blocks[offset - 1].order + 1) return null;
    }
    const slices = blocks.map((block, blockIndex) => ({
      block,
      start: blockIndex === 0 ? textRange.startOffset : 0,
      end: blockIndex === blocks.length - 1 ? textRange.endOffset : block.text.length,
    }));
    const descriptor = descriptorFromSlices(slices, index.generation);
    if (!descriptor || descriptor.quote !== textRange.quote) return null;
    return slices;
  }

  function fragmentSpecsForSlice(slice) {
    const fragments = [];
    for (const run of slice.block.runs) {
      if (run.type !== "text") continue;
      const start = Math.max(slice.start, run.start);
      const end = Math.min(slice.end, run.end);
      if (end <= start) continue;
      fragments.push({
        blockKey: slice.block.key,
        canonicalStart: start,
        canonicalEnd: end,
        node: run.node,
        start: start - run.start,
        end: end - run.start,
        text: run.text.slice(start - run.start, end - run.start),
      });
    }
    return fragments;
  }

  function mergeFragmentSpecs(fragments) {
    const merged = [];
    for (const fragment of fragments) {
      const previous = merged.at(-1);
      if (previous && previous.node === fragment.node && previous.end === fragment.start) {
        previous.end = fragment.end;
        previous.canonicalEnd = fragment.canonicalEnd;
        previous.text += fragment.text;
      } else {
        merged.push({ ...fragment });
      }
    }
    return merged;
  }

  function reconstructTextRange(textRange, index) {
    const slices = slicesFromTextRange(textRange, index);
    if (!slices) return { available: false, ranges: [] };
    const fragments = mergeFragmentSpecs(slices.flatMap(fragmentSpecsForSlice));
    if (!fragments.length) return { available: false, ranges: [] };
    const entries = [];
    try {
      for (const fragment of fragments) {
        const owner = fragment.node.ownerDocument || globalThis.document;
        const range = owner.createRange();
        range.setStart(fragment.node, fragment.start);
        range.setEnd(fragment.node, fragment.end);
        const previous = entries.at(-1);
        if (
          previous
          && previous.blockKey === fragment.blockKey
          && previous.canonicalEnd === fragment.canonicalStart
        ) {
          const combined = owner.createRange();
          combined.setStart(previous.range.startContainer, previous.range.startOffset);
          combined.setEnd(range.endContainer, range.endOffset);
          if (typeof combined.toString === "function" && combined.toString() === previous.text + fragment.text) {
            previous.range = combined;
            previous.canonicalEnd = fragment.canonicalEnd;
            previous.text += fragment.text;
            continue;
          }
        }
        entries.push({
          blockKey: fragment.blockKey,
          canonicalEnd: fragment.canonicalEnd,
          range,
          text: fragment.text,
        });
        if (entries.length > maxFragments) return { available: false, ranges: [] };
      }
    } catch {
      return { available: false, ranges: [] };
    }
    return { available: true, ranges: entries.map((entry) => entry.range) };
  }

  function shortQuote(value, limit = 96) {
    const normalized = String(value || "").replace(/\s+/gu, " ").trim();
    if ([...normalized].length <= limit) return normalized;
    return `${[...normalized].slice(0, Math.max(1, limit - 1)).join("")}…`;
  }

  function supportsHighlights(scope) {
    const css = scope?.CSS;
    return Boolean(
      css?.highlights
      && typeof scope?.Highlight === "function"
      && typeof css.supports === "function"
      && css.supports("selector(::highlight(mdshelf-text-comments))"),
    );
  }

  function planHighlightGroups(comments, activeID, available) {
    const current = [];
    let active = [];
    let fallback = false;
    for (const comment of comments || []) {
      if (!comment?.textRange || comment.rangeUnavailable || comment.outdated || !Array.isArray(comment.ranges)) continue;
      if (available && (comment.status === "open" || comment.status === "addressed")) current.push(...comment.ranges);
      if (comment.id !== activeID) continue;
      if (available) active = [...comment.ranges];
      else fallback = comment.ranges.length > 0;
    }
    return { current, active, fallback };
  }

  function pointInRange(range, x, y) {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return false;
    try {
      const rects = [...(range.getClientRects?.() || [])];
      if (!rects.length && range.getBoundingClientRect) rects.push(range.getBoundingClientRect());
      return rects.some((rect) => (
        rect
        && rect.right > rect.left
        && rect.bottom > rect.top
        && x >= rect.left
        && x <= rect.right
        && y >= rect.top
        && y <= rect.bottom
      ));
    } catch {
      return false;
    }
  }

  function textCommentIDsAtPoint(comments, x, y, activeID = "") {
    const ids = [];
    for (const comment of comments || []) {
      if (
        !comment?.textRange
        || comment.outdated
        || comment.rangeUnavailable
        || !Array.isArray(comment.ranges)
        || (!["open", "addressed"].includes(comment.status) && comment.id !== activeID)
      ) continue;
      if (comment.ranges.some((range) => pointInRange(range, x, y))) ids.push(comment.id);
    }
    return ids;
  }

  function textCommentIDAtPoint(comments, x, y, activeID = "") {
    const ids = textCommentIDsAtPoint(comments, x, y, activeID);
    if (!ids.length) return "";
    const activeIndex = ids.indexOf(activeID);
    return activeIndex >= 0 && ids.length > 1 ? ids[(activeIndex + 1) % ids.length] : ids[0];
  }

  function rangeRects(ranges) {
    if (!Array.isArray(ranges)) return [];
    try {
      return ranges.map((range) => range.getBoundingClientRect());
    } catch {
      return [];
    }
  }

  function rangeOutsideViewport(ranges, top, bottom) {
    const rects = rangeRects(ranges);
    if (!rects.length) return true;
    const rangeTop = Math.min(...rects.map((rect) => rect.top));
    const rangeBottom = Math.max(...rects.map((rect) => rect.bottom));
    return rangeBottom <= top || rangeTop >= bottom;
  }

  function rangeScrollDelta(ranges, top, bottom) {
    const rects = rangeRects(ranges);
    if (!rects.length) return 0;
    const rangeTop = Math.min(...rects.map((rect) => rect.top));
    const rangeBottom = Math.max(...rects.map((rect) => rect.bottom));
    if (rangeBottom > top && rangeTop < bottom) return 0;
    const target = rangeBottom <= top
      ? rects.reduce((closest, rect) => (rect.bottom > closest.bottom ? rect : closest))
      : rects.reduce((closest, rect) => (rect.top < closest.top ? rect : closest));
    return ((target.top + target.bottom) / 2) - ((top + bottom) / 2);
  }

  function shortcutCommentTarget(selectionDescriptor, activeBlockKey) {
    return selectionDescriptor ? "selection" : (activeBlockKey ? "block" : "none");
  }

  const api = {
    buildIndex,
    canonicalBlock,
    captureSelection,
    descriptorFromSlices,
    descriptorRect,
    excludedSelectors: [...excludedSelectors],
    fragmentSpecsForSlice,
    highlightNames: { ...highlightNames },
    indexVersion,
    isExcludedElement,
    latchDescriptor,
    maxBlocks,
    maxFragments,
    maxQuoteBytes,
    mergeFragmentSpecs,
    planHighlightGroups,
    pointInRange,
    rangeIncludesExcludedText,
    rangeOutsideViewport,
    rangeScrollDelta,
    rangeVersion,
    reconstructTextRange,
    requestSelection,
    selectionForGeneration,
    shortQuote,
    shortcutCommentTarget,
    slicesForRange,
    splitsSurrogate,
    supportsHighlights,
    textByteLength,
    textCommentIDAtPoint,
    textCommentIDsAtPoint,
  };

  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (typeof window !== "undefined") window.MDShelfTextSelection = api;
})();
