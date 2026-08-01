// 浏览器烟测固定未登录状态、核心交互、控制台健康度和移动端无横向溢出。
import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.route("**/travel/api/session", (route) => route.fulfill({
    status: 204
  }));
});

test("登录入口在目标视口可用且没有控制台错误", async ({ page }) => {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });

  await page.goto("./");
  await expect(page.getByRole("heading", { name: "欢迎回来" })).toBeVisible();
  await page.getByLabel("手机号").pressSequentially("abc13800138000");
  await expect(page.getByLabel("手机号")).toHaveValue("13800138000");
  await page.getByRole("button", { name: "显示密码" }).click();
  await expect(page.locator("#password")).toHaveAttribute("type", "text");

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});
