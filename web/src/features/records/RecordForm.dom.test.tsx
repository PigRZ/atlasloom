// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { resources } from "../../lib/resourceDefinitions";
import { RecordForm } from "./RecordForm";

const bookingDefinition = resources.find((resource) => resource.id === "bookings");
if (!bookingDefinition) throw new Error("缺少预订资源定义");
const dayDefinition = resources.find((resource) => resource.id === "days");
if (!dayDefinition) throw new Error("缺少执行日资源定义");
const expenseDefinition = resources.find((resource) => resource.id === "expenses");
if (!expenseDefinition) throw new Error("缺少费用资源定义");
const segmentDefinition = resources.find((resource) => resource.id === "segments");
if (!segmentDefinition) throw new Error("缺少交通段资源定义");

describe("RecordForm 内置分类选项", () => {
  it("调用方只提供地点选项时仍保留交通方式选项", () => {
    render(
      <RecordForm
        definition={segmentDefinition}
        defaults={{
          dayId: "day-a",
          fromPlaceId: "place-a",
          toPlaceId: "place-b",
          mode: ""
        }}
        options={{
          dayId: [{ value: "day-a", label: "第一天" }],
          fromPlaceId: [{ value: "place-a", label: "游客中心" }],
          toPlaceId: [{ value: "place-b", label: "酒店" }]
        }}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
      />
    );

    expect(screen.getByRole("option", { name: "步行" })).toHaveValue("walk");
    expect(screen.getByRole("option", { name: "自驾" })).toHaveValue("drive");
  });
});

describe("RecordForm 预订日期时间", () => {
  it("要求选择预订类型和预订人", () => {
    render(
      <RecordForm
        definition={bookingDefinition}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
      />
    );

    expect(screen.getByLabelText(/预订类型/)).toBeRequired();
    expect(screen.getByLabelText(/预订人/)).toBeRequired();
  });

  it("使用日期时间选择器，并在未修改时间时保留原有复合值", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordForm
        definition={bookingDefinition}
        initial={{
          item: "酒店",
          type: "expense-hotel",
          bookerMemberId: "member-a",
          dateTime: "2026-09-28至09-30"
        }}
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />
    );
    const dateTime = screen.getByLabelText(/日期 \/ 时间/);

    expect(dateTime).toHaveAttribute("type", "datetime-local");
    expect(dateTime).toHaveValue("");
    expect(screen.getByText(/当前记录为“2026-09-28至09-30”/)).toBeVisible();

    fireEvent.change(screen.getByLabelText(/项目/), { target: { value: "酒店预订" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit.mock.calls[0]?.[1]).toMatchObject({
      item: "酒店预订",
      dateTime: "2026-09-28至09-30"
    });
  });

  it("选择日期时间后提交精确到分钟的本地值", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordForm
        definition={bookingDefinition}
        initial={{
          item: "景区门票",
          type: "expense-ticket",
          bookerMemberId: "member-a",
          dateTime: "2026-09-28；08:00→17:00"
        }}
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />
    );
    const dateTime = screen.getByLabelText(/日期 \/ 时间/);

    fireEvent.change(dateTime, { target: { value: "2026-09-28T08:30" } });
    expect(screen.queryByText(/当前记录为/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit.mock.calls[0]?.[1]).toMatchObject({
      item: "景区门票",
      dateTime: "2026-09-28T08:30"
    });
  });
});

describe("RecordForm 自动费用", () => {
  it("锁定来源预订同步的费用类型和支付人", () => {
    render(
      <RecordForm
        definition={expenseDefinition}
        initial={{
          item: "酒店",
          type: "expense-hotel",
          payerMemberId: "member-a",
          amountMinor: 12000,
          splitMode: "all"
        }}
        readOnlyFields={["type", "payerMemberId"]}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
      />
    );

    expect(screen.getByLabelText(/费用类型/)).toBeDisabled();
    expect(screen.getByLabelText(/支付人/)).toBeDisabled();
    expect(screen.getByLabelText(/金额/)).not.toBeDisabled();
  });

  it("逐键保留小数金额，并只在提交时转换为分", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordForm
        definition={expenseDefinition}
        initial={{
          item: "景区门票",
          type: "expense-ticket",
          payerMemberId: "member-a",
          amountMinor: 1234,
          splitMode: "all"
        }}
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />
    );
    const amount = screen.getByLabelText(/金额/);

    expect(amount).toHaveValue(12.34);
    await user.clear(amount);
    await user.type(amount, "12.34");
    expect(amount).toHaveValue(12.34);

    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce());
    expect(onSubmit.mock.calls[0]?.[1]).toMatchObject({
      amountMinor: 1234
    });
  });
});

describe("RecordForm 地点快捷创建", () => {
  it("预订创建地点后立即加入选项并自动选中", () => {
    const onCreatePlace = vi.fn((
      _targetLabel: string,
      onCreated: (option: { value: string; label: string }) => void
    ) => onCreated({ value: "place-new", label: "新建车站" }));
    render(
      <RecordForm
        definition={bookingDefinition}
        options={{
          placeId: [{ value: "place-old", label: "原有酒店" }]
        }}
        onCreatePlace={onCreatePlace}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole("button", {
      name: "新增地点并选为关联地点"
    }));

    expect(onCreatePlace).toHaveBeenCalledWith("关联地点", expect.any(Function));
    const placeSelect = screen.getByRole("combobox", { name: /^关联地点/ });
    expect(placeSelect).toHaveValue("place-new");
    expect(placeSelect.querySelector('option[value="place-new"]')).toHaveTextContent(
      "新建车站"
    );
    expect(screen.getByText(
      "已创建并选择地点“新建车站”作为关联地点"
    )).toBeInTheDocument();
  });

  it("交通段的起点和终点都提供独立新增入口", () => {
    render(
      <RecordForm
        definition={segmentDefinition}
        onCreatePlace={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        onCancel={vi.fn()}
      />
    );

    expect(screen.getByRole("button", {
      name: "新增地点并选为起点地点"
    })).toBeVisible();
    expect(screen.getByRole("button", {
      name: "新增地点并选为终点地点"
    })).toBeVisible();
  });
});

describe("RecordForm 执行日范围", () => {
  it("在提交前阻止超出旅行日期范围的执行日", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const { container } = render(
      <RecordForm
        definition={dayDefinition}
        initial={{ title: "越界日程", date: "2026-09-29" }}
        fieldConstraints={{
          date: {
            min: "2026-09-25",
            max: "2026-09-28",
            helperText: "旅行日期范围：2026-09-25 至 2026-09-28",
            errorMessage: "执行日必须位于旅行日期范围 2026-09-25 至 2026-09-28 内"
          }
        }}
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />
    );

    expect(container.querySelector('input[name="date"]'))
      .toHaveAttribute("max", "2026-09-28");
    fireEvent.submit(container.querySelector("form")!);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "执行日必须位于旅行日期范围 2026-09-25 至 2026-09-28 内"
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
