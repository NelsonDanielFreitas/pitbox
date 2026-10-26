import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["dist", "coverage", "src/gen", "src/app/routeTree.gen.ts"],
  },
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, ...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    rules: {
      "@typescript-eslint/consistent-type-imports": "error",
      "@typescript-eslint/no-misused-promises": ["error", { checksVoidReturn: false }],
      "no-console": ["warn", { allow: ["warn", "error"] }],
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "Do not inject HTML into the app.",
        },
        {
          selector: "CallExpression[callee.name='eval']",
          message: "eval is forbidden.",
        },
        {
          selector: "NewExpression[callee.name='Function']",
          message: "new Function is forbidden.",
        },
      ],
    },
  },
  {
    files: ["**/*.tsx"],
    extends: [reactHooks.configs.flat["recommended-latest"], reactRefresh.configs.vite],
  },
  {
    files: ["src/app/routes/**/*.{ts,tsx}"],
    rules: {
      "@typescript-eslint/only-throw-error": "off",
      "react-refresh/only-export-components": "off",
    },
  },
  {
    files: ["src/features/*/ui/**/*.{ts,tsx}", "src/app/routes/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@connectrpc/*", "@/gen/**"],
              message: "Only the feature data layer may talk to the API.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/db/**", "src/i18n/**"],
    rules: {
      "no-restricted-properties": [
        "error",
        {
          object: "navigator",
          property: "storage",
          message: "Storage access belongs in src/db.",
        },
      ],
      "no-restricted-globals": [
        "error",
        {
          name: "localStorage",
          message: "localStorage is reserved for the non-sensitive language preference.",
        },
        { name: "sessionStorage", message: "sessionStorage is not allowed." },
        { name: "indexedDB", message: "indexedDB is not allowed here." },
      ],
      "no-restricted-syntax": [
        "error",
        {
          selector:
            "MemberExpression[object.name='window'][property.name='localStorage']",
          message: "localStorage is reserved for the non-sensitive language preference.",
        },
        {
          selector:
            "MemberExpression[object.name='window'][property.name='sessionStorage']",
          message: "sessionStorage is not allowed.",
        },
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "Do not inject HTML into the app.",
        },
        {
          selector: "CallExpression[callee.name='eval']",
          message: "eval is forbidden.",
        },
        {
          selector: "NewExpression[callee.name='Function']",
          message: "new Function is forbidden.",
        },
      ],
    },
  },
);
