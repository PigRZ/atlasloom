// 旅行创建时按日期范围生成完整 Day 骨架，避免聚合根存在但用户无处开始。
package service

import (
	"fmt"
	"time"

	"my-travel/server/internal/model"
)

func initialTripDays(trip model.Trip) ([]model.Record, error) {
	start, err := time.Parse("2006-01-02", trip.StartDate)
	if err != nil {
		return nil, fmt.Errorf("解析旅行开始日期: %w", err)
	}
	end, err := time.Parse("2006-01-02", trip.EndDate)
	if err != nil {
		return nil, fmt.Errorf("解析旅行结束日期: %w", err)
	}
	days := make([]model.Record, 0, int(end.Sub(start)/(24*time.Hour))+1)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		value := date.Format("2006-01-02")
		days = append(days, model.Record{
			TripID:    trip.ID,
			Resource:  model.Days,
			RecordKey: "day-" + date.Format("20060102"),
			Data: map[string]any{
				"title":        "待规划",
				"date":         value,
				"placeEntries": []any{},
			},
		})
	}
	return days, nil
}
