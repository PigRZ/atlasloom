// 当日摘要强调地点清单，不把可选时间推断成真实执行进度。
import { MapPin } from "@phosphor-icons/react";
import type { RecordRow } from "../../lib/types";
import type { DayPlan } from "./dayModel";

type DayBriefingProps = {
  plan: DayPlan;
  bookings: RecordRow[];
  packing: RecordRow[];
};

export function DayBriefing({
  plan,
  bookings,
  packing
}: DayBriefingProps) {
  const activeBookings = bookings.filter((row) => String(row.data.status ?? "") !== "cancelled");
  const pendingBookings = activeBookings.filter(
    (row) => String(row.data.status ?? "") === "needs_booking"
  ).length;
  const confirmedBookings = activeBookings.filter(
    (row) => String(row.data.status ?? "") === "booked"
  ).length;
  const packedItems = packing.filter((row) => row.data.packed === true).length;
  const placeCount = plan.entries.length;

  return (
    <section className="day-briefing" aria-labelledby="day-briefing-title">
      <div className="day-briefing-main">
        <span className="day-briefing-icon" aria-hidden="true">
          <MapPin size={22} weight="duotone" />
        </span>
        <div className="day-briefing-copy">
          <span>地点优先</span>
          <h2 id="day-briefing-title">
            {placeCount ? `当天有 ${placeCount} 个想去的地点` : "先收集当天想去的地点"}
          </h2>
          <p>顺序只是参考，时间和交通都可以不填，到了现场再决定怎么走。</p>
        </div>
      </div>
      <dl className="day-briefing-facts" aria-label="当天地点概况">
        <div><dt>地点</dt><dd>{placeCount} 个</dd></div>
        <div><dt>交通</dt><dd>{plan.segments.length ? `${plan.segments.length} 段参考` : "可不填"}</dd></div>
        <div>
          <dt>预订</dt>
          <dd>
            {activeBookings.length === 0
              ? "暂无"
              : pendingBookings > 0
                ? `${pendingBookings} 待处理`
                : `${confirmedBookings || activeBookings.length} 已确认`}
          </dd>
        </div>
        <div>
          <dt>我的物品</dt>
          <dd>{packing.length ? `${packedItems}/${packing.length} 已装` : "暂无"}</dd>
        </div>
      </dl>
    </section>
  );
}
