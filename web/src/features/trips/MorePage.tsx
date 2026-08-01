// “更多”集中低频规划与协作入口，常用费用与全局基础资料不在此重复。
import { ArrowRight } from "@phosphor-icons/react";
import { Link } from "react-router";
import { navGroups, pagePathForContext, type NavPage } from "../../app/navigation";
import type { Trip } from "../../lib/types";

export function MorePage({
  trip,
  date
}: {
  trip: Trip;
  date: string;
}) {
  const pages = navGroups.flatMap((group) => (
    group.label === "执行"
      ? []
      : [{
        group: group.label,
        pages: group.pages.filter((page): page is NavPage => page.id !== "expenses")
      }]
  ));
  return (
    <div className="more-page page-enter">
      <section className="page-heading">
        <div><h1>更多</h1></div>
      </section>
      {pages.map((group) => (
        <section className="more-group" key={group.group}>
          <h2>{group.group}</h2>
          <div>
            {group.pages.map((page) => (
              <Link key={page.id} to={pagePathForContext(trip.id, page.id, date)}>
                <page.icon aria-hidden="true" size={20} weight="duotone" />
                <span><b>{page.label}</b></span>
                <ArrowRight aria-hidden="true" size={17} />
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
