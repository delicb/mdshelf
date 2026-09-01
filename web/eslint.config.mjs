// Flat ESLint config for the embedded web frontend. Deliberately dependency
// free: the rule set below mirrors eslint:recommended (so no @eslint/js
// import is needed), and the globals are declared by hand (no globals
// package). Run it from the repository root with a pinned ESLint, e.g.:
//
//   npx --yes eslint@9.39.5 --config web/eslint.config.mjs "web/*.js"
//
// or via the devDependency in e2e/: cd e2e && npm run lint

const browserGlobals = {
  AbortController: "readonly",
  CSS: "readonly",
  Highlight: "readonly",
  Node: "readonly",
  TextEncoder: "readonly",
  URL: "readonly",
  console: "readonly",
  document: "readonly",
  fetch: "readonly",
  navigator: "readonly",
  window: "readonly",
  // text-selection.js also loads under node:test via require(), and exports
  // itself behind a typeof guard.
  module: "writable",
};

const nodeGlobals = {
  __dirname: "readonly",
  console: "readonly",
  exports: "writable",
  module: "writable",
  process: "readonly",
  require: "readonly",
};

export default [
  {
    files: ["**/*.js", "**/*.mjs"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "script",
      globals: browserGlobals,
    },
    linterOptions: {
      reportUnusedDisableDirectives: "error",
    },
    rules: {
      // eslint:recommended, spelled out to avoid a dependency on @eslint/js.
      "constructor-super": "error",
      "for-direction": "error",
      "getter-return": "error",
      "no-async-promise-executor": "error",
      "no-case-declarations": "error",
      "no-class-assign": "error",
      "no-compare-neg-zero": "error",
      "no-cond-assign": "error",
      "no-const-assign": "error",
      "no-constant-binary-expression": "error",
      "no-constant-condition": "error",
      "no-control-regex": "error",
      "no-debugger": "error",
      "no-delete-var": "error",
      "no-dupe-args": "error",
      "no-dupe-class-members": "error",
      "no-dupe-else-if": "error",
      "no-dupe-keys": "error",
      "no-duplicate-case": "error",
      "no-empty": "error",
      "no-empty-character-class": "error",
      "no-empty-pattern": "error",
      "no-empty-static-block": "error",
      "no-ex-assign": "error",
      "no-extra-boolean-cast": "error",
      "no-fallthrough": "error",
      "no-func-assign": "error",
      "no-global-assign": "error",
      "no-import-assign": "error",
      "no-invalid-regexp": "error",
      "no-irregular-whitespace": "error",
      "no-loss-of-precision": "error",
      "no-misleading-character-class": "error",
      "no-new-native-nonconstructor": "error",
      "no-nonoctal-decimal-escape": "error",
      "no-obj-calls": "error",
      "no-octal": "error",
      "no-prototype-builtins": "error",
      "no-redeclare": "error",
      "no-regex-spaces": "error",
      "no-self-assign": "error",
      "no-setter-return": "error",
      "no-shadow-restricted-names": "error",
      "no-sparse-arrays": "error",
      "no-this-before-super": "error",
      "no-undef": "error",
      "no-unexpected-multiline": "error",
      "no-unreachable": "error",
      "no-unsafe-finally": "error",
      "no-unsafe-negation": "error",
      "no-unsafe-optional-chaining": "error",
      "no-unused-labels": "error",
      "no-unused-private-class-members": "error",
      // Kept at "warn" while known dead code is removed in a parallel change;
      // raise this to "error" once that lands.
      "no-unused-vars": "warn",
      "no-useless-backreference": "error",
      "no-useless-catch": "error",
      "no-useless-escape": "error",
      "no-with": "error",
      "require-yield": "error",
      "use-isnan": "error",
      "valid-typeof": "error",
    },
  },
  {
    // node:test suites for the same sources.
    files: ["**/*.cjs"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "commonjs",
      globals: { ...browserGlobals, ...nodeGlobals },
    },
  },
];
