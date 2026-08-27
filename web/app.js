(() => {
  "use strict";

  const elements = {
    backdrop: document.querySelector("#backdrop"),
    brand: document.querySelector("#brand"),
    closeButton: document.querySelector("#close-button"),
    appearance: document.querySelector("#appearance"),
    design: document.querySelector("#design"),
    commentBody: document.querySelector("#comment-body"),
    commentBodyHelp: document.querySelector("#comment-body-help"),
    commentCancel: document.querySelector("#comment-cancel"),
    commentComposer: document.querySelector("#comment-composer"),
    commentComposerTitle: document.querySelector("#comment-composer-title"),
    commentError: document.querySelector("#comment-error"),
    commentSave: document.querySelector("#comment-save"),
    commentSelectedQuote: document.querySelector("#comment-selected-quote"),
    commentTarget: document.querySelector("#comment-target"),
    currentFile: document.querySelector("#current-file"),
    document: document.querySelector("#document"),
    documentPath: document.querySelector("#document-path"),
    drawer: document.querySelector("#drawer"),
    demoLink: document.querySelector("#demo-link"),
    fileCount: document.querySelector("#file-count"),
    fileFilter: document.querySelector("#file-filter"),
    fileNav: document.querySelector("#file-nav"),
    outlineList: document.querySelector("#outline-list"),
    outlineRail: document.querySelector("#outline-rail"),
    menuButton: document.querySelector("#menu-button"),
    reader: document.querySelector("#reader"),
    reviewBackdrop: document.querySelector("#review-backdrop"),
    reviewButton: document.querySelector("#review-button"),
    reviewButtonCount: document.querySelector("#review-button-count"),
    reviewClose: document.querySelector("#review-close"),
    reviewComments: document.querySelector("#review-comments"),
    reviewCountSummary: document.querySelector("#review-count-summary"),
    reviewError: document.querySelector("#review-error"),
    reviewLiveStatus: document.querySelector("#review-live-status"),
    reviewLoadState: document.querySelector("#review-load-state"),
    reviewPanel: document.querySelector("#review-panel"),
    reviewPanelScroll: document.querySelector("#review-panel-scroll"),
    reviewPanelTitle: document.querySelector("#review-panel-title"),
    routeStatus: document.querySelector("#route-status"),
    settings: document.querySelector(".settings"),
    settingsButton: document.querySelector("#settings-button"),
    settingsPopup: document.querySelector("#settings-popup"),
    selectionCommentAction: document.querySelector("#selection-comment-action"),
    skipLink: document.querySelector(".skip-link"),
    statusMessage: document.querySelector("#status-message"),
    statusView: document.querySelector("#status-view"),
    shortcutBackdrop: document.querySelector("#shortcut-backdrop"),
    shortcutButton: document.querySelector("#shortcut-button"),
    shortcutClose: document.querySelector("#shortcut-close"),
    shortcutDialog: document.querySelector("#shortcut-dialog"),
    syntaxTheme: document.querySelector("#syntax-theme"),
    topbar: document.querySelector(".topbar"),
    updateNotice: document.querySelector("#update-notice"),
  };

  const textSelection = window.MDShelfTextSelection || null;
  const desktop = window.matchMedia("(min-width: 56.25rem)");
  const reviewWide = window.matchMedia("(min-width: 105rem)");
  const touchComments = window.matchMedia("(hover: none), (pointer: coarse)");
  const colorScheme = window.matchMedia("(prefers-color-scheme: dark)");
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const demoDocumentPath = "__mdshelf_demo__";
  const designs = new Set(["ink", "signal", "column"]);
  const appearances = new Set(["system", "light", "dark"]);
  const syntaxThemes = new Set([
    "github-auto", "catppuccin-auto", "solarized-auto", "dracula", "monokai", "nord", "tokyonight-night",
  ]);
  const themeStorage = {
    design: "mdshelf.design",
    appearance: "mdshelf.appearance",
    syntax: "mdshelf.syntaxTheme",
  };
  const state = {
    abortController: null,
    design: "ink",
    appearance: "system",
    outlineHeadings: [],
    outlineFrame: 0,
    currentPath: "",
    files: [],
    fileSet: new Set(),
    titles: new Map(),
    removed: new Map(),
    daemonMode: false,
    filter: "",
    focusAfterNavigation: false,
    highlightBaseline: [],
    highlightTimer: 0,
    activeNavigationBlockKey: "",
    sectionFrame: 0,
    sectionTrackingLocked: false,
    sectionTrackingTimer: 0,
    openFolders: new Set(),
    pendingUpdate: null,
    updateTimer: 0,
    renderGeneration: 0,
    mermaidCounter: 0,
    mermaidQueue: Promise.resolve(),
    syntaxTheme: "github-auto",
    reviewEnabled: false,
    reviewRevision: 0,
    reviewStatus: "needs_review",
    reviewSourceHash: "",
    reviewComments: [],
    reviewPanelOpen: false,
    activeBlockKey: "",
    activeCommentID: "",
    reviewBlocks: new Map(),
    reviewLoading: false,
    reviewMutationPending: false,
    reviewMutationToken: 0,
    reviewLoadToken: 0,
    reviewError: "",
    reviewBlockError: "",
    reviewErrorNeedsRender: false,
    reviewComposer: null,
    reviewTextIndex: null,
    reviewRenderGeneration: 0,
    liveSelection: null,
    latchedSelection: null,
    selectionFrame: 0,
    textCommentHoverFrame: 0,
    textCommentHoverPoint: null,
    highlightAvailable: false,
    shortcutsOpen: false,
    shortcutsReturnFocus: null,
    fileReviews: new Map(),
  };

  const reviewStatuses = new Set(["needs_review", "comments", "updated", "removed"]);
  const commentStatuses = new Set(["open", "addressed", "resolved"]);
  const replyAuthors = new Set(["reviewer", "agent"]);
  const sourceHashPattern = /^[0-9a-f]{64}$/;
  const blockKeyPattern = /^[0-9a-f]{24}$/;
  const commentIDPattern = /^comment_[0-9a-f]{24}$/;
  const replyIDPattern = /^reply_[0-9a-f]{24}$/;
  const maxReviewTextBytes = 16 * 1024;

  class APIError extends Error {
    constructor(message, status = 0, code = "") {
      super(message);
      this.name = "APIError";
      this.status = status;
      this.code = code;
    }
  }

  function reviewStatusLabel(status) {
    return ({
      needs_review: "No comments",
      comments: "Comments",
      updated: "Document updated",
      removed: "Removed",
    })[status] || "Review status unavailable";
  }

  function blockCommentTapAction({ touch = false, commentButton = false, block = false, interactive = false } = {}) {
    if (commentButton) return "open";
    if (touch && block && !interactive) return "arm";
    return "none";
  }

  function reviewMutationIsCurrent(mutation, current) {
    return mutation.token === current.token && mutation.path === current.path && current.enabled;
  }

  function commentComposerEscapeAction({ open = false, pending = false } = {}) {
    if (!open) return "none";
    return pending ? "wait" : "close";
  }

  function commentSubmitShortcut(event = {}) {
    return event.key === "Enter" && !event.isComposing && Boolean(event.metaKey || event.ctrlKey);
  }

  function readingShortcutAction(event = {}) {
    if (event.isComposing || event.metaKey || event.ctrlKey || event.altKey) return "";
    const key = event.key;
    if (["ArrowUp", "ArrowLeft", "k", "K", "h", "H"].includes(key)) return "previous-block";
    if (["ArrowDown", "ArrowRight", "j", "J", "l", "L"].includes(key)) return "next-block";
    if (key === "Home") return "first-block";
    if (key === "End") return "last-block";
    if (key === "c" || key === "C") return "comment";
    if (key === "/") return "documents";
    if (key === "r" || key === "R") return "comments";
    if (key === "?") return "shortcuts";
    return "";
  }

  function blockIndexAtViewport(rects, offset) {
    if (!Array.isArray(rects) || !rects.length) return -1;
    for (let index = 0; index < rects.length; index += 1) {
      if (rects[index].bottom >= offset) return index;
    }
    return rects.length - 1;
  }

  function navigationScrollDelta(rect, viewportTop, viewportBottom, direction) {
    if (!rect || viewportBottom <= viewportTop || direction === 0) return 0;
    const marker = rect.top + Math.min(Math.max((rect.bottom - rect.top) / 2, 0), 16);
    const midpoint = viewportTop + ((viewportBottom - viewportTop) / 2);
    if (direction > 0 && marker > midpoint) return marker - midpoint;
    if (direction < 0 && marker < midpoint) return marker - midpoint;
    return 0;
  }

  function listNavigationIndex(current, count, action) {
    if (!Number.isInteger(count) || count < 1) return -1;
    if (action === "first") return 0;
    if (action === "last") return count - 1;
    if (action === "next") return current < 0 ? 0 : Math.min(current + 1, count - 1);
    if (action === "previous") return current < 0 ? count - 1 : Math.max(current - 1, 0);
    return -1;
  }

  function commentStateAction(status) {
    return status === "resolved" ? "reopen" : "resolve";
  }

  function commentComposerControlState({ blocked = false, pending = false, invalid = false } = {}) {
    return {
      bodyDisabled: false,
      bodyReadOnly: blocked || pending,
      saveDisabled: blocked || pending || invalid,
      actionsDisabled: pending,
    };
  }

  function commentCountsByBlockKey(comments) {
    const counts = {};
    if (!Array.isArray(comments)) return counts;
    for (const comment of comments) {
      if (
        !comment
        || typeof comment.currentBlockKey !== "string"
        || !comment.currentBlockKey
        || comment.outdated === true
        || !["open", "addressed", "resolved"].includes(comment.status)
      ) continue;
      if (!counts[comment.currentBlockKey]) {
        counts[comment.currentBlockKey] = { unresolved: 0, total: 0 };
      }
      const count = counts[comment.currentBlockKey];
      if (comment.status !== "resolved") count.unresolved += 1;
      count.total += 1;
    }
    return counts;
  }

  function planLiveChanges(payload, currentPath, daemonMode) {
    const plan = {
      refreshFiles: false,
      renderCurrent: false,
      refreshCurrentReview: false,
      reviewAfterRender: false,
      currentRemoved: false,
    };
    const reset = payload?.reset === true;
    const changes = Array.isArray(payload?.changes)
      ? payload.changes.filter((change) => (
        change
        && typeof change.path === "string"
        && ["added", "removed", "updated", "review"].includes(change.kind)
      ))
      : [];
    if (daemonMode) plan.refreshFiles = reset || changes.length > 0;
    else plan.refreshFiles = reset || changes.some((change) => change.kind === "added" || change.kind === "removed");

    if (!currentPath || currentPath === demoDocumentPath) return plan;
    const current = changes.filter((change) => change.path === currentPath);
    plan.currentRemoved = current.some((change) => change.kind === "removed");
    const fileChanged = current.some((change) => ["added", "removed", "updated"].includes(change.kind));
    const reviewChanged = daemonMode && current.some((change) => change.kind === "review");
    if (reset || fileChanged) {
      plan.renderCurrent = !plan.currentRemoved;
      plan.reviewAfterRender = daemonMode && !plan.currentRemoved;
    } else if (reviewChanged) {
      plan.refreshCurrentReview = true;
    }
    return plan;
  }

  function commentAddAvailable(input = {}) {
    return input.loaded !== false && !input.loading && !input.pending && !input.error;
  }

  function reviewPanelAvailable(input = {}) {
    return Boolean(
      input.daemonMode
      && input.currentPath
      && input.currentPath !== demoDocumentPath
      && !input.removed,
    );
  }

  function safeDecode(value) {
    try {
      return decodeURIComponent(value);
    } catch {
      return value;
    }
  }

  function normalizePath(value) {
    const parts = [];
    for (const part of String(value).replaceAll("\\", "/").split("/")) {
      if (!part || part === ".") continue;
      if (part === "..") {
        if (!parts.length) return "";
        parts.pop();
        continue;
      }
      parts.push(part);
    }
    return parts.join("/");
  }

  function encodePath(path) {
    return path.split("/").map(encodeURIComponent).join("/");
  }

  function buildRoute(path, fragment = "") {
    const encodedFragment = fragment ? `?${encodeURIComponent(safeDecode(fragment))}` : "";
    return `#/${encodePath(path)}${encodedFragment}`;
  }

  function readRoute() {
    if (!window.location.hash.startsWith("#/")) return { path: "", fragment: "" };

    const route = window.location.hash.slice(2);
    const fragmentIndex = route.search(/[?#]/);
    const rawPath = fragmentIndex === -1 ? route : route.slice(0, fragmentIndex);
    const rawFragment = fragmentIndex === -1 ? "" : route.slice(fragmentIndex + 1);
    const path = normalizePath(rawPath.split("/").map(safeDecode).join("/"));

    return { path, fragment: safeDecode(rawFragment) };
  }

  function splitReference(reference) {
    const hashIndex = reference.indexOf("#");
    const withNoFragment = hashIndex === -1 ? reference : reference.slice(0, hashIndex);
    const queryIndex = withNoFragment.indexOf("?");
    return {
      path: queryIndex === -1 ? withNoFragment : withNoFragment.slice(0, queryIndex),
      fragment: hashIndex === -1 ? "" : reference.slice(hashIndex + 1),
    };
  }

  function isRemoteReference(reference) {
    return reference.startsWith("//") || /^[a-z][a-z\d+.-]*:/i.test(reference);
  }

  function isAssetReference(reference) {
    return reference.startsWith("/api/asset?");
  }

  function resolveReference(documentPath, reference) {
    const decoded = safeDecode(reference);
    const fromRoot = decoded.startsWith("/");
    const base = fromRoot ? "" : documentPath.split("/").slice(0, -1).join("/");
    return normalizePath([base, decoded.replace(/^\/+/, "")].filter(Boolean).join("/"));
  }

  function fileName(path) {
    return path.split("/").pop() || path;
  }

  function titleFromPath(path) {
    return fileName(path).replace(/\.(?:md|markdown|mdown|mkd)$/i, "");
  }

  function displayName(path) {
    if (path === demoDocumentPath) return "MDShelf demo";
    return state.titles.get(path) || fileName(path);
  }

  function isDocumentAvailable(path) {
    if (path === demoDocumentPath) return true;
    return state.fileSet.has(path) && (!state.daemonMode || state.removed.get(path) !== true);
  }

  function storedTheme(key, choices, fallback) {
    try {
      const value = window.localStorage.getItem(key);
      return choices.has(value) ? value : fallback;
    } catch {
      return fallback;
    }
  }

  function loadThemePreferences() {
    state.design = storedTheme(themeStorage.design, designs, "ink");
    state.appearance = storedTheme(themeStorage.appearance, appearances, "system");
    state.syntaxTheme = storedTheme(themeStorage.syntax, syntaxThemes, "github-auto");
  }

  function isDarkColorTheme() {
    return state.appearance === "system" ? colorScheme.matches : state.appearance === "dark";
  }

  /* Signal is the only design that keeps the file list on screen. */
  function sidebarPinned() {
    return state.design === "signal" && desktop.matches;
  }

  /* The stylesheet decides whether notes sit beside the block or under it,
     so ask the layout instead of repeating the breakpoint here. */
  function sideComments(controls) {
    if (!controls || typeof window.getComputedStyle !== "function") return false;
    return window.getComputedStyle(controls).position === "absolute";
  }

  function growCommentBody() {
    const field = elements.commentBody;
    if (!field?.style || typeof field.scrollHeight !== "number") return;
    field.style.height = "auto";
    field.style.height = `${field.scrollHeight}px`;
  }

  function resolvedSyntaxTheme() {
    const dark = isDarkColorTheme();
    switch (state.syntaxTheme) {
      case "github-auto": return dark ? "github-dark" : "github";
      case "catppuccin-auto": return dark ? "catppuccin-mocha" : "catppuccin-latte";
      case "solarized-auto": return dark ? "solarized-dark" : "solarized-light";
      default: return state.syntaxTheme;
    }
  }

  function applyThemePreferences() {
    const root = document.documentElement;
    root.dataset.design = state.design;
    root.dataset.appearance = state.appearance;
    root.dataset.scheme = isDarkColorTheme() ? "dark" : "light";
    root.dataset.syntaxTheme = resolvedSyntaxTheme();
    elements.design.value = state.design;
    elements.appearance.value = state.appearance;
    elements.syntaxTheme.value = state.syntaxTheme;
  }

  function saveTheme(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      return;
    }
  }

  function refreshDocumentTheme() {
    initializeMermaid();
    if (!state.currentPath || elements.document.hidden || !isDocumentAvailable(state.currentPath)) return;
    const route = readRoute();
    void loadDocument(state.currentPath, route.fragment, { force: true });
  }

  function setDesign(value) {
    if (!designs.has(value)) return;
    state.design = value;
    saveTheme(themeStorage.design, value);
    applyThemePreferences();
    refreshDocumentTheme();
  }

  function setAppearance(value) {
    if (!appearances.has(value)) return;
    state.appearance = value;
    saveTheme(themeStorage.appearance, value);
    applyThemePreferences();
    refreshDocumentTheme();
  }

  function setSyntaxTheme(value) {
    if (!syntaxThemes.has(value)) return;
    state.syntaxTheme = value;
    saveTheme(themeStorage.syntax, value);
    applyThemePreferences();
  }

  function defaultDocument(exclude = "") {
    const available = state.files.filter((path) => path !== exclude && isDocumentAvailable(path));
    if (state.daemonMode) return available[0] || "";
    return available.find((path) => !path.includes("/") && /^readme\.(?:md|markdown)$/i.test(path))
      || available[0]
      || "";
  }

  function setDrawer(open, restoreFocus = true) {
    if (sidebarPinned()) {
      document.body.classList.remove("drawer-open");
      elements.drawer.inert = false;
      elements.drawer.removeAttribute("aria-hidden");
      elements.menuButton.setAttribute("aria-expanded", "false");
      return;
    }

    document.body.classList.toggle("drawer-open", open);
    elements.drawer.inert = !open;
    elements.drawer.setAttribute("aria-hidden", String(!open));
    elements.menuButton.setAttribute("aria-expanded", String(open));
    if (open) {
      window.requestAnimationFrame(() => elements.fileFilter.focus());
    } else if (restoreFocus) {
      elements.menuButton.focus();
    }
  }

  function setSettingsPopup(open, restoreFocus = true) {
    elements.settingsPopup.hidden = !open;
    elements.settingsButton.setAttribute("aria-expanded", String(open));
    elements.settingsButton.setAttribute("aria-label", open ? "Close settings" : "Open settings");
    if (open) {
      window.requestAnimationFrame(() => elements.design.focus());
    } else if (restoreFocus) {
      elements.settingsButton.focus();
    }
  }

  function setShortcutDialog(open, restoreFocus = true) {
    if (open && state.reviewMutationPending) return;
    if (open) {
      state.shortcutsReturnFocus = document.activeElement;
      if (state.reviewComposer) closeCommentComposer(false);
      if (state.reviewPanelOpen) setReviewPanel(false, false);
      setSettingsPopup(false, false);
      setDrawer(false, false);
    }
    state.shortcutsOpen = open;
    elements.shortcutBackdrop.hidden = !open;
    elements.shortcutDialog.hidden = !open;
    elements.shortcutButton.setAttribute("aria-expanded", String(open));
    document.body.classList.toggle("shortcuts-open", open);
    elements.skipLink.inert = open;
    elements.topbar.inert = open;
    elements.reader.inert = open;
    elements.outlineRail.inert = open;
    elements.reviewPanel.inert = open;
    elements.drawer.inert = open || (!sidebarPinned() && !document.body.classList.contains("drawer-open"));
    if (open) {
      window.requestAnimationFrame(() => elements.shortcutClose.focus());
      return;
    }
    const returnFocus = state.shortcutsReturnFocus;
    state.shortcutsReturnFocus = null;
    if (restoreFocus && returnFocus?.isConnected && !returnFocus.inert) {
      returnFocus.focus();
    } else if (restoreFocus) {
      elements.shortcutButton.focus();
    }
  }

  function focusDocumentFilter() {
    if (state.reviewMutationPending) return;
    if (state.shortcutsOpen) setShortcutDialog(false, false);
    if (state.reviewComposer) closeCommentComposer(false);
    if (state.reviewPanelOpen) setReviewPanel(false, false);
    setSettingsPopup(false, false);
    setDrawer(true, false);
    window.requestAnimationFrame(() => {
      elements.fileFilter.focus();
      elements.fileFilter.select();
    });
  }

  function toggleReviewPanelShortcut() {
    if (state.reviewMutationPending) return;
    if (!state.reviewEnabled || !reviewAvailable()) {
      elements.routeStatus.textContent = "Comments are not available for this document.";
      return;
    }
    if (state.shortcutsOpen) setShortcutDialog(false, false);
    if (state.reviewComposer) closeCommentComposer(false);
    setSettingsPopup(false, false);
    setReviewPanel(!state.reviewPanelOpen);
  }

  function shortcutTargetIsEditable(target) {
    return Boolean(target?.closest?.("input, select, textarea, [contenteditable='true']"));
  }

  function shortcutTargetIsInteractive(target) {
    return Boolean(target?.closest?.(
      "a, button, input, select, textarea, summary, [role='button'], [contenteditable='true']",
    ));
  }

  function showLoading() {
    elements.document.hidden = true;
    elements.statusView.hidden = false;
    elements.statusView.setAttribute("aria-busy", "true");
    elements.statusMessage.hidden = true;
    const skeleton = elements.statusView.querySelector(".document-skeleton");
    if (skeleton) skeleton.hidden = false;
  }

  function setDocumentPath(path) {
    const value = typeof path === "string" ? path : "";
    elements.documentPath.textContent = value;
    elements.documentPath.hidden = !value;
  }

  function showMessage(title, message, retry) {
    elements.document.hidden = true;
    state.activeNavigationBlockKey = "";
    elements.statusView.hidden = false;
    elements.statusView.setAttribute("aria-busy", "false");
    const skeleton = elements.statusView.querySelector(".document-skeleton");
    if (skeleton) skeleton.hidden = true;
    elements.statusMessage.replaceChildren();

    const heading = document.createElement("h1");
    heading.textContent = title;
    const body = document.createElement("p");
    body.textContent = message;
    elements.statusMessage.append(heading, body);

    if (retry) {
      const button = document.createElement("button");
      button.className = "retry-button";
      button.type = "button";
      button.textContent = "Try again";
      button.addEventListener("click", retry, { once: true });
      elements.statusMessage.append(button);
    }

    elements.statusMessage.hidden = false;
  }

  function showDocument() {
    elements.statusView.hidden = true;
    elements.statusView.setAttribute("aria-busy", "false");
    elements.document.hidden = false;
  }

  function announceUpdate(message) {
    window.clearTimeout(state.updateTimer);
    elements.updateNotice.classList.remove("is-visible");
    void elements.updateNotice.offsetWidth;
    elements.updateNotice.textContent = message;
    elements.updateNotice.classList.add("is-visible");
    state.updateTimer = window.setTimeout(() => {
      elements.updateNotice.classList.remove("is-visible");
    }, 2800);
  }

  function pageIsActive() {
    return document.visibilityState === "visible" && document.hasFocus();
  }

  function showUpdate(update) {
    state.pendingUpdate = null;
    if (update.signatures) state.highlightBaseline = update.signatures;

    window.clearTimeout(state.highlightTimer);
    for (const block of update.blocks) block.classList.remove("content-change");
    if (update.blocks.length) {
      void update.blocks[0].offsetWidth;
      for (const block of update.blocks) block.classList.add("content-change");
      state.highlightTimer = window.setTimeout(() => {
        for (const block of update.blocks) block.classList.remove("content-change");
      }, 1200);
    }
    announceUpdate(update.message);
  }

  function queueUpdate(message, blocks = [], signatures = null) {
    const update = { blocks, message, signatures };
    if (pageIsActive()) {
      showUpdate(update);
      return;
    }
    if (state.pendingUpdate && !signatures) {
      state.pendingUpdate.message = message;
      return;
    }
    state.pendingUpdate = update;
  }

  function showPendingUpdate() {
    if (pageIsActive() && state.pendingUpdate) showUpdate(state.pendingUpdate);
  }

  function blockSignatures(root) {
    return [...root.children].map((block) => block.outerHTML);
  }

  function changedBlockIndexes(previous, next) {
    let start = 0;
    while (start < previous.length && start < next.length && previous[start] === next[start]) start += 1;

    let previousEnd = previous.length;
    let nextEnd = next.length;
    while (previousEnd > start && nextEnd > start && previous[previousEnd - 1] === next[nextEnd - 1]) {
      previousEnd -= 1;
      nextEnd -= 1;
    }

    const changed = new Set();
    for (let index = start; index < nextEnd; index += 1) changed.add(index);
    const previousLength = previousEnd - start;
    const nextLength = nextEnd - start;
    if (
      !previousLength
      || !nextLength
      || previousLength > 2000
      || nextLength > 2000
      || previousLength * nextLength > 1_000_000
    ) return changed;

    const matches = Array.from({ length: previousLength + 1 }, () => new Uint32Array(nextLength + 1));
    for (let previousIndex = previousLength - 1; previousIndex >= 0; previousIndex -= 1) {
      for (let nextIndex = nextLength - 1; nextIndex >= 0; nextIndex -= 1) {
        matches[previousIndex][nextIndex] = previous[start + previousIndex] === next[start + nextIndex]
          ? matches[previousIndex + 1][nextIndex + 1] + 1
          : Math.max(matches[previousIndex + 1][nextIndex], matches[previousIndex][nextIndex + 1]);
      }
    }

    let previousIndex = 0;
    let nextIndex = 0;
    while (previousIndex < previousLength && nextIndex < nextLength) {
      if (previous[start + previousIndex] === next[start + nextIndex]) {
        changed.delete(start + nextIndex);
        previousIndex += 1;
        nextIndex += 1;
      } else if (matches[previousIndex + 1][nextIndex] >= matches[previousIndex][nextIndex + 1]) {
        previousIndex += 1;
      } else {
        nextIndex += 1;
      }
    }
    return changed;
  }

  async function responseError(response) {
    let body = "";
    try {
      body = (await response.text()).trim();
    } catch {
      body = "";
    }
    let detail = body;
    let code = "";
    if (body) {
      try {
        const payload = JSON.parse(body);
        if (typeof payload?.error === "string") detail = payload.error;
        if (typeof payload?.code === "string") code = payload.code;
      } catch {
        detail = body;
      }
    }
    return new APIError(detail || `The server returned ${response.status}.`, response.status, code);
  }

  async function fetchJSON(url, options = {}) {
    const response = await fetch(url, {
      ...options,
      headers: { Accept: "application/json", ...options.headers },
    });
    if (!response.ok) throw await responseError(response);
    return response.json();
  }

  async function fetchText(url, options = {}) {
    const response = await fetch(url, {
      ...options,
      headers: { Accept: "text/markdown, text/plain", ...options.headers },
    });
    if (!response.ok) throw await responseError(response);
    return response.text();
  }

  function makeTree(files) {
    const root = { directories: new Map(), files: [] };
    for (const path of files) {
      const parts = state.daemonMode ? [displayName(path)] : path.split("/");
      const name = parts.pop();
      let node = root;
      for (const part of parts) {
        if (!node.directories.has(part)) {
          node.directories.set(part, { directories: new Map(), files: [] });
        }
        node = node.directories.get(part);
      }
      node.files.push({ name, path });
    }
    return root;
  }

  function treeContainsPath(node, path) {
    if (node.files.some((file) => file.path === path)) return true;
    return [...node.directories.values()].some((child) => treeContainsPath(child, path));
  }

  function renderTreeNode(node, parentPath = "") {
    const list = document.createElement("ul");

    const directories = [...node.directories.entries()].sort(([a], [b]) => collator.compare(a, b));
    for (const [name, child] of directories) {
      const folderPath = parentPath ? `${parentPath}/${name}` : name;
      const item = document.createElement("li");
      const details = document.createElement("details");
      details.className = "folder";
      details.dataset.folder = folderPath;
      details.open = Boolean(state.filter) || state.openFolders.has(folderPath) || treeContainsPath(child, state.currentPath);
      const summary = document.createElement("summary");
      summary.textContent = name;
      details.append(summary, renderTreeNode(child, folderPath));
      details.addEventListener("toggle", () => {
        if (details.open) state.openFolders.add(folderPath);
        else state.openFolders.delete(folderPath);
      });
      item.append(details);
      list.append(item);
    }

    node.files.sort((a, b) => collator.compare(a.name, b.name));
    for (const file of node.files) {
      const item = document.createElement("li");
      const link = document.createElement("a");
      link.className = "file-link";
      link.href = buildRoute(file.path);
      link.dataset.path = file.path;
      const title = document.createElement("span");
      title.className = "file-link-title";
      title.textContent = file.name;
      link.append(title);
      if (state.daemonMode) {
        const review = state.fileReviews.get(file.path) || { status: "needs_review", unresolved: 0 };
        const badge = document.createElement("span");
        badge.className = "file-review-status";
        badge.textContent = reviewStatusLabel(review.status);
        link.append(badge);
        if (review.unresolved > 0) {
          const count = document.createElement("span");
          count.className = "file-review-count";
          count.textContent = String(review.unresolved);
          count.setAttribute("aria-hidden", "true");
          link.append(count);
        }
        const detail = document.createElement("span");
        detail.className = "sr-only";
        detail.textContent = `, ${review.unresolved} unresolved ${review.unresolved === 1 ? "comment" : "comments"}`;
        link.append(detail);
      }
      if (state.removed.get(file.path)) link.classList.add("is-removed");
      if (file.path === state.currentPath) link.setAttribute("aria-current", "page");
      if (state.daemonMode) {
        const row = document.createElement("div");
        row.className = "file-row";
        const removeButton = document.createElement("button");
        removeButton.className = "file-remove";
        removeButton.type = "button";
        removeButton.dataset.path = file.path;
        removeButton.textContent = "×";
        removeButton.title = "Remove from MDShelf";
        removeButton.setAttribute("aria-label", `Remove ${file.name} from MDShelf`);
        row.append(link, removeButton);
        item.append(row);
      } else {
        item.append(link);
      }
      list.append(item);
    }

    return list;
  }

  function renderFileTree() {
    const query = state.filter.trim().toLocaleLowerCase();
    const visibleFiles = query
      ? state.files.filter((path) => displayName(path).toLocaleLowerCase().includes(query))
      : state.files;

    elements.fileNav.replaceChildren();
    if (!visibleFiles.length) {
      const empty = document.createElement("p");
      empty.className = "nav-empty";
      empty.textContent = query ? "No documents match this filter." : "No Markdown files found.";
      elements.fileNav.append(empty);
    } else {
      const tree = renderTreeNode(makeTree(visibleFiles));
      tree.classList.add("file-tree");
      elements.fileNav.append(tree);
    }

    const total = state.files.length;
    elements.fileCount.textContent = query
      ? `${visibleFiles.length} of ${total} ${total === 1 ? "document" : "documents"}`
      : `${total} ${total === 1 ? "document" : "documents"}`;
  }

  function visibleFileLinks() {
    const links = [...elements.fileNav.querySelectorAll(".folder > summary, .file-link"), elements.demoLink];
    return links.filter((link) => !link.closest("[hidden]") && link.getClientRects().length > 0);
  }

  function moveFileListFocus(action) {
    const links = visibleFileLinks();
    const current = links.indexOf(document.activeElement);
    const index = listNavigationIndex(current, links.length, action);
    if (index < 0) return false;
    links[index].focus();
    return true;
  }

  function updateActiveFile() {
    for (const link of elements.fileNav.querySelectorAll(".file-link")) {
      if (link.dataset.path === state.currentPath) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
    if (state.currentPath === demoDocumentPath) elements.demoLink.setAttribute("aria-current", "page");
    else elements.demoLink.removeAttribute("aria-current");
  }

  function setFileList(payload) {
    if (!Array.isArray(payload?.files)) throw new Error("The server returned an invalid file list.");
    const daemonMode = payload.mode === "daemon";
    const allStrings = payload.files.every((row) => typeof row === "string");
    const allObjects = payload.files.every((row) => (
      row
      && typeof row === "object"
      && typeof row.path === "string"
      && typeof row.title === "string"
      && typeof row.reviewStatus === "string"
      && Number.isSafeInteger(row.openComments)
      && row.openComments >= 0
    ));
    if ((!daemonMode && !allStrings) || (daemonMode && !allObjects)) {
      throw new Error("The server returned an invalid file list.");
    }

    const titles = new Map();
    const removed = new Map();
    const fileReviews = new Map();
    const values = payload.files.map((row) => {
      if (typeof row === "string") return normalizePath(row);
      const path = normalizePath(row.path);
      titles.set(path, row.title.trim() || titleFromPath(path));
      removed.set(path, row.removed === true);
      fileReviews.set(path, {
        status: reviewStatuses.has(row.reviewStatus) ? row.reviewStatus : "unknown",
        unresolved: row.openComments,
      });
      return path;
    }).filter(Boolean);
    state.daemonMode = daemonMode;
    state.titles = titles;
    state.removed = removed;
    state.fileReviews = fileReviews;
    state.files = [...new Set(values)].sort((a, b) => collator.compare(displayName(a), displayName(b)));
    state.fileSet = new Set(state.files);
    const home = defaultDocument();
    elements.brand.href = home ? buildRoute(home) : "#";
    renderFileTree();
  }

  async function refreshFileList() {
    setFileList(await fetchJSON("/api/files"));
  }

  function daemonRemoveRequest(path) {
    return {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: path }),
    };
  }

  async function removeDaemonDocument(path, button) {
    if (!state.daemonMode || !state.fileSet.has(path)) return;
    const name = displayName(path);
    button.disabled = true;
    try {
      await fetchJSON("/api/control/remove", daemonRemoveRequest(path));
      await refreshFileList();
      if (state.currentPath === path) {
        const fallback = defaultDocument(path) || demoDocumentPath;
        if (!sidebarPinned()) setDrawer(false, false);
        window.location.hash = buildRoute(fallback);
      }
      announceUpdate(`${name} removed from MDShelf`);
    } catch (error) {
      button.disabled = false;
      const message = error instanceof TypeError ? "MDShelf could not reach the local server." : error.message;
      announceUpdate(`Could not remove ${name}: ${message}`);
    }
  }

  function assetURL(documentPath, reference) {
    const { path, fragment } = splitReference(reference);
    const resolved = resolveReference(documentPath, path);
    if (!resolved) return "";
    return `/api/asset?path=${encodeURIComponent(resolved)}${fragment ? `#${fragment}` : ""}`;
  }

  function rewriteImage(image, documentPath) {
    const source = image.getAttribute("src");
    if (!source || isAssetReference(source) || isRemoteReference(source)) return;
    const rewritten = assetURL(documentPath, source);
    if (!rewritten) return;
    image.setAttribute("src", rewritten);
    image.loading = "lazy";
    image.decoding = "async";
  }

  function rewriteSourceSet(element, documentPath) {
    const sourceSet = element.getAttribute("srcset");
    if (!sourceSet) return;
    const candidates = sourceSet.split(",").map((candidate) => {
      const match = candidate.trim().match(/^(\S+)(\s+.+)?$/);
      if (!match || isAssetReference(match[1]) || isRemoteReference(match[1])) return candidate.trim();
      const rewritten = assetURL(documentPath, match[1]);
      return rewritten ? `${rewritten}${match[2] || ""}` : candidate.trim();
    });
    element.setAttribute("srcset", candidates.join(", "));
  }

  function rewriteLink(link, documentPath) {
    const reference = link.getAttribute("href");
    if (!reference) return;

    if (reference.startsWith("#/")) {
      link.dataset.documentRoute = "true";
      return;
    }
    if (reference.startsWith("#")) {
      link.href = buildRoute(documentPath, reference.slice(1));
      link.dataset.documentRoute = "true";
      return;
    }

    if (isRemoteReference(reference)) {
      if (/^https?:/i.test(reference)) link.rel = "noopener noreferrer";
      return;
    }

    const { path, fragment } = splitReference(reference);
    const resolved = resolveReference(documentPath, path);
    if (!resolved) return;
    const isMarkdown = state.fileSet.has(resolved) || /\.(?:md|markdown|mdown|mkd)$/i.test(resolved);
    if (!isMarkdown) return;

    link.href = buildRoute(resolved, fragment);
    link.dataset.documentRoute = "true";
  }

  function codeBlockText(figure) {
    const source = figure.querySelector(".lntd:last-child pre") || figure.querySelector("pre");
    if (!source) return "";
    const clone = source.cloneNode(true);
    for (const lineNumber of clone.querySelectorAll(".ln, .lnt")) lineNumber.remove();
    return clone.textContent;
  }

  function addCodeBlockTools(root) {
    const figures = new Set(root.querySelectorAll("figure.code-block"));
    for (const pre of root.querySelectorAll("pre:not(.mermaid)")) {
      let figure = pre.closest("figure.code-block");
      if (!figure) {
        figure = document.createElement("figure");
        figure.className = "code-block";
        pre.before(figure);
        figure.append(pre);
      }
      figures.add(figure);
    }

    for (const figure of figures) {
      if (figure.querySelector(".code-toolbar")) continue;
      const toolbar = document.createElement("div");
      toolbar.className = "code-toolbar";
      const title = figure.dataset.codeTitle?.trim();
      if (title) {
        const caption = document.createElement("figcaption");
        caption.className = "code-title";
        caption.textContent = title;
        toolbar.append(caption);
      }
      const button = document.createElement("button");
      button.className = "code-copy";
      button.type = "button";
      button.textContent = "Copy";
      button.setAttribute("aria-label", title ? `Copy code from ${title}` : "Copy code");
      button.setAttribute("aria-live", "polite");
      toolbar.append(button);
      figure.prepend(toolbar);
    }
  }

  async function copyCodeBlock(button) {
    const figure = button.closest("figure.code-block");
    if (!figure) return;
    const source = codeBlockText(figure);
    const original = button.textContent;
    try {
      await navigator.clipboard.writeText(source);
      button.textContent = "Copied";
    } catch {
      button.textContent = "Copy failed";
    }
    window.setTimeout(() => { button.textContent = original; }, 1600);
  }

  function addHeadingPermalinks(root, documentPath) {
    for (const heading of root.querySelectorAll("h1[id], h2[id], h3[id], h4[id], h5[id], h6[id]")) {
      if (heading.querySelector(".heading-permalink")) continue;
      const link = document.createElement("a");
      const label = heading.textContent.trim() || "heading";
      link.className = "heading-permalink";
      link.href = buildRoute(documentPath, heading.id);
      link.dataset.documentRoute = "true";
      link.setAttribute("aria-label", `Link to ${label}`);
      link.title = "Link to this heading";
      link.textContent = "#";
      heading.append(" ", link);
    }
  }

  function prepareDocument(root, documentPath) {
    for (const image of root.querySelectorAll("img[src]")) {
      rewriteImage(image, documentPath);
      rewriteSourceSet(image, documentPath);
    }
    for (const source of root.querySelectorAll("source[srcset]")) {
      rewriteSourceSet(source, documentPath);
    }
    for (const link of root.querySelectorAll("a[href]")) {
      rewriteLink(link, documentPath);
    }
    for (const table of root.querySelectorAll("table")) {
      if (table.parentElement?.classList.contains("table-scroll")) continue;
      const wrapper = document.createElement("div");
      wrapper.className = "table-scroll";
      wrapper.tabIndex = 0;
      wrapper.setAttribute("role", "region");
      wrapper.setAttribute("aria-label", "Scrollable table");
      table.before(wrapper);
      wrapper.append(table);
    }
    addCodeBlockTools(root);
    addHeadingPermalinks(root, documentPath);
  }

  function renderMath(root) {
    const expressions = [...root.querySelectorAll(".math-source")];
    if (!expressions.length || !window.katex?.render) return;
    for (const expression of expressions) {
      const source = expression.textContent;
      try {
        window.katex.render(source, expression, {
          displayMode: expression.dataset.display === "true",
          output: "htmlAndMathml",
          strict: "warn",
          throwOnError: false,
          trust: false,
        });
      } catch (error) {
        expression.textContent = source;
        expression.classList.add("math-error");
        expression.title = error instanceof Error ? error.message : "Math rendering failed";
      }
    }
  }

  function renderMermaid(root, isCurrent) {
    const diagrams = [...root.querySelectorAll("pre.mermaid")];
    if (!diagrams.length || !window.mermaid) return Promise.resolve();
    const generation = ++state.renderGeneration;
    const render = async () => {
      for (let index = 0; index < diagrams.length; index += 1) {
        if (!isCurrent()) return;
        const diagram = diagrams[index];
        const source = diagram.textContent;
        try {
          await window.mermaid.parse(source);
          if (!isCurrent()) return;
          const id = `mdshelf-mermaid-${generation}-${index}-${++state.mermaidCounter}`;
          const rendered = await window.mermaid.render(id, source);
          if (!isCurrent()) return;
          diagram.innerHTML = rendered.svg;
          diagram.classList.add("mermaid-rendered");
          rendered.bindFunctions?.(diagram);
        } catch {
          if (!isCurrent()) return;
          diagram.textContent = source;
          diagram.classList.add("mermaid-error");
          const message = document.createElement("span");
          message.className = "mermaid-error-message";
          message.textContent = "MDShelf could not render this diagram.";
          message.setAttribute("role", "alert");
          diagram.prepend(message);
        }
      }
    };
    const queued = state.mermaidQueue.then(render, render);
    state.mermaidQueue = queued.catch(() => {});
    return queued;
  }

  // Review interface

  function validLineRange(value) {
    return value
      && Number.isSafeInteger(value.startLine)
      && Number.isSafeInteger(value.endLine)
      && value.startLine > 0
      && value.endLine >= value.startLine;
  }

  function validateReviewAnchor(anchor) {
    if (
      !anchor
      || !blockKeyPattern.test(anchor.blockKey)
      || typeof anchor.kind !== "string"
      || !validLineRange(anchor)
      || !Array.isArray(anchor.headingPath)
      || !anchor.headingPath.every((heading) => typeof heading === "string")
      || typeof anchor.quote !== "string"
    ) throw new Error("The server returned an invalid comment anchor.");
    return {
      blockKey: anchor.blockKey,
      kind: anchor.kind,
      startLine: anchor.startLine,
      endLine: anchor.endLine,
      headingPath: [...anchor.headingPath],
      quote: anchor.quote,
    };
  }

  function reviewAnchorsEqual(left, right) {
    return left.blockKey === right.blockKey
      && left.kind === right.kind
      && left.startLine === right.startLine
      && left.endLine === right.endLine
      && left.quote === right.quote
      && left.headingPath.length === right.headingPath.length
      && left.headingPath.every((heading, index) => heading === right.headingPath[index]);
  }

  function validateReviewView(payload, path) {
    if (payload?.schemaVersion !== 1 || !Number.isSafeInteger(payload.revision) || payload.revision < 0) {
      throw new Error("The server returned an invalid review.");
    }
    const reviewDocument = payload.document;
    if (
      !reviewDocument
      || reviewDocument.id !== path
      || reviewDocument.path !== path
      || typeof reviewDocument.title !== "string"
      || !sourceHashPattern.test(reviewDocument.sourceHash)
      || !reviewStatuses.has(reviewDocument.reviewStatus)
    ) throw new Error("The server returned an invalid comment document.");

    if (!Array.isArray(payload.comments)) throw new Error("The server returned an invalid comment list.");
    const ids = new Set();
    const replyIDs = new Set();
    const comments = payload.comments.map((comment) => {
      if (
        !comment
        || !commentIDPattern.test(comment.id)
        || ids.has(comment.id)
        || typeof comment.body !== "string"
        || !commentStatuses.has(comment.status)
        || !sourceHashPattern.test(comment.baseHash)
        || typeof comment.outdated !== "boolean"
        || !Array.isArray(comment.replies)
      ) throw new Error("The server returned an invalid comment.");
      ids.add(comment.id);
      const anchor = comment.anchor === null ? null : validateReviewAnchor(comment.anchor);
      let textRange = null;
      if (comment.textRange !== undefined) {
        const value = comment.textRange;
        if (
          !textSelection
          || !value
          || value.version !== textSelection.rangeVersion
          || !Array.isArray(value.anchors)
          || value.anchors.length < 1
          || value.anchors.length > textSelection.maxBlocks
          || !Number.isSafeInteger(value.startOffset)
          || !Number.isSafeInteger(value.endOffset)
          || value.startOffset < 0
          || value.endOffset <= 0
          || typeof value.quote !== "string"
          || !value.quote.trim()
          || value.quote.includes("\0")
          || textSelection.textByteLength(value.quote) > textSelection.maxQuoteBytes
        ) throw new Error("The server returned an invalid text range.");
        const anchors = value.anchors.map(validateReviewAnchor);
        if (
          new Set(anchors.map((candidate) => candidate.blockKey)).size !== anchors.length
          || anchors.some((candidate, index) => index > 0 && candidate.startLine <= anchors[index - 1].endLine)
        ) throw new Error("The server returned unordered text range anchors.");
        if (!anchor || !reviewAnchorsEqual(anchor, anchors[0])) {
          throw new Error("The text range anchor does not match the comment anchor.");
        }
        if (anchors.length === 1 && value.startOffset >= value.endOffset) {
          throw new Error("The server returned invalid text range offsets.");
        }
        const currentBlockKeys = value.currentBlockKeys === undefined ? null : value.currentBlockKeys;
        if (
          currentBlockKeys !== null
          && (!Array.isArray(currentBlockKeys)
            || currentBlockKeys.length !== anchors.length
            || !currentBlockKeys.every((key) => blockKeyPattern.test(key))
            || new Set(currentBlockKeys).size !== currentBlockKeys.length)
        ) throw new Error("The server returned invalid current text range blocks.");
        if (!comment.outdated && currentBlockKeys === null) {
          throw new Error("The server returned no current text range blocks.");
        }
        textRange = {
          version: value.version,
          anchors,
          startOffset: value.startOffset,
          endOffset: value.endOffset,
          quote: value.quote,
          currentBlockKeys: currentBlockKeys ? [...currentBlockKeys] : null,
        };
      }
      if (comment.currentLocation !== null && !validLineRange(comment.currentLocation)) {
        throw new Error("The server returned an invalid comment location.");
      }
      if (comment.currentBlockKey !== null && !blockKeyPattern.test(comment.currentBlockKey)) {
        throw new Error("The server returned an invalid current block key.");
      }
      if (textRange?.currentBlockKeys && comment.currentBlockKey !== textRange.currentBlockKeys[0]) {
        throw new Error("The current text range does not match the current block key.");
      }
      const replies = comment.replies.map((reply) => {
        if (
          !reply
          || !replyIDPattern.test(reply.id)
          || replyIDs.has(reply.id)
          || typeof reply.body !== "string"
          || !replyAuthors.has(reply.author)
          || typeof reply.createdAt !== "string"
          || Number.isNaN(Date.parse(reply.createdAt))
        ) throw new Error("The server returned an invalid comment reply.");
        replyIDs.add(reply.id);
        return { id: reply.id, body: reply.body, author: reply.author, createdAt: reply.createdAt };
      });
      return {
        id: comment.id,
        body: comment.body,
        status: comment.status,
        baseHash: comment.baseHash,
        outdated: comment.outdated,
        anchor,
        textRange,
        ranges: [],
        rangeUnavailable: false,
        currentLocation: comment.currentLocation ? { ...comment.currentLocation } : null,
        currentBlockKey: comment.currentBlockKey,
        replies,
      };
    });
    return {
      revision: payload.revision,
      sourceHash: reviewDocument.sourceHash,
      reviewStatus: reviewDocument.reviewStatus,
      comments,
    };
  }

  function buildReviewBlockMap(root, metadata) {
    if (!Array.isArray(metadata)) throw new Error("The server returned invalid document blocks.");
    const wrappers = [...root.querySelectorAll(".md-block[data-md-block]")];
    const elementsByKey = new Map();
    for (const wrapper of wrappers) {
      const key = wrapper.dataset.mdBlock;
      if (!blockKeyPattern.test(key) || elementsByKey.has(key)) {
        throw new Error("The document contains invalid block keys.");
      }
      elementsByKey.set(key, wrapper);
    }
    if (metadata.length !== wrappers.length) throw new Error("Document block metadata does not match the document.");
    const blocks = new Map();
    for (let index = 0; index < metadata.length; index += 1) {
      const block = metadata[index];
      if (
        !block
        || !blockKeyPattern.test(block.key)
        || typeof block.kind !== "string"
        || !validLineRange(block)
        || blocks.has(block.key)
        || !elementsByKey.has(block.key)
        || elementsByKey.get(block.key) !== wrappers[index]
      ) throw new Error("The server returned invalid document block metadata.");
      blocks.set(block.key, { ...block, element: elementsByKey.get(block.key) });
    }
    if (blocks.size !== elementsByKey.size) throw new Error("Document block metadata does not match the document.");
    return blocks;
  }

  function appendReviewBlockControls(blocks) {
    for (const block of blocks.values()) {
      const controls = document.createElement("div");
      controls.className = "md-block-review-controls";
      const button = document.createElement("button");
      const label = `Comment on ${lineRangeLabel(block.startLine, block.endLine)}`;
      button.className = "md-block-comment";
      button.type = "button";
      button.dataset.blockKey = block.key;
      button.dataset.focusKey = `block:${block.key}:comment`;
      button.textContent = "+";
      button.title = label;
      button.setAttribute("aria-label", label);
      button.disabled = true;
      const count = document.createElement("button");
      count.className = "md-block-comment-count";
      count.type = "button";
      count.dataset.blockKey = block.key;
      count.hidden = true;
      const bubbles = document.createElement("div");
      bubbles.className = "md-block-comment-bubbles";
      controls.append(button, count, bubbles);
      block.element.append(controls);
    }
  }

  function headingText(heading) {
    let text = "";
    for (const node of heading.childNodes) {
      if (node.nodeType === 1 && node.classList?.contains("heading-permalink")) continue;
      text += node.textContent || "";
    }
    return text.trim();
  }

  /* Unresolved comments belong to the last heading before their block. */
  function outlineCommentCounts(headings) {
    const counts = new Map();
    if (!state.reviewEnabled || !headings.length) return counts;
    const byBlock = commentCountsByBlockKey(state.reviewComments);
    const headingBlocks = new Map();
    for (const heading of headings) {
      const block = heading.closest?.(".md-block");
      if (block) headingBlocks.set(block, heading);
    }
    let current = null;
    for (const block of elements.document.querySelectorAll(".md-block[data-md-block]")) {
      if (headingBlocks.has(block)) current = headingBlocks.get(block);
      if (!current) continue;
      const count = byBlock[block.dataset.mdBlock];
      if (!count || !count.unresolved) continue;
      counts.set(current, (counts.get(current) || 0) + count.unresolved);
    }
    return counts;
  }

  function updateOutline() {
    if (!elements.outlineRail || !elements.outlineList) return;
    const headings = elements.document.hidden
      ? []
      : [...elements.document.querySelectorAll("h2, h3, h4")];
    let section = 0;
    for (const heading of headings) {
      if (heading.tagName !== "H2") continue;
      section += 1;
      heading.dataset.section = `\u00a7 ${section}`;
    }
    const counts = outlineCommentCounts(headings);
    const linked = headings.filter((heading) => heading.id);
    const items = linked.map((heading) => {
      const link = document.createElement("a");
      link.className = `outline-item level-${heading.tagName.slice(1)}`;
      link.href = buildRoute(state.currentPath, heading.id);
      link.dataset.documentRoute = "true";
      link.dataset.outlineTarget = heading.id;
      const tick = document.createElement("span");
      tick.className = "outline-tick";
      tick.setAttribute("aria-hidden", "true");
      const label = document.createElement("span");
      label.className = "outline-text";
      label.textContent = headingText(heading);
      link.append(tick, label);
      const unresolved = counts.get(heading) || 0;
      if (unresolved) {
        const badge = document.createElement("span");
        badge.className = "outline-count";
        badge.textContent = String(unresolved);
        badge.setAttribute("aria-label", `${unresolved} unresolved ${unresolved === 1 ? "comment" : "comments"}`);
        link.append(badge);
        link.classList.add("has-comments");
      }
      return link;
    });
    state.outlineHeadings = linked;
    elements.outlineList.replaceChildren(...items);
    const show = items.length > 1;
    elements.outlineRail.hidden = !show;
    document.body.classList.toggle("has-outline", show);
    trackOutline();
  }

  function trackOutline() {
    if (!elements.outlineList || !elements.outlineRail || elements.outlineRail.hidden) return;
    const limit = elements.topbar.getBoundingClientRect().height + 24;
    let current = "";
    for (const heading of state.outlineHeadings) {
      if (heading.getBoundingClientRect().top > limit) break;
      current = heading.id;
    }
    if (!current && state.outlineHeadings.length) current = state.outlineHeadings[0].id;
    for (const item of elements.outlineList.children) {
      item.classList.toggle("is-current", item.dataset.outlineTarget === current);
    }
  }

  function scheduleOutlineTracking() {
    if (state.outlineFrame) return;
    state.outlineFrame = window.requestAnimationFrame(() => {
      state.outlineFrame = 0;
      trackOutline();
    });
  }

  function navigationBlocks() {
    if (elements.document.hidden) return [];
    return [...elements.document.querySelectorAll(".md-block[data-md-block]")];
  }

  function setActiveNavigationBlock(block, announce = false) {
    const blocks = navigationBlocks();
    if (!block || !blocks.includes(block)) return false;
    state.activeNavigationBlockKey = block.dataset.mdBlock || "";
    for (const candidate of blocks) {
      candidate.classList.toggle("is-keyboard-active", candidate === block);
    }
    if (announce) {
      const index = blocks.indexOf(block);
      elements.routeStatus.textContent = `Block ${index + 1} of ${blocks.length}`;
    }
    return true;
  }

  function syncActiveNavigationBlock() {
    const blocks = navigationBlocks();
    const active = blocks.find((block) => block.dataset.mdBlock === state.activeNavigationBlockKey);
    if (active) {
      setActiveNavigationBlock(active);
      return;
    }
    trackActiveNavigationBlock();
  }

  function trackActiveNavigationBlock() {
    const blocks = navigationBlocks();
    if (!blocks.length) {
      state.activeNavigationBlockKey = "";
      return;
    }
    const offset = elements.topbar.getBoundingClientRect().bottom + 20;
    const index = blockIndexAtViewport(blocks.map((block) => block.getBoundingClientRect()), offset);
    setActiveNavigationBlock(blocks[Math.max(0, index)]);
  }

  function scheduleActiveNavigationTracking() {
    if (state.sectionTrackingLocked || state.sectionFrame) return;
    state.sectionFrame = window.requestAnimationFrame(() => {
      state.sectionFrame = 0;
      trackActiveNavigationBlock();
    });
  }

  function activeNavigationBlock() {
    const blocks = navigationBlocks();
    const active = blocks.find((block) => block.dataset.mdBlock === state.activeNavigationBlockKey);
    if (active) return active;
    if (!blocks.length) return null;
    const offset = elements.topbar.getBoundingClientRect().bottom + 20;
    const index = blockIndexAtViewport(blocks.map((block) => block.getBoundingClientRect()), offset);
    return blocks[Math.max(0, index)];
  }

  function moveActiveNavigationBlock(action) {
    const blocks = navigationBlocks();
    if (!blocks.length) return false;
    const current = activeNavigationBlock();
    const currentIndex = blocks.indexOf(current);
    let index = currentIndex;
    if (action === "previous-block") index = Math.max(0, currentIndex - 1);
    else if (action === "next-block") index = Math.min(blocks.length - 1, currentIndex + 1);
    else if (action === "first-block") index = 0;
    else if (action === "last-block") index = blocks.length - 1;
    else return false;

    if (state.sectionFrame) {
      window.cancelAnimationFrame(state.sectionFrame);
      state.sectionFrame = 0;
    }
    window.clearTimeout(state.sectionTrackingTimer);
    state.sectionTrackingLocked = false;

    const block = blocks[index];
    setActiveNavigationBlock(block, true);
    const direction = Math.sign(index - currentIndex);
    const delta = navigationScrollDelta(
      block.getBoundingClientRect(),
      elements.topbar.getBoundingClientRect().bottom,
      window.innerHeight,
      direction,
    );
    if (delta === 0) return true;

    state.sectionTrackingLocked = true;
    const root = document.documentElement;
    const previousScrollBehavior = root.style.scrollBehavior;
    root.style.scrollBehavior = "auto";
    window.scrollTo(0, window.scrollY + delta);
    root.style.scrollBehavior = previousScrollBehavior;
    state.sectionTrackingTimer = window.setTimeout(() => {
      state.sectionTrackingLocked = false;
    }, 120);
    return true;
  }

  function disarmBlockCommentControls(except = null) {
    for (const block of elements.document.querySelectorAll(".md-block.is-comment-armed")) {
      if (block !== except) block.classList.remove("is-comment-armed");
    }
  }

  function armBlockCommentControl(block) {
    disarmBlockCommentControls(block);
    block.classList.add("is-comment-armed");
  }

  function clearOwnedHighlights() {
    if (!textSelection || !window.CSS?.highlights) return;
    window.CSS.highlights.delete(textSelection.highlightNames.comments);
    window.CSS.highlights.delete(textSelection.highlightNames.active);
  }

  function hideSelectionCommentAction() {
    state.liveSelection = null;
    state.latchedSelection = null;
    if (elements.selectionCommentAction) elements.selectionCommentAction.hidden = true;
  }

  function clearTextCommentHover() {
    if (state.textCommentHoverFrame) {
      window.cancelAnimationFrame(state.textCommentHoverFrame);
      state.textCommentHoverFrame = 0;
    }
    state.textCommentHoverPoint = null;
    elements.document.classList.remove("is-text-comment-hover");
  }

  function updateTextCommentHover() {
    state.textCommentHoverFrame = 0;
    const point = state.textCommentHoverPoint;
    const commentID = point && state.highlightAvailable
      ? textSelection.textCommentIDAtPoint(
        state.reviewComments,
        point.x,
        point.y,
        state.activeCommentID,
      )
      : "";
    elements.document.classList.toggle("is-text-comment-hover", Boolean(commentID));
  }

  function scheduleTextCommentHover(event) {
    state.textCommentHoverPoint = { x: event.clientX, y: event.clientY };
    if (state.textCommentHoverFrame) return;
    state.textCommentHoverFrame = window.requestAnimationFrame(updateTextCommentHover);
  }

  function clearTextSelectionState() {
    if (state.selectionFrame) {
      window.clearTimeout(state.selectionFrame);
      state.selectionFrame = 0;
    }
    clearTextCommentHover();
    hideSelectionCommentAction();
    clearOwnedHighlights();
    state.reviewTextIndex = null;
  }

  function resolveCommentTextRanges(comments) {
    for (const comment of comments) {
      comment.ranges = [];
      comment.rangeUnavailable = false;
      if (!comment.textRange || comment.outdated || !state.reviewTextIndex || !textSelection) continue;
      const result = textSelection.reconstructTextRange(comment.textRange, state.reviewTextIndex);
      comment.ranges = result.ranges;
      comment.rangeUnavailable = !result.available;
    }
  }

  function refreshTextHighlights() {
    clearOwnedHighlights();
    if (!textSelection || !state.highlightAvailable) return;
    const groups = textSelection.planHighlightGroups(
      state.reviewComments,
      state.activeCommentID,
      state.highlightAvailable,
    );
    try {
      if (groups.current.length) {
        window.CSS.highlights.set(textSelection.highlightNames.comments, new window.Highlight(...groups.current));
      }
      if (groups.active.length) {
        window.CSS.highlights.set(textSelection.highlightNames.active, new window.Highlight(...groups.active));
      }
    } catch {
      state.highlightAvailable = false;
      clearOwnedHighlights();
    }
  }

  function positionSelectionCommentAction(descriptor) {
    const button = elements.selectionCommentAction;
    const rect = textSelection?.descriptorRect(descriptor);
    if (!button || !rect || button.hidden) return;
    const viewport = window.visualViewport;
    const leftEdge = (viewport?.offsetLeft || 0) + 8;
    const topEdge = (viewport?.offsetTop || 0) + 8;
    const rightEdge = leftEdge + (viewport?.width || window.innerWidth) - 16;
    const bottomEdge = topEdge + (viewport?.height || window.innerHeight) - 16;
    const buttonRect = button.getBoundingClientRect();
    const left = Math.min(Math.max(rect.left, leftEdge), Math.max(leftEdge, rightEdge - buttonRect.width));
    let top = rect.bottom + 8;
    if (top + buttonRect.height > bottomEdge) top = rect.top - buttonRect.height - 8;
    top = Math.min(Math.max(top, topEdge), Math.max(topEdge, bottomEdge - buttonRect.height));
    button.style.left = `${left}px`;
    button.style.top = `${top}px`;
  }

  function captureLiveSelection() {
    if (!textSelection || !state.reviewTextIndex || !canAddComment() || state.reviewComposer) return null;
    const descriptor = textSelection.captureSelection(window.getSelection(), state.reviewTextIndex, elements.document);
    return textSelection.selectionForGeneration(descriptor, state.reviewRenderGeneration);
  }

  function selectionCommentActionDescriptor(latchedDescriptor, liveDescriptor) {
    return latchedDescriptor || liveDescriptor || null;
  }

  function selectionCommentLiveDescriptor(capturedDescriptor, currentDescriptor, actionFocused) {
    return capturedDescriptor || (actionFocused ? currentDescriptor : null);
  }

  function updateSelectionCommentAction() {
    state.selectionFrame = 0;
    const button = elements.selectionCommentAction;
    const descriptor = selectionCommentLiveDescriptor(
      captureLiveSelection(),
      state.liveSelection,
      button === document.activeElement,
    );
    state.liveSelection = descriptor;
    if (!button) return;
    button.hidden = !descriptor;
    if (!descriptor) return;
    const quote = textSelection.shortQuote(descriptor.quote, 72);
    button.setAttribute("aria-label", `Comment on selected text: ${quote}`);
    positionSelectionCommentAction(descriptor);
  }

  function scheduleSelectionCommentAction() {
    if (state.selectionFrame) return;
    state.selectionFrame = window.setTimeout(updateSelectionCommentAction, 0);
  }

  function clearReviewState(closePanel = true) {
    clearTextSelectionState();
    state.reviewEnabled = false;
    state.reviewRevision = 0;
    state.reviewStatus = "needs_review";
    state.reviewSourceHash = "";
    state.reviewComments = [];
    state.reviewBlocks = new Map();
    state.reviewLoading = false;
    state.reviewMutationPending = false;
    state.reviewMutationToken += 1;
    state.reviewLoadToken += 1;
    state.reviewError = "";
    state.reviewBlockError = "";
    state.reviewErrorNeedsRender = false;
    state.reviewComposer = null;
    renderCommentComposer();
    state.activeBlockKey = "";
    state.activeCommentID = "";
    if (closePanel) setReviewPanel(false, false);
    updateReviewButton();
  }

  function reviewAvailable() {
    return reviewPanelAvailable({
      daemonMode: state.daemonMode,
      currentPath: state.currentPath,
      removed: state.removed.get(state.currentPath) === true,
      loadError: Boolean(state.reviewError),
    });
  }

  function unresolvedComments() {
    return state.reviewComments.filter((comment) => comment.status === "open" || comment.status === "addressed");
  }

  function lineRangeLabel(start, end) {
    return start === end ? `line ${start}` : `lines ${start} through ${end}`;
  }

  function commentStatusLabel(status) {
    return ({ open: "Open", addressed: "Addressed", resolved: "Resolved" })[status] || "Unknown";
  }

  function updateReviewButton() {
    const available = reviewAvailable() && state.reviewEnabled;
    elements.reviewButton.hidden = !available;
    elements.reviewButton.setAttribute("aria-expanded", String(available && state.reviewPanelOpen));
    if (!available) return;
    const unresolved = unresolvedComments().length;
    elements.reviewButtonCount.hidden = unresolved === 0;
    elements.reviewButtonCount.textContent = String(unresolved);
    elements.reviewButton.setAttribute(
      "aria-label",
      `Open comments, ${unresolved ? `${unresolved} unresolved` : "none unresolved"}`,
    );
  }

  function canAddComment() {
    return commentAddAvailable({
      loading: state.reviewLoading,
      pending: state.reviewMutationPending,
      error: state.reviewError || state.reviewBlockError,
      loaded: state.reviewEnabled,
    });
  }

  function reviewTextError(value, allowEmpty = false) {
    if (!allowEmpty && !String(value).trim()) return "Enter a comment.";
    if (String(value).includes("\0")) return "Remove the NUL character.";
    if (new TextEncoder().encode(String(value)).length > maxReviewTextBytes) return "Text must not exceed 16 KiB.";
    return "";
  }

  function renderReviewError() {
    elements.reviewError.replaceChildren();
    const detail = state.reviewBlockError || state.reviewError;
    elements.reviewError.hidden = !detail;
    if (!detail) return;
    const message = document.createElement("p");
    message.textContent = detail;
    const retry = document.createElement("button");
    retry.type = "button";
    retry.textContent = "Retry";
    retry.addEventListener("click", () => {
      if (state.reviewBlockError || state.reviewErrorNeedsRender) {
        const route = readRoute();
        void loadDocument(state.currentPath, route.fragment, { force: true, live: true });
      } else {
        void loadReview({ automatic: false });
      }
    });
    elements.reviewError.append(message, retry);
  }

  function commentLocationText(comment) {
    if (!comment.anchor) return "Whole document";
    const heading = comment.anchor.headingPath.length ? comment.anchor.headingPath.join(" > ") : "Document root";
    const location = `${heading}, ${lineRangeLabel(comment.anchor.startLine, comment.anchor.endLine)}`;
    return comment.textRange ? `Selected text in ${location}` : location;
  }

  function makeSelectedQuote(comment) {
    if (!comment.textRange) return null;
    const quote = document.createElement("span");
    quote.className = "review-selected-quote";
    quote.textContent = `“${textSelection.shortQuote(comment.textRange.quote)}”`;
    return quote;
  }

  function makeCommentReplies(comment, compact = false) {
    const replies = document.createElement("ol");
    replies.className = compact ? "comment-replies comment-replies-compact" : "comment-replies";
    for (const reply of comment.replies) {
      const item = document.createElement("li");
      const author = document.createElement("strong");
      author.textContent = reply.author === "agent" ? "Agent" : "Reviewer";
      const body = document.createElement("span");
      body.textContent = reply.body;
      item.append(author, body);
      replies.append(item);
    }
    return replies;
  }

  function makeCommentActions(comment, location) {
    const actions = document.createElement("div");
    actions.className = "comment-thread-actions";
    const reply = document.createElement("button");
    reply.type = "button";
    reply.textContent = "Reply";
    reply.dataset.commentAction = "reply";
    reply.dataset.actionCommentId = comment.id;
    reply.dataset.actionLocation = location;
    reply.dataset.focusKey = `${location}:${comment.id}:reply`;
    reply.disabled = comment.status === "resolved" || state.reviewMutationPending;
    if (comment.status === "resolved") reply.title = "Reopen the comment before you reply.";

    const stateAction = document.createElement("button");
    stateAction.type = "button";
    const action = commentStateAction(comment.status);
    stateAction.textContent = action === "reopen" ? "Reopen" : "Resolve";
    stateAction.dataset.commentAction = action;
    stateAction.dataset.actionCommentId = comment.id;
    stateAction.dataset.actionLocation = location;
    stateAction.dataset.focusKey = `${location}:${comment.id}:state`;
    stateAction.disabled = state.reviewMutationPending;
    actions.append(reply, stateAction);
    return actions;
  }

  function makeReviewThread(comment) {
    const item = document.createElement("li");
    const thread = document.createElement("article");
    thread.className = "review-thread";
    thread.dataset.commentId = comment.id;

    const select = document.createElement("button");
    select.className = "review-thread-select";
    select.type = "button";
    select.dataset.selectCommentId = comment.id;
    select.dataset.focusKey = `panel:${comment.id}:select`;

    const header = document.createElement("span");
    header.className = "review-thread-header";
    const title = document.createElement("strong");
    title.textContent = commentLocationText(comment);
    const status = document.createElement("span");
    status.className = "review-thread-status";
    status.textContent = commentStatusLabel(comment.status);
    header.append(title, status);

    const body = document.createElement("span");
    body.className = "review-comment-body";
    body.textContent = comment.body;
    select.append(header);
    const quote = makeSelectedQuote(comment);
    if (quote) select.append(quote);
    select.append(body);
    if (comment.replies.length) select.append(makeCommentReplies(comment));
    if (comment.outdated || comment.rangeUnavailable) {
      const unavailable = document.createElement("span");
      unavailable.className = "review-outdated";
      unavailable.textContent = comment.outdated
        ? "Original section no longer exists"
        : "Selected text mark is not available";
      select.append(unavailable);
    }
    const host = document.createElement("div");
    host.className = "comment-composer-host";
    thread.classList.toggle("is-active", comment.id === state.activeCommentID);
    thread.append(select, makeCommentActions(comment, "panel"), host);
    item.append(thread);
    return item;
  }

  function currentFocusKey() {
    return document.activeElement?.dataset?.focusKey || "";
  }

  function focusByKey(key) {
    if (!key) return false;
    const target = [...document.querySelectorAll("[data-focus-key]")].find((node) => node.dataset.focusKey === key);
    if (!target) return false;
    target.focus({ preventScroll: true });
    return document.activeElement === target;
  }

  function renderReviewInterface(options = {}) {
    const focusKey = options.preserveFocus ? currentFocusKey() : "";
    const pageScroll = options.preserveFocus ? window.scrollY : null;
    const panelScroll = options.preserveFocus ? elements.reviewPanelScroll.scrollTop : null;
    detachCommentComposer();
    if (state.reviewComposer?.baseHash && state.reviewComposer.baseHash !== state.reviewSourceHash) {
      state.reviewComposer = null;
      elements.reviewLiveStatus.textContent = "Comment closed because the document changed";
    }
    updateReviewButton();
    elements.reviewLoadState.textContent = state.reviewLoading ? "Loading review" : "";
    elements.reviewPanelScroll.setAttribute("aria-busy", String(state.reviewLoading));
    renderReviewError();

    const count = state.reviewComments.length;
    elements.reviewCountSummary.textContent = `${count} ${count === 1 ? "comment" : "comments"}`;
    elements.reviewComments.replaceChildren(...state.reviewComments.map(makeReviewThread));
    if (!state.reviewComments.length && !state.reviewLoading) {
      const empty = document.createElement("li");
      empty.className = "review-empty";
      empty.textContent = "No comments.";
      elements.reviewComments.append(empty);
    }

    updateBlockCommentControls();
    renderCommentComposer();
    if (!state.reviewComposer) scheduleSelectionCommentAction();
    if (options.preserveFocus) {
      if (focusKey) focusByKey(focusKey);
      elements.reviewPanelScroll.scrollTop = panelScroll;
      window.scrollTo({ top: pageScroll, behavior: "auto" });
    }
  }

  function syncBlockReviewVisibility() {
    const visible = elements.document.querySelector(
      ".md-block-review-controls.has-comments, .md-block-review-controls.has-composer",
    );
    document.body.classList.toggle("review-comments-visible", Boolean(visible));
  }

  function detachCommentComposer() {
    const block = elements.commentComposer.closest(".md-block");
    const controls = elements.commentComposer.closest(".md-block-review-controls");
    controls?.classList.remove("has-composer");
    if (elements.commentComposer.parentElement !== document.body) document.body.append(elements.commentComposer);
    syncBlockReviewVisibility();
    if (block && controls) {
      block.style.minHeight = sideComments(controls) && controls.classList.contains("has-comments")
        ? `${controls.scrollHeight}px`
        : "";
    }
  }

  function commentComposerHost(composer) {
    if (composer.kind === "comment") {
      return state.reviewBlocks.get(composer.blockKey)?.element.querySelector(".md-block-comment-bubbles") || null;
    }
    const root = composer.location === "panel" ? elements.reviewComments : elements.document;
    const threads = root.querySelectorAll(composer.location === "panel" ? ".review-thread" : ".md-comment-bubble");
    const thread = [...threads].find((candidate) => candidate.dataset.commentId === composer.commentID);
    return thread?.querySelector(".comment-composer-host") || null;
  }

  function mountCommentComposer(composer) {
    const host = commentComposerHost(composer);
    if (!host) {
      detachCommentComposer();
      return;
    }
    host.append(elements.commentComposer);
    const block = host.closest(".md-block");
    const controls = block?.querySelector(".md-block-review-controls");
    controls?.classList.add("has-composer");
    syncBlockReviewVisibility();
    if (block && controls && sideComments(controls)) block.style.minHeight = `${controls.scrollHeight}px`;
  }

  function renderCommentComposer() {
    const composer = state.reviewComposer;
    elements.commentComposer.hidden = !composer;
    if (!composer) {
      detachCommentComposer();
      return;
    }
    const block = composer.blockKey ? state.reviewBlocks.get(composer.blockKey) : null;
    const replying = composer.kind === "reply";
    elements.commentComposerTitle.textContent = replying ? "Reply" : "Add comment";
    elements.commentTarget.textContent = replying
      ? "Add a reply to this comment."
      : (composer.selection ? composer.targetLabel : (block ? `Comment on ${lineRangeLabel(block.startLine, block.endLine)}` : composer.targetLabel));
    const selectedQuote = composer.selection ? textSelection.shortQuote(composer.selection.quote) : "";
    elements.commentSelectedQuote.hidden = !selectedQuote;
    elements.commentSelectedQuote.textContent = selectedQuote ? `“${selectedQuote}”` : "";
    elements.commentSave.textContent = replying ? "Save reply" : "Save comment";
    elements.commentBody.value = composer.body;
    const detail = state.reviewError || state.reviewBlockError;
    const blocked = Boolean(detail);
    elements.commentError.textContent = detail;
    elements.commentError.hidden = !detail;
    const error = reviewTextError(composer.body);
    const controls = commentComposerControlState({
      blocked,
      pending: state.reviewMutationPending,
      invalid: Boolean(error),
    });
    elements.commentComposer.setAttribute("aria-busy", String(state.reviewMutationPending));
    elements.commentBody.readOnly = controls.bodyReadOnly;
    elements.commentBody.disabled = controls.bodyDisabled;
    const action = replying ? "reply" : "comment";
    elements.commentBodyHelp.textContent = state.reviewMutationPending ? `Saving ${action}` : (error || "16 KiB maximum.");
    elements.commentSave.disabled = controls.saveDisabled;
    elements.commentCancel.disabled = controls.actionsDisabled;
    mountCommentComposer(composer);
    growCommentBody();
  }

  function commentBlockKey(comment) {
    if (!comment || comment.outdated) return "";
    return comment.currentBlockKey || comment.anchor?.blockKey || "";
  }

  function syncActiveComment() {
    for (const [key, block] of state.reviewBlocks) {
      block.element.classList.toggle("is-review-active", key === state.activeBlockKey);
    }
    for (const button of document.querySelectorAll("[data-comment-id]")) {
      button.classList.toggle("is-active", button.dataset.commentId === state.activeCommentID);
    }
    refreshTextHighlights();
  }

  function activateComment(commentID, scroll = false) {
    const comment = state.reviewComments.find((candidate) => candidate.id === commentID);
    if (!comment) return;
    state.activeCommentID = comment.id;
    state.activeBlockKey = commentBlockKey(comment);
    syncActiveComment();
    if (comment.textRange && (comment.rangeUnavailable || !state.highlightAvailable)) {
      elements.reviewLiveStatus.textContent = comment.rangeUnavailable
        ? "The selected text is not available in this rendering."
        : "Inline text marks are not available in this browser.";
    }
    const viewportTop = elements.topbar.getBoundingClientRect().bottom;
    const outside = comment.ranges?.length
      ? textSelection.rangeOutsideViewport(comment.ranges, viewportTop, window.innerHeight)
      : true;
    if (scroll && state.activeBlockKey && outside) {
      const behavior = window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth";
      const delta = comment.textRange && comment.ranges?.length
        ? textSelection.rangeScrollDelta(comment.ranges, viewportTop, window.innerHeight)
        : 0;
      if (delta) window.scrollBy({ top: delta, behavior });
      else state.reviewBlocks.get(state.activeBlockKey)?.element.scrollIntoView({ block: "center", behavior });
    }
  }

  function activateTextCommentAtPoint(event) {
    if (
      !state.highlightAvailable
      || state.reviewMutationPending
      || state.reviewComposer
      || event.metaKey
      || event.ctrlKey
      || event.altKey
      || event.shiftKey
    ) return false;
    const selection = window.getSelection();
    if (selection?.rangeCount && !selection.isCollapsed) return false;
    const commentID = textSelection.textCommentIDAtPoint(
      state.reviewComments,
      event.clientX,
      event.clientY,
      state.activeCommentID,
    );
    if (!commentID) return false;
    event.preventDefault();
    event.stopPropagation();
    activateComment(commentID);
    return true;
  }

  function makeBlockCommentBubble(comment) {
    const bubble = document.createElement("article");
    bubble.className = "md-comment-bubble";
    bubble.dataset.commentId = comment.id;
    const select = document.createElement("button");
    select.className = "md-comment-bubble-select";
    select.type = "button";
    select.dataset.selectCommentId = comment.id;
    select.dataset.focusKey = `block:${comment.id}:select`;
    select.setAttribute("aria-label", `${commentStatusLabel(comment.status)} comment: ${comment.body}`);
    const body = document.createElement("span");
    body.textContent = comment.body;
    const quote = makeSelectedQuote(comment);
    if (quote) select.append(quote);
    select.append(body);
    if (comment.replies.length) select.append(makeCommentReplies(comment, true));
    const host = document.createElement("div");
    host.className = "comment-composer-host";
    bubble.append(select, makeCommentActions(comment, "block"), host);
    return bubble;
  }

  function updateBlockCommentControls() {
    const remountComposer = Boolean(elements.commentComposer.closest(".md-block-comment-bubbles"));
    if (remountComposer) detachCommentComposer();
    const counts = commentCountsByBlockKey(state.reviewComments);
    const canAdd = canAddComment();
    for (const [key, block] of state.reviewBlocks) {
      const controls = block.element.querySelector(".md-block-review-controls");
      const button = block.element.querySelector(".md-block-comment");
      const marker = block.element.querySelector(".md-block-comment-count");
      const bubbles = block.element.querySelector(".md-block-comment-bubbles");
      if (!controls || !button || !marker || !bubbles) continue;
      button.disabled = !state.reviewEnabled || !canAdd;
      if (button.disabled) block.element.classList.remove("is-comment-armed");
      const comments = state.reviewComments.filter((comment) => commentBlockKey(comment) === key);
      const count = counts[key];
      controls.classList.toggle("has-comments", comments.length > 0);
      marker.hidden = !count;
      marker.textContent = count ? String(count.total) : "";
      marker.dataset.commentId = comments[0]?.id || "";
      if (count) {
        marker.setAttribute(
          "aria-label",
          `Show ${count.total} ${count.total === 1 ? "comment" : "comments"} on this section`,
        );
      } else {
        marker.removeAttribute("aria-label");
      }
      bubbles.replaceChildren(...comments.map(makeBlockCommentBubble));
      block.element.style.minHeight = sideComments(controls) && comments.length
        ? `${controls.scrollHeight}px`
        : "";
    }
    syncBlockReviewVisibility();
    updateOutline();
    if (state.activeCommentID && !state.reviewComments.some((comment) => comment.id === state.activeCommentID)) {
      state.activeCommentID = "";
      state.activeBlockKey = "";
    }
    syncActiveComment();
    if (remountComposer && state.reviewComposer) mountCommentComposer(state.reviewComposer);
  }

  function applyReviewView(view) {
    state.reviewRevision = view.revision;
    state.reviewStatus = view.reviewStatus;
    state.reviewSourceHash = view.sourceHash;
    resolveCommentTextRanges(view.comments);
    state.reviewComments = view.comments;
    state.reviewError = "";
    state.reviewErrorNeedsRender = false;
  }

  async function loadReview(options = {}) {
    if (!state.reviewEnabled || !reviewAvailable()) return;
    const token = ++state.reviewLoadToken;
    const path = state.currentPath;
    state.reviewLoading = true;
    state.reviewError = "";
    state.reviewErrorNeedsRender = false;
    renderReviewInterface({ preserveFocus: options.automatic === true });
    try {
      const payload = await fetchJSON(`/api/review?path=${encodeURIComponent(path)}&includeResolved=true`);
      if (token !== state.reviewLoadToken || path !== state.currentPath) return;
      const view = validateReviewView(payload, path);
      if (state.reviewSourceHash && view.sourceHash !== state.reviewSourceHash) {
        throw new Error("The document changed. Reload it before you add a comment.");
      }
      applyReviewView(view);
    } catch (error) {
      if (token !== state.reviewLoadToken || path !== state.currentPath) return;
      if (error instanceof APIError && error.code === "document_removed") {
        await refreshFileList().catch(() => {});
        showRemovedDocument(path);
        return;
      }
      state.reviewError = error instanceof TypeError
        ? "MDShelf could not reach the local server."
        : error.message;
      state.reviewErrorNeedsRender = state.reviewError === "The document changed. Reload it before you add a comment.";
    } finally {
      if (token === state.reviewLoadToken && path === state.currentPath) {
        state.reviewLoading = false;
        renderReviewInterface({ preserveFocus: options.automatic === true });
      }
    }
  }

  function syncReviewPanelMode() {
    const overlay = state.reviewPanelOpen && !reviewWide.matches;
    elements.reviewPanel.setAttribute("role", overlay ? "dialog" : "complementary");
    if (overlay) elements.reviewPanel.setAttribute("aria-modal", "true");
    else elements.reviewPanel.removeAttribute("aria-modal");
    elements.reviewBackdrop.hidden = !overlay;
    document.body.classList.toggle("review-open", state.reviewPanelOpen);
    document.body.classList.toggle("review-overlay", overlay);
    elements.skipLink.inert = overlay;
    elements.topbar.inert = overlay;
    elements.reader.inert = overlay;
    if (overlay) elements.drawer.inert = true;
    else if (sidebarPinned()) elements.drawer.inert = false;
  }

  function setReviewPanel(open, restoreFocus = true) {
    if (open && (!state.reviewEnabled || !reviewAvailable())) return;
    if (!open && state.reviewComposer?.location === "panel") {
      state.reviewComposer = null;
      renderCommentComposer();
    }
    state.reviewPanelOpen = open;
    elements.reviewPanel.hidden = !open;
    elements.reviewButton.setAttribute("aria-expanded", String(open));
    if (open) {
      setDrawer(false, false);
      setSettingsPopup(false, false);
    }
    syncReviewPanelMode();
    updateBlockCommentControls();
    if (open) {
      window.requestAnimationFrame(() => elements.reviewPanelTitle.focus());
    } else if (restoreFocus && !elements.reviewButton.hidden) {
      elements.reviewButton.focus();
    }
  }

  function openCommentComposer(blockKey = "", returnFocusKey = `block:${blockKey}:comment`) {
    const block = state.reviewBlocks.get(blockKey);
    if (!block || !canAddComment()) return;
    state.reviewComposer = {
      kind: "comment",
      blockKey,
      body: "",
      baseHash: state.reviewSourceHash,
      targetLabel: `Comment on ${lineRangeLabel(block.startLine, block.endLine)}`,
      returnFocusKey,
    };
    renderCommentComposer();
    window.requestAnimationFrame(() => elements.commentBody.focus());
  }

  function openSelectionCommentComposer(descriptor, returnFocusKey = "reader") {
    if (
      !descriptor
      || descriptor.generation !== state.reviewRenderGeneration
      || !descriptor.blockKeys.length
      || !state.reviewBlocks.has(descriptor.blockKeys[0])
      || !canAddComment()
    ) return false;
    const blockKey = descriptor.blockKeys[0];
    const block = state.reviewBlocks.get(blockKey);
    state.reviewComposer = {
      kind: "comment",
      blockKey,
      body: "",
      baseHash: state.reviewSourceHash,
      targetLabel: `Comment on selected text in ${lineRangeLabel(block.startLine, block.endLine)}`,
      returnFocusKey,
      selection: textSelection.requestSelection(descriptor),
      savedRange: descriptor.nativeRange?.cloneRange?.() || null,
      generation: descriptor.generation,
    };
    hideSelectionCommentAction();
    renderCommentComposer();
    window.requestAnimationFrame(() => elements.commentBody.focus());
    return true;
  }

  function commentOnActiveNavigationBlock() {
    const block = activeNavigationBlock();
    if (!block) return false;
    setActiveNavigationBlock(block);
    const key = block.dataset.mdBlock || "";
    if (!state.reviewEnabled || !state.reviewBlocks.has(key) || !canAddComment()) {
      elements.routeStatus.textContent = "Comments are not available for this block.";
      return true;
    }
    openCommentComposer(key, "reader");
    return true;
  }

  function openReplyComposer(commentID, location, returnFocusKey) {
    const comment = state.reviewComments.find((candidate) => candidate.id === commentID);
    if (!comment || comment.status === "resolved" || !canAddComment()) return;
    state.activeCommentID = commentID;
    state.activeBlockKey = commentBlockKey(comment);
    state.reviewComposer = {
      kind: "reply",
      commentID,
      location,
      body: "",
      baseHash: state.reviewSourceHash,
      returnFocusKey,
    };
    syncActiveComment();
    renderCommentComposer();
    window.requestAnimationFrame(() => elements.commentBody.focus());
  }

  function composerReturnKey(composer) {
    return composer?.returnFocusKey || "";
  }

  function restoreComposerSelection(composer) {
    if (!composer?.savedRange || composer.generation !== state.reviewRenderGeneration) return;
    try {
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(composer.savedRange);
      state.liveSelection = textSelection.captureSelection(selection, state.reviewTextIndex, elements.document);
    } catch {
      state.liveSelection = null;
    }
  }

  function closeCommentComposer(restoreFocus = true) {
    const composer = state.reviewComposer;
    state.reviewComposer = null;
    renderCommentComposer();
    updateBlockCommentControls();
    if (!restoreFocus) return;
    if (composerReturnKey(composer) === "reader") {
      elements.reader.focus({ preventScroll: true });
      restoreComposerSelection(composer);
      scheduleSelectionCommentAction();
      return;
    }
    if (focusByKey(composerReturnKey(composer))) return;
    if (state.reviewPanelOpen) elements.reviewPanelTitle.focus();
    else if (!elements.reviewButton.hidden) elements.reviewButton.focus();
  }

  function reviewMutationBody(extra = {}) {
    return {
      path: state.currentPath,
      expectedRevision: state.reviewRevision,
      expectedSourceHash: state.reviewSourceHash,
      ...extra,
    };
  }

  function reviewPOST(body) {
    return {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    };
  }

  async function mutateReview(endpoint, body, options = {}) {
    if (state.reviewMutationPending || !state.reviewEnabled) return false;
    const mutation = { token: ++state.reviewMutationToken, path: state.currentPath };
    const isCurrent = () => reviewMutationIsCurrent(mutation, {
      token: state.reviewMutationToken,
      path: state.currentPath,
      enabled: state.reviewEnabled,
    });
    state.reviewMutationPending = true;
    state.reviewError = "";
    renderReviewInterface();
    try {
      const response = await fetchJSON(endpoint, reviewPOST(body));
      if (!isCurrent()) return false;
      options.beforeRefresh?.(response);
      await refreshFileList();
      if (!isCurrent()) return false;
      await loadReview({ automatic: false });
      if (!isCurrent()) return false;
      if (options.message) elements.reviewLiveStatus.textContent = options.message;
      if (options.focusKey) {
        window.requestAnimationFrame(() => {
          if (!isCurrent()) return;
          if (!focusByKey(options.focusKey)) elements.reviewPanelTitle.focus();
        });
      }
      return true;
    } catch (error) {
      if (!isCurrent()) return false;
      if (error instanceof APIError && error.code === "document_removed") {
        await refreshFileList().catch(() => {});
        if (isCurrent()) showRemovedDocument(mutation.path);
        return false;
      }
      if (error instanceof APIError && error.code === "stale_review") {
        await loadReview({ automatic: true });
        if (!isCurrent()) return false;
        state.reviewError = "The comments changed. Check the latest comments, then try again.";
      } else if (error instanceof APIError && error.code === "stale_document") {
        const route = readRoute();
        await loadDocument(mutation.path, route.fragment, { force: true, live: true });
        if (!isCurrent()) return false;
        state.reviewError = error.message;
      } else if (error instanceof APIError && error.code === "invalid_transition") {
        await loadReview({ automatic: true });
        if (!isCurrent()) return false;
        state.reviewError = error.message;
      } else {
        state.reviewError = error instanceof TypeError
          ? "MDShelf could not reach the local server."
          : error.message;
      }
      return false;
    } finally {
      if (isCurrent()) {
        state.reviewMutationPending = false;
        renderReviewInterface();
      }
    }
  }

  async function saveComment() {
    const composer = state.reviewComposer;
    if (!composer || composer.baseHash !== state.reviewSourceHash) return;
    const body = composer.body;
    const error = reviewTextError(body);
    if (error) {
      elements.commentBodyHelp.textContent = error;
      elements.commentBody.focus();
      return;
    }
    const returnKey = composerReturnKey(composer);
    const replying = composer.kind === "reply";
    const endpoint = replying ? "/api/control/review/comments/reply" : "/api/control/review/comments/add";
    const extra = replying
      ? { body, commentId: composer.commentID }
      : (composer.selection ? { body, selection: composer.selection } : { body, blockKey: composer.blockKey });
    await mutateReview(endpoint, reviewMutationBody(extra), {
      message: replying ? "Reply published" : "Comment published",
      focusKey: returnKey,
      beforeRefresh(response) {
        state.reviewComposer = null;
        state.activeCommentID = response.comment?.id || composer.commentID || "";
        state.activeBlockKey = replying
          ? commentBlockKey(state.reviewComments.find((comment) => comment.id === composer.commentID))
          : composer.blockKey;
      },
    });
  }

  async function changeCommentState(commentID, action, focusKey) {
    if (!commentID || !["resolve", "reopen"].includes(action)) return;
    if (state.reviewComposer) closeCommentComposer(false);
    state.activeCommentID = commentID;
    const comment = state.reviewComments.find((candidate) => candidate.id === commentID);
    state.activeBlockKey = commentBlockKey(comment);
    await mutateReview(`/api/control/review/comments/${action}`, reviewMutationBody({ commentId: commentID }), {
      message: action === "resolve" ? "Comment resolved" : "Comment reopened",
      focusKey,
    });
  }

  function handleCommentAction(button) {
    const commentID = button?.dataset.actionCommentId || "";
    const action = button?.dataset.commentAction || "";
    const location = button?.dataset.actionLocation || "";
    if (!commentID || !action || !location) return false;
    activateComment(commentID);
    if (action === "reply") {
      openReplyComposer(commentID, location, button.dataset.focusKey || "");
    } else {
      void changeCommentState(commentID, action, button.dataset.focusKey || "");
    }
    return true;
  }

  function trapFocus(container, event) {
    const focusable = [...container.querySelectorAll(
      "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex='-1'])",
    )].filter((element) => !element.closest("[hidden]") && element.getClientRects().length);
    if (!focusable.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function isCurrentLoad(controller) {
    return state.abortController === controller && !controller.signal.aborted;
  }

  function cancelDocumentLoad() {
    state.abortController?.abort();
    state.abortController = null;
  }

  function scrollToRouteFragment(fragment, focus = false) {
    if (!fragment) {
      window.scrollTo({ top: 0, behavior: "auto" });
      if (focus) elements.reader.focus({ preventScroll: true });
      return;
    }

    const target = document.getElementById(fragment) || document.getElementById(safeDecode(fragment));
    if (!target || !elements.document.contains(target)) return;
    target.scrollIntoView();
    if (focus) {
      const hadTabIndex = target.hasAttribute("tabindex");
      if (!hadTabIndex) target.tabIndex = -1;
      target.focus({ preventScroll: true });
      if (!hadTabIndex) target.addEventListener("blur", () => target.removeAttribute("tabindex"), { once: true });
    }
  }

  function finishNavigation(fragment, title, isCurrent = () => true) {
    window.requestAnimationFrame(() => {
      if (!isCurrent()) return;
      window.requestAnimationFrame(() => {
        if (!isCurrent()) return;
        scrollToRouteFragment(fragment, state.focusAfterNavigation);
        state.focusAfterNavigation = false;
        trackActiveNavigationBlock();
      });
    });
    if (isCurrent()) elements.routeStatus.textContent = `Loaded ${title}`;
  }

  async function loadDocument(path, fragment = "", options = {}) {
    const live = options.live === true;
    if (!options.force && path === state.currentPath && !elements.document.hidden) {
      finishNavigation(fragment, elements.currentFile.textContent || titleFromPath(path));
      return;
    }

    if (path !== state.currentPath) {
      clearReviewState(true);
      window.clearTimeout(state.sectionTrackingTimer);
      state.sectionTrackingLocked = false;
      state.activeNavigationBlockKey = "";
    }
    state.abortController?.abort();
    const controller = new AbortController();
    state.abortController = controller;
    state.currentPath = path;
    elements.currentFile.textContent = displayName(path);
    updateActiveFile();
    if (!live) {
      setDocumentPath("");
      showLoading();
    }

    try {
      const payload = await fetchJSON(`/api/render?path=${encodeURIComponent(path)}`, { signal: controller.signal });
      if (!isCurrentLoad(controller)) return;
      if (
        typeof payload?.html !== "string"
        || (payload.absolutePath !== undefined && typeof payload.absolutePath !== "string")
      ) throw new Error("The server returned an invalid document.");

      const renderedPath = normalizePath(typeof payload.path === "string" ? payload.path : path) || path;
      const title = typeof payload.title === "string" && payload.title.trim()
        ? payload.title.trim()
        : titleFromPath(renderedPath);
      const reviewEnabled = reviewPanelAvailable({
        daemonMode: state.daemonMode,
        currentPath: renderedPath,
        removed: state.removed.get(renderedPath) === true,
      });
      if (reviewEnabled && (!sourceHashPattern.test(payload.sourceHash) || !Array.isArray(payload.blocks))) {
        throw new Error("The server returned invalid review metadata.");
      }
      const previousScroll = window.scrollY;
      const template = document.createElement("template");
      template.innerHTML = payload.html;
      const renderedBlocks = [...template.content.children];
      const signatures = blockSignatures(template.content);
      const changedIndexes = live ? changedBlockIndexes(state.highlightBaseline, signatures) : new Set();
      const changedBlocks = renderedBlocks.filter((_, index) => changedIndexes.has(index));
      prepareDocument(template.content, renderedPath);

      let reviewBlocks = new Map();
      let blockError = "";
      if (reviewEnabled) {
        try {
          reviewBlocks = buildReviewBlockMap(template.content, payload.blocks);
          appendReviewBlockControls(reviewBlocks);
        } catch (error) {
          blockError = error.message;
        }
      }

      renderMath(template.content);
      await renderMermaid(template.content, () => isCurrentLoad(controller));
      if (!isCurrentLoad(controller)) return;
      const reviewGeneration = ++state.reviewRenderGeneration;
      let reviewTextIndex = null;
      if (reviewEnabled && !blockError && textSelection) {
        try {
          reviewTextIndex = textSelection.buildIndex(
            [...reviewBlocks].map(([key, block]) => ({ key, element: block.element })),
            reviewGeneration,
          );
        } catch (error) {
          blockError = error.message;
        }
      }
      state.currentPath = renderedPath;
      state.reviewEnabled = reviewEnabled;
      state.reviewSourceHash = reviewEnabled ? payload.sourceHash : "";
      state.reviewBlocks = reviewBlocks;
      clearTextSelectionState();
      state.reviewTextIndex = reviewTextIndex;
      elements.document.replaceChildren(template.content);
      elements.document.setAttribute("aria-label", title);
      elements.currentFile.textContent = displayName(renderedPath);
      setDocumentPath(payload.absolutePath || "");
      document.title = `${title} | MDShelf`;
      updateActiveFile();
      showDocument();
      updateOutline();
      updateReviewButton();
      syncActiveNavigationBlock();

      if (reviewEnabled) {
        state.reviewBlockError = blockError;
        await loadReview({ automatic: live });
      } else {
        clearReviewState(true);
      }
      if (!isCurrentLoad(controller)) return;

      if (live) {
        window.requestAnimationFrame(() => {
          if (isCurrentLoad(controller)) window.scrollTo({ top: previousScroll, behavior: "auto" });
        });
        elements.routeStatus.textContent = `Updated ${title}`;
        queueUpdate(`${fileName(renderedPath)} updated`, changedBlocks, signatures);
      } else {
        window.clearTimeout(state.highlightTimer);
        state.highlightBaseline = signatures;
        state.pendingUpdate = null;
        finishNavigation(fragment, title, () => isCurrentLoad(controller));
      }
    } catch (error) {
      if (error.name === "AbortError" || !isCurrentLoad(controller)) return;
      const message = error instanceof TypeError
        ? "MDShelf could not reach the local server."
        : error.message;
      clearReviewState(true);
      setDocumentPath("");
      showMessage("Could not open document", message, () => loadDocument(path, fragment));
      elements.routeStatus.textContent = `Could not load ${fileName(path)}`;
    }
  }

  function handleRoute() {
    const route = readRoute();
    if (route.path === demoDocumentPath) return loadDocument(demoDocumentPath, route.fragment);
    if (!state.files.length) return;
    if (!route.path) {
      const path = defaultDocument();
      if (!path && state.daemonMode && state.files.length) {
        window.history.replaceState(null, "", buildRoute(state.files[0]));
        showRemovedDocument(state.files[0]);
        return;
      }
      window.history.replaceState(null, "", buildRoute(path));
      return loadDocument(path);
    }

    if (!state.fileSet.has(route.path)) {
      cancelDocumentLoad();
      clearReviewState(true);
      state.currentPath = "";
      updateActiveFile();
      setDocumentPath("");
      elements.currentFile.textContent = "Document not found";
      document.title = "Document not found | MDShelf";
      elements.routeStatus.textContent = "Document not found";
      showMessage(
        "Document not found",
        "This file is not in the current folder.",
        () => {
          window.location.hash = buildRoute(defaultDocument());
        },
      );
      return;
    }

    if (!isDocumentAvailable(route.path)) {
      showRemovedDocument(route.path);
      return;
    }

    return loadDocument(route.path, route.fragment);
  }

  function showRemovedDocument(path) {
    cancelDocumentLoad();
    clearReviewState(true);
    state.currentPath = path;
    state.highlightBaseline = [];
    state.pendingUpdate = null;
    updateActiveFile();
    setDocumentPath("");
    elements.currentFile.textContent = "Document removed";
    document.title = "Document removed | MDShelf";
    elements.routeStatus.textContent = `${displayName(path)} was removed`;
    const fallback = defaultDocument(path);
    showMessage(
      "Document removed",
      "This Markdown file no longer exists.",
      fallback ? () => { window.location.hash = buildRoute(fallback); } : null,
    );
    queueUpdate(`${displayName(path)} removed`);
  }

  function shouldReloadDocument(path, reset, change) {
    return path !== demoDocumentPath && Boolean(path && (reset || change));
  }

  async function applyChanges(payload) {
    if (!Number.isSafeInteger(payload?.revision) || payload.revision < 0 || !Array.isArray(payload?.changes)) {
      throw new Error("The server returned an invalid change list.");
    }
    const changes = payload.changes.filter((change) => (
      change
      && typeof change.path === "string"
      && ["added", "removed", "updated", "review"].includes(change.kind)
    )).map((change) => ({ ...change, path: normalizePath(change.path) }));
    const normalized = { ...payload, changes };
    const currentPath = state.currentPath;
    const plan = planLiveChanges(normalized, currentPath, state.daemonMode);

    if (plan.refreshFiles) await refreshFileList();
    if (plan.currentRemoved || (currentPath && currentPath !== demoDocumentPath && !isDocumentAvailable(currentPath))) {
      showRemovedDocument(currentPath);
      return;
    }
    if (plan.renderCurrent) {
      const route = readRoute();
      await loadDocument(currentPath, route.fragment, { force: true, live: true });
      return;
    }
    if (plan.refreshCurrentReview) {
      await loadReview({ automatic: true });
      return;
    }

    if (!currentPath && state.files.length) {
      const nextPath = readRoute().path;
      const path = isDocumentAvailable(nextPath) ? nextPath : defaultDocument();
      window.history.replaceState(null, "", buildRoute(path));
      await loadDocument(path, "", { force: true, live: true });
      return;
    }

    const latest = changes[changes.length - 1];
    if (latest) {
      const action = latest.kind === "review" ? "review updated" : latest.kind;
      queueUpdate(`${displayName(latest.path)} ${action}`);
    }
  }

  function wait(duration) {
    return new Promise((resolve) => window.setTimeout(resolve, duration));
  }

  async function watchChanges() {
    let revision = 0;
    let retryDelay = 500;
    while (true) {
      try {
        const payload = await fetchJSON(`/api/watch?since=${revision}`);
        await applyChanges(payload);
        revision = payload.revision;
        retryDelay = 500;
      } catch {
        await wait(retryDelay);
        retryDelay = Math.min(retryDelay * 2, 8000);
      }
    }
  }

  function designTokens() {
    if (typeof window.getComputedStyle !== "function") return null;
    const style = window.getComputedStyle(document.documentElement);
    const read = (name) => (style.getPropertyValue(name) || "").trim();
    const surface = read("--surface");
    const text = read("--text");
    return surface && text ? { style, read, surface, text } : null;
  }

  /* Diagrams take their colours from the design, so they stop looking pasted in. */
  function mermaidThemeVariables() {
    const tokens = designTokens();
    if (!tokens) return null;
    const { read, surface, text } = tokens;
    const line = read("--line-strong") || read("--line");
    const soft = read("--surface-soft") || surface;
    const accent = read("--accent");
    const accentSoft = read("--accent-soft") || soft;
    return {
      background: read("--page") || surface,
      fontFamily: read("--sans") || "sans-serif",
      fontSize: "14px",
      primaryColor: soft,
      primaryTextColor: text,
      primaryBorderColor: line,
      secondaryColor: accentSoft,
      tertiaryColor: surface,
      lineColor: line,
      textColor: text,
      mainBkg: soft,
      nodeBorder: line,
      nodeTextColor: text,
      clusterBkg: surface,
      clusterBorder: line,
      titleColor: text,
      edgeLabelBackground: surface,
      labelBoxBkgColor: soft,
      labelBoxBorderColor: line,
      labelTextColor: text,
      actorBkg: soft,
      actorBorder: line,
      actorTextColor: text,
      signalColor: line,
      signalTextColor: text,
      noteBkgColor: accentSoft,
      noteBorderColor: accent || line,
      noteTextColor: text,
      altBackground: surface,
    };
  }

  function initializeMermaid() {
    const variables = mermaidThemeVariables();
    window.mermaid?.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: variables ? "base" : (isDarkColorTheme() ? "dark" : "default"),
      ...(variables ? { themeVariables: variables } : {}),
    });
  }

  function handleColorSchemeChange() {
    if (state.appearance !== "system") return;
    applyThemePreferences();
    refreshDocumentTheme();
  }

  async function initialize() {
    initializeMermaid();
    showLoading();
    elements.fileCount.textContent = "Loading documents";
    try {
      await refreshFileList();

      if (!state.files.length && readRoute().path !== demoDocumentPath) {
        setDocumentPath("");
        elements.currentFile.textContent = "No documents";
        document.title = "MDShelf";
        showMessage(
          "No Markdown files found",
          "Add a Markdown file to this folder, then reload the page.",
          () => window.location.reload(),
        );
        return;
      }

      await handleRoute();
    } catch (error) {
      const message = error instanceof TypeError
        ? "MDShelf could not reach the local server."
        : error.message;
      setDocumentPath("");
      elements.fileNav.replaceChildren();
      const empty = document.createElement("p");
      empty.className = "nav-empty";
      empty.textContent = "The document list could not be loaded.";
      elements.fileNav.append(empty);
      elements.fileCount.textContent = "Load failed";
      showMessage("Could not load documents", message, initialize);
    }
  }

  loadThemePreferences();
  applyThemePreferences();
  state.highlightAvailable = Boolean(textSelection?.supportsHighlights(window));

  if (window.__MDSHELF_TEST__) {
    window.__MDSHELF_TEST_API__ = {
      addCodeBlockTools,
      addHeadingPermalinks,
      buildRoute,
      codeBlockText,
      blockCommentTapAction,
      blockIndexAtViewport,
      navigationScrollDelta,
      commentComposerControlState,
      commentBlockKey,
      commentStateAction,
      commentComposerEscapeAction,
      commentCountsByBlockKey,
      commentSubmitShortcut,
      cancelDocumentLoad,
      appearanceElement: elements.appearance,
      designElement: elements.design,
      daemonRemoveRequest,
      documentPathElement: elements.documentPath,
      initializeMermaid,
      isCurrentLoad,
      isDocumentAvailable,
      listNavigationIndex,
      planLiveChanges,
      readingShortcutAction,
      renderMath,
      renderMermaid,
      reviewMutationIsCurrent,
      reviewPanelAvailable,
      reviewStatusLabel,
      rootElement: document.documentElement,
      selectionCommentActionDescriptor,
      selectionCommentLiveDescriptor,
      setAppearance,
      setDesign,
      setDocumentPath,
      setSyntaxTheme,
      shouldReloadDocument,
      validateReviewView,
      commentAddAvailable,
      syntaxThemeElement: elements.syntaxTheme,
      setAbortController(controller) { state.abortController = controller; },
    };
    return;
  }

  document.addEventListener("selectionchange", scheduleSelectionCommentAction);
  elements.selectionCommentAction.addEventListener("blur", scheduleSelectionCommentAction);
  elements.selectionCommentAction.addEventListener("pointerdown", (event) => {
    const descriptor = state.liveSelection || captureLiveSelection();
    state.latchedSelection = textSelection.latchDescriptor(descriptor);
    event.preventDefault();
  });
  elements.selectionCommentAction.addEventListener("click", () => {
    const descriptor = selectionCommentActionDescriptor(
      state.latchedSelection,
      state.liveSelection || captureLiveSelection(),
    );
    state.latchedSelection = null;
    if (!openSelectionCommentComposer(descriptor, "reader")) scheduleSelectionCommentAction();
  });
  window.visualViewport?.addEventListener("scroll", () => positionSelectionCommentAction(state.liveSelection));
  window.visualViewport?.addEventListener("resize", () => positionSelectionCommentAction(state.liveSelection));
  window.addEventListener("pagehide", clearTextSelectionState);

  elements.menuButton.addEventListener("click", () => {
    if (state.reviewMutationPending) return;
    if (state.reviewComposer) closeCommentComposer(false);
    if (state.reviewPanelOpen) setReviewPanel(false, false);
    setDrawer(true);
  });
  elements.shortcutButton.addEventListener("click", () => setShortcutDialog(!state.shortcutsOpen));
  elements.shortcutClose.addEventListener("click", () => setShortcutDialog(false));
  elements.shortcutBackdrop.addEventListener("click", () => setShortcutDialog(false));
  elements.closeButton.addEventListener("click", () => setDrawer(false));
  elements.backdrop.addEventListener("click", () => setDrawer(false));
  elements.settingsButton.addEventListener("click", () => {
    const open = elements.settingsPopup.hidden;
    if (open && state.reviewMutationPending) return;
    if (open && state.reviewComposer) closeCommentComposer(false);
    if (open && state.reviewPanelOpen) setReviewPanel(false, false);
    setSettingsPopup(open, false);
  });
  elements.design.addEventListener("change", () => {
    setDesign(elements.design.value);
    setDrawer(false, false);
    updateBlockCommentControls();
    updateOutline();
  });
  elements.appearance.addEventListener("change", () => setAppearance(elements.appearance.value));
  elements.syntaxTheme.addEventListener("change", () => setSyntaxTheme(elements.syntaxTheme.value));
  elements.commentBody.addEventListener("input", growCommentBody);
  elements.fileFilter.addEventListener("input", () => {
    state.filter = elements.fileFilter.value;
    renderFileTree();
  });
  elements.drawer.addEventListener("keydown", (event) => {
    if (event.isComposing || event.metaKey || event.ctrlKey || event.altKey) return;
    const inFilter = event.target === elements.fileFilter;
    const listItem = event.target.closest(".folder > summary, .file-link, .demo-link");
    const inList = Boolean(listItem);
    if (inList && event.key === "Enter") {
      event.preventDefault();
      listItem.click();
      return;
    }
    let action = "";
    if (event.key === "ArrowDown" || (inList && ["j", "J"].includes(event.key))) action = "next";
    else if (event.key === "ArrowUp" || (inList && ["k", "K"].includes(event.key))) action = "previous";
    else if (inList && event.key === "Home") action = "first";
    else if (inList && event.key === "End") action = "last";
    if ((!inFilter && !inList) || !action || !moveFileListFocus(action)) return;
    event.preventDefault();
  });

  elements.document.addEventListener("pointermove", scheduleTextCommentHover, { passive: true });
  elements.document.addEventListener("pointerleave", clearTextCommentHover);

  elements.document.addEventListener("click", (event) => {
    const block = event.target.closest(".md-block[data-md-block]");
    if (block) setActiveNavigationBlock(block);
    if (activateTextCommentAtPoint(event)) return;
    const copyButton = event.target.closest(".code-copy");
    if (copyButton) {
      void copyCodeBlock(copyButton);
      return;
    }
    const commentAction = event.target.closest("[data-comment-action]");
    if (handleCommentAction(commentAction)) return;
    const existingComment = event.target.closest("[data-select-comment-id], .md-block-comment-count");
    const commentID = existingComment?.dataset.selectCommentId || existingComment?.dataset.commentId;
    if (commentID) {
      activateComment(commentID);
      return;
    }
    const commentButton = event.target.closest(".md-block-comment");
    const interactive = Boolean(event.target.closest("a, button, input, select, textarea, summary, label, [role='button'], [contenteditable='true']"));
    const action = blockCommentTapAction({
      touch: touchComments.matches || event.pointerType === "touch",
      commentButton: Boolean(commentButton),
      block: Boolean(block),
      interactive,
    });
    if (action === "open") {
      disarmBlockCommentControls();
      openCommentComposer(commentButton.dataset.blockKey || "");
    } else if (action === "arm") {
      armBlockCommentControl(block);
    }
  });

  elements.document.addEventListener("focusin", (event) => {
    const block = event.target.closest(".md-block[data-md-block]");
    if (block) setActiveNavigationBlock(block);
  });

  document.addEventListener("click", (event) => {
    const touch = touchComments.matches || event.pointerType === "touch";
    if (touch && !event.target.closest(".md-block")) disarmBlockCommentControls();
    if (
      state.activeCommentID
      && !event.target.closest(".md-comment-bubble, .md-block-comment-count, .review-thread")
    ) {
      state.activeCommentID = "";
      state.activeBlockKey = "";
      syncActiveComment();
    }
  });
  touchComments.addEventListener("change", () => disarmBlockCommentControls());

  elements.reviewButton.addEventListener("click", () => {
    if (state.reviewMutationPending) return;
    const open = !state.reviewPanelOpen;
    if (open && state.reviewComposer) closeCommentComposer(false);
    setReviewPanel(open);
  });
  elements.reviewClose.addEventListener("click", () => {
    if (!state.reviewMutationPending) setReviewPanel(false);
  });
  elements.reviewBackdrop.addEventListener("click", () => {
    if (!state.reviewMutationPending) setReviewPanel(false);
  });
  elements.commentBody.addEventListener("input", () => {
    if (!state.reviewComposer) return;
    state.reviewComposer.body = elements.commentBody.value;
    const error = reviewTextError(state.reviewComposer.body);
    elements.commentBodyHelp.textContent = error || "16 KiB maximum.";
    elements.commentSave.disabled = Boolean(state.reviewError || state.reviewBlockError)
      || state.reviewMutationPending
      || Boolean(error);
  });
  elements.commentBody.addEventListener("keydown", (event) => {
    if (!commentSubmitShortcut(event)) return;
    event.preventDefault();
    if (!elements.commentSave.disabled) void saveComment();
  });
  elements.commentComposer.addEventListener("submit", (event) => {
    event.preventDefault();
    void saveComment();
  });
  elements.commentCancel.addEventListener("click", () => closeCommentComposer());
  elements.reviewComments.addEventListener("click", (event) => {
    const commentAction = event.target.closest("[data-comment-action]");
    if (handleCommentAction(commentAction)) return;
    const button = event.target.closest("[data-select-comment-id]");
    if (!button) return;
    const commentID = button.dataset.selectCommentId;
    activateComment(commentID, true);
    if (!reviewWide.matches) {
      setReviewPanel(false, false);
      window.requestAnimationFrame(() => {
        if (!focusByKey(`block:${commentID}:select`) && !elements.reviewButton.hidden) elements.reviewButton.focus();
      });
    }
  });

  elements.demoLink.addEventListener("click", (event) => {
    state.focusAfterNavigation = true;
    if (!desktop.matches) setDrawer(false, false);
    if (elements.demoLink.hash === window.location.hash) {
      event.preventDefault();
      handleRoute();
    }
  });

  elements.fileNav.addEventListener("click", (event) => {
    const removeButton = event.target.closest(".file-remove");
    if (removeButton) {
      event.preventDefault();
      void removeDaemonDocument(removeButton.dataset.path, removeButton);
      return;
    }

    const link = event.target.closest(".file-link");
    if (!link) return;
    state.focusAfterNavigation = true;
    if (!desktop.matches) setDrawer(false, false);
    if (link.hash === window.location.hash) {
      event.preventDefault();
      handleRoute();
    }
  });

  elements.document.addEventListener("click", (event) => {
    const link = event.target.closest("a[data-document-route]");
    if (!link) return;
    state.focusAfterNavigation = true;
    if (link.hash === window.location.hash) {
      event.preventDefault();
      handleRoute();
    }
  });

  document.addEventListener("click", (event) => {
    if (!elements.settingsPopup.hidden && !elements.settings.contains(event.target)) {
      setSettingsPopup(false, false);
    }
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && state.shortcutsOpen) {
      setShortcutDialog(false);
      return;
    }
    if (event.key === "Escape" && state.reviewComposer) {
      if (commentComposerEscapeAction({ open: true, pending: state.reviewMutationPending }) === "close") {
        closeCommentComposer();
      }
      return;
    }
    if (event.key === "Escape" && !elements.settingsPopup.hidden) {
      setSettingsPopup(false);
      return;
    }
    if (event.key === "Escape" && state.reviewPanelOpen) {
      setReviewPanel(false);
      return;
    }
    if (event.key === "Escape" && document.body.classList.contains("drawer-open")) {
      setDrawer(false);
      return;
    }
    if (event.key !== "Tab") return;
    if (state.shortcutsOpen) {
      trapFocus(elements.shortcutDialog, event);
      return;
    }
    if (state.reviewPanelOpen && !reviewWide.matches) {
      trapFocus(elements.reviewPanel, event);
      return;
    }
    if (!sidebarPinned() && document.body.classList.contains("drawer-open")) trapFocus(elements.drawer, event);
  });

  document.addEventListener("keydown", (event) => {
    const action = readingShortcutAction(event);
    if (!action || shortcutTargetIsEditable(event.target)) return;
    if (event.repeat && ["comment", "documents", "comments", "shortcuts"].includes(action)) return;

    if (action === "shortcuts") {
      event.preventDefault();
      setShortcutDialog(!state.shortcutsOpen);
      return;
    }
    if (action === "documents") {
      event.preventDefault();
      focusDocumentFilter();
      return;
    }
    if (action === "comments") {
      event.preventDefault();
      toggleReviewPanelShortcut();
      return;
    }
    if (
      state.shortcutsOpen
      || state.reviewComposer
      || state.reviewPanelOpen
      || !elements.settingsPopup.hidden
      || document.body.classList.contains("drawer-open")
    ) return;
    if (action === "comment") {
      const descriptor = captureLiveSelection();
      if (descriptor) {
        if (openSelectionCommentComposer(descriptor, "reader")) event.preventDefault();
      } else if (!shortcutTargetIsInteractive(event.target) && commentOnActiveNavigationBlock()) {
        event.preventDefault();
      }
      return;
    }
    if (shortcutTargetIsInteractive(event.target)) return;
    if (moveActiveNavigationBlock(action)) event.preventDefault();
  });

  window.addEventListener("resize", () => window.requestAnimationFrame(() => {
    updateBlockCommentControls();
    positionSelectionCommentAction(state.liveSelection);
  }));
  window.addEventListener("scroll", scheduleOutlineTracking, { passive: true });
  window.addEventListener("scroll", scheduleActiveNavigationTracking, { passive: true });
  window.addEventListener("scroll", () => {
    clearTextCommentHover();
    positionSelectionCommentAction(state.liveSelection);
  }, { passive: true });
  window.addEventListener("resize", scheduleOutlineTracking);
  window.addEventListener("resize", scheduleActiveNavigationTracking);
  window.addEventListener("focus", showPendingUpdate);
  window.addEventListener("hashchange", handleRoute);
  document.addEventListener("visibilitychange", showPendingUpdate);
  desktop.addEventListener("change", () => {
    setDrawer(false, false);
    updateBlockCommentControls();
  });

  /* One key reaches the document list from anywhere. */
  document.addEventListener("keydown", (event) => {
    if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
    if (event.key.toLowerCase() !== "k") return;
    if (state.reviewMutationPending) return;
    event.preventDefault();
    focusDocumentFilter();
  });
  reviewWide.addEventListener("change", () => {
    syncReviewPanelMode();
    updateBlockCommentControls();
  });
  colorScheme.addEventListener("change", handleColorSchemeChange);
  setDrawer(false, false);
  setReviewPanel(false, false);
  setShortcutDialog(false, false);
  initialize().then(watchChanges);
})();
