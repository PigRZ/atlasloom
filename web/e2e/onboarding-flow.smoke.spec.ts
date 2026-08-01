// 首建烟测覆盖无旅行状态、创建后落点、日期骨架和从日程调整中新增地点。
import { expect, test, type Page, type Route } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.addInitScript({
    content: `
      {
        const NativeDate = Date;
        const fixed = "2026-07-27T16:30:00Z";
        class FixedDate extends NativeDate {
          constructor(...args) {
            super(...(args.length ? args : [fixed]));
          }
          static now() {
            return new NativeDate(fixed).getTime();
          }
        }
        window.Date = FixedDate;
        Object.defineProperty(window.crypto, "randomUUID", {
          value: () => "test01-test01-4est-8est-test01test01"
        });
      }
    `
  });
  await mockOnboardingAPI(page);
});

test("从空看板创建旅行并安排第一个地点", async ({ page }) => {
  const errors = watchErrors(page);
  await page.goto("./app/trips");

  await expect(page.getByText("尚未创建旅行")).toHaveCount(1);
  await expect(page.getByRole("navigation", { name: "旅行页面" })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "创建第一场旅行" })).toBeVisible();
  await expect(page.getByText("系统会生成每一天的日程")).toBeVisible();

  await page.getByRole("button", { name: "创建旅行" }).click();
  const tripEditor = page.getByRole("dialog");
  await expect(tripEditor.getByLabel("开始日期")).toHaveValue("2026-07-28");
  await tripEditor.getByLabel("旅行名称").fill("第一次完整旅行");
  await tripEditor.getByLabel("旅行说明").fill("三天轻量行程，优先照顾同行人的休息节奏。");
  await tripEditor.getByLabel("开始日期").fill("2026-08-10");
  await tripEditor.getByLabel("结束日期").fill("2026-08-12");
  const createRequestPromise = page.waitForRequest((request) => (
    request.method() === "POST"
      && new URL(request.url()).pathname.endsWith("/travel/api/trips")
  ));
  await tripEditor.getByRole("button", { name: "创建并安排日程" }).click();
  const createRequest = await createRequestPromise;
  expect(createRequest.postDataJSON()).toEqual({
    id: "trip-20260810-test01",
    title: "第一次完整旅行",
    summary: "三天轻量行程，优先照顾同行人的休息节奏。",
    startDate: "2026-08-10",
    endDate: "2026-08-12",
    timezone: "Asia/Shanghai",
    currency: "CNY"
  });

  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(page).toHaveURL(/\/app\/trips\/trip-20260810-test01\/itinerary$/);
  await expect(page.getByRole("heading", { name: "日期骨架已准备好" })).toBeVisible();
  await expect(page.getByText("已经生成 3 个执行日")).toBeVisible();
  await expect(page.locator(".itinerary-list > li")).toHaveCount(3);

  await page.getByRole("button", { name: /添加第一天地点/ }).click();
  await expect(page.getByText("当天还没有地点", { exact: true })).toBeVisible();
  await expect(page.getByText(/先收集想去的地点即可/)).toBeVisible();
  await expect(page.getByRole("button", { name: "新增地点", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "整理地点" }).click();
  let dayEditor = page.getByRole("dialog", { name: "整理当天地点" });
  await dayEditor.getByRole("button", { name: "新增地点并加入当天" }).click();

  const placeEditor = page.getByRole("dialog", { name: "新增地点" });
  await expect(placeEditor.getByText("自动选为当天地点")).toBeVisible();
  await placeEditor.getByRole("combobox", { name: /^地点类型/ }).selectOption("tag-place");
  await placeEditor.getByLabel("搜索地点").fill("示例东站");
  await placeEditor.getByRole("button", { name: "搜索", exact: true }).click();
  await placeEditor.getByRole("button", { name: /示例东站/ }).click();
  await expect(placeEditor.getByText("已定位")).toBeVisible();
  await placeEditor.getByRole("button", { name: "创建并选中" }).click();

  dayEditor = page.getByRole("dialog", { name: "整理当天地点" });
  await expect(dayEditor.locator(".day-entry-editor article").filter({
    hasText: "示例东站"
  })).toHaveCount(1);
  await dayEditor.getByRole("button", { name: "保存" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const routeSheet = page.getByRole("complementary", { name: "当天地点与交通" });
  await expect(routeSheet.getByRole("button", { name: /示例东站/ })).toBeVisible();
  await expect(routeSheet.getByText("1 个地点", { exact: true })).toBeVisible();
  await expect(page.getByRole("complementary", {
    name: "示例东站地点详情"
  })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
  expect(errors).toEqual([]);
});

async function mockOnboardingAPI(page: Page) {
  let trips: Record<string, unknown>[] = [];
  let days: Record<string, unknown>[] = [];
  let places: Record<string, unknown>[] = [];
  await page.route("**/travel/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace("/travel/api", "");
    if (path === "/session") {
      return json(route, {
        user: {
          id: "member-a",
          displayName: "示例成员",
          mobile: "13800138000",
          admin: true
        },
        csrfToken: "test-csrf",
        expiresAt: "2026-09-30T00:00:00Z"
      });
    }
    if (path === "/trips" && request.method() === "GET") {
      return json(route, { trips });
    }
    if (path === "/trips" && request.method() === "POST") {
      const input = request.postDataJSON() as Record<string, unknown>;
      const created = {
        ...input,
        version: 1,
        role: "owner"
      };
      trips = [created];
      days = ["2026-08-10", "2026-08-11", "2026-08-12"].map((date, index) => (
        row(`day-${index + 1}`, "days", {
          title: "待规划",
          date,
          placeEntries: []
        }, index)
      ));
      return json(route, {
        trip: created,
        snapshot: snapshot(1),
        replayed: false
      }, 201);
    }
    if (path === "/library/tags") {
      return json(route, {
        records: [
          libraryRow("tag-place", "tags", {
            name: "交通枢纽",
            category: "place-type",
            status: "active",
            color: "#477566"
          })
        ]
      });
    }
    if (path === "/library/members") {
      return json(route, {
        records: [
          libraryRow("member-a", "members", {
            displayName: "示例成员",
            status: "active",
            admin: true
          })
        ]
      });
    }
    if (path.endsWith("/map-config")) {
      return json(route, { enabled: false, jsapiKey: "" });
    }
    if (path.endsWith("/place-search")) {
      return json(route, {
        candidates: [{
          id: "amap-place-a",
          name: "示例东站",
          address: "示例省示例市示例区",
          longitude: 116.397,
          latitude: 39.909
        }]
      });
    }
    if (path.endsWith("/reverse-geocode")) {
      return json(route, {
        address: "示例省示例市示例区示例东站",
        region: "示例市"
      });
    }
    if (
      path === "/trips/trip-20260810-test01/places"
      && request.method() === "POST"
    ) {
      const input = request.postDataJSON() as {
        recordKey: string;
        data: Record<string, unknown>;
      };
      const place = row("place-1", "places", input.data);
      places = [place];
      return json(route, {
        record: place,
        snapshot: snapshot(2),
        replayed: false
      }, 201);
    }
    const updateDayMatch = path.match(
      /^\/trips\/trip-20260810-test01\/days\/([^/]+)$/
    );
    if (updateDayMatch && request.method() === "PATCH") {
      const input = request.postDataJSON() as {
        data: Record<string, unknown>;
      };
      let updated: Record<string, unknown> | undefined;
      days = days.map((candidate) => {
        if (candidate.id !== updateDayMatch[1]) return candidate;
        updated = {
          ...candidate,
          version: Number(candidate.version) + 1,
          data: input.data
        };
        return updated;
      });
      return json(route, {
        record: updated,
        snapshot: snapshot(3)
      });
    }
    const createPlaceMatch = path.match(
      /^\/trips\/trip-20260810-test01\/days\/([^/]+)\/places$/
    );
    if (createPlaceMatch && request.method() === "POST") {
      const input = request.postDataJSON() as {
        recordKey: string;
        data: Record<string, unknown>;
      };
      const place = row("place-1", "places", input.data);
      places = [place];
      days = days.map((candidate) => {
        if (candidate.id !== createPlaceMatch[1]) return candidate;
        const data = candidate.data as Record<string, unknown>;
        return {
          ...candidate,
          version: Number(candidate.version) + 1,
          data: {
            ...data,
            placeEntries: [{ placeId: "place-1" }]
          }
        };
      });
      const day = days.find((candidate) => candidate.id === createPlaceMatch[1]);
      return json(route, {
        place,
        day,
        snapshot: snapshot(2),
        replayed: false
      }, 201);
    }
    const resourceMatch = path.match(
      /^\/trips\/trip-20260810-test01\/([^/]+)$/
    );
    if (resourceMatch) {
      const data: Record<string, Record<string, unknown>[]> = {
        days,
        places,
        segments: [],
        bookings: [],
        "packing-items": [],
        members: [],
        expenses: [],
        "research-evidence": []
      };
      if (resourceMatch[1] === "overview") {
        return json(route, {
          counts: { days: days.length, places: places.length }
        });
      }
      return json(route, { records: data[resourceMatch[1]] ?? [] });
    }
    return json(route, { records: [] });
  });
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

function row(
  id: string,
  resource: string,
  data: Record<string, unknown>,
  position = 0
) {
  return {
    id,
    tripId: "trip-20260810-test01",
    recordKey: id,
    resource,
    position,
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

function snapshot(revision: number) {
  return {
    id: `snapshot-${revision}`,
    revision,
    sha256: "a".repeat(64),
    contentChecksum: "b".repeat(64),
    mirrorState: "mirrored",
    createdAt: "2026-07-28T00:00:00Z"
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
