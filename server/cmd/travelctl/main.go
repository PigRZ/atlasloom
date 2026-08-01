// travelctl 提供不连接运行时服务的一次性离线维护命令。
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "validate-snapshot":
		config, err := parseValidateFlags(args[1:], stderr)
		if err != nil {
			return 2
		}
		summary, err := validateSnapshotFile(config.inputPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeJSON(stdout, summary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "validate-trip-backup":
		config, err := parseValidateTripBackupFlags(args[1:], stderr)
		if err != nil {
			return 2
		}
		summary, err := validateTripBackupFile(config.inputPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeJSON(stdout, summary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "prepare-snapshot":
		config, err := parsePrepareFlags(args[1:], stderr)
		if err != nil {
			return 2
		}
		summary, err := prepareSnapshotFile(
			config.inputPath,
			config.outputPath,
			config.revision,
			config.hasRevision,
		)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeJSON(stdout, summary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "diff-snapshots":
		config, err := parseDiffFlags(args[1:], stderr)
		if err != nil {
			return 2
		}
		diff, err := diffSnapshotFiles(config.basePath, config.incomingPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeJSON(stdout, diff); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "build-trip-backup":
		config, err := parseBuildTripBackupFlags(args[1:], stderr)
		if err != nil {
			return 2
		}
		summary, err := buildTripBackupFile(config.inputPath, config.outputPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeJSON(stdout, summary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		printUsage(stderr)
		return 2
	}
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  travelctl validate-snapshot --input FILE")
	fmt.Fprintln(writer, "  travelctl validate-trip-backup --input FILE")
	fmt.Fprintln(writer, "  travelctl prepare-snapshot --input FILE --output FILE [--revision CURRENT]")
	fmt.Fprintln(writer, "  travelctl diff-snapshots --base A --incoming B")
	fmt.Fprintln(writer, "  travelctl build-trip-backup --input PLAN.json --output BACKUP.json")
}
