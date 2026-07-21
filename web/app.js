(() => {
  "use strict";

  const elements = {
    backdrop: document.querySelector("#backdrop"),
    brand: document.querySelector("#brand"),
    closeButton: document.querySelector("#close-button"),
    currentFile: document.querySelector("#current-file"),
    document: document.querySelector("#document"),
    drawer: document.querySelector("#drawer"),
    fileCount: document.querySelector("#file-count"),
    fileFilter: document.querySelector("#file-filter"),
    fileNav: document.querySelector("#file-nav"),
    menuButton: document.querySelector("#menu-button"),
    reader: document.querySelector("#reader"),
    routeStatus: document.querySelector("#route-status"),
    statusMessage: document.querySelector("#status-message"),
    statusView: document.querySelector("#status-view"),
  };

  const desktop = window.matchMedia("(min-width: 56.25rem)");
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const state = {
    abortController: null,
    currentPath: "",
    files: [],
    fileSet: new Set(),
    filter: "",
    focusAfterNavigation: false,
    openFolders: new Set(),
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

  function defaultDocument() {
    return state.files.find((path) => !path.includes("/") && /^readme\.(?:md|markdown)$/i.test(path))
      || state.files[0]
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
      const parts = path.split("/");
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
      if (file.path === state.currentPath) link.setAttribute("aria-current", "page");
      item.append(link);
      list.append(item);
    }

    return list;
  }

  function renderFileTree() {
    const query = state.filter.trim().toLocaleLowerCase();
    const visibleFiles = query
      ? state.files.filter((path) => path.toLocaleLowerCase().includes(query))
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

  function finishNavigation(fragment, title) {
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        scrollToRouteFragment(fragment, state.focusAfterNavigation);
        state.focusAfterNavigation = false;
      });
    });
    elements.routeStatus.textContent = `Loaded ${title}`;
  }

  async function loadDocument(path, fragment = "") {
    if (path === state.currentPath && !elements.document.hidden) {
      finishNavigation(fragment, elements.currentFile.textContent || titleFromPath(path));
      return;
    }

    state.abortController?.abort();
    const controller = new AbortController();
    state.abortController = controller;
    state.currentPath = path;
    elements.currentFile.textContent = fileName(path);
    updateActiveFile();
    showLoading();

    try {
      const payload = await fetchJSON(`/api/render?path=${encodeURIComponent(path)}`, { signal: controller.signal });
      if (typeof payload?.html !== "string") throw new Error("The server returned an invalid document.");

      const renderedPath = normalizePath(typeof payload.path === "string" ? payload.path : path) || path;
      const title = typeof payload.title === "string" && payload.title.trim()
        ? payload.title.trim()
        : titleFromPath(renderedPath);

      state.currentPath = renderedPath;
      const template = document.createElement("template");
      template.innerHTML = payload.html;
      prepareDocument(template.content, renderedPath);
      elements.document.replaceChildren(template.content);
      elements.document.setAttribute("aria-label", title);
      elements.currentFile.textContent = fileName(renderedPath);
      document.title = `${title} | MDShelf`;
      updateActiveFile();
      showDocument();
      finishNavigation(fragment, title);
    } catch (error) {
      if (error.name === "AbortError") return;
      const message = error instanceof TypeError
        ? "MDShelf could not reach the local server."
        : error.message;
      showMessage("Could not open document", message, () => loadDocument(path, fragment));
      elements.routeStatus.textContent = `Could not load ${fileName(path)}`;
    }
  }

  function handleRoute() {
    if (!state.files.length) return;
    const route = readRoute();
    if (!route.path) {
      const path = defaultDocument();
      window.history.replaceState(null, "", buildRoute(path));
      loadDocument(path);
      return;
    }

    if (!state.fileSet.has(route.path)) {
      state.currentPath = "";
      updateActiveFile();
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

    loadDocument(route.path, route.fragment);
  }

  async function initialize() {
    showLoading();
    elements.fileCount.textContent = "Loading documents";
    try {
      const payload = await fetchJSON("/api/files");
      if (!Array.isArray(payload?.files)) throw new Error("The server returned an invalid file list.");

      state.files = [...new Set(payload.files
        .filter((path) => typeof path === "string")
        .map(normalizePath)
        .filter(Boolean))]
        .sort(collator.compare);
      state.fileSet = new Set(state.files);
      renderFileTree();

      if (!state.files.length) {
        elements.currentFile.textContent = "No documents";
        document.title = "MDShelf";
        showMessage(
          "No Markdown files found",
          "Add a Markdown file to this folder, then reload the page.",
          () => window.location.reload(),
        );
        return;
      }

      elements.brand.href = buildRoute(defaultDocument());
      handleRoute();
    } catch (error) {
      const message = error instanceof TypeError
        ? "MDShelf could not reach the local server."
        : error.message;
      elements.fileNav.replaceChildren();
      const empty = document.createElement("p");
      empty.className = "nav-empty";
      empty.textContent = "The document list could not be loaded.";
      elements.fileNav.append(empty);
      elements.fileCount.textContent = "Load failed";
      showMessage("Could not load documents", message, initialize);
    }
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

  window.addEventListener("hashchange", handleRoute);
  desktop.addEventListener("change", () => setDrawer(false, false));
  setDrawer(false, false);
  initialize();
})();
