// DOM 测试统一扩展可访问性断言并在每例后清理 React 挂载点。
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => cleanup());
