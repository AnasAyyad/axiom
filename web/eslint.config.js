import babelParser from "@babel/eslint-parser";
import eslint from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";

export default [
  { ignores: ["dist/**", "src/api/generated/**"] },
  eslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parser: babelParser,
      globals: globals.browser,
      parserOptions: {
        requireConfigFile: false,
        sourceType: "module",
        babelOptions: {
          parserOpts: { plugins: ["typescript", "jsx"] },
        },
      },
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.flat.recommended.rules,
      ...reactRefresh.configs.vite.rules,
      "no-undef": "off",
      "no-unused-vars": "off",
    },
  },
  {
    files: ["*.config.ts", "*.js"],
    languageOptions: {
      globals: globals.node,
    },
  },
];
