package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type managedPortRange struct {
	start int
	end   int
}

const maxManagedPortRangeSegments = 128

func normalizeManagedPortRanges(ranges []managedPortRange) []managedPortRange {
	normalized := make([]managedPortRange, 0, len(ranges))
	for _, item := range ranges {
		if item.start < 1 || item.start > 65535 || item.end < 1 || item.end > 65535 || item.start > item.end {
			continue
		}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return nil
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].start != normalized[j].start {
			return normalized[i].start < normalized[j].start
		}
		return normalized[i].end < normalized[j].end
	})
	merged := normalized[:1]
	for _, item := range normalized[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end+1 {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func managedPortRangesFromPorts(ports []int) []managedPortRange {
	ranges := make([]managedPortRange, 0, len(ports))
	for _, port := range ports {
		ranges = append(ranges, managedPortRange{start: port, end: port})
	}
	return normalizeManagedPortRanges(ranges)
}

func parseManagedPortRange(raw string) (managedPortRange, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return managedPortRange{}, fmt.Errorf("empty port range")
	}
	separator := ""
	if strings.Contains(raw, "-") {
		separator = "-"
	} else if strings.Contains(raw, ":") {
		separator = ":"
	}
	if separator == "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return managedPortRange{}, fmt.Errorf("invalid port %q", raw)
		}
		return managedPortRange{start: port, end: port}, nil
	}
	parts := strings.Split(raw, separator)
	if len(parts) != 2 {
		return managedPortRange{}, fmt.Errorf("invalid port range %q", raw)
	}
	start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if startErr != nil || endErr != nil || start < 1 || end > 65535 || start > end {
		return managedPortRange{}, fmt.Errorf("invalid port range %q", raw)
	}
	return managedPortRange{start: start, end: end}, nil
}

func formatManagedPortRange(item managedPortRange, separator string) string {
	if item.start == item.end {
		return strconv.Itoa(item.start)
	}
	return strconv.Itoa(item.start) + separator + strconv.Itoa(item.end)
}

func joinManagedPortRanges(ranges []managedPortRange) string {
	ranges = normalizeManagedPortRanges(ranges)
	values := make([]string, 0, len(ranges))
	for _, item := range ranges {
		values = append(values, formatManagedPortRange(item, "-"))
	}
	return strings.Join(values, ",")
}

func managedPortRangeCount(ranges []managedPortRange) int {
	total := 0
	for _, item := range normalizeManagedPortRanges(ranges) {
		total += item.end - item.start + 1
	}
	return total
}

func managedPortRangesOverlap(left, right managedPortRange) (managedPortRange, bool) {
	start := max(left.start, right.start)
	end := min(left.end, right.end)
	return managedPortRange{start: start, end: end}, start <= end
}

func managedPortRangeContains(item managedPortRange, port int) bool {
	return port >= item.start && port <= item.end
}
