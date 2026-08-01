// ESLint 只校验前端源码与测试，构建产物和依赖目录始终排除。
import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["coverage", "dist", "node_modules", "playwright-report", "test-results"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs.flat["recommended-latest"],
  {
    files: ["src/**/*.{ts,tsx}", "e2e/**/*.ts", "*.config.ts"],
    languageOptions: { parserOptions: { ecmaFeatures: { jsx: true } } }
  }
);
