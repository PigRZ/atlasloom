// AI 旅程计划编译器把可审查的简化 JSON 转为当前模型的完整旅行备份。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

type buildTripBackupConfig struct {
	inputPath  string
	outputPath string
}

type tripPlan struct {
	Trip             planTrip               `json:"trip"`
	Tags             []planTag              `json:"tags"`
	Members          []planMember           `json:"members"`
	Days             []planDay              `json:"days"`
	Places           []planPlace            `json:"places"`
	Segments         []planSegment          `json:"segments"`
	Bookings         []planBooking          `json:"bookings"`
	Expenses         []planExpense          `json:"expenses"`
	PackingItems     []planPackingItem      `json:"packingItems"`
	ResearchEvidence []planResearchEvidence `json:"researchEvidence"`
}

type planTrip struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Timezone  string `json:"timezone,omitempty"`
	Currency  string `json:"currency,omitempty"`
}

type planTag struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Color    string `json:"color,omitempty"`
}

type planMember struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"`
	Note        string `json:"note,omitempty"`
}

type planDay struct {
	Key            string           `json:"key"`
	Title          string           `json:"title"`
	Date           string           `json:"date"`
	PlaceEntries   []planPlaceEntry `json:"placeEntries"`
	AdditionalInfo string           `json:"additionalInfo,omitempty"`
}

type planPlaceEntry struct {
	PlaceKey       string `json:"placeKey"`
	ArrivalTime    string `json:"arrivalTime,omitempty"`
	DepartureTime  string `json:"departureTime,omitempty"`
	AdditionalInfo string `json:"additionalInfo,omitempty"`
}

type planPlace struct {
	Key                       string   `json:"key"`
	Name                      string   `json:"name"`
	TypeTagKey                string   `json:"typeTagKey"`
	Address                   string   `json:"address,omitempty"`
	Region                    string   `json:"region,omitempty"`
	Latitude                  *float64 `json:"latitude,omitempty"`
	Longitude                 *float64 `json:"longitude,omitempty"`
	WeatherAdcode             string   `json:"weatherAdcode,omitempty"`
	NavigationURL             string   `json:"navigationURL,omitempty"`
	OpeningHours              string   `json:"openingHours,omitempty"`
	RecommendedTicketPlatform string   `json:"recommendedTicketPlatform,omitempty"`
}

type planSegment struct {
	Key             string `json:"key"`
	DayKey          string `json:"dayKey"`
	FromPlaceKey    string `json:"fromPlaceKey"`
	ToPlaceKey      string `json:"toPlaceKey"`
	Mode            string `json:"mode"`
	DepartureTime   string `json:"departureTime,omitempty"`
	DurationMinutes *int64 `json:"durationMinutes,omitempty"`
	DistanceMeters  *int64 `json:"distanceMeters,omitempty"`
	NavigationURL   string `json:"navigationURL,omitempty"`
	AdditionalInfo  string `json:"additionalInfo,omitempty"`
}

type planBooking struct {
	Key             string `json:"key"`
	Item            string `json:"item"`
	TypeTagKey      string `json:"typeTagKey"`
	BookerMemberKey string `json:"bookerMemberKey"`
	PlaceKey        string `json:"placeKey,omitempty"`
	PlatformTagKey  string `json:"platformTagKey,omitempty"`
	DateTime        string `json:"dateTime,omitempty"`
	Status          string `json:"status,omitempty"`
	Link            string `json:"link,omitempty"`
	Notes           string `json:"notes,omitempty"`
}

type planExpense struct {
	Key             string   `json:"key"`
	Item            string   `json:"item"`
	OccurredAt      string   `json:"occurredAt,omitempty"`
	TypeTagKey      string   `json:"typeTagKey"`
	AmountMinor     *int64   `json:"amountMinor,omitempty"`
	PayerMemberKey  string   `json:"payerMemberKey,omitempty"`
	SplitMode       string   `json:"splitMode"`
	SplitMemberKeys []string `json:"splitMemberKeys"`
	BookingKey      string   `json:"bookingKey,omitempty"`
	Notes           string   `json:"notes,omitempty"`
}

type planPackingItem struct {
	Key            string   `json:"key"`
	Item           string   `json:"item"`
	Category       string   `json:"category,omitempty"`
	DayKeys        []string `json:"dayKeys,omitempty"`
	OwnerMemberKey string   `json:"ownerMemberKey,omitempty"`
	Quantity       *int64   `json:"quantity,omitempty"`
	Packed         *bool    `json:"packed,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

type planResearchEvidence struct {
	Key                 string   `json:"key"`
	Title               string   `json:"title"`
	Content             string   `json:"content,omitempty"`
	PlatformTagKey      string   `json:"platformTagKey,omitempty"`
	RecordedByMemberKey string   `json:"recordedByMemberKey,omitempty"`
	DayKeys             []string `json:"dayKeys,omitempty"`
	PlaceKey            string   `json:"placeKey,omitempty"`
}

type buildTripBackupSummary struct {
	Valid           bool           `json:"valid"`
	TripID          string         `json:"tripId"`
	ContentChecksum string         `json:"contentChecksum"`
	RecordCounts    map[string]int `json:"recordCounts"`
	Output          string         `json:"output"`
}

type planCompiler struct {
	plan        tripPlan
	now         time.Time
	tags        map[string]planTag
	members     map[string]planMember
	days        map[string]planDay
	places      map[string]planPlace
	bookings    map[string]planBooking
	usedTagKeys map[string]struct{}
}

func parseBuildTripBackupFlags(args []string, stderr io.Writer) (buildTripBackupConfig, error) {
	flags := flag.NewFlagSet("build-trip-backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config buildTripBackupConfig
	flags.StringVar(&config.inputPath, "input", "", "AI trip plan JSON")
	flags.StringVar(&config.outputPath, "output", "", "new trip backup JSON")
	if err := flags.Parse(args); err != nil {
		return buildTripBackupConfig{}, err
	}
	if flags.NArg() != 0 ||
		strings.TrimSpace(config.inputPath) == "" ||
		strings.TrimSpace(config.outputPath) == "" {
		fmt.Fprintln(stderr, "usage: travelctl build-trip-backup --input PLAN.json --output BACKUP.json")
		return buildTripBackupConfig{}, errors.New("invalid build-trip-backup arguments")
	}
	return config, nil
}

func buildTripBackupFile(inputPath, outputPath string) (buildTripBackupSummary, error) {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return buildTripBackupSummary{}, fmt.Errorf("读取 AI 旅程计划失败: %w", err)
	}
	plan, err := decodeTripPlan(raw)
	if err != nil {
		return buildTripBackupSummary{}, err
	}
	backup, err := compileTripPlan(plan, time.Now().UTC())
	if err != nil {
		return buildTripBackupSummary{}, fmt.Errorf("AI 旅程计划无效: %w", err)
	}
	encoded, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return buildTripBackupSummary{}, fmt.Errorf("编码旅行备份失败: %w", err)
	}
	encoded = append(encoded, '\n')
	validated, err := repository.ValidateTripBackup(encoded)
	if err != nil {
		return buildTripBackupSummary{}, fmt.Errorf("生成结果未通过当前旅行备份契约: %w", err)
	}
	if err := writeFileExclusive(outputPath, encoded); err != nil {
		return buildTripBackupSummary{}, err
	}
	counts := make(map[string]int, len(model.Resources()))
	for _, resource := range model.Resources() {
		counts[string(resource)] = len(validated.Snapshot.Datasets[string(resource)])
	}
	return buildTripBackupSummary{
		Valid:           true,
		TripID:          validated.Snapshot.Trip.ID,
		ContentChecksum: validated.ContentChecksum,
		RecordCounts:    counts,
		Output:          outputPath,
	}, nil
}

func decodeTripPlan(raw []byte) (tripPlan, error) {
	var plan tripPlan
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return tripPlan{}, fmt.Errorf("AI 旅程计划 JSON 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return tripPlan{}, errors.New("AI 旅程计划必须且只能包含一个 JSON 对象")
	}
	return plan, nil
}

func compileTripPlan(plan tripPlan, now time.Time) (model.TripBackup, error) {
	compiler := &planCompiler{
		plan:        plan,
		now:         now,
		tags:        make(map[string]planTag),
		members:     make(map[string]planMember),
		days:        make(map[string]planDay),
		places:      make(map[string]planPlace),
		bookings:    make(map[string]planBooking),
		usedTagKeys: make(map[string]struct{}),
	}
	trip := compiler.buildTrip()
	if err := trip.Validate(); err != nil {
		return model.TripBackup{}, fmt.Errorf("trip: %w", err)
	}
	if err := compiler.indexDefinitions(); err != nil {
		return model.TripBackup{}, err
	}
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		datasets[string(resource)] = []model.Record{}
	}
	if err := compiler.buildRecords(trip, datasets); err != nil {
		return model.TripBackup{}, err
	}
	if err := compiler.rejectUnusedTags(); err != nil {
		return model.TripBackup{}, err
	}
	snapshot := model.SnapshotPayload{
		ExportedAt: now,
		Reason:     "ai-trip-plan",
		Revision:   trip.Version,
		Trip:       trip,
		Datasets:   datasets,
	}
	checksum, err := snapshot.ComputedContentChecksum()
	if err != nil {
		return model.TripBackup{}, err
	}
	snapshot.ContentChecksum = checksum
	backup := model.TripBackup{
		Kind:         model.TripBackupKind,
		ExportedAt:   now,
		Snapshot:     snapshot,
		Dependencies: compiler.buildDependencies(),
	}
	backup.ContentChecksum, err = backup.ComputedContentChecksum()
	if err != nil {
		return model.TripBackup{}, err
	}
	raw, err := json.Marshal(backup)
	if err != nil {
		return model.TripBackup{}, err
	}
	if _, err := repository.ValidateTripBackup(raw); err != nil {
		return model.TripBackup{}, err
	}
	return backup, nil
}

func (c *planCompiler) indexDefinitions() error {
	for index, tag := range c.plan.Tags {
		if err := validatePlanKey("tags", index, tag.Key); err != nil {
			return err
		}
		if strings.TrimSpace(tag.Name) == "" {
			return fmt.Errorf("tags[%d].name 不能为空", index)
		}
		switch tag.Category {
		case model.TagCategoryPlaceType, model.TagCategoryExpenseType, model.TagCategoryPlatformType:
		default:
			return fmt.Errorf(
				"tags[%d].category 必须是 place-type、expense-type 或 platform-type",
				index,
			)
		}
		if _, exists := c.tags[tag.Key]; exists {
			return fmt.Errorf("tags 中的 key %q 重复", tag.Key)
		}
		if err := model.ValidateRecordID(tagSourceID(tag.Key)); err != nil {
			return fmt.Errorf("tags[%d].key 加前缀后过长或无效: %w", index, err)
		}
		c.tags[tag.Key] = tag
	}
	for index, member := range c.plan.Members {
		if err := validatePlanKey("members", index, member.Key); err != nil {
			return err
		}
		if strings.TrimSpace(member.DisplayName) == "" {
			return fmt.Errorf("members[%d].displayName 不能为空", index)
		}
		if _, exists := c.members[member.Key]; exists {
			return fmt.Errorf("members 中的 key %q 重复", member.Key)
		}
		if err := model.ValidateRecordID(memberSourceID(member.Key)); err != nil {
			return fmt.Errorf("members[%d].key 加前缀后过长或无效: %w", index, err)
		}
		if err := model.ValidateRecordID(recordID("trip-member", member.Key)); err != nil {
			return fmt.Errorf("members[%d].key 加前缀后过长或无效: %w", index, err)
		}
		c.members[member.Key] = member
	}
	if len(c.members) == 0 {
		return errors.New("members 至少需要一个待映射的同行成员")
	}
	for index, day := range c.plan.Days {
		if err := validatePlanKey("days", index, day.Key); err != nil {
			return err
		}
		if _, exists := c.days[day.Key]; exists {
			return fmt.Errorf("days 中的 key %q 重复", day.Key)
		}
		if err := model.ValidateRecordID(recordID("day", day.Key)); err != nil {
			return fmt.Errorf("days[%d].key 加前缀后过长或无效: %w", index, err)
		}
		c.days[day.Key] = day
	}
	for index, place := range c.plan.Places {
		if err := validatePlanKey("places", index, place.Key); err != nil {
			return err
		}
		if _, exists := c.places[place.Key]; exists {
			return fmt.Errorf("places 中的 key %q 重复", place.Key)
		}
		if err := model.ValidateRecordID(recordID("place", place.Key)); err != nil {
			return fmt.Errorf("places[%d].key 加前缀后过长或无效: %w", index, err)
		}
		c.places[place.Key] = place
	}
	for index, booking := range c.plan.Bookings {
		if err := validatePlanKey("bookings", index, booking.Key); err != nil {
			return err
		}
		if _, exists := c.bookings[booking.Key]; exists {
			return fmt.Errorf("bookings 中的 key %q 重复", booking.Key)
		}
		if err := model.ValidateRecordID(recordID("booking", booking.Key)); err != nil {
			return fmt.Errorf("bookings[%d].key 加前缀后过长或无效: %w", index, err)
		}
		c.bookings[booking.Key] = booking
	}
	return c.validateTripDays()
}

func (c *planCompiler) validateTripDays() error {
	start, err := time.Parse("2006-01-02", c.plan.Trip.StartDate)
	if err != nil {
		return errors.New("trip.startDate 必须是 YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", c.plan.Trip.EndDate)
	if err != nil {
		return errors.New("trip.endDate 必须是 YYYY-MM-DD")
	}
	if end.Before(start) {
		return errors.New("trip.endDate 不能早于 trip.startDate")
	}
	expected := make(map[string]struct{})
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		expected[date.Format("2006-01-02")] = struct{}{}
	}
	seen := make(map[string]struct{}, len(c.plan.Days))
	for index, day := range c.plan.Days {
		if strings.TrimSpace(day.Title) == "" {
			return fmt.Errorf("days[%d].title 不能为空", index)
		}
		if _, ok := expected[day.Date]; !ok {
			return fmt.Errorf("days[%d].date %q 不在旅行日期范围内", index, day.Date)
		}
		if _, duplicate := seen[day.Date]; duplicate {
			return fmt.Errorf("days 中的日期 %q 重复", day.Date)
		}
		seen[day.Date] = struct{}{}
	}
	for date := range expected {
		if _, ok := seen[date]; !ok {
			return fmt.Errorf("days 缺少旅行日期 %s", date)
		}
	}
	return nil
}

func (c *planCompiler) buildTrip() model.Trip {
	timezone := c.plan.Trip.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	currency := c.plan.Trip.Currency
	if currency == "" {
		currency = "CNY"
	}
	return model.Trip{
		ID:        c.plan.Trip.ID,
		Title:     c.plan.Trip.Title,
		Summary:   c.plan.Trip.Summary,
		StartDate: c.plan.Trip.StartDate,
		EndDate:   c.plan.Trip.EndDate,
		Timezone:  timezone,
		Currency:  currency,
		Version:   1,
		CreatedAt: c.now,
		UpdatedAt: c.now,
	}
}

func (c *planCompiler) buildRecords(trip model.Trip, datasets map[string][]model.Record) error {
	if err := c.buildMemberRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildPlaceRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildDayRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildSegmentRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildBookingRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildExpenseRecords(trip, datasets); err != nil {
		return err
	}
	if err := c.buildPackingRecords(trip, datasets); err != nil {
		return err
	}
	return c.buildResearchRecords(trip, datasets)
}

func (c *planCompiler) buildMemberRecords(trip model.Trip, datasets map[string][]model.Record) error {
	for index, member := range c.plan.Members {
		role := "editor"
		if index == 0 {
			role = "owner"
		}
		data := map[string]any{
			"memberId": memberSourceID(member.Key),
			"role":     role,
		}
		addOptionalString(data, "note", member.Note)
		row, err := c.newRecord(trip, model.Members, member.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("members[%d]: %w", index, err)
		}
		row.ID = recordID("trip-member", member.Key)
		datasets[string(model.Members)] = append(datasets[string(model.Members)], row)
	}
	return nil
}

func (c *planCompiler) buildPlaceRecords(trip model.Trip, datasets map[string][]model.Record) error {
	for index, place := range c.plan.Places {
		if strings.TrimSpace(place.Name) == "" {
			return fmt.Errorf("places[%d].name 不能为空", index)
		}
		tagID, err := c.requireTag(
			place.TypeTagKey,
			model.TagCategoryPlaceType,
			fmt.Sprintf("places[%d].typeTagKey", index),
		)
		if err != nil {
			return err
		}
		data := map[string]any{"name": place.Name, "type": tagID}
		addOptionalString(data, "address", place.Address)
		addOptionalString(data, "region", place.Region)
		addOptionalString(data, "weatherAdcode", place.WeatherAdcode)
		addOptionalString(data, "navigationURL", place.NavigationURL)
		addOptionalString(data, "openingHours", place.OpeningHours)
		addOptionalString(data, "recommendedTicketPlatform", place.RecommendedTicketPlatform)
		if place.Latitude != nil {
			data["latitude"] = *place.Latitude
		}
		if place.Longitude != nil {
			data["longitude"] = *place.Longitude
		}
		row, err := c.newRecord(trip, model.Places, place.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("places[%d]: %w", index, err)
		}
		datasets[string(model.Places)] = append(datasets[string(model.Places)], row)
	}
	return nil
}

func (c *planCompiler) buildDayRecords(trip model.Trip, datasets map[string][]model.Record) error {
	days := append([]planDay(nil), c.plan.Days...)
	sort.SliceStable(days, func(left, right int) bool {
		return days[left].Date < days[right].Date
	})
	for index, day := range days {
		seenPlaces := make(map[string]struct{}, len(day.PlaceEntries))
		entries := make([]map[string]any, 0, len(day.PlaceEntries))
		for entryIndex, entry := range day.PlaceEntries {
			if _, exists := c.places[entry.PlaceKey]; !exists {
				return fmt.Errorf(
					"days[%s].placeEntries[%d].placeKey 引用了不存在的地点 %q",
					day.Key,
					entryIndex,
					entry.PlaceKey,
				)
			}
			if _, duplicate := seenPlaces[entry.PlaceKey]; duplicate {
				return fmt.Errorf("days[%s] 重复关联地点 %q", day.Key, entry.PlaceKey)
			}
			seenPlaces[entry.PlaceKey] = struct{}{}
			value := map[string]any{"placeId": recordID("place", entry.PlaceKey)}
			addOptionalString(value, "arrivalTime", entry.ArrivalTime)
			addOptionalString(value, "departureTime", entry.DepartureTime)
			addOptionalString(value, "additionalInfo", entry.AdditionalInfo)
			entries = append(entries, value)
		}
		data := map[string]any{
			"title":        day.Title,
			"date":         day.Date,
			"placeEntries": entries,
		}
		addOptionalString(data, "additionalInfo", day.AdditionalInfo)
		row, err := c.newRecord(trip, model.Days, day.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("days[%s]: %w", day.Key, err)
		}
		datasets[string(model.Days)] = append(datasets[string(model.Days)], row)
	}
	return nil
}

func (c *planCompiler) buildSegmentRecords(trip model.Trip, datasets map[string][]model.Record) error {
	positions := make(map[string]int64)
	seen := make(map[string]struct{}, len(c.plan.Segments))
	for index, segment := range c.plan.Segments {
		if err := validatePlanKey("segments", index, segment.Key); err != nil {
			return err
		}
		if _, duplicate := seen[segment.Key]; duplicate {
			return fmt.Errorf("segments 中的 key %q 重复", segment.Key)
		}
		seen[segment.Key] = struct{}{}
		if _, exists := c.days[segment.DayKey]; !exists {
			return fmt.Errorf("segments[%d].dayKey 引用了不存在的执行日 %q", index, segment.DayKey)
		}
		for field, key := range map[string]string{
			"fromPlaceKey": segment.FromPlaceKey,
			"toPlaceKey":   segment.ToPlaceKey,
		} {
			if _, exists := c.places[key]; !exists {
				return fmt.Errorf("segments[%d].%s 引用了不存在的地点 %q", index, field, key)
			}
		}
		if !dayContainsPlace(c.days[segment.DayKey], segment.FromPlaceKey) ||
			!dayContainsPlace(c.days[segment.DayKey], segment.ToPlaceKey) {
			return fmt.Errorf("segments[%d] 的起终点必须都已关联到执行日 %q", index, segment.DayKey)
		}
		data := map[string]any{
			"dayId":       recordID("day", segment.DayKey),
			"fromPlaceId": recordID("place", segment.FromPlaceKey),
			"toPlaceId":   recordID("place", segment.ToPlaceKey),
			"mode":        segment.Mode,
		}
		addOptionalString(data, "departureTime", segment.DepartureTime)
		addOptionalString(data, "navigationURL", segment.NavigationURL)
		addOptionalString(data, "additionalInfo", segment.AdditionalInfo)
		if segment.DurationMinutes != nil {
			data["durationMinutes"] = *segment.DurationMinutes
		}
		if segment.DistanceMeters != nil {
			data["distanceMeters"] = *segment.DistanceMeters
		}
		position := positions[segment.DayKey]
		positions[segment.DayKey] = position + 1
		row, err := c.newRecord(trip, model.Segments, segment.Key, position, data)
		if err != nil {
			return fmt.Errorf("segments[%d]: %w", index, err)
		}
		datasets[string(model.Segments)] = append(datasets[string(model.Segments)], row)
	}
	return nil
}

func (c *planCompiler) buildBookingRecords(trip model.Trip, datasets map[string][]model.Record) error {
	for index, booking := range c.plan.Bookings {
		if strings.TrimSpace(booking.Item) == "" {
			return fmt.Errorf("bookings[%d].item 不能为空", index)
		}
		typeID, err := c.requireTag(
			booking.TypeTagKey,
			model.TagCategoryExpenseType,
			fmt.Sprintf("bookings[%d].typeTagKey", index),
		)
		if err != nil {
			return err
		}
		bookerID, err := c.requireMember(
			booking.BookerMemberKey,
			fmt.Sprintf("bookings[%d].bookerMemberKey", index),
		)
		if err != nil {
			return err
		}
		data := map[string]any{
			"item": booking.Item, "type": typeID, "bookerMemberId": bookerID,
		}
		if booking.PlaceKey != "" {
			if _, exists := c.places[booking.PlaceKey]; !exists {
				return fmt.Errorf("bookings[%d].placeKey 引用了不存在的地点 %q", index, booking.PlaceKey)
			}
			data["placeId"] = recordID("place", booking.PlaceKey)
		}
		if booking.PlatformTagKey != "" {
			tagID, err := c.requireTag(
				booking.PlatformTagKey,
				model.TagCategoryPlatformType,
				fmt.Sprintf("bookings[%d].platformTagKey", index),
			)
			if err != nil {
				return err
			}
			data["platformId"] = tagID
		}
		if booking.DateTime != "" {
			if err := validateLocalDateTime(booking.DateTime); err != nil {
				return fmt.Errorf("bookings[%d].dateTime: %w", index, err)
			}
			data["dateTime"] = booking.DateTime
		}
		status := booking.Status
		if status == "" {
			status = "needs_booking"
		}
		data["status"] = status
		addOptionalString(data, "link", booking.Link)
		addOptionalString(data, "notes", booking.Notes)
		row, err := c.newRecord(trip, model.Bookings, booking.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("bookings[%d]: %w", index, err)
		}
		datasets[string(model.Bookings)] = append(datasets[string(model.Bookings)], row)
	}
	return nil
}

func (c *planCompiler) buildExpenseRecords(trip model.Trip, datasets map[string][]model.Record) error {
	seen := make(map[string]struct{}, len(c.plan.Expenses))
	linkedBookings := make(map[string]struct{}, len(c.plan.Bookings))
	for index, expense := range c.plan.Expenses {
		if err := validatePlanKey("expenses", index, expense.Key); err != nil {
			return err
		}
		if _, duplicate := seen[expense.Key]; duplicate {
			return fmt.Errorf("expenses 中的 key %q 重复", expense.Key)
		}
		seen[expense.Key] = struct{}{}
		if strings.TrimSpace(expense.Item) == "" {
			return fmt.Errorf("expenses[%d].item 不能为空", index)
		}
		typeTagKey := expense.TypeTagKey
		payerMemberKey := expense.PayerMemberKey
		if expense.BookingKey != "" {
			booking, exists := c.bookings[expense.BookingKey]
			if !exists {
				return fmt.Errorf("expenses[%d].bookingKey 引用了不存在的预订 %q", index, expense.BookingKey)
			}
			if typeTagKey != "" && typeTagKey != booking.TypeTagKey {
				return fmt.Errorf(
					"expenses[%d].typeTagKey 必须与关联预订 %q 完全一致",
					index,
					expense.BookingKey,
				)
			}
			if payerMemberKey != "" && payerMemberKey != booking.BookerMemberKey {
				return fmt.Errorf(
					"expenses[%d].payerMemberKey 必须与关联预订 %q 的预订人完全一致",
					index,
					expense.BookingKey,
				)
			}
			typeTagKey = booking.TypeTagKey
			payerMemberKey = booking.BookerMemberKey
		}
		tagID, err := c.requireTag(
			typeTagKey,
			model.TagCategoryExpenseType,
			fmt.Sprintf("expenses[%d].typeTagKey", index),
		)
		if err != nil {
			return err
		}
		data := map[string]any{
			"item":           expense.Item,
			"type":           tagID,
			"splitMode":      expense.SplitMode,
			"splitMemberIds": []string{},
		}
		if expense.OccurredAt != "" {
			if err := validateLocalDateTime(expense.OccurredAt); err != nil {
				return fmt.Errorf("expenses[%d].occurredAt: %w", index, err)
			}
			data["occurredAt"] = expense.OccurredAt
		}
		addOptionalString(data, "notes", expense.Notes)
		if expense.AmountMinor != nil {
			data["amountMinor"] = *expense.AmountMinor
		}
		if payerMemberKey != "" {
			memberID, err := c.requireMember(
				payerMemberKey,
				fmt.Sprintf("expenses[%d].payerMemberKey", index),
			)
			if err != nil {
				return err
			}
			data["payerMemberId"] = memberID
		}
		splitIDs := make([]string, 0, len(expense.SplitMemberKeys))
		seenMembers := make(map[string]struct{}, len(expense.SplitMemberKeys))
		for splitIndex, memberKey := range expense.SplitMemberKeys {
			if _, duplicate := seenMembers[memberKey]; duplicate {
				return fmt.Errorf("expenses[%d].splitMemberKeys 重复包含 %q", index, memberKey)
			}
			seenMembers[memberKey] = struct{}{}
			memberID, err := c.requireMember(
				memberKey,
				fmt.Sprintf("expenses[%d].splitMemberKeys[%d]", index, splitIndex),
			)
			if err != nil {
				return err
			}
			splitIDs = append(splitIDs, memberID)
		}
		data["splitMemberIds"] = splitIDs
		if expense.BookingKey != "" {
			booking := c.bookings[expense.BookingKey]
			if expense.Item != booking.Item {
				return fmt.Errorf(
					"expenses[%d].item 必须与关联预订 %q 的 item 完全一致",
					index,
					expense.BookingKey,
				)
			}
			if _, duplicate := linkedBookings[expense.BookingKey]; duplicate {
				return fmt.Errorf("预订 %q 被多笔费用重复关联", expense.BookingKey)
			}
			linkedBookings[expense.BookingKey] = struct{}{}
			data["bookingRecordId"] = recordID("booking", expense.BookingKey)
		}
		row, err := c.newRecord(trip, model.Expenses, expense.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("expenses[%d]: %w", index, err)
		}
		datasets[string(model.Expenses)] = append(datasets[string(model.Expenses)], row)
	}
	for key := range c.bookings {
		if _, linked := linkedBookings[key]; !linked {
			return fmt.Errorf("预订 %q 缺少一笔通过 bookingKey 关联的费用", key)
		}
	}
	return nil
}

func (c *planCompiler) buildPackingRecords(trip model.Trip, datasets map[string][]model.Record) error {
	seen := make(map[string]struct{}, len(c.plan.PackingItems))
	for index, item := range c.plan.PackingItems {
		if err := validatePlanKey("packingItems", index, item.Key); err != nil {
			return err
		}
		if _, duplicate := seen[item.Key]; duplicate {
			return fmt.Errorf("packingItems 中的 key %q 重复", item.Key)
		}
		seen[item.Key] = struct{}{}
		if strings.TrimSpace(item.Item) == "" {
			return fmt.Errorf("packingItems[%d].item 不能为空", index)
		}
		data := map[string]any{"item": item.Item}
		addOptionalString(data, "category", item.Category)
		addOptionalString(data, "notes", item.Notes)
		dayIDs, err := c.dayIDs(item.DayKeys, fmt.Sprintf("packingItems[%d].dayKeys", index))
		if err != nil {
			return err
		}
		if len(dayIDs) > 0 {
			data["dayIds"] = dayIDs
		}
		if item.OwnerMemberKey != "" {
			memberID, err := c.requireMember(
				item.OwnerMemberKey,
				fmt.Sprintf("packingItems[%d].ownerMemberKey", index),
			)
			if err != nil {
				return err
			}
			data["ownerMemberId"] = memberID
		}
		if item.Quantity != nil {
			data["quantity"] = *item.Quantity
		}
		if item.Packed != nil {
			data["packed"] = *item.Packed
		}
		row, err := c.newRecord(trip, model.PackingItems, item.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("packingItems[%d]: %w", index, err)
		}
		datasets[string(model.PackingItems)] = append(datasets[string(model.PackingItems)], row)
	}
	return nil
}

func (c *planCompiler) buildResearchRecords(trip model.Trip, datasets map[string][]model.Record) error {
	seen := make(map[string]struct{}, len(c.plan.ResearchEvidence))
	for index, evidence := range c.plan.ResearchEvidence {
		if err := validatePlanKey("researchEvidence", index, evidence.Key); err != nil {
			return err
		}
		if _, duplicate := seen[evidence.Key]; duplicate {
			return fmt.Errorf("researchEvidence 中的 key %q 重复", evidence.Key)
		}
		seen[evidence.Key] = struct{}{}
		if strings.TrimSpace(evidence.Title) == "" {
			return fmt.Errorf("researchEvidence[%d].title 不能为空", index)
		}
		data := map[string]any{"title": evidence.Title}
		addOptionalString(data, "content", evidence.Content)
		if evidence.PlatformTagKey != "" {
			tagID, err := c.requireTag(
				evidence.PlatformTagKey,
				model.TagCategoryPlatformType,
				fmt.Sprintf("researchEvidence[%d].platformTagKey", index),
			)
			if err != nil {
				return err
			}
			data["platformId"] = tagID
		}
		if evidence.RecordedByMemberKey != "" {
			memberID, err := c.requireMember(
				evidence.RecordedByMemberKey,
				fmt.Sprintf("researchEvidence[%d].recordedByMemberKey", index),
			)
			if err != nil {
				return err
			}
			data["recordedByMemberId"] = memberID
		}
		dayIDs, err := c.dayIDs(
			evidence.DayKeys,
			fmt.Sprintf("researchEvidence[%d].dayKeys", index),
		)
		if err != nil {
			return err
		}
		if len(dayIDs) > 0 {
			data["dayIds"] = dayIDs
		}
		if evidence.PlaceKey != "" {
			if _, exists := c.places[evidence.PlaceKey]; !exists {
				return fmt.Errorf(
					"researchEvidence[%d].placeKey 引用了不存在的地点 %q",
					index,
					evidence.PlaceKey,
				)
			}
			data["placeId"] = recordID("place", evidence.PlaceKey)
		}
		row, err := c.newRecord(trip, model.ResearchEvidence, evidence.Key, int64(index), data)
		if err != nil {
			return fmt.Errorf("researchEvidence[%d]: %w", index, err)
		}
		datasets[string(model.ResearchEvidence)] = append(
			datasets[string(model.ResearchEvidence)],
			row,
		)
	}
	return nil
}

func (c *planCompiler) newRecord(
	trip model.Trip,
	resource model.Resource,
	key string,
	position int64,
	data map[string]any,
) (model.Record, error) {
	if err := model.ValidateRecordKey(key); err != nil {
		return model.Record{}, err
	}
	if err := model.ValidateRecordData(resource, data); err != nil {
		return model.Record{}, err
	}
	actorMemberID := memberSourceID(c.plan.Members[0].Key)
	return model.Record{
		ID:                recordID(strings.TrimSuffix(string(resource), "s"), key),
		TripID:            trip.ID,
		Resource:          resource,
		RecordKey:         key,
		Position:          position,
		Version:           1,
		Data:              data,
		CreatedByMemberID: actorMemberID,
		UpdatedByMemberID: actorMemberID,
		CreatedAt:         c.now,
		UpdatedAt:         c.now,
	}, nil
}

func (c *planCompiler) requireTag(key, category, field string) (string, error) {
	tag, exists := c.tags[key]
	if !exists {
		return "", fmt.Errorf("%s 引用了不存在的标签 %q", field, key)
	}
	if tag.Category != category {
		return "", fmt.Errorf("%s 必须引用 %s 标签，实际为 %s", field, category, tag.Category)
	}
	c.usedTagKeys[key] = struct{}{}
	return tagSourceID(key), nil
}

func (c *planCompiler) requireMember(key, field string) (string, error) {
	if _, exists := c.members[key]; !exists {
		return "", fmt.Errorf("%s 引用了不存在的成员 %q", field, key)
	}
	return memberSourceID(key), nil
}

func (c *planCompiler) dayIDs(keys []string, field string) ([]string, error) {
	result := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("%s 重复包含 %q", field, key)
		}
		seen[key] = struct{}{}
		if _, exists := c.days[key]; !exists {
			return nil, fmt.Errorf("%s 引用了不存在的执行日 %q", field, key)
		}
		result = append(result, recordID("day", key))
	}
	return result, nil
}

func (c *planCompiler) rejectUnusedTags() error {
	unused := make([]string, 0)
	for key := range c.tags {
		if _, used := c.usedTagKeys[key]; !used {
			unused = append(unused, key)
		}
	}
	if len(unused) == 0 {
		return nil
	}
	sort.Strings(unused)
	return fmt.Errorf("tags 包含未被旅程使用的定义：%s", strings.Join(unused, ", "))
}

func (c *planCompiler) buildDependencies() model.TripBackupDependencies {
	memberKeys := make([]string, 0, len(c.members))
	for key := range c.members {
		memberKeys = append(memberKeys, key)
	}
	sort.Strings(memberKeys)
	tagKeys := make([]string, 0, len(c.usedTagKeys))
	for key := range c.usedTagKeys {
		tagKeys = append(tagKeys, key)
	}
	sort.Strings(tagKeys)
	dependencies := model.TripBackupDependencies{
		Members: make([]model.TripBackupMemberDependency, 0, len(memberKeys)),
		Tags:    make([]model.TripBackupTagDependency, 0, len(tagKeys)),
	}
	for _, key := range memberKeys {
		member := c.members[key]
		dependencies.Members = append(dependencies.Members, model.TripBackupMemberDependency{
			SourceID:    memberSourceID(key),
			DisplayName: member.DisplayName,
		})
	}
	for _, key := range tagKeys {
		tag := c.tags[key]
		dependencies.Tags = append(dependencies.Tags, model.TripBackupTagDependency{
			SourceID: tagSourceID(key),
			Name:     tag.Name,
			Category: tag.Category,
			Color:    tag.Color,
		})
	}
	return dependencies
}

func validatePlanKey(collection string, index int, key string) error {
	if key != strings.TrimSpace(key) || key == "" {
		return fmt.Errorf("%s[%d].key 不能为空或包含首尾空白", collection, index)
	}
	if err := model.ValidateRecordKey(key); err != nil {
		return fmt.Errorf("%s[%d].key 无效: %w", collection, index, err)
	}
	return nil
}

func recordID(prefix, key string) string {
	return prefix + ":" + key
}

func tagSourceID(key string) string {
	return recordID("tag", key)
}

func memberSourceID(key string) string {
	return recordID("member", key)
}

func addOptionalString(data map[string]any, field, value string) {
	if value != "" {
		data[field] = value
	}
}

func dayContainsPlace(day planDay, placeKey string) bool {
	for _, entry := range day.PlaceEntries {
		if entry.PlaceKey == placeKey {
			return true
		}
	}
	return false
}

func validateLocalDateTime(value string) error {
	if _, err := time.Parse("2006-01-02T15:04", value); err != nil {
		return errors.New("必须是 YYYY-MM-DDTHH:MM")
	}
	return nil
}
