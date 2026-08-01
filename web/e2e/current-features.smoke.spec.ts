// 当前模型烟测使用稳定 API 响应覆盖桌面与 390px 的关键编辑和地图布局。
import { expect, test, type Page, type Route } from "@playwright/test";
import { resolve } from "node:path";

const trip = {
  id: "trip-a",
  title: "山海漫游示例",
  summary: "四天示例行程，优先关注天气和末班车时间。",
  startDate: "2030-04-10",
  endDate: "2030-04-13",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 3,
  role: "owner"
};

const records = {
  days: [
    row("day-a", "days", {
      title: "抵达与山谷漫步",
      date: "2030-04-10",
      placeEntries: [
        {
          placeId: "place-a",
          arrivalTime: "09:00",
          departureTime: "10:20",
          additionalInfo: "领取纸质导览后再出发"
        },
        {
          placeId: "place-b",
          arrivalTime: "11:30",
          departureTime: "14:00",
          additionalInfo: "寄存行李并办理入住"
        },
        {
          placeId: "place-c",
          additionalInfo: "日落前返回，天气变化时启用 Plan B"
        }
      ]
    }),
    row("day-b", "days", {
      title: "第二天",
      date: "2030-04-11",
      placeEntries: [
        { placeId: "place-b", arrivalTime: "08:00" }
      ]
    })
  ],
  places: [
    row("place-a", "places", {
      name: "示例游客中心",
      type: "tag-place",
      address: "示例省示例市山谷路 1 号",
      longitude: 120.101,
      latitude: 30.101,
      weatherAdcode: "110101",
      openingHours: "08:30-17:30"
    }),
    row("place-b", "places", {
      name: "酒店",
      type: "tag-place-hotel",
      address: "示例省示例市河畔路 8 号",
      longitude: 120.102,
      latitude: 30.102,
      weatherAdcode: "110101"
    }),
    row("place-c", "places", {
      name: "示例观景台",
      type: "tag-place",
      address: "示例省示例市山脊步道",
      longitude: 120.103,
      latitude: 30.103,
      weatherAdcode: "110101"
    })
  ],
  segments: [
    row("segment-a", "segments", {
      dayId: "day-a",
      fromPlaceId: "place-a",
      toPlaceId: "place-b",
      mode: "drive",
      durationMinutes: 35,
      departureTime: "10:20",
      additionalInfo: "山区道路预留缓冲"
    }),
    row("segment-b", "segments", {
      dayId: "day-a",
      fromPlaceId: "place-b",
      toPlaceId: "place-c",
      mode: "scenic_bus",
      durationMinutes: 45,
      departureTime: "14:10",
      additionalInfo: "末班车时间需在当天复核"
    })
  ],
  "research-evidence": [
    row("research-a", "research-evidence", {
      title: "开放时间",
      content: "以当天公告为准。",
      platformId: "tag-official",
      recordedByMemberId: "member-a",
      dayIds: ["day-a"],
      placeId: "place-a"
    })
  ],
  bookings: [
    row("booking-a", "bookings", {
      item: "酒店预订",
      type: "tag-expense-hotel",
      bookerMemberId: "member-a",
      placeId: "place-b",
      dateTime: "2030-04-10T18:00",
      status: "booked",
      link: "https://example.com/orders/hotel"
    }),
    row("booking-b", "bookings", {
      item: "景区门票",
      type: "tag-expense-ticket",
      bookerMemberId: "member-a",
      placeId: "place-a",
      status: "needs_booking"
    })
  ],
  expenses: [
    row("expense-a", "expenses", {
      item: "酒店预订",
      type: "tag-expense-hotel",
      payerMemberId: "member-a",
      amountMinor: 88000,
      splitMode: "all",
      splitMemberIds: ["member-a", "member-b"],
      bookingRecordId: "booking-a"
    }),
    row("expense-b", "expenses", {
      item: "景区门票",
      type: "tag-expense-ticket",
      payerMemberId: "member-a",
      splitMode: "all",
      splitMemberIds: ["member-a"],
      bookingRecordId: "booking-b"
    }),
    row("expense-c", "expenses", {
      item: "早餐",
      type: "tag-expense-hotel",
      payerMemberId: "member-b",
      amountMinor: 3200,
      splitMode: "selected",
      splitMemberIds: ["member-b"]
    })
  ],
  members: [
    row("trip-member-a", "members", {
      memberId: "member-a",
      role: "owner"
    }),
    row("trip-member-b", "members", {
      memberId: "member-b",
      role: "同行成员"
    })
  ],
  "packing-items": [
    row("packing-a", "packing-items", {
      item: "雨衣",
      category: "gear",
      dayIds: ["day-a"],
      quantity: 1,
      packed: true
    }),
    row("packing-b", "packing-items", {
      item: "充电宝",
      category: "electronics",
      dayIds: ["day-b"],
      quantity: 1,
      packed: false
    })
  ]
};

test.beforeEach(async ({ page }) => {
  await mockAPI(page);
});

test("执行日编辑保留焦点、限制日期并一次确认关闭", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/itinerary");
  await expect(page.getByRole("heading", { name: "日程" })).toBeVisible();
  await expect(page.getByText(/2 个执行日，4 个地点，2 段交通参考/)).toBeVisible();
  await expect(page.getByText("示例游客中心、酒店、示例观景台")).toBeVisible();

  await page.getByRole("button", { name: "编辑" }).first().click();
  const editor = page.getByRole("dialog");
  const title = editor.getByRole("textbox", { name: "标题", exact: true });
  await title.click();
  await title.pressSequentially("新");
  await expect(title).toBeFocused();
  await expect(page.getByRole("button", { name: "关闭编辑执行日" })).not.toBeFocused();

  const date = editor.getByRole("textbox", { name: "日期", exact: true });
  await expect(date).toHaveAttribute("type", "date");
  await expect(date).toHaveAttribute("min", trip.startDate);
  await expect(date).toHaveAttribute("max", trip.endDate);
  await expect(editor.getByRole("button", { name: "选择日期" })).toBeVisible();

  await date.fill("2030-04-14");
  await editor.getByRole("button", { name: "保存" }).click();
  await expect(editor.getByRole("alert")).toContainText(
    "执行日必须位于旅行日期范围 2030-04-10 至 2030-04-13 内"
  );

  await page.locator(".editor-backdrop").click({ position: { x: 3, y: 3 } });
  await expect(page.getByRole("alertdialog")).toContainText("放弃未保存修改");
  await page.getByRole("button", { name: "放弃修改" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("旅行沙盘联动日期、地点、模式与上下文", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=plan");

  await expect(page.getByRole("heading", { name: "抵达与山谷漫步", level: 1 })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "选择执行日" }).getByRole("button")).toHaveCount(2);
  await expect(page.getByRole("button", { name: "规划", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(page).toHaveURL(/mode=plan/);
  await expect(page).toHaveURL(/place=place-a/);

  const routeSheet = page.getByRole("complementary", { name: "当天地点与交通" });
  await expect(routeSheet.locator(".day-route-place").filter({
    hasText: "示例游客中心"
  })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("complementary", { name: "示例游客中心地点详情" })).toContainText("08:30-17:30");

  await routeSheet.locator(".day-route-place").filter({ hasText: "酒店" }).click();
  await expect(page).toHaveURL(/place=place-b/);
  const hotelPanel = page.getByRole("complementary", { name: "酒店地点详情" });
  await expect(hotelPanel.getByText("酒店预订", { exact: true })).toBeVisible();
  await expect(hotelPanel.getByRole("link", { name: "打开酒店预订订单" })).toBeVisible();
  await expect(hotelPanel.getByText("1/1 已携带")).toBeVisible();

  await page.getByRole("button", { name: "在途", exact: true }).click();
  await expect(page).toHaveURL(/mode=travel/);
  await expect(page.getByRole("heading", { name: "地点天气" })).toBeVisible();
  await expect(page.getByText("18~26°C")).toBeVisible();
  await expect(hotelPanel.getByRole("link", { name: /在高德导航/ })).toBeVisible();

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("规划模式不请求天气，在途模式只读取选中地点", async ({ page }) => {
  const errors = watchErrors(page);
  let mapConfigRequests = 0;
  let weatherRequests = 0;
  page.on("request", (request) => {
    const pathname = new URL(request.url()).pathname;
    if (pathname.endsWith("/trips/trip-a/map-config")) {
      mapConfigRequests += 1;
    }
    if (pathname.endsWith("/trips/trip-a/weather")) {
      weatherRequests += 1;
    }
  });

  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=plan");
  await expect(page.getByRole("heading", { name: "抵达与山谷漫步" })).toBeVisible();
  await page.waitForLoadState("networkidle");
  expect(mapConfigRequests).toBe(1);
  expect(weatherRequests).toBe(0);

  await page.getByRole("button", { name: "在途", exact: true }).click();
  await expect(page.getByRole("heading", { name: "地点天气" })).toBeVisible();
  await expect.poll(() => weatherRequests).toBe(1);
  await page.getByRole("complementary", { name: "当天地点与交通" })
    .locator(".day-route-place").filter({ hasText: "酒店" }).click();
  await page.waitForLoadState("networkidle");
  expect(weatherRequests).toBe(1);
  expect(errors).toEqual([]);
});

test("旅行沙盘在深色模式保持层级和视口边界", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=travel&place=place-a");
  await page.getByRole("button", { name: "切换至深色模式" }).click();

  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("region", { name: "旅行沙盘" })).toBeVisible();
  await expect(page.getByRole("complementary", {
    name: "示例游客中心地点详情"
  })).toBeVisible();
  const surfaces = await page.locator(".day-map-stage, .place-context-panel")
    .evaluateAll((elements) => elements.map((element) => (
      getComputedStyle(element).backgroundColor
    )));
  expect(surfaces.every((color) => color !== "rgba(0, 0, 0, 0)")).toBe(true);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("相邻交通与地点预订使用上下文默认值", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=plan&place=place-a");

  const firstConnector = page.locator(".day-route-connector").first();
  await firstConnector.getByRole("button", { name: "再添加" }).click();
  let editor = page.getByRole("dialog", { name: "添加相邻地点交通" });
  await expect(editor.getByRole("combobox", { name: /^起点地点/ })).toHaveValue("place-a");
  await expect(editor.getByRole("combobox", { name: /^终点地点/ })).toHaveValue("place-b");
  await expect(editor.getByRole("combobox", { name: /^交通方式/ })).toHaveValue("");
  await editor.getByRole("combobox", { name: /^交通方式/ }).selectOption("walk");
  const segmentRequest = page.waitForRequest((request) => (
    request.method() === "POST"
    && new URL(request.url()).pathname.endsWith("/trips/trip-a/segments")
  ));
  await editor.getByRole("button", { name: "保存" }).click();
  expect((await segmentRequest).postDataJSON()).toMatchObject({
    data: {
      dayId: "day-a",
      fromPlaceId: "place-a",
      toPlaceId: "place-b",
      mode: "walk"
    }
  });
  await expect(firstConnector.getByRole("button", { name: /编辑.*步行/ })).toBeVisible();

  const placePanel = page.getByRole("complementary", { name: "示例游客中心地点详情" });
  await placePanel.getByRole("button", { name: "添加预订" }).click();
  editor = page.getByRole("dialog", { name: "添加地点预订" });
  await expect(editor.getByRole("combobox", { name: /^关联地点/ })).toHaveValue("place-a");
  await expect(editor.getByRole("combobox", { name: /^预订人/ })).toHaveValue("member-a");
  await expect(editor.getByRole("combobox", { name: /^状态/ })).toHaveValue("needs_booking");
  await expect(editor.getByRole("textbox", { name: /^日期 \/ 时间/ })).toHaveValue("");
  await editor.getByRole("textbox", { name: "项目", exact: true }).fill("新增门票");
  await editor.getByRole("combobox", { name: /^预订类型/ }).selectOption("tag-expense-ticket");
  const bookingRequest = page.waitForRequest((request) => (
    request.method() === "POST"
    && new URL(request.url()).pathname.endsWith("/trips/trip-a/bookings")
  ));
  await editor.getByRole("button", { name: "保存" }).click();
  expect((await bookingRequest).postDataJSON()).toMatchObject({
    data: {
      item: "新增门票",
      placeId: "place-a",
      bookerMemberId: "member-a",
      status: "needs_booking"
    }
  });
  await expect(placePanel.getByText("新增门票", { exact: true })).toBeVisible();

  await placePanel.getByRole("button", { name: "添加攻略" }).click();
  editor = page.getByRole("dialog", { name: "添加地点攻略" });
  await expect(editor.getByRole("combobox", { name: /^关联地点/ })).toHaveValue("place-a");
  await expect(editor.getByRole("checkbox", { name: /2030-04-10/ })).toBeChecked();
  await expect(editor.getByRole("combobox", { name: /^记录人/ })).toHaveValue("member-a");
  await editor.getByRole("button", { name: "取消" }).click();

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("整理地点排序保留焦点并播报新位置", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=plan");
  await expect(page.getByRole("navigation", { name: "选择执行日" })).toBeVisible();
  await page.getByRole("button", { name: "整理地点" }).click();

  const editor = page.getByRole("dialog", { name: "整理当天地点" });
  await expect(editor.getByText("地点备注和时间都可以不填")).toBeVisible();
  await expect(editor.getByText("补充时间（可选）").first()).toBeVisible();
  await expect(editor.getByRole("textbox", { name: /^标题/ })).not.toBeVisible();
  const moveHotel = editor.getByRole("button", { name: "将酒店上移" });
  await moveHotel.click();

  await expect(editor.getByRole("button", { name: "将酒店下移" })).toBeFocused();
  await expect(editor.getByRole("status")).toHaveText("酒店已移到第 1 位");
  await expect(editor.getByRole("button", {
    name: "从当天删除酒店"
  })).toBeVisible();
  expect(errors).toEqual([]);
});

test("浏览器返回也会保护未保存的日程修改", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/itinerary");
  await page.locator(".itinerary-open").first().click();
  await expect(page).toHaveURL(/\/day\/2030-04-10(?:\?.*)?$/);
  await page.getByRole("button", { name: "整理地点" }).click();
  const editor = page.getByRole("dialog", { name: "整理当天地点" });
  await editor.getByText("日期与当天说明", { exact: true }).click();
  await editor.getByRole("textbox", { name: /^标题/ }).fill("尚未保存的新标题");
  await expect(editor.getByRole("button", { name: "保存" })).toBeEnabled();

  await page.evaluate(() => window.history.back());
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("离开并放弃修改");
  await confirm.getByRole("button", { name: "取消" }).click();
  await expect(page).toHaveURL(/\/day\/2030-04-10(?:\?.*)?$/);
  await expect(editor).toBeVisible();

  await page.evaluate(() => window.history.back());
  await page.getByRole("button", { name: "放弃并离开" }).click();
  await expect(page).toHaveURL(/\/itinerary$/);
  expect(errors).toEqual([]);
});

test("地点、预订、费用和标签筛选可组合使用", async ({ page }) => {
  const errors = watchErrors(page);

  await page.goto("./app/trips/trip-a/places");
  await page.getByLabel("按地点类型").selectOption("tag-place-hotel");
  await page.getByLabel("按关联日期").selectOption("day-b");
  await expect(page.getByRole("heading", { name: "酒店", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "示例游客中心", exact: true })).toHaveCount(0);

  await page.goto("./app/trips/trip-a/bookings");
  await expect(page.getByRole("button", { name: "新增预订" })).toBeEnabled();
  await page.getByLabel("按预订类型").selectOption("tag-expense-ticket");
  await page.getByLabel("按预订人").selectOption("member-a");
  await expect(page.getByRole("heading", { name: "景区门票", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "酒店预订", exact: true })).toHaveCount(0);

  await page.goto("./app/trips/trip-a/packing-items?day=day-a");
  await expect(page.getByText("雨衣", { exact: true })).toBeVisible();
  await expect(page.getByText("充电宝", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "新增物品" }).click();
  const packingEditor = page.getByRole("dialog", { name: "新增物品" });
  await expect(packingEditor.getByRole("checkbox", { name: /2030-04-10/ })).toBeChecked();
  await packingEditor.getByRole("button", { name: "取消" }).click();

  await page.goto("./app/trips/trip-a/expenses");
  const expenseRecords = page.locator(".records-panel");
  await expenseRecords.getByRole("button", { name: /^待补录/ }).click();
  await expect(page).toHaveURL(/state=pending/);
  await expect(expenseRecords.locator(".record-card")).toHaveCount(1);
  await expect(expenseRecords.getByRole("heading", { name: "景区门票", exact: true }))
    .toBeVisible();
  await expenseRecords.getByRole("button", { name: /^全部/ }).click();
  const hotelExpense = expenseRecords.locator(".record-card").filter({ hasText: "酒店预订" });
  await hotelExpense.getByText("来源预订", { exact: true }).click();
  await expect(hotelExpense.getByText("2030/04/10 18:00", { exact: true })).toBeVisible();
  await page.getByLabel("按费用类型").selectOption("tag-expense-hotel");
  await expect(expenseRecords.getByRole("button", { name: /^待补录\s*0$/ })).toBeVisible();
  await expect(expenseRecords.getByRole("button", { name: /^已登记\s*2$/ })).toBeVisible();
  await expect(page.getByRole("heading", { name: "酒店预订", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "景区门票", exact: true })).toHaveCount(0);

  await page.goto("./app/library?section=tags");
  await page.getByLabel("按标签类型").selectOption("expense-type");
  await expect(page.getByRole("heading", { name: /费用类型/ })).toBeVisible();
  await expect(page.getByText("景点", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "补充常用类型" }).click();
  await expect(page.getByRole("status")).toContainText("已补充 1 个常用地点与费用类型");

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("预订自动费用锁定类型和支付人", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/expenses");
  const card = page.locator(".record-card").filter({ hasText: "酒店预订" });
  await card.getByRole("button", { name: "编辑" }).click();

  const editor = page.getByRole("dialog");
  await expect(editor.getByRole("combobox", { name: /^费用类型/ })).toBeDisabled();
  await expect(editor.getByRole("combobox", { name: /^支付人/ })).toBeDisabled();
  await expect(editor.getByRole("spinbutton", { name: /^金额/ })).toBeEnabled();
  await expect(editor).toContainText("项目、费用类型与支付人跟随来源预订");
  expect(errors).toEqual([]);
});

test("资料使用当前精简字段和执行日地点筛选", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/research-evidence");
  await expect(page.getByRole("heading", { name: "资料" })).toBeVisible();
  await expect(page.getByLabel("按执行日")).toBeVisible();
  await expect(page.getByLabel("按地点")).toBeVisible();

  await page.getByRole("button", { name: "新增资料" }).click();
  const editor = page.getByRole("dialog");
  await expect(editor.getByRole("textbox", { name: "标题", exact: true })).toBeVisible();
  await expect(editor.getByRole("textbox", { name: "内容", exact: true })).toBeVisible();
  await expect(editor.getByRole("combobox", { name: /^平台类型/ })).toContainText("景区官方");
  await expect(editor.getByRole("combobox", { name: /^记录人/ })).toContainText("示例成员");
  await expect(editor.getByRole("group", { name: "适用执行日" })).toBeVisible();
  await expect(editor.getByRole("combobox", { name: /^关联地点/ })).toBeVisible();

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("地点关联字段可弹窗新增、保留父表单并自动选中", async ({ page }, testInfo) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/bookings");
  await page.getByRole("button", { name: "新增预订" }).click();
  let parentEditor = page.getByRole("dialog", { name: "新增预订" });
  await parentEditor.getByRole("textbox", { name: "项目", exact: true }).fill("临时接驳");
  await parentEditor.getByRole("button", {
    name: "新增地点并选为关联地点"
  }).click();

  let placeEditor = page.getByRole("dialog", { name: "新增地点" });
  await expect(placeEditor).toBeVisible();
  await expect(page.locator(".editor-backdrop[aria-hidden='true']")).toHaveCount(1);
  if (testInfo.project.name === "mobile-390") {
    await expect.poll(async () => {
      const box = await placeEditor.boundingBox();
      const viewport = page.viewportSize();
      if (!box || !viewport) return Number.POSITIVE_INFINITY;
      return Math.abs(box.y + box.height - viewport.height);
    }).toBeLessThanOrEqual(1);
  }
  await createPlaceInEditor(placeEditor);

  parentEditor = page.getByRole("dialog", { name: "新增预订" });
  await expect(parentEditor.getByRole("textbox", {
    name: "项目",
    exact: true
  })).toHaveValue("临时接驳");
  await expect(parentEditor.getByRole("combobox", {
    name: /^关联地点/
  })).toHaveValue("place-new-1");
  await expect(parentEditor.getByRole("option", {
    name: "临时集合点"
  })).toBeAttached();
  await parentEditor.getByRole("button", { name: "取消" }).click();
  await page.getByRole("button", { name: "放弃修改" }).click();

  await page.goto("./app/trips/trip-a/research-evidence");
  await page.getByRole("button", { name: "新增资料" }).click();
  await expect(page.getByRole("dialog", { name: "新增资料" }).getByRole(
    "button",
    { name: "新增地点并选为关联地点" }
  )).toBeVisible();
  await page.getByRole("dialog", { name: "新增资料" }).getByRole(
    "button",
    { name: "取消" }
  ).click();

  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=plan");
  await page.getByRole("button", { name: "整理地点" }).click();
  let dayEditor = page.getByRole("dialog", { name: "整理当天地点" });
  await dayEditor.getByRole("button", { name: "新增地点并加入当天" }).click();
  placeEditor = page.getByRole("dialog", { name: "新增地点" });
  await createPlaceInEditor(placeEditor);

  dayEditor = page.getByRole("dialog", { name: "整理当天地点" });
  await expect(dayEditor.locator(".day-entry-editor article").filter({
    hasText: "临时集合点"
  })).toHaveCount(1);
  await expect(dayEditor.getByRole("option", {
    name: "临时集合点（已加入）"
  })).toBeAttached();
  await dayEditor.getByRole("button", { name: "取消" }).click();
  await page.getByRole("button", { name: "放弃修改" }).click();

  await page.getByRole("button", {
    name: "编辑示例游客中心到酒店的自驾"
  }).click();
  let segmentEditor = page.getByRole("dialog", { name: "编辑交通段" });
  await expect(segmentEditor.getByRole("button", {
    name: "新增地点并选为起点地点"
  })).toBeVisible();
  await segmentEditor.getByRole("button", {
    name: "新增地点并选为终点地点"
  }).click();
  placeEditor = page.getByRole("dialog", { name: "新增地点" });
  const createForDayRequest = page.waitForRequest((request) => (
    request.method() === "POST"
    && new URL(request.url()).pathname.endsWith("/trips/trip-a/days/day-a/places")
  ));
  await createPlaceInEditor(placeEditor);
  await createForDayRequest;

  segmentEditor = page.getByRole("dialog", { name: "编辑交通段" });
  const destinationSelect = segmentEditor.getByRole("combobox", {
    name: /^终点地点/
  });
  await expect(destinationSelect).toHaveValue("place-new-3");
  await expect(destinationSelect.getByRole("option", {
    name: "临时集合点"
  })).toBeAttached();

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("删除同行成员会显示具体引用并隐藏私人物品细节", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/members");
  await expect(page.getByRole("heading", { name: "同行成员" })).toBeVisible();

  await page.locator(".record-card").filter({
    hasText: "示例成员"
  }).getByRole("button", { name: "删除", exact: true }).click();
  const notice = page.getByRole("alertdialog");
  await expect(notice).toContainText("暂时不能删除同行成员“示例成员”");
  await expect(notice).toContainText("资料“景区攻略”（资料记录人）");
  await expect(notice).toContainText("费用“晚餐”（付款人、分摊成员）");
  await expect(notice).toContainText("不展示物品名称和数量");
  await expect(notice.getByRole("button", { name: "知道了" })).toBeFocused();
  await expect(notice.getByRole("button", { name: "取消" })).toHaveCount(0);

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("同行成员仅剩手工费用和个人物品时可确认连带删除", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/members");
  const member = page.locator(".record-card").filter({
    hasText: "可连带清理成员"
  });
  await member.getByRole("button", { name: "删除", exact: true }).click();

  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("删除同行成员“可连带清理成员”并清理关联资料");
  await expect(confirm).toContainText("费用“早餐”（分摊成员）");
  await expect(confirm).toContainText("不展示物品名称和数量");
  await expect(confirm.getByRole("button", {
    name: "删除成员和关联资料"
  })).toBeVisible();
  const deleteRequest = page.waitForRequest((request) => (
    request.method() === "DELETE"
    && request.url().endsWith(
      "/trips/trip-a/members/trip-member-b?cascade=true"
    )
  ));
  await confirm.getByRole("button", { name: "删除成员和关联资料" }).click();
  await deleteRequest;
  await expect(member).toHaveCount(0);

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("只读成员可以执行行程但看不到资料写入入口", async ({ page }) => {
  const errors = watchErrors(page);
  await page.unroute("**/travel/api/**");
  await mockAPI(page, "viewer");

  await page.goto("./app/trips/trip-a/members");
  await expect(page.getByText("你拥有本次旅行的只读权限")).toBeVisible();
  await expect(page.getByRole("button", { name: "新增同行成员" })).toHaveCount(0);
  const memberCard = page.locator(".record-card").first();
  await expect(memberCard.getByRole("button", { name: "编辑" })).toHaveCount(0);
  await expect(memberCard.getByRole("button", { name: "删除" })).toHaveCount(0);

  await page.goto("./app/trips/trip-a/itinerary");
  await expect(page.getByText("可以查看和打开日程，但不能调整安排")).toBeVisible();
  await expect(page.getByRole("button", { name: "补充执行日" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "删除", exact: true })).toHaveCount(0);

  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=travel");
  await expect(page.getByText("当前为只读访问")).toBeVisible();
  await expect(page.getByRole("button", { name: "整理地点" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "新增地点" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "再添加" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "添加预订" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "添加攻略" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("已停用且无引用的登录成员可注销凭据后永久删除", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/library?section=members");
  const member = page.locator(".library-row").filter({
    hasText: "待永久删除成员"
  });
  await member.getByRole("button", { name: "删除", exact: true }).click();

  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("永久删除成员“待永久删除成员”");
  await expect(confirm).toContainText("注销其登录凭据");
  await expect(confirm).toContainText("历史操作记录仍会保留");
  const deleteRequest = page.waitForRequest((request) => (
    request.method() === "DELETE"
    && request.url().endsWith(
      "/library/members/member-c?deleteLoginCredential=true"
    )
  ));
  await confirm.getByRole("button", { name: "注销账号并删除" }).click();
  await deleteRequest;
  await expect(member).toHaveCount(0);

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("地图为路线节点保留独立边栏和圆点空间", async ({ page }, testInfo) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips/trip-a/map?date=2030-04-10");
  await expect(page.getByRole("complementary", { name: "路线地点" })).toBeVisible();
  await expect(page.getByRole("button", { name: /示例游客中心/ })).toBeVisible();

  const metrics = await page.locator(".route-stop-button").first().evaluate((element) => {
    const button = element.getBoundingClientRect();
    const side = element.closest(".route-side")!.getBoundingClientRect();
    const workspace = element.closest(".route-workspace")!.getBoundingClientRect();
    const map = element.closest(".route-workspace")!
      .querySelector(".route-map-shell")!
      .getBoundingClientRect();
    const pseudoLeft = Number.parseFloat(getComputedStyle(element, "::before").left);
    return {
      sideLeft: side.left,
      sideWidth: side.width,
      mapWidth: map.width,
      workspaceWidth: workspace.width,
      dotLeft: button.left + pseudoLeft
    };
  });

  expect(metrics.dotLeft).toBeGreaterThanOrEqual(metrics.sideLeft);
  if (testInfo.project.name === "desktop-chromium") {
    expect(metrics.sideWidth).toBeGreaterThanOrEqual(339);
    expect(metrics.mapWidth).toBeLessThan(metrics.workspaceWidth - 339);
  }
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

test("生成公开文档截图", async ({ page }, testInfo) => {
  test.skip(
    process.env.UPDATE_DOCS_SCREENSHOTS !== "1",
    "只在显式更新公开文档截图时运行"
  );
  await page.emulateMedia({ reducedMotion: "reduce" });
  const imageDirectory = resolve(process.cwd(), "..", "docs", "images");

  await page.goto("./app/trips/trip-a/day/2030-04-10?mode=travel");
  await expect(page.getByRole("heading", {
    name: "抵达与山谷漫步",
    level: 1
  })).toBeVisible();
  await expect(page.getByText("18~26°C")).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));

  if (testInfo.project.name === "mobile-390") {
    await page.screenshot({
      path: resolve(imageDirectory, "today-mobile.png"),
      fullPage: false
    });
    return;
  }

  await page.screenshot({
    path: resolve(imageDirectory, "today-desktop.png"),
    fullPage: false
  });
  await page.goto("./app/trips/trip-a/expenses");
  await expect(page.getByRole("heading", { name: "费用", exact: true, level: 1 })).toBeVisible();
  await page.screenshot({
    path: resolve(imageDirectory, "expenses-desktop.png"),
    fullPage: false
  });
});

async function mockAPI(page: Page, role: "owner" | "editor" | "viewer" = "owner") {
  const state = Object.fromEntries(
    Object.entries(records).map(([resource, rows]) => [resource, [...rows]])
  ) as Record<string, ReturnType<typeof row>[]>;
  let libraryMembers = [
    libraryRow("member-a", "members", {
      displayName: "示例成员",
      status: "active",
      admin: true
    }),
    libraryRow("member-b", "members", {
      displayName: "可连带清理成员",
      status: "active",
      admin: false
    }),
    libraryRow("member-c", "members", {
      displayName: "待永久删除成员",
      mobile: "13900139000",
      status: "inactive",
      admin: false
    })
  ];
  let createdPlaceCount = 0;
  let createdRecordCount = 0;
  await page.route("**/travel/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace("/travel/api", "");
    if (path === "/session") {
      return json(route, {
        user: {
          id: "member-a",
          displayName: "示例成员",
          mobile: "13800138000",
          admin: true
        },
        csrfToken: "test-csrf",
        expiresAt: "2030-04-30T00:00:00Z"
      });
    }
    if (path === "/trips") return json(route, { trips: [{ ...trip, role }] });
    if (path === "/library/members") {
      return json(route, { records: libraryMembers });
    }
    if (path === "/library/members/member-c/deletion-impact") {
      return json(route, {
        references: [],
        hasLoginCredential: true,
        hasTripOwnership: false,
        canDeleteWithCredential: true,
        canReplaceWithFallback: false
      });
    }
    if (
      path === "/library/members/member-c"
      && request.method() === "DELETE"
    ) {
      libraryMembers = libraryMembers.filter((row) => row.id !== "member-c");
      return json(route, {
        deleted: true,
        snapshot: snapshot(12)
      });
    }
    if (path === "/library/tags") {
      return json(route, {
        records: [
          libraryRow("tag-place", "tags", {
            name: "景点",
            category: "place-type",
            status: "active",
            color: "#477566"
          }),
          libraryRow("tag-place-hotel", "tags", {
            name: "住宿",
            category: "place-type",
            status: "active",
            color: "#6B6FA9"
          }),
          libraryRow("tag-expense-hotel", "tags", {
            name: "住宿",
            category: "expense-type",
            status: "active",
            color: "#6B6FA9"
          }),
          libraryRow("tag-expense-ticket", "tags", {
            name: "门票",
            category: "expense-type",
            status: "active",
            color: "#4D8B68"
          }),
          libraryRow("tag-official", "tags", {
            name: "景区官方",
            category: "platform-type",
            status: "active",
            color: "#2D6854"
          })
        ]
      });
    }
    if (path === "/library/tags/presets" && route.request().method() === "POST") {
      return json(route, {
        records: [
          libraryRow("tag-expense-emergency", "tags", {
            name: "应急",
            category: "expense-type",
            status: "active",
            color: "#A85D47"
          })
        ],
        snapshot: {
          id: "library-snapshot-a",
          revision: 2,
          sha256: "sha",
          contentChecksum: "checksum",
          mirrorState: "mirrored",
          createdAt: "2026-07-28T00:00:00Z"
        },
        replayed: false
      });
    }
    if (path === "/trips/trip-a/map-config") {
      return json(route, { enabled: false, jsapiKey: "" });
    }
    if (path === "/trips/trip-a/place-search") {
      return json(route, {
        candidates: [{
          id: "amap-place-new",
          name: "临时集合点",
          address: "示例市临时路 1 号",
          longitude: 120.104,
          latitude: 30.104
        }]
      });
    }
    if (path === "/trips/trip-a/reverse-geocode") {
      return json(route, {
        address: "示例市临时路 1 号",
        adcode: "110101",
        region: "示例市"
      });
    }
    if (path === "/trips/trip-a/weather") {
      return json(route, {
        city: "示例市",
        adcode: "110101",
        province: "示例省",
        reportedAt: "2030-04-09T23:00:00Z",
        fetchedAt: "2030-04-09T23:30:00Z",
        expiresAt: "2030-04-10T01:00:00Z",
        freshness: "fresh",
        cached: true,
        forecasts: [{
          date: "2030-04-10",
          dayWeather: "多云",
          nightWeather: "晴",
          dayTemp: "26",
          nightTemp: "18",
          dayWind: "南",
          nightWind: "南",
          dayPower: "≤3",
          nightPower: "≤3"
        }]
      });
    }
    const createRecordMatch = path.match(
      /^\/trips\/trip-a\/(segments|bookings|research-evidence)$/
    );
    if (createRecordMatch && request.method() === "POST") {
      const resource = createRecordMatch[1]!;
      const input = request.postDataJSON() as {
        recordKey: string;
        data: Record<string, unknown>;
      };
      createdRecordCount += 1;
      const created = row(
        `${resource}-new-${createdRecordCount}`,
        resource,
        input.data
      );
      state[resource].push(created);
      if (resource === "bookings") {
        state.expenses.push(row(`expense-auto-${createdRecordCount}`, "expenses", {
          item: input.data.item,
          type: input.data.type,
          payerMemberId: input.data.bookerMemberId,
          splitMode: "all",
          splitMemberIds: state.members.map((member) => member.data.memberId),
          bookingRecordId: created.id
        }));
      }
      return json(route, {
        record: created,
        snapshot: snapshot(20 + createdRecordCount),
        replayed: false
      }, 201);
    }
    const updateRecordMatch = path.match(
      /^\/trips\/trip-a\/(segments|bookings|research-evidence)\/([^/]+)$/
    );
    if (updateRecordMatch && request.method() === "PATCH") {
      const resource = updateRecordMatch[1]!;
      const input = request.postDataJSON() as { data: Record<string, unknown> };
      const current = state[resource].find((record) => record.id === updateRecordMatch[2]);
      if (!current) return json(route, { message: "记录不存在" }, 404);
      const updated = { ...current, version: current.version + 1, data: input.data };
      state[resource] = state[resource].map((record) => (
        record.id === updated.id ? updated : record
      ));
      return json(route, { record: updated, snapshot: snapshot(30 + updated.version) });
    }
    if (updateRecordMatch && request.method() === "DELETE") {
      const resource = updateRecordMatch[1]!;
      state[resource] = state[resource].filter((record) => record.id !== updateRecordMatch[2]);
      return json(route, { deleted: true, snapshot: snapshot(40) });
    }
    if (path === "/trips/trip-a/places" && request.method() === "POST") {
      const input = request.postDataJSON() as {
        recordKey: string;
        data: Record<string, unknown>;
      };
      createdPlaceCount += 1;
      const created = row(`place-new-${createdPlaceCount}`, "places", input.data);
      state.places.push(created);
      return json(route, {
        record: created,
        snapshot: snapshot(createdPlaceCount),
        replayed: false
      }, 201);
    }
    const createPlaceForDay = path.match(
      /^\/trips\/trip-a\/days\/([^/]+)\/places$/
    );
    if (createPlaceForDay && request.method() === "POST") {
      const input = request.postDataJSON() as {
        recordKey: string;
        data: Record<string, unknown>;
      };
      createdPlaceCount += 1;
      const created = row(`place-new-${createdPlaceCount}`, "places", input.data);
      state.places.push(created);
      const dayIndex = state.days.findIndex((day) => day.id === createPlaceForDay[1]);
      const previousDay = state.days[dayIndex];
      if (!previousDay) throw new Error("缺少待关联执行日");
      const previousEntries = Array.isArray(previousDay.data.placeEntries)
        ? previousDay.data.placeEntries
        : [];
      const updatedDay = {
        ...previousDay,
        version: previousDay.version + 1,
        data: {
          ...previousDay.data,
          placeEntries: [...previousEntries, { placeId: created.id }]
        }
      };
      state.days[dayIndex] = updatedDay;
      return json(route, {
        place: created,
        day: updatedDay,
        snapshot: snapshot(createdPlaceCount),
        replayed: false
      }, 201);
    }
    if (path === "/trips/trip-a/members/trip-member-a/deletion-impact") {
      return json(route, {
        references: [{
          resource: "research-evidence",
          recordId: "research-a",
          label: "景区攻略",
          relations: ["research-recorder"]
        }],
        cascadeExpenses: [{
          resource: "expenses",
          recordId: "expense-a",
          label: "晚餐",
          relations: ["expense-payer", "expense-split"]
        }],
        hasPrivatePackingItems: true
      });
    }
    if (path === "/trips/trip-a/members/trip-member-b/deletion-impact") {
      return json(route, {
        references: [],
        cascadeExpenses: [{
          resource: "expenses",
          recordId: "expense-c",
          label: "早餐",
          relations: ["expense-split"]
        }],
        hasPrivatePackingItems: true
      });
    }
    if (
      path === "/trips/trip-a/members/trip-member-b"
      && request.method() === "DELETE"
    ) {
      state.members = state.members.filter((row) => row.id !== "trip-member-b");
      state.expenses = state.expenses.filter((row) => row.id !== "expense-c");
      return json(route, {
        deleted: true,
        snapshot: snapshot(11)
      });
    }
    const match = path.match(/^\/trips\/trip-a\/([^/]+)$/);
    if (match && request.method() === "GET" && match[1] in state) {
      return json(route, {
        records: state[match[1]]
      });
    }
    return json(route, { records: [] });
  });
}

async function createPlaceInEditor(editor: ReturnType<Page["getByRole"]>) {
  await editor.getByRole("combobox", { name: /^地点类型/ }).selectOption("tag-place");
  await editor.getByLabel("搜索地点").fill("临时集合点");
  await editor.getByRole("button", { name: "搜索", exact: true }).click();
  await editor.getByRole("button", { name: /临时集合点/ }).click();
  await expect(editor.getByText("已定位")).toBeVisible();
  await editor.getByRole("button", { name: "创建并选中" }).click();
  await expect(editor).toHaveCount(0);
}

function snapshot(revision: number) {
  return {
    id: `snapshot-${revision}`,
    revision,
    sha256: `sha-${revision}`,
    contentChecksum: `checksum-${revision}`,
    mirrorState: "mirrored",
    createdAt: "2026-07-28T00:00:00Z"
  };
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

function row(id: string, resource: string, data: Record<string, unknown>) {
  return {
    id,
    tripId: "trip-a",
    recordKey: id,
    resource,
    position: 0,
    version: 1,
    data,
    createdAt: "2026-07-28T00:00:00Z",
    updatedAt: "2026-07-28T00:00:00Z"
  };
}

function libraryRow(id: string, resource: string, data: Record<string, unknown>) {
  return {
    id,
    resource,
    recordKey: id,
    position: 0,
    version: 1,
    data,
    createdAt: "2026-07-28T00:00:00Z",
    updatedAt: "2026-07-28T00:00:00Z"
  };
}

function watchErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  return errors;
}
