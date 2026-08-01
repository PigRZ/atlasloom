// 地点上下文面板根据规划/在途模式组织现有预订、资料、天气和物品能力。
import {
  ArrowSquareOut,
  Backpack,
  BookOpenText,
  Clock,
  MapPin,
  PencilSimple,
  Plus,
  Ticket
} from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { formatDateTime } from "../../lib/formatters";
import { taxonomyLabel } from "../../lib/taxonomy";
import type { DayData, RecordRow, Trip } from "../../lib/types";
import type { DayPlanEntry, DayWorkspaceMode } from "./dayModel";
import { PlaceWeather } from "./PlaceWeather";

type PlaceContextPanelProps = {
  mode: DayWorkspaceMode;
  trip: Trip;
  day: RecordRow<DayData>;
  entry?: DayPlanEntry;
  bookings: RecordRow[];
  research: RecordRow[];
  packing: RecordRow[];
  navigationURL: string;
  readOnly: boolean;
  canCreateBooking: boolean;
  canCreateResearch: boolean;
  bookingDisabledReason?: string;
  researchDisabledReason?: string;
  onCreateBooking: () => void;
  onEditBooking: (row: RecordRow) => void;
  onCreateResearch: () => void;
  onEditResearch: (row: RecordRow) => void;
};

export function PlaceContextPanel({
  mode,
  trip,
  day,
  entry,
  bookings,
  research,
  packing,
  navigationURL,
  readOnly,
  canCreateBooking,
  canCreateResearch,
  bookingDisabledReason,
  researchDisabledReason,
  onCreateBooking,
  onEditBooking,
  onCreateResearch,
  onEditResearch
}: PlaceContextPanelProps) {
  if (!entry?.place) {
    return (
      <aside className="place-context-panel is-empty" aria-label="地点详情">
        <MapPin aria-hidden="true" size={28} weight="duotone" />
        <h2>选择一个地点</h2>
        <p>从地图或地点清单中选择，这里会显示关联资料。</p>
      </aside>
    );
  }

  const place = entry.place;
  const packedCount = packing.filter((row) => row.data.packed === true).length;
  const dayPackingHref = `/app/trips/${encodeURIComponent(trip.id)}/packing-items?day=${encodeURIComponent(day.id)}`;
  const bookingsHref = `/app/trips/${encodeURIComponent(trip.id)}/bookings`;
  const researchHref = `/app/trips/${encodeURIComponent(trip.id)}/research-evidence?day=${encodeURIComponent(day.id)}&place=${encodeURIComponent(place.id)}`;

  return (
    <aside className="place-context-panel" aria-label={`${place.data.name}地点详情`}>
      <header className="place-context-heading">
        <span>{mode === "travel" ? "在途地点" : "规划地点"}</span>
        <div>
          <i aria-hidden="true">{entry.order}</i>
          <div>
            <h2>{place.data.name}</h2>
            {place.data.address && <p>{place.data.address}</p>}
          </div>
        </div>
      </header>

      {mode === "travel" && (
        <section className="place-travel-actions" aria-label="在途快捷操作">
          {navigationURL ? (
            <a className="primary-button" href={navigationURL} target="_blank" rel="noreferrer">
              <MapPin aria-hidden="true" size={17} weight="fill" />
              在高德导航
              <ArrowSquareOut aria-hidden="true" size={14} />
            </a>
          ) : (
            <p>这个地点还没有可用的导航链接或坐标。</p>
          )}
        </section>
      )}

      {mode === "plan" && (
        <section className="place-plan-actions" aria-label="地点规划操作">
          {!readOnly && (
            <>
              <button
                className="primary-button"
                type="button"
                disabled={!canCreateBooking}
                title={bookingDisabledReason}
                onClick={onCreateBooking}
              >
                <Plus aria-hidden="true" size={16} weight="bold" />添加预订
              </button>
              <button
                className="secondary-button"
                type="button"
                disabled={!canCreateResearch}
                title={researchDisabledReason}
                onClick={onCreateResearch}
              >
                <Plus aria-hidden="true" size={16} weight="bold" />添加攻略
              </button>
            </>
          )}
          {navigationURL && (
            <a href={navigationURL} target="_blank" rel="noreferrer">
              查看地图<ArrowSquareOut aria-hidden="true" size={14} />
            </a>
          )}
        </section>
      )}

      <section className="place-context-facts" aria-label="地点计划信息">
        <div>
          <Clock aria-hidden="true" size={17} />
          <span>开放时间</span>
          <b>{place.data.openingHours || "待补充"}</b>
        </div>
        {(entry.entry.arrivalTime || entry.entry.departureTime) && (
          <div>
            <Clock aria-hidden="true" size={17} />
            <span>时间参考</span>
            <b>{[entry.entry.arrivalTime, entry.entry.departureTime].filter(Boolean).join(" 至 ")}</b>
          </div>
        )}
        {entry.entry.additionalInfo && <p>{entry.entry.additionalInfo}</p>}
      </section>

      {mode === "travel" && <PlaceWeather trip={trip} date={day.data.date} place={place} />}

      <ContextList
        icon={<Ticket aria-hidden="true" size={18} />}
        title="关联预订"
        count={bookings.length}
        href={bookingsHref}
        empty="这个地点还没有预订"
      >
        {bookings.map((booking) => (
          <article key={booking.id}>
            <div>
              <b>{String(booking.data.item ?? booking.recordKey)}</b>
              <p>
                <span>{taxonomyLabel("bookings", "status", booking.data.status) || "未设置状态"}</span>
                {Boolean(booking.data.dateTime) && <time dateTime={String(booking.data.dateTime)}>{formatDateTime(String(booking.data.dateTime))}</time>}
              </p>
            </div>
            <div className="context-row-actions">
              {Boolean(booking.data.link) && (
                <a
                  href={String(booking.data.link)}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={`打开${String(booking.data.item ?? "预订")}订单`}
                >
                  <ArrowSquareOut aria-hidden="true" size={15} />
                </a>
              )}
              {!readOnly && mode === "plan" && (
                <button
                  type="button"
                  aria-label={`编辑${String(booking.data.item ?? "预订")}`}
                  onClick={() => onEditBooking(booking)}
                >
                  <PencilSimple aria-hidden="true" size={15} />
                </button>
              )}
            </div>
          </article>
        ))}
      </ContextList>

      <ContextList
        icon={<BookOpenText aria-hidden="true" size={18} />}
        title="地点攻略"
        count={research.length}
        href={researchHref}
        empty="这个地点还没有攻略"
      >
        {research.map((item) => (
          <article key={item.id}>
            <div>
              <b>{String(item.data.title ?? item.recordKey)}</b>
              <p>{String(item.data.content ?? "")}</p>
            </div>
            {!readOnly && mode === "plan" && (
              <button
                type="button"
                aria-label={`编辑${String(item.data.title ?? "攻略")}`}
                onClick={() => onEditResearch(item)}
              >
                <PencilSimple aria-hidden="true" size={15} />
              </button>
            )}
          </article>
        ))}
      </ContextList>

      <section className="place-packing-link">
        <Backpack aria-hidden="true" size={19} weight="duotone" />
        <div>
          <b>当天物品</b>
          <span>{packing.length ? `${packedCount}/${packing.length} 已携带` : "暂无关联物品"}</span>
        </div>
        <Link to={dayPackingHref}>查看</Link>
      </section>
    </aside>
  );
}

function ContextList({
  icon,
  title,
  count,
  href,
  empty,
  children
}: {
  icon: ReactNode;
  title: string;
  count: number;
  href: string;
  empty: string;
  children: ReactNode;
}) {
  return (
    <section className="place-context-list">
      <header>
        <div>{icon}<h3>{title}</h3><span>{count}</span></div>
        <Link to={href}>全部</Link>
      </header>
      {count ? <div>{children}</div> : <p className="place-context-empty">{empty}</p>}
    </section>
  );
}
