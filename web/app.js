(() => {
  "use strict";

  const elements = {
    backdrop: document.querySelector("#backdrop"),
    brand: document.querySelector("#brand"),
    closeButton: document.querySelector("#close-button"),
    currentFile: document.querySelector("#current-file"),
    document: document.querySelector("#document"),
    documentPath: document.querySelector("#document-path"),
    drawer: document.querySelector("#drawer"),
    fileCount: document.querySelector("#file-count"),
    fileFilter: document.querySelector("#file-filter"),
    fileNav: document.querySelector("#file-nav"),
    menuButton: document.querySelector("#menu-button"),
    reader: document.querySelector("#reader"),
    routeStatus: document.querySelector("#route-status"),
    statusMessage: document.querySelector("#status-message"),
    statusView: document.querySelector("#status-view"),
    updateNotice: document.querySelector("#update-notice"),
  };

  const desktop = window.matchMedia("(min-width: 56.25rem)");
  const colorScheme = window.matchMedia("(prefers-color-scheme: dark)");
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const state = {
    abortController: null,
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
    openFolders: new Set(),
    pendingUpdate: null,
    updateTimer: 0,
    renderGeneration: 0,
    mermaidCounter: 0,
    mermaidQueue: Promise.resolve(),
  };

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
    const encodedFragment = fragment ? `#${encodeURIComponent(safeDecode(fragment))}` : "";
    return `#/${encodePath(path)}${encodedFragment}`;
  }

  function readRoute() {
    if (!window.location.hash.startsWith("#/")) return { path: "", fragment: "" };

    const route = window.location.hash.slice(2);
    const fragmentIndex = route.indexOf("#");
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
    return state.titles.get(path) || fileName(path);
  }

  function isDocumentAvailable(path) {
    return state.fileSet.has(path) && (!state.daemonMode || state.removed.get(path) !== true);
  }

  function defaultDocument(exclude = "") {
    const available = state.files.filter((path) => path !== exclude && isDocumentAvailable(path));
    if (state.daemonMode) return available[0] || "";
    return available.find((path) => !path.includes("/") && /^readme\.(?:md|markdown)$/i.test(path))
      || available[0]
      || "";
  }

  function setDrawer(open, restoreFocus = true) {
    if (desktop.matches) {
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

  async function fetchJSON(url, options = {}) {
    const response = await fetch(url, {
      ...options,
      headers: { Accept: "application/json", ...options.headers },
    });
    if (!response.ok) {
      let body = "";
      try {
        body = (await response.text()).trim();
      } catch {
        body = "";
      }

      let detail = body;
      if (body) {
        try {
          const payload = JSON.parse(body);
          if (typeof payload?.error === "string") detail = payload.error;
        } catch {
          detail = body;
        }
      }
      throw new Error(detail || `The server returned ${response.status}.`);
    }
    return response.json();
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
      link.textContent = file.name;
      if (state.removed.get(file.path)) {
        link.classList.add("is-removed");
        link.setAttribute("aria-label", `${file.name}, removed`);
      }
      if (file.path === state.currentPath) link.setAttribute("aria-current", "page");
      item.append(link);
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

  function updateActiveFile() {
    for (const link of elements.fileNav.querySelectorAll(".file-link")) {
      if (link.dataset.path === state.currentPath) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
  }

  function setFileList(payload) {
    if (!Array.isArray(payload?.files)) throw new Error("The server returned an invalid file list.");
    const daemonMode = payload.mode === "daemon";
    const allStrings = payload.files.every((row) => typeof row === "string");
    const allObjects = payload.files.every((row) => (
      row && typeof row === "object" && typeof row.path === "string" && typeof row.title === "string"
    ));
    if ((!daemonMode && !allStrings) || (daemonMode && !allObjects)) {
      throw new Error("The server returned an invalid file list.");
    }

    const titles = new Map();
    const removed = new Map();
    const values = payload.files.map((row) => {
      if (typeof row === "string") return normalizePath(row);
      const path = normalizePath(row.path);
      titles.set(path, row.title.trim() || titleFromPath(path));
      removed.set(path, row.removed === true);
      return path;
    }).filter(Boolean);
    state.daemonMode = daemonMode;
    state.titles = titles;
    state.removed = removed;
    state.files = [...new Set(values)].sort((a, b) => collator.compare(displayName(a), displayName(b)));
    state.fileSet = new Set(state.files);
    const home = defaultDocument();
    elements.brand.href = home ? buildRoute(home) : "#";
    renderFileTree();
  }

  async function refreshFileList() {
    setFileList(await fetchJSON("/api/files"));
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
      if (typeof payload?.html !== "string" || typeof payload.absolutePath !== "string" || !payload.absolutePath) {
        throw new Error("The server returned an invalid document.");
      }

      const renderedPath = normalizePath(typeof payload.path === "string" ? payload.path : path) || path;
      const title = typeof payload.title === "string" && payload.title.trim()
        ? payload.title.trim()
        : titleFromPath(renderedPath);
      const previousScroll = window.scrollY;

      const template = document.createElement("template");
      template.innerHTML = payload.html;
      const blocks = [...template.content.children];
      const signatures = blockSignatures(template.content);
      const changedIndexes = live ? changedBlockIndexes(state.highlightBaseline, signatures) : new Set();
      const changedBlocks = blocks.filter((_, index) => changedIndexes.has(index));
      prepareDocument(template.content, renderedPath);
      await renderMermaid(template.content, () => isCurrentLoad(controller));
      if (!isCurrentLoad(controller)) return;
      state.currentPath = renderedPath;
      elements.document.replaceChildren(template.content);
      elements.document.setAttribute("aria-label", title);
      elements.currentFile.textContent = displayName(renderedPath);
      setDocumentPath(payload.absolutePath);
      document.title = `${title} | MDShelf`;
      updateActiveFile();
      showDocument();
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
      setDocumentPath("");
      showMessage("Could not open document", message, () => loadDocument(path, fragment));
      elements.routeStatus.textContent = `Could not load ${fileName(path)}`;
    }
  }

  function handleRoute() {
    if (!state.files.length) return;
    const route = readRoute();
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

  async function applyChanges(payload) {
    if (!Number.isSafeInteger(payload?.revision) || payload.revision < 0 || !Array.isArray(payload?.changes)) {
      throw new Error("The server returned an invalid change list.");
    }

    const changes = payload.changes.filter((change) => (
      change
      && typeof change.path === "string"
      && ["added", "removed", "updated"].includes(change.kind)
    ));
    if (state.daemonMode ? changes.length > 0 || payload.reset : payload.reset || changes.some((change) => change.kind !== "updated")) {
      await refreshFileList();
    }

    const currentPath = state.currentPath;
    let currentChange = null;
    for (let index = changes.length - 1; index >= 0; index -= 1) {
      if (normalizePath(changes[index].path) === currentPath) {
        currentChange = changes[index];
        break;
      }
    }

    if (currentPath && (payload.reset || currentChange)) {
      if (!isDocumentAvailable(currentPath) || currentChange?.kind === "removed") {
        showRemovedDocument(currentPath);
        return;
      }
      const route = readRoute();
      await loadDocument(currentPath, route.fragment, { force: true, live: true });
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
    if (latest) queueUpdate(`${fileName(normalizePath(latest.path))} ${latest.kind}`);
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

  function initializeMermaid() {
    window.mermaid?.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: colorScheme.matches ? "dark" : "default",
    });
  }

  function handleColorSchemeChange() {
    initializeMermaid();
    if (!state.currentPath || elements.document.hidden || !isDocumentAvailable(state.currentPath)) return;
    const route = readRoute();
    void loadDocument(state.currentPath, route.fragment, { force: true });
  }

  async function initialize() {
    initializeMermaid();
    showLoading();
    elements.fileCount.textContent = "Loading documents";
    try {
      await refreshFileList();

      if (!state.files.length) {
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

  if (window.__MDSHELF_TEST__) {
    window.__MDSHELF_TEST_API__ = {
      cancelDocumentLoad,
      documentPathElement: elements.documentPath,
      initializeMermaid,
      isCurrentLoad,
      renderMermaid,
      setDocumentPath,
      setAbortController(controller) { state.abortController = controller; },
    };
    return;
  }

  elements.menuButton.addEventListener("click", () => setDrawer(true));
  elements.closeButton.addEventListener("click", () => setDrawer(false));
  elements.backdrop.addEventListener("click", () => setDrawer(false));
  elements.fileFilter.addEventListener("input", () => {
    state.filter = elements.fileFilter.value;
    renderFileTree();
  });

  elements.fileNav.addEventListener("click", (event) => {
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

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && document.body.classList.contains("drawer-open")) {
      setDrawer(false);
      return;
    }
    if (event.key !== "Tab" || desktop.matches || !document.body.classList.contains("drawer-open")) return;

    const focusable = [...elements.drawer.querySelectorAll(
      "a[href], button:not([disabled]), input:not([disabled]), summary, [tabindex]:not([tabindex='-1'])",
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
  });

  window.addEventListener("focus", showPendingUpdate);
  window.addEventListener("hashchange", handleRoute);
  document.addEventListener("visibilitychange", showPendingUpdate);
  desktop.addEventListener("change", () => setDrawer(false, false));
  colorScheme.addEventListener("change", handleColorSchemeChange);
  setDrawer(false, false);
  initialize().then(watchChanges);
})();
