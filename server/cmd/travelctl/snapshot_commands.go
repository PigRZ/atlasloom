// 当前快照与可移植备份工具只做离线校验、规范化和语义差异比较，不连接运行时服务。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

type inputConfig struct {
	inputPath string
}

type prepareConfig struct {
	inputPath   string
	outputPath  string
	revision    int64
	hasRevision bool
}

type diffConfig struct {
	basePath     string
	incomingPath string
}

type validationSummary struct {
	Valid           bool           `json:"valid"`
	TripID          string         `json:"tripId"`
	Revision        int64          `json:"revision"`
	ContentChecksum string         `json:"contentChecksum"`
	RecordCounts    map[string]int `json:"recordCounts"`
}

type tripBackupValidationSummary struct {
	Valid           bool   `json:"valid"`
	TripID          string `json:"tripId"`
	Revision        int64  `json:"revision"`
	ContentChecksum string `json:"contentChecksum"`
}

func parseValidateFlags(args []string, stderr io.Writer) (inputConfig, error) {
	flags := flag.NewFlagSet("validate-snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config inputConfig
	flags.StringVar(&config.inputPath, "input", "", "current snapshot JSON")
	if err := flags.Parse(args); err != nil {
		return inputConfig{}, err
	}
	if flags.NArg() != 0 || strings.TrimSpace(config.inputPath) == "" {
		fmt.Fprintln(stderr, "usage: travelctl validate-snapshot --input FILE")
		return inputConfig{}, errors.New("invalid validate-snapshot arguments")
	}
	return config, nil
}

func parseValidateTripBackupFlags(args []string, stderr io.Writer) (inputConfig, error) {
	flags := flag.NewFlagSet("validate-trip-backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config inputConfig
	flags.StringVar(&config.inputPath, "input", "", "portable trip backup JSON")
	if err := flags.Parse(args); err != nil {
		return inputConfig{}, err
	}
	if flags.NArg() != 0 || strings.TrimSpace(config.inputPath) == "" {
		fmt.Fprintln(stderr, "usage: travelctl validate-trip-backup --input FILE")
		return inputConfig{}, errors.New("invalid validate-trip-backup arguments")
	}
	return config, nil
}

func parsePrepareFlags(args []string, stderr io.Writer) (prepareConfig, error) {
	flags := flag.NewFlagSet("prepare-snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config prepareConfig
	flags.StringVar(&config.inputPath, "input", "", "current snapshot JSON")
	flags.StringVar(&config.outputPath, "output", "", "new prepared snapshot JSON")
	flags.Int64Var(&config.revision, "revision", 0, "revision to assign to both snapshot and trip")
	if err := flags.Parse(args); err != nil {
		return prepareConfig{}, err
	}
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "revision" {
			config.hasRevision = true
		}
	})
	if flags.NArg() != 0 ||
		strings.TrimSpace(config.inputPath) == "" ||
		strings.TrimSpace(config.outputPath) == "" ||
		(config.hasRevision && config.revision < 1) {
		fmt.Fprintln(stderr, "usage: travelctl prepare-snapshot --input FILE --output FILE [--revision CURRENT]")
		return prepareConfig{}, errors.New("invalid prepare-snapshot arguments")
	}
	return config, nil
}

func parseDiffFlags(args []string, stderr io.Writer) (diffConfig, error) {
	flags := flag.NewFlagSet("diff-snapshots", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var config diffConfig
	flags.StringVar(&config.basePath, "base", "", "validated base snapshot JSON")
	flags.StringVar(&config.incomingPath, "incoming", "", "validated incoming snapshot JSON")
	if err := flags.Parse(args); err != nil {
		return diffConfig{}, err
	}
	if flags.NArg() != 0 ||
		strings.TrimSpace(config.basePath) == "" ||
		strings.TrimSpace(config.incomingPath) == "" {
		fmt.Fprintln(stderr, "usage: travelctl diff-snapshots --base A --incoming B")
		return diffConfig{}, errors.New("invalid diff-snapshots arguments")
	}
	return config, nil
}

func validateSnapshotFile(path string) (validationSummary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return validationSummary{}, fmt.Errorf("读取快照失败: %w", err)
	}
	payload, err := validateSnapshotBytes(raw)
	if err != nil {
		return validationSummary{}, fmt.Errorf("快照无效: %w", err)
	}
	counts := make(map[string]int, len(payload.Datasets))
	for _, resource := range model.Resources() {
		counts[string(resource)] = len(payload.Datasets[string(resource)])
	}
	return validationSummary{
		Valid:           true,
		TripID:          payload.Trip.ID,
		Revision:        payload.Revision,
		ContentChecksum: payload.ContentChecksum,
		RecordCounts:    counts,
	}, nil
}

func validateSnapshotBytes(raw []byte) (model.SnapshotPayload, error) {
	var identity struct {
		Trip struct {
			ID string `json:"id"`
		} `json:"trip"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot JSON is invalid: %w", err)
	}
	if strings.TrimSpace(identity.Trip.ID) == "" {
		return model.SnapshotPayload{}, errors.New("snapshot trip id is required")
	}
	return repository.ValidateSnapshotPayload(raw, identity.Trip.ID)
}

func validateTripBackupFile(path string) (tripBackupValidationSummary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return tripBackupValidationSummary{}, fmt.Errorf("读取旅行备份失败: %w", err)
	}
	backup, err := repository.ValidateTripBackup(raw)
	if err != nil {
		return tripBackupValidationSummary{}, fmt.Errorf("旅行备份无效: %w", err)
	}
	return tripBackupValidationSummary{
		Valid:           true,
		TripID:          backup.Snapshot.Trip.ID,
		Revision:        backup.Snapshot.Revision,
		ContentChecksum: backup.ContentChecksum,
	}, nil
}

func prepareSnapshotFile(inputPath, outputPath string, revision int64, hasRevision bool) (validationSummary, error) {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return validationSummary{}, fmt.Errorf("读取输入快照失败: %w", err)
	}
	payload, err := decodeSnapshotPayload(raw)
	if err != nil {
		return validationSummary{}, err
	}
	if hasRevision {
		payload.Revision = revision
		payload.Trip.Version = revision
	}
	stableSortDatasets(payload.Datasets)
	payload.ExportedAt = time.Now().UTC()
	payload.ContentChecksum, err = payload.ComputedContentChecksum()
	if err != nil {
		return validationSummary{}, fmt.Errorf("计算内容校验和失败: %w", err)
	}
	prepared, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return validationSummary{}, fmt.Errorf("编码规范快照失败: %w", err)
	}
	prepared = append(prepared, '\n')
	validated, err := repository.ValidateSnapshotPayload(prepared, payload.Trip.ID)
	if err != nil {
		return validationSummary{}, fmt.Errorf("规范快照未通过完整校验: %w", err)
	}
	if err := writeFileExclusive(outputPath, prepared); err != nil {
		return validationSummary{}, err
	}
	counts := make(map[string]int, len(validated.Datasets))
	for _, resource := range model.Resources() {
		counts[string(resource)] = len(validated.Datasets[string(resource)])
	}
	return validationSummary{
		Valid:           true,
		TripID:          validated.Trip.ID,
		Revision:        validated.Revision,
		ContentChecksum: validated.ContentChecksum,
		RecordCounts:    counts,
	}, nil
}

func decodeSnapshotPayload(raw []byte) (model.SnapshotPayload, error) {
	var payload model.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return model.SnapshotPayload{}, fmt.Errorf("输入不是当前快照契约: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return model.SnapshotPayload{}, errors.New("输入快照必须且只能包含一个 JSON 对象")
	}
	return payload, nil
}

func stableSortDatasets(datasets map[string][]model.Record) {
	for _, resource := range model.Resources() {
		key := string(resource)
		rows := datasets[key]
		sort.SliceStable(rows, func(left, right int) bool {
			leftGroup := recordSortGroup(resource, rows[left])
			rightGroup := recordSortGroup(resource, rows[right])
			if leftGroup != rightGroup {
				return leftGroup < rightGroup
			}
			if rows[left].Position != rows[right].Position {
				return rows[left].Position < rows[right].Position
			}
			if rows[left].RecordKey != rows[right].RecordKey {
				return rows[left].RecordKey < rows[right].RecordKey
			}
			return rows[left].ID < rows[right].ID
		})
		datasets[key] = rows
	}
}

func recordSortGroup(resource model.Resource, record model.Record) string {
	if resource == model.Days {
		return strings.TrimSpace(fmt.Sprint(record.Data["date"]))
	}
	if dayID, ok := record.Data["dayId"].(string); ok {
		return strings.TrimSpace(dayID)
	}
	return ""
}

func writeFileExclusive(path string, content []byte) error {
	cleanPath := filepath.Clean(path)
	directory := filepath.Dir(cleanPath)
	temp, err := os.CreateTemp(directory, "."+filepath.Base(cleanPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时输出失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("设置临时输出权限失败: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("写入临时输出失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("同步临时输出失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭临时输出失败: %w", err)
	}
	if err := os.Link(tempPath, cleanPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("拒绝覆盖已存在的输出文件: %s", cleanPath)
		}
		return fmt.Errorf("提交输出失败: %w", err)
	}
	return nil
}

func diffSnapshotFiles(basePath, incomingPath string) (repository.SnapshotDiff, error) {
	baseRaw, err := os.ReadFile(basePath)
	if err != nil {
		return repository.SnapshotDiff{}, fmt.Errorf("读取基准快照失败: %w", err)
	}
	incomingRaw, err := os.ReadFile(incomingPath)
	if err != nil {
		return repository.SnapshotDiff{}, fmt.Errorf("读取待比较快照失败: %w", err)
	}
	base, err := validateSnapshotBytes(baseRaw)
	if err != nil {
		return repository.SnapshotDiff{}, fmt.Errorf("基准快照无效: %w", err)
	}
	incoming, err := validateSnapshotBytes(incomingRaw)
	if err != nil {
		return repository.SnapshotDiff{}, fmt.Errorf("待比较快照无效: %w", err)
	}
	if base.Trip.ID != incoming.Trip.ID {
		return repository.SnapshotDiff{}, errors.New("两个快照属于不同旅行")
	}
	if err := repository.ValidateRecordIdentityTransition(base.Datasets, incoming.Datasets); err != nil {
		return repository.SnapshotDiff{}, err
	}
	result := repository.SnapshotDiff{
		BaseRevision:    base.Revision,
		CurrentRevision: incoming.Revision,
		Checksum:        incoming.ContentChecksum,
		TripChanges:     repository.DiffTripMetadata(base.Trip, incoming.Trip),
		Resources:       make(map[string]repository.ResourceDiff, len(model.Resources())),
		MembershipAccess: repository.MembershipAccessDiff{
			Created: []repository.MembershipAccessChange{},
			Deleted: []repository.MembershipAccessChange{},
		},
	}
	for _, resource := range model.Resources() {
		key := string(resource)
		currentRows := recordsByKey(base.Datasets[key])
		incomingRows := recordsByKey(incoming.Datasets[key])
		resourceDiff := repository.ResourceDiff{
			Current:  len(currentRows),
			Incoming: len(incomingRows),
		}
		for recordKey, incomingRow := range incomingRows {
			currentRow, exists := currentRows[recordKey]
			if !exists {
				resourceDiff.Created++
				continue
			}
			if currentRow.ID != incomingRow.ID {
				return repository.SnapshotDiff{}, fmt.Errorf(
					"%w: %s recordKey %s",
					repository.ErrSnapshotIdentity,
					resource,
					recordKey,
				)
			}
			if !sameRecordMeaning(currentRow, incomingRow) {
				resourceDiff.Updated++
			}
		}
		for recordKey := range currentRows {
			if _, exists := incomingRows[recordKey]; !exists {
				resourceDiff.Deleted++
				result.DeleteCount++
			}
		}
		result.Resources[key] = resourceDiff
	}
	result.Finalize()
	return result, nil
}

func recordsByKey(rows []model.Record) map[string]model.Record {
	result := make(map[string]model.Record, len(rows))
	for _, row := range rows {
		result[row.RecordKey] = row
	}
	return result
}

func sameRecordMeaning(left, right model.Record) bool {
	if left.Position != right.Position {
		return false
	}
	return model.SameJSONMeaning(left.Data, right.Data)
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
